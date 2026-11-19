# =====================================================================
# clothp2 部署脚本（Docker Caddy 挂载修正版）
#
# 关键：Caddy 容器把宿主 /app/clothP-2/dist  bind mount 到 /srv/clothpm。
# 不能用 mv 整体替换 dist 目录（inode 变了，容器仍挂载旧目录，用户看到旧页面）。
# 必须原地清空 dist 内容再拷入新文件，保持 inode，最后重启 caddy2 刷新缓存。
#
# 用法（PowerShell，在 src 目录执行）：
#   .\deploy\deploy_v2.ps1                       # 构建前后端并部署
#   .\deploy\deploy_v2.ps1 -WebOnly              # 只部署前端
#   .\deploy\deploy_v2.ps1 -BackendOnly          # 只部署后端
# =====================================================================
param(
  [string]$ServerHost = '106.52.168.135',
  [string]$ServerUser = 'root',
  [int]   $SshPort    = 22,
  [string]$AppDir     = '/app/clothP-2',
  [string]$Service    = 'clothp2',
  [int]   $Port       = 18390,
  [switch]$WebOnly,
  [switch]$BackendOnly
)
$ErrorActionPreference = 'Stop'
$Root     = $PSScriptRoot | Split-Path
$BuildDir = Join-Path $Root 'deploy\build'
$Stamp    = Get-Date -Format 'yyyyMMddHHmmss'
function Step($m) { Write-Host "`n==> $m" -ForegroundColor Cyan }
New-Item -ItemType Directory -Force -Path $BuildDir | Out-Null

$doWeb = -not $BackendOnly
$doBackend = -not $WebOnly

if ($doWeb) {
  Step '构建前端'
  Push-Location (Join-Path $Root 'web'); npm run build; Pop-Location
  if ($LASTEXITCODE -ne 0) { throw '前端构建失败' }
  tar -czf (Join-Path $BuildDir 'web.tgz') -C (Join-Path $Root 'web\dist') .
  if ($LASTEXITCODE -ne 0) { throw '前端打包失败' }
}
if ($doBackend) {
  Step '交叉编译后端'
  $env:GOOS='linux'; $env:GOARCH='amd64'
  go build -trimpath -ldflags '-s -w' -o (Join-Path $BuildDir 'clothp-linux-amd64') ./cmd/server
  $code=$LASTEXITCODE; Remove-Item Env:GOOS,Env:GOARCH -ErrorAction SilentlyContinue
  if ($code -ne 0) { throw '后端构建失败' }
}

Step '上传'
$SshBase=@('-P',"$SshPort")
ssh @SshBase "$ServerUser@$ServerHost" 'mkdir -p /tmp/clothp2-deploy'
if ($doWeb) { scp @SshBase (Join-Path $BuildDir 'web.tgz') "${ServerUser}@${ServerHost}:/tmp/clothp2-deploy/web.tgz" }
if ($doBackend) { scp @SshBase (Join-Path $BuildDir 'clothp-linux-amd64') "${ServerUser}@${ServerHost}:/tmp/clothp2-deploy/clothp-linux-amd64" }

$doWebInt = if ($doWeb) { '1' } else { '0' }
$doBackendInt = if ($doBackend) { '1' } else { '0' }
$Remote = @'
set -e
APP=__APP__; SVC=__SVC__; PORT=__PORT__; TS=__TS__; DO_WEB=__DOWEB__; DO_BACKEND=__DOBACKEND__
cd "$APP"
if [ "$DO_WEB" = "1" ]; then
  # 原地覆盖 dist（保持 inode），Docker bind mount 才可见
  rm -rf /tmp/web.incoming; mkdir -p /tmp/web.incoming
  tar xzf /tmp/clothp2-deploy/web.tgz -C /tmp/web.incoming
  cp -a "$APP/dist" "$APP/dist.content-bak-$TS"
  find "$APP/dist" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
  cp -a /tmp/web.incoming/. "$APP/dist"/
  rm -rf /tmp/web.incoming
  docker restart caddy2 >/dev/null 2>&1 || true
  echo "web redeployed, caddy2 restarted"
fi
if [ "$DO_BACKEND" = "1" ]; then
  cp "$APP/clothp-linux-amd64" "$APP/clothp-linux-amd64.bak-$TS"
  mv /tmp/clothp2-deploy/clothp-linux-amd64 "$APP/clothp-linux-amd64"
  chmod +x "$APP/clothp-linux-amd64"
  systemctl restart "$SVC"
fi
sleep 3
echo "SERVICE=$(systemctl is-active "$SVC" || true)"
curl -fsS "http://127.0.0.1:$PORT/api/v1/system/health/ready" && echo " <- ready on :$PORT"
echo "--- served index hash ---"
curl -fsS "http://127.0.0.1:18389/" | grep -o 'index-[A-Za-z0-9_-]*\.js' | head -1 || true
'@
$Remote = $Remote.Replace('__APP__',$AppDir).Replace('__SVC__',$Service).Replace('__PORT__',"$Port").Replace('__TS__',$Stamp).Replace('__DOWEB__',$doWebInt).Replace('__DOBACKEND__',$doBackendInt)
$Remote | ssh @SshBase "$ServerUser@$ServerHost" 'bash -s'
ssh @SshBase "$ServerUser@$ServerHost" 'rm -rf /tmp/clothp2-deploy'
Write-Host "`n==> 完成" -ForegroundColor Green
