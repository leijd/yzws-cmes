# =====================================================================
# 一键部署脚本：构建前端 + 后端 → 上传云服务器 → 替换文件 → 改端口 → 重启
#
# 用法（PowerShell，在 src 目录执行）：
#   .\deploy\deploy.ps1                                          # 全部默认值
#   .\deploy\deploy.ps1 -BackendPort 18390                       # 指定后端端口（会改服务器 config.yaml 的 listen）
#   .\deploy\deploy.ps1 -ServerHost 1.2.3.4 -ServerUser root     # 指定服务器
#   .\deploy\deploy.ps1 -UploadConfig                            # 首次部署：连本地 config.yaml 一起上传
#
# 前提：
#   1. 本机已配置到服务器的 SSH 免密登录（ssh-copy-id / 公钥），或接受交互输密码
#   2. 服务器已安装 systemd；对外入口用 Caddy（配置见 deploy/Caddyfile）
#   3. 腾讯云短信等环境变量写在服务器的 systemd 单元或 /etc/default/clothp 中
# =====================================================================
param(
  [string]$ServerHost   = '106.52.168.135',   # 云服务器地址
  [string]$ServerUser   = 'root',             # SSH 用户
  [int]   $SshPort      = 22,
  [int]   $BackendPort  = 18370,              # 后端监听端口（部署时写入服务器 config.yaml 的 server.listen）
  [string]$FrontendDir  = '/opt/clothp/web',  # 服务器上前端静态文件目录（Caddy FRONTEND_ROOT 指向这里）
  [string]$BackendDir   = '/opt/clothp/server', # 服务器上后端目录（二进制 + config.yaml）
  [string]$ServiceName  = 'clothp',           # systemd 服务名
  [string]$Goos         = 'linux',            # 云服务器操作系统（Windows Server 改成 windows）
  [string]$Goarch       = 'amd64',
  [switch]$UploadConfig                       # 首次部署：把本地 config.yaml（改好端口）一起上传
)
$ErrorActionPreference = 'Stop'
$Root     = $PSScriptRoot | Split-Path        # src 目录
$BuildDir = Join-Path $Root 'deploy\build'
$Stamp    = Get-Date -Format 'yyyyMMddHHmmss'

function Step($msg) { Write-Host "`n==> $msg" -ForegroundColor Cyan }

# 1. 构建前端
Step '构建前端 (npm run build)'
Push-Location (Join-Path $Root 'web'); npm run build; Pop-Location
if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }

# 2. 构建后端（交叉编译到服务器平台）
Step "构建后端 ($Goos/$Goarch)"
$env:GOOS = $Goos; $env:GOARCH = $Goarch
Push-Location $Root; go build -trimpath -ldflags '-s -w' -o (Join-Path $BuildDir 'clothp-server') ./cmd/server; Pop-Location
Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
if ($LASTEXITCODE -ne 0) { throw '后端构建失败' }

