#!/usr/bin/env bash
# backupFull.sh —— MES 双库完整逻辑备份（明文库 + 密文库），Linux 使用。
# 依赖：postgresql-client（pg_dump/pg_restore/psql）或 docker（临时容器客户端，用于版本不兼容）。
#
# 用法： ./backupFull.sh
# 输出： ${BACKUP_ROOT}/<YYYYmmdd-HHMMSS>/{plain.dump,secure.dump,manifest.txt,SHA256SUMS,backup.log}
# 恢复： ./restoryFull.sh <备份目录>
#
# 连接参数优先级：环境变量 > backup/backup.env > <repo>/src/config.yaml > 内置默认。
#   PGHOST PGPORT PGUSER PGPASSWORD
#   PLAIN_DB（默认 yzws-v1） SECURE_DB（默认 yzws-v1-sec）
#   PLAIN_PGHOST/PLAIN_PGPORT/PLAIN_PGUSER/PLAIN_PGPASSWORD、SECURE_* 可单库覆盖
#   BACKUP_ROOT（默认 <repo>/db-backup/full） BACKUP_KEEP（默认 14）
#   BACKUP_GLOBALS=1 额外导出 pg_dumpall --globals-only；PG_BIN 指定客户端目录
#   BACKUP_CONFIG_YAML 指定配置文件；BACKUP_NO_CONFIG=1 不使用 config.yaml
#   默认自动探测：宿主机 pg_dump 主版本低于服务端时，改用 docker run postgres:<服务端主版本> 的客户端；
#   PG_CLIENT=local|docker|auto 可指定；PG_DOCKER_IMAGE 指定镜像；
#   PG_DOCKER_HOST 指定数据库宿主机在临时容器内的可达地址（默认 docker 网桥网关 172.17.0.1）。
#   docker 模式仍导出 -Fc custom dump（不是 SQL），数据经 stdout 写入宿主机备份目录。
#   HSM：HSM_TENANTS_FILE + HSM_TOKEN_DIR（宿主机路径），或 HSM_DOCKER_CONTAINER +
#        HSM_TENANTS_IN_CONTAINER + HSM_TOKEN_IN_CONTAINER；HSM_PIN 或 HSM_PIN_FILE；
#        HSM_BACKUP_PASSPHRASE 或 HSM_BACKUP_PASSPHRASE_FILE；HSM_BACKUP_REQUIRED=1 缺失即失败。
#
# !!! 密文库只含密文和密钥包裹引用，真正的租户密钥在 SoftHSM2/KMS。
#     HSM_BACKUP=auto|1|0 控制是否把 tenants.json + SoftHSM2 token 目录 + PIN 打包为
#     hsm-keys.tar.gz.enc（openssl AES-256-CBC/PBKDF2 加密，口令不写入 manifest）。
#     仅恢复数据库而不恢复 HSM 密钥资料将无法解密历史密文。
set -Eeuo pipefail
umask 077

DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO="$(cd -- "$DIR/.." && pwd -P)"
ENVFILE="${BACKUP_ENV_FILE:-$DIR/backup.env}"
# shellcheck disable=SC1090
[[ -f "$ENVFILE" ]] && source "$ENVFILE"
[[ -n "${PG_BIN:-}" ]] && PATH="$PG_BIN:$PATH"

# ---------- 从 src/config.yaml 读取 databases.plain / databases.secure 作为兜底 ----------
CONFIG="${BACKUP_CONFIG_YAML:-}"
if [[ -z "$CONFIG" ]]; then
  for _c in "$REPO/src/config.yaml" "$REPO/config.yaml"; do
    if [[ -f "$_c" ]]; then CONFIG="$_c"; break; fi
  done
fi
[[ "${BACKUP_NO_CONFIG:-0}" == "1" ]] && CONFIG=""