# 3. 准备配置：复制 config.yaml 并把 server.listen 改成本次端口
$RemoteConfig = Join-Path $BuildDir 'config.yaml'
(Get-Content (Join-Path $Root 'config.yaml') -Raw) -replace '(?m)^(\s*listen:\s*).+$', ("`$1 `":$BackendPort`"") |
  Set-Content $RemoteConfig -Encoding utf8
Write-Host "    listen 已改为 :$BackendPort"

# 4. 打包并上传：前端 dist + 后端二进制 + systemd 单元 +（可选）配置
Step '打包上传'
tar -czf (Join-Path $BuildDir 'web.tgz') -C (Join-Path $Root 'web\dist') .
if ($LASTEXITCODE -ne 0) { throw '前端打包失败' }
$SshBase = @('-P', $SshPort)
ssh @SshBase "$ServerUser@$ServerHost" 'mkdir -p /tmp/clothp-deploy'
scp @SshBase (Join-Path $BuildDir 'web.tgz') "${ServerUser}@${ServerHost}:/tmp/clothp-deploy/web.tgz"
scp @SshBase (Join-Path $BuildDir 'clothp-server') "${ServerUser}@${ServerHost}:/tmp/clothp-deploy/clothp-server"
scp @SshBase "$PSScriptRoot\clothp.service" "${ServerUser}@${ServerHost}:/tmp/clothp-deploy/clothp.service"
if ($UploadConfig) {
  scp @SshBase $RemoteConfig "${ServerUser}@${ServerHost}:/tmp/clothp-deploy/config.yaml"
}

# 5. 服务器端执行：替换前端 → 改配置端口 → 替换后端 → 重启 → 健康检查
Step '服务器端部署（旧版本自动备份为 *.bak-时间戳）'
$Remote = @'
set -e
WEB_DIR=__WEB_DIR__; SRV_DIR=__SRV_DIR__; SVC=__SVC__; PORT=__PORT__; TS=__TS__
mkdir -p "$WEB_DIR" "$SRV_DIR"

# ---- 前端：先解到新目录，再原子替换 ----
mkdir -p "$WEB_DIR.new-$TS"
tar xzf /tmp/clothp-deploy/web.tgz -C "$WEB_DIR.new-$TS"
[ -d "$WEB_DIR" ] && mv "$WEB_DIR" "$WEB_DIR.bak-$TS"
mv "$WEB_DIR.new-$TS" "$WEB_DIR"

# ---- 后端配置：改 listen 端口（首次部署 -UploadConfig 会先落整个配置文件）----
if [ -f /tmp/clothp-deploy/config.yaml ]; then mv /tmp/clothp-deploy/config.yaml "$SRV_DIR/config.yaml"; fi
if [ -f "$SRV_DIR/config.yaml" ]; then
  sed -i -E "s/^([[:space:]]*listen:).*/\1 \":$PORT\"/" "$SRV_DIR/config.yaml"
  echo "config.yaml listen -> :$PORT"
fi

# ---- 后端：停服 → 换二进制（保留旧版备份）→ 启动 ----
systemctl stop "$SVC" 2>/dev/null || true
[ -f "$SRV_DIR/clothp-server" ] && cp "$SRV_DIR/clothp-server" "$SRV_DIR/clothp-server.bak-$TS"
mv /tmp/clothp-deploy/clothp-server "$SRV_DIR/clothp-server"
chmod +x "$SRV_DIR/clothp-server"
if [ ! -f /etc/systemd/system/$SVC.service ]; then
  cp /tmp/clothp-deploy/clothp.service /etc/systemd/system/$SVC.service
  sed -i -E "s|^ExecStart=.*|ExecStart=$SRV_DIR/clothp-server -config $SRV_DIR/config.yaml|" /etc/systemd/system/$SVC.service
  systemctl daemon-reload
  systemctl enable "$SVC"
fi
systemctl restart "$SVC"
rm -rf /tmp/clothp-deploy

# ---- 健康检查 ----
sleep 2
systemctl is-active "$SVC"
curl -fsS "http://127.0.0.1:$PORT/api/v1/system/health/ready" && echo " <- backend ready on :$PORT"
'@
$Remote = $Remote.Replace('__WEB_DIR__', $FrontendDir).Replace('__SRV_DIR__', $BackendDir).
  Replace('__SVC__', $ServiceName).Replace('__PORT__', "$BackendPort").Replace('__TS__', $Stamp)
$Remote | ssh @SshBase "$ServerUser@$ServerHost" 'bash -s'
if ($LASTEXITCODE -ne 0) { throw '服务器端部署失败（可ssh登录服务器检查日志：journalctl -u clothp -n 50）' }

# 6. 后端端口变化时提醒同步 Caddy upstream
Write-Host "`n==> 部署完成：前端 $FrontendDir，后端 $BackendDir，端口 :$BackendPort" -ForegroundColor Green
Write-Host "    如 BACKEND_PORT 与服务器 Caddyfile 中的不一致，请执行："
Write-Host "    ssh $ServerUser@$ServerHost \"caddy reload --config /etc/caddy/Caddyfile\""
Write-Host "    回滚：ssh $ServerUser@$ServerHost 恢复 *.bak-$Stamp 并 systemctl restart $ServiceName"