cfg() { # cfg <section> <key> ；只解析两空格缩进的 plain/secure 段
  [[ -n "$CONFIG" && -f "$CONFIG" ]] || return 0
  awk -v sect="$1" -v key="$2" '
    /^[[:space:]]*#/ { next }
    /^databases:[[:space:]]*$/ { db = 1; next }
    db && /^[^[:space:]]/ { db = 0 }
    db && $0 ~ "^  " sect ":[[:space:]]*$" { s = 1; next }
    s && /^  [^[:space:]]/ { s = 0 }
    s && $0 ~ "^    " key ":" {
      sub(/^[[:space:]]*[^:]+:[[:space:]]*/, "")
      gsub(/"/, "")
      sub(/[[:space:]]+#.*$/, "")
      print; exit
    }
  ' "$CONFIG"
}

cfg_top() { # 顶层 database: 段（旧版单库配置，plain 缺省时兜底）
  [[ -n "$CONFIG" && -f "$CONFIG" ]] || return 0
  awk -v key="$1" '
    /^[[:space:]]*#/ { next }
    /^database:[[:space:]]*$/ { d = 1; next }
    d && /^[^[:space:]]/ { d = 0 }
    d && $0 ~ "^  " key ":" {
      sub(/^[[:space:]]*[^:]+:[[:space:]]*/, "")
      gsub(/"/, "")
      sub(/[[:space:]]+#.*$/, "")
      print; exit
    }
  ' "$CONFIG"
}

CFG_P_HOST="$(cfg plain host)";   CFG_P_PORT="$(cfg plain port)"
CFG_P_NAME="$(cfg plain name)";   CFG_P_USER="$(cfg plain user)"
CFG_P_PASS="$(cfg plain password)"
CFG_S_HOST="$(cfg secure host)";  CFG_S_PORT="$(cfg secure port)"
CFG_S_NAME="$(cfg secure name)";  CFG_S_USER="$(cfg secure user)"
CFG_S_PASS="$(cfg secure password)"
CFG_D_HOST="$(cfg_top host)";     CFG_D_PORT="$(cfg_top port)"
CFG_D_NAME="$(cfg_top name)";     CFG_D_USER="$(cfg_top user)"
CFG_D_PASS="$(cfg_top password)"

export PGCONNECT_TIMEOUT="${PGCONNECT_TIMEOUT:-10}"
PGHOST="${PGHOST:-}"; PGPORT="${PGPORT:-}"; PGPASSWORD="${PGPASSWORD:-}"
PGUSER="${PGUSER:-${CFG_P_USER:-${CFG_D_USER:-postgres}}}"
PLAIN_DB="${PLAIN_DB:-${CFG_P_NAME:-${CFG_D_NAME:-yzws-v1}}}"
SECURE_DB="${SECURE_DB:-${CFG_S_NAME:-yzws-v1-sec}}"
PLAIN_PGHOST="${PLAIN_PGHOST:-${PGHOST:-${CFG_P_HOST:-${CFG_D_HOST:-127.0.0.1}}}}"
PLAIN_PGPORT="${PLAIN_PGPORT:-${PGPORT:-${CFG_P_PORT:-${CFG_D_PORT:-5432}}}}"
PLAIN_PGUSER="${PLAIN_PGUSER:-${PGUSER}}"
PLAIN_PGPASSWORD="${PLAIN_PGPASSWORD:-${PGPASSWORD:-${CFG_P_PASS:-${CFG_D_PASS:-}}}}"
SECURE_PGHOST="${SECURE_PGHOST:-${PGHOST:-${CFG_S_HOST:-$PLAIN_PGHOST}}}"
SECURE_PGPORT="${SECURE_PGPORT:-${PGPORT:-${CFG_S_PORT:-$PLAIN_PGPORT}}}"
SECURE_PGUSER="${SECURE_PGUSER:-${PGUSER}}"
SECURE_PGPASSWORD="${SECURE_PGPASSWORD:-${PGPASSWORD:-${CFG_S_PASS:-$PLAIN_PGPASSWORD}}}"
CFG_PLAIN_FROM_TOP=0
if [[ -n "$CONFIG" && -z "$PGPORT" && -z "$PGHOST" && -z "$CFG_P_PORT" && -n "$CFG_D_PORT" ]]; then
  CFG_PLAIN_FROM_TOP=1
fi
BACKUP_ROOT="${BACKUP_ROOT:-$REPO/db-backup/full}"
BACKUP_KEEP="${BACKUP_KEEP:-14}"

HSM_BACKUP="${HSM_BACKUP:-auto}"
HSM_TENANTS_FILE="${HSM_TENANTS_FILE:-}"
HSM_TOKEN_DIR="${HSM_TOKEN_DIR:-}"
HSM_DOCKER_CONTAINER="${HSM_DOCKER_CONTAINER:-}"
HSM_TENANTS_IN_CONTAINER="${HSM_TENANTS_IN_CONTAINER:-}"
HSM_TOKEN_IN_CONTAINER="${HSM_TOKEN_IN_CONTAINER:-}"
HSM_PIN="${HSM_PIN:-${CLOTHP_SOFTHSM2_PIN:-}}"
HSM_PIN_FILE="${HSM_PIN_FILE:-}"
HSM_BACKUP_PASSPHRASE="${HSM_BACKUP_PASSPHRASE:-}"
HSM_BACKUP_PASSPHRASE_FILE="${HSM_BACKUP_PASSPHRASE_FILE:-}"
HSM_KEYS=0; HSM_KEYS_FILE=""; HSM_KEYS_SHA=""; HSM_TENANTS_SHA=""

STAMP="$(date '+%Y%m%d-%H%M%S')"
TARGET="$BACKUP_ROOT/$STAMP"
LOG="$BACKUP_ROOT/logs/backup-$STAMP.log"

log() {
  local line; line="[$(date '+%F %T')] $*"
  if [[ -d "${LOG%/*}" ]]; then printf '%s\n' "$line" | tee -a "$LOG" >&2; else printf '%s\n' "$line" >&2; fi
}
die() { log "错误：$*"; exit 1; }
trap 'rc=$?; if [[ $rc -ne 0 ]]; then log "备份失败（rc=$rc，行 $LINENO），保留 $TARGET"; touch "$TARGET/.incomplete" 2>/dev/null || true; fi; exit $rc' EXIT

# ---------- 客户端选择：本机 pg_* 或 docker run postgres:<major> ----------
PG_CLIENT="${PG_CLIENT:-auto}"
PG_DOCKER_IMAGE="${PG_DOCKER_IMAGE:-}"
local_major=""; server_major=""
if command -v pg_dump >/dev/null 2>&1; then
  local_major="$(pg_dump --version 2>/dev/null | grep -oE '[0-9]+(\.[0-9]+)?' | head -n1 || true)"
  local_major="${local_major%%.*}"
fi
if command -v psql >/dev/null 2>&1; then
  server_major="$(PGPASSWORD="$PLAIN_PGPASSWORD" psql -AtX -h "$PLAIN_PGHOST" -p "$PLAIN_PGPORT" -U "$PLAIN_PGUSER" -d "$PLAIN_DB" -c 'SHOW server_version' 2>/dev/null | grep -oE '[0-9]+(\.[0-9]+)?' | head -n1 || true)"
  server_major="${server_major%%.*}"
fi
if [[ "$PG_CLIENT" == "auto" ]]; then
  if [[ -z "$local_major" || ( -n "$server_major" && "$server_major" -gt "$local_major" ) ]]; then
    PG_CLIENT=docker
  else
    PG_CLIENT=local
  fi
fi
[[ -n "$PG_DOCKER_IMAGE" ]] && PG_CLIENT=docker

if [[ "$PG_CLIENT" == "docker" ]]; then
  command -v docker >/dev/null 2>&1 || die "缺少 docker 命令（本机 pg_dump 版本不足）"
  docker info >/dev/null 2>&1 || die "docker daemon 不可用"
  PG_DOCKER_IMAGE="${PG_DOCKER_IMAGE:-postgres:${server_major:-18}}"
  DOCKER_PG_HOST="${PG_DOCKER_HOST:-}"
  if [[ -z "$DOCKER_PG_HOST" ]]; then
    DOCKER_PG_HOST="$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || true)"
  fi
  DOCKER_PG_HOST="${DOCKER_PG_HOST:-172.17.0.1}"
else
  if [[ -n "$server_major" && -n "$local_major" && "$server_major" -gt "$local_major" ]]; then
    log "警告：本机 pg_dump $local_major 低于服务端 $server_major，将很可能失败（建议改用自动 docker 模式）"
  fi
  for c in pg_dump pg_restore psql sha256sum flock df awk; do
    command -v "$c" >/dev/null 2>&1 || die "缺少命令 $c（安装 postgresql-client 或设置 PG_BIN）"
  done
fi
for c in sha256sum flock df awk; do
  command -v "$c" >/dev/null 2>&1 || die "缺少命令 $c"
done
[[ "$BACKUP_KEEP" =~ ^[0-9]+$ ]] && ((BACKUP_KEEP >= 1)) || die "BACKUP_KEEP 必须是 >=1 的整数"
[[ "$PLAIN_DB" != "$SECURE_DB" ]] || die "明文库与密文库不能同名"
case "$BACKUP_ROOT" in "/"|"$REPO") die "BACKUP_ROOT 不安全：$BACKUP_ROOT" ;; esac

mkdir -p "$BACKUP_ROOT/logs"
exec 9>"$BACKUP_ROOT/.backup.lock"
flock -n 9 || die "已有备份正在运行"

docker_pg_host() { # 容器内访问数据库宿主机：127.0.0.1 需换成 docker 网桥网关
  case "$1" in
    127.0.0.1|localhost|'::1') printf '%s' "$DOCKER_PG_HOST" ;;
    *) printf '%s' "$1" ;;
  esac
}

psqlq() { # host port user pass db sql
  if [[ "$PG_CLIENT" == "docker" ]]; then
    docker run --rm -e PGPASSWORD="$4" "$PG_DOCKER_IMAGE" psql -AtX -v ON_ERROR_STOP=1 \
      -h "$(docker_pg_host "$1")" -p "$2" -U "$3" -d "$5" -c "$6"
  else
    PGPASSWORD="$4" psql -AtX -v ON_ERROR_STOP=1 -h "$1" -p "$2" -U "$3" -d "$5" -c "$6"
  fi
}

pg_dump_fc() { # host port user pass db outfile
  if [[ "$PG_CLIENT" == "docker" ]]; then
    docker run --rm -e PGPASSWORD="$4" "$PG_DOCKER_IMAGE" \
      pg_dump -h "$(docker_pg_host "$1")" -p "$2" -U "$3" -d "$5" \
      -Fc -Z 6 --lock-wait-timeout=30s > "$6"
  else
    PGPASSWORD="$4" pg_dump -h "$1" -p "$2" -U "$3" -d "$5" \
      -Fc -Z 6 --lock-wait-timeout=30s -f "$6"
  fi
}

pg_list() { # dumpfile
  if [[ "$PG_CLIENT" == "docker" ]]; then
    docker run --rm -i "$PG_DOCKER_IMAGE" pg_restore --list < "$1"
  else
    pg_restore --list "$1"
  fi
}

pg_dump_version() {
  if [[ "$PG_CLIENT" == "docker" ]]; then docker run --rm "$PG_DOCKER_IMAGE" pg_dump --version
  else pg_dump --version; fi
}

dump() { # label db host port user pass outfile
  local label="$1" db="$2" host="$3" port="$4" user="$5" pass="$6" out="$7" t0=$SECONDS
  local errf="$TARGET/.dump.err"
  log "导出${label}库 $db（$host:$port）..."
  if ! pg_dump_fc "$host" "$port" "$user" "$pass" "$db" "$out" 2>"$errf"; then
    cat "$errf" >&2
    rm -f "$errf"
    die "pg_dump 导出 $db 失败（见上方错误）"
  fi
  rm -f "$errf"
  [[ -s "$out" ]] || die "导出文件为空：$out"
  pg_list "$out" >/dev/null 2>"$errf" || { cat "$errf" >&2; die "导出文件自检失败：$out"; }
  log "${label}库完成：$(du -h "$out" | awk '{print $1}')，耗时 $((SECONDS - t0)) 秒"
}

hsm_backup() {
  local enabled="$HSM_BACKUP" stage="" pass pin
  if [[ "$enabled" == "auto" ]]; then
    if [[ -n "$HSM_TENANTS_FILE" || -n "$HSM_TOKEN_DIR" || -n "$HSM_DOCKER_CONTAINER" ]]; then
      enabled=1
    else
      enabled=0
    fi
  fi
  if [[ "$enabled" != "1" ]]; then
    [[ "${HSM_BACKUP_REQUIRED:-0}" != "1" ]] || die "HSM_BACKUP_REQUIRED=1 但未启用 HSM 密钥备份"
    log "警告：未配置 HSM 密钥资料，备份包不含 tenants.json/token/PIN，密文库无法独立恢复"
    return 0
  fi
  command -v openssl >/dev/null 2>&1 || die "缺少 openssl，无法加密 HSM 密钥包"
  pass="$HSM_BACKUP_PASSPHRASE"
  if [[ -z "$pass" && -n "$HSM_BACKUP_PASSPHRASE_FILE" ]]; then
    IFS= read -r pass < "$HSM_BACKUP_PASSPHRASE_FILE" || true
  fi
  [[ -n "$pass" ]] || die "缺少 HSM_BACKUP_PASSPHRASE / HSM_BACKUP_PASSPHRASE_FILE（加密密钥包口令）"
  pin="$HSM_PIN"
  if [[ -z "$pin" && -n "$HSM_PIN_FILE" ]]; then
    IFS= read -r pin < "$HSM_PIN_FILE" || true
  fi
  [[ -n "$pin" ]] || die "缺少 HSM_PIN / HSM_PIN_FILE（SoftHSM2 PIN）"

  stage="$(mktemp -d "${TMPDIR:-/tmp}/hsmkeys.XXXXXX")" || die "无法创建 HSM 临时目录"
  hsm_fail() { rm -rf -- "$stage"; die "$*"; }
  mkdir -p "$stage/token" || hsm_fail "无法创建临时 token 目录"
  if [[ -n "$HSM_DOCKER_CONTAINER" ]]; then
    command -v docker >/dev/null 2>&1 || hsm_fail "缺少 docker 命令"
    [[ -n "$HSM_TENANTS_IN_CONTAINER" ]] || hsm_fail "需设置 HSM_TENANTS_IN_CONTAINER"
    [[ -n "$HSM_TOKEN_IN_CONTAINER" ]] || hsm_fail "需设置 HSM_TOKEN_IN_CONTAINER"
    docker cp "$HSM_DOCKER_CONTAINER:$HSM_TENANTS_IN_CONTAINER" "$stage/tenants.json" >/dev/null \
      || hsm_fail "从容器复制 tenants.json 失败"
    docker cp "$HSM_DOCKER_CONTAINER:$HSM_TOKEN_IN_CONTAINER/." "$stage/token/" >/dev/null \
      || hsm_fail "从容器复制 token 目录失败"
  else
    [[ -n "$HSM_TENANTS_FILE" && -f "$HSM_TENANTS_FILE" ]] || hsm_fail "HSM_TENANTS_FILE 不可用：$HSM_TENANTS_FILE"
    [[ -n "$HSM_TOKEN_DIR" && -d "$HSM_TOKEN_DIR" ]] || hsm_fail "HSM_TOKEN_DIR 不可用：$HSM_TOKEN_DIR"
    cp -p "$HSM_TENANTS_FILE" "$stage/tenants.json" || hsm_fail "复制 tenants.json 失败"
    cp -a "$HSM_TOKEN_DIR/." "$stage/token/" || hsm_fail "复制 token 目录失败"
  fi
  [[ -s "$stage/tenants.json" ]] || hsm_fail "tenants.json 为空"
  [[ -n "$(find "$stage/token" -mindepth 1 -print -quit)" ]] || hsm_fail "token 目录为空"
  printf '%s\n' "$pin" > "$stage/pin.txt" || hsm_fail "写入 PIN 失败"
  chmod 600 "$stage/pin.txt"
  HSM_TENANTS_SHA="$(sha256sum "$stage/tenants.json" | awk '{print $1}')"

  export HSM_BACKUP_PASSPHRASE_INTERNAL="$pass"
  if ! tar --numeric-owner -czf - -C "$stage" . \
      | openssl enc -aes-256-cbc -pbkdf2 -iter 200000 -salt \
          -pass env:HSM_BACKUP_PASSPHRASE_INTERNAL -out "$TARGET/hsm-keys.tar.gz.enc"; then
    unset HSM_BACKUP_PASSPHRASE_INTERNAL
    hsm_fail "加密 HSM 密钥包失败"
  fi
  unset HSM_BACKUP_PASSPHRASE_INTERNAL
  rm -rf -- "$stage"
  [[ -s "$TARGET/hsm-keys.tar.gz.enc" ]] || die "HSM 密钥包为空"
  HSM_KEYS=1
  HSM_KEYS_FILE="hsm-keys.tar.gz.enc"
  HSM_KEYS_SHA="$(sha256sum "$TARGET/$HSM_KEYS_FILE" | awk '{print $1}')"
  log "HSM 密钥包已加密生成：$HSM_KEYS_FILE（tenants.json + token + PIN）"
}

mkdir -p "$TARGET"
log "备份开始：$TARGET"
if [[ -n "$CONFIG" ]]; then log "连接参数默认取自 $CONFIG（环境变量优先）"; fi
if [[ "$CFG_PLAIN_FROM_TOP" == "1" ]]; then
  log "提示：config.yaml 未定义 databases.plain 的连接参数，明文库改用顶层 database 段"
fi
if [[ "$PG_CLIENT" == "docker" ]]; then
  log "PG 客户端：docker run $PG_DOCKER_IMAGE（容器内经 $DOCKER_PG_HOST 访问数据库，导出 -Fc）"
else
  log "PG 客户端：本机 $(pg_dump --version)"
fi
log "明文库 $PLAIN_DB @ $PLAIN_PGHOST:$PLAIN_PGPORT（用户 $PLAIN_PGUSER）"
log "密文库 $SECURE_DB @ $SECURE_PGHOST:$SECURE_PGPORT（用户 $SECURE_PGUSER）"

# 预检连接与版本（psql 原始错误直接输出到终端）
PLAIN_VER="$(psqlq "$PLAIN_PGHOST" "$PLAIN_PGPORT" "$PLAIN_PGUSER" "$PLAIN_PGPASSWORD" "$PLAIN_DB" 'SHOW server_version')" \
  || die "无法连接明文库 $PLAIN_DB（$PLAIN_PGHOST:$PLAIN_PGPORT，用户 $PLAIN_PGUSER）"
SECURE_VER="$(psqlq "$SECURE_PGHOST" "$SECURE_PGPORT" "$SECURE_PGUSER" "$SECURE_PGPASSWORD" "$SECURE_DB" 'SHOW server_version')" \
  || die "无法连接密文库 $SECURE_DB（$SECURE_PGHOST:$SECURE_PGPORT，用户 $SECURE_PGUSER）"
PLAIN_BYTES="$(psqlq "$PLAIN_PGHOST" "$PLAIN_PGPORT" "$PLAIN_PGUSER" "$PLAIN_PGPASSWORD" "$PLAIN_DB" 'SELECT pg_database_size(current_database())')" \
  || die "无法获取明文库大小"
SECURE_BYTES="$(psqlq "$SECURE_PGHOST" "$SECURE_PGPORT" "$SECURE_PGUSER" "$SECURE_PGPASSWORD" "$SECURE_DB" 'SELECT pg_database_size(current_database())')" \
  || die "无法获取密文库大小"
NEED_KB=$(( (PLAIN_BYTES + SECURE_BYTES) / 1024 * 12 / 10 ))
AVAIL_KB="$(df -Pk "$BACKUP_ROOT" | awk 'NR==2{print $4}')"
(( AVAIL_KB > NEED_KB )) || die "磁盘空间不足：需要约 $((NEED_KB/1024))MB，可用 $((AVAIL_KB/1024))MB"

dump 明文 "$PLAIN_DB" "$PLAIN_PGHOST" "$PLAIN_PGPORT" "$PLAIN_PGUSER" "$PLAIN_PGPASSWORD" "$TARGET/plain.dump"
dump 密文 "$SECURE_DB" "$SECURE_PGHOST" "$SECURE_PGPORT" "$SECURE_PGUSER" "$SECURE_PGPASSWORD" "$TARGET/secure.dump"

GLOBALS=0
if [[ "${BACKUP_GLOBALS:-0}" == "1" ]]; then
  log "导出角色/表空间等全局对象..."
  if [[ "$PG_CLIENT" == "docker" ]]; then
    docker run --rm -e PGPASSWORD="$PLAIN_PGPASSWORD" "$PG_DOCKER_IMAGE" \
      pg_dumpall -h "$(docker_pg_host "$PLAIN_PGHOST")" -p "$PLAIN_PGPORT" -U "$PLAIN_PGUSER" \
      --globals-only > "$TARGET/globals.sql" 2>>"$LOG" || die "pg_dumpall --globals-only 失败"
  else
    PGPASSWORD="$PLAIN_PGPASSWORD" pg_dumpall -h "$PLAIN_PGHOST" -p "$PLAIN_PGPORT" -U "$PLAIN_PGUSER" \
      --globals-only -f "$TARGET/globals.sql" 2>>"$LOG" || die "pg_dumpall --globals-only 失败"
  fi
  GLOBALS=1
fi

hsm_backup

(
  cd "$TARGET"
  SUM_FILES=()
  [[ -f "${PLAIN_DUMP_NAME:-plain.dump}" ]] && SUM_FILES+=("${PLAIN_DUMP_NAME:-plain.dump}")
  [[ -f "${SECURE_DUMP_NAME:-secure.dump}" ]] && SUM_FILES+=("${SECURE_DUMP_NAME:-secure.dump}")
  [[ "$HSM_KEYS" == "1" && -f "$HSM_KEYS_FILE" ]] && SUM_FILES+=("$HSM_KEYS_FILE")
  ((${#SUM_FILES[@]} > 0)) || { echo "错误：没有可校验的文件" >&2; exit 1; }
  sha256sum "${SUM_FILES[@]}" > SHA256SUMS
)
sum_sha() { # filename ；兼容 sha256sum 的 "hash  file" 与 "hash *file" 格式
  awk -v want="$1" '{ f=$2; sub(/^\*/,"",f); sub(/^\.\//,"",f); if (f==want) print $1 }' "$TARGET/SHA256SUMS"
}
PLAIN_SHA="$(sum_sha plain.dump)"
SECURE_SHA="$(sum_sha secure.dump)"

cat > "$TARGET/manifest.txt" <<EOF
FORMAT_VERSION=1
CREATED_AT=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
HOSTNAME=$(hostname 2>/dev/null || echo unknown)
PG_DUMP_VERSION=$(pg_dump_version | sed 's/^[^ ]* //')
PLAIN_DB=$PLAIN_DB
PLAIN_SERVER_VERSION=$PLAIN_VER
PLAIN_DUMP=plain.dump
PLAIN_SHA256=$PLAIN_SHA
SECURE_DB=$SECURE_DB
SECURE_SERVER_VERSION=$SECURE_VER
SECURE_DUMP=secure.dump
SECURE_SHA256=$SECURE_SHA
GLOBALS=$GLOBALS
HSM_KEYS=$HSM_KEYS
HSM_KEYS_FILE=$HSM_KEYS_FILE
HSM_KEYS_SHA256=$HSM_KEYS_SHA
HSM_TENANTS_SHA256=$HSM_TENANTS_SHA
CONSISTENCY=per-database MVCC snapshot; cross-database not atomic
NOTE=secure dump needs HSM tenant keys to decrypt; HSM key archive is AES-256 encrypted
EOF
rm -f "$TARGET/.incomplete"

log "备份完成：明文 $(du -h "$TARGET/plain.dump" | awk '{print $1}')，密文 $(du -h "$TARGET/secure.dump" | awk '{print $1}')"
log "校验清单：$TARGET/SHA256SUMS"

# 保留最近 N 份
while IFS= read -r old; do
  [[ -z "$old" ]] && continue
  log "清理过期备份：$old"
  rm -rf -- "${BACKUP_ROOT:?}/$old"
done < <(find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -name '20*' -printf '%f\n' | sort -r | tail -n +$((BACKUP_KEEP + 1)))

if [[ "$HSM_KEYS" == "1" ]]; then
  log "全部成功。HSM 密钥包（tenants.json + token + PIN）已加密随附；解密口令与 PIN 必须离线分开保管。"
else
  log "全部成功，但备份包不含 HSM 密钥资料，密文库历史数据无法独立解密，请另做 HSM 离线备份。"
fi
