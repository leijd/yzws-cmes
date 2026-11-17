#!/usr/bin/env bash
# restoryFull.sh —— 用 backupFull.sh 的备份包恢复明文库 + 密文库（Linux）。
#
# 用法：
#   ./restoryFull.sh <备份目录>             # 目录含 manifest.txt/SHA256SUMS/*.dump
#   ./restoryFull.sh --latest               # 取 BACKUP_ROOT 下最新一份
#   ./restoryFull.sh --yes --latest         # 免交互（脚本化容灾演练）
#   选项：--keep-existing（不删库，pg_restore --clean 覆盖）
#         --no-analyze     恢复后不执行 ANALYZE
#         --no-owner       忽略属主（跨集群恢复到不同角色时用）
#         --globals        先恢复备份包内的 globals.sql（角色/表空间）
#         --target-plain NAME / --target-secure NAME  恢复到不同库名
#
# 连接目标集群：环境变量 > backup/backup.env > src/config.yaml 的 databases.plain；
# 维护库默认 postgres（RESTORE_MAINTENANCE_DB 可改），属主默认 PGUSER（RESTORE_DB_OWNER）。
# 目标库名默认取 manifest 中记录的原库名。
# 默认自动探测：本机 pg_* 主版本低于服务端时用 docker run postgres:<major> 的客户端；
# PG_CLIENT=local|docker|auto 可指定，PG_DOCKER_IMAGE 指定镜像，PG_DOCKER_HOST 指定数据库宿主机在临时容器内的可达地址。
#
# !!! 密文库恢复后仍需 HSM 租户密钥（tenants.json + token + PIN）才能解密字段；
#     备份包若含 hsm-keys.tar.gz.enc，为 AES-256 加密的密钥包（解密口令与 PIN 不随包提供）。
set -Eeuo pipefail
umask 077

DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO="$(cd -- "$DIR/.." && pwd -P)"
ENVFILE="${BACKUP_ENV_FILE:-$DIR/backup.env}"
# shellcheck disable=SC1090
[[ -f "$ENVFILE" ]] && source "$ENVFILE"
[[ -n "${PG_BIN:-}" ]] && PATH="$PG_BIN:$PATH"

# 目标集群默认取自环境变量 / backup.env / src/config.yaml 的 databases.plain
CONFIG="${BACKUP_CONFIG_YAML:-}"
if [[ -z "$CONFIG" ]]; then
  for _c in "$REPO/src/config.yaml" "$REPO/config.yaml"; do
    if [[ -f "$_c" ]]; then CONFIG="$_c"; break; fi
  done
fi
[[ "${BACKUP_NO_CONFIG:-0}" == "1" ]] && CONFIG=""
cfg() {
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
cfg_top() { # 顶层 database: 段（plain 缺省时兜底）
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
PGHOST="${PGHOST:-$(cfg plain host)}"; PGHOST="${PGHOST:-$(cfg_top host)}"; PGHOST="${PGHOST:-127.0.0.1}"
PGPORT="${PGPORT:-$(cfg plain port)}"; PGPORT="${PGPORT:-$(cfg_top port)}"; PGPORT="${PGPORT:-5432}"
PGUSER="${PGUSER:-$(cfg plain user)}"; PGUSER="${PGUSER:-$(cfg_top user)}"; PGUSER="${PGUSER:-postgres}"
export PGCONNECT_TIMEOUT="${PGCONNECT_TIMEOUT:-10}"
PGPASSWORD="${PGPASSWORD:-$(cfg plain password)}"; PGPASSWORD="${PGPASSWORD:-$(cfg_top password)}"
BACKUP_ROOT="${BACKUP_ROOT:-$REPO/db-backup/full}"
MAINT_DB="${RESTORE_MAINTENANCE_DB:-postgres}"
OWNER="${RESTORE_DB_OWNER:-$PGUSER}"

ASSUME_YES=0; KEEP_EXISTING=0; ANALYZE_AFTER=1; NO_OWNER=0; DO_GLOBALS=0; LATEST=0
SRC=""; TARGET_PLAIN=""; TARGET_SECURE=""
while (($#)); do
  case "$1" in
    --yes|-y) ASSUME_YES=1 ;;
    --latest) LATEST=1 ;;
    --keep-existing) KEEP_EXISTING=1 ;;
    --no-analyze) ANALYZE_AFTER=0 ;;
    --no-owner) NO_OWNER=1 ;;
    --globals) DO_GLOBALS=1 ;;
    --target-plain) shift; TARGET_PLAIN="${1:-}" ;;
    --target-secure) shift; TARGET_SECURE="${1:-}" ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    -*) echo "未知选项：$1" >&2; exit 2 ;;
    *) SRC="$1" ;;
  esac
  shift
done

die() { echo "错误：$*" >&2; exit 1; }

PG_CLIENT="${PG_CLIENT:-auto}"
PG_DOCKER_IMAGE="${PG_DOCKER_IMAGE:-}"
local_major=""; server_major=""
if command -v psql >/dev/null 2>&1; then
  local_major="$(psql --version 2>/dev/null | grep -oE '[0-9]+(\.[0-9]+)?' | head -n1 || true)"
  local_major="${local_major%%.*}"
  server_major="$(PGPASSWORD="$PGPASSWORD" psql -AtX -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$MAINT_DB" -c 'SHOW server_version' 2>/dev/null | grep -oE '[0-9]+(\.[0-9]+)?' | head -n1 || true)"
  server_major="${server_major%%.*}"
elif command -v pg_restore >/dev/null 2>&1; then
  local_major="$(pg_restore --version 2>/dev/null | grep -oE '[0-9]+(\.[0-9]+)?' | head -n1 || true)"
  local_major="${local_major%%.*}"
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
  command -v docker >/dev/null 2>&1 || die "缺少 docker 命令（本机 pg_* 版本不足）"
  docker info >/dev/null 2>&1 || die "docker daemon 不可用"
  PG_DOCKER_IMAGE="${PG_DOCKER_IMAGE:-postgres:${server_major:-18}}"
  DOCKER_PG_HOST="${PG_DOCKER_HOST:-}"
  [[ -n "$DOCKER_PG_HOST" ]] || DOCKER_PG_HOST="$(docker network inspect bridge --format '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null || true)"
  DOCKER_PG_HOST="${DOCKER_PG_HOST:-172.17.0.1}"
else
  command -v pg_restore >/dev/null || die "缺少 pg_restore"
  command -v psql >/dev/null || die "缺少 psql"
fi
command -v sha256sum >/dev/null || die "缺少 sha256sum"

docker_pg_host() {
  case "$1" in
    127.0.0.1|localhost|'::1') printf '%s' "$DOCKER_PG_HOST" ;;
    *) printf '%s' "$1" ;;
  esac
}

qdb() { # db sql
  if [[ "$PG_CLIENT" == "docker" ]]; then
    docker run --rm -e PGPASSWORD="$PGPASSWORD" "$PG_DOCKER_IMAGE" psql -AtX -v ON_ERROR_STOP=1 \
      -h "$(docker_pg_host "$PGHOST")" -p "$PGPORT" -U "$PGUSER" -d "$1" -c "$2"
  else
    PGPASSWORD="$PGPASSWORD" psql -AtX -v ON_ERROR_STOP=1 -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$1" -c "$2"
  fi
}
q() { qdb "$MAINT_DB" "$1"; }

psql_file() { # db file
  if [[ "$PG_CLIENT" == "docker" ]]; then
    docker run --rm -i -e PGPASSWORD="$PGPASSWORD" "$PG_DOCKER_IMAGE" \
      psql -X -v ON_ERROR_STOP=1 -h "$(docker_pg_host "$PGHOST")" -p "$PGPORT" -U "$PGUSER" -d "$1" \
      < "$2" >/dev/null
  else
    PGPASSWORD="$PGPASSWORD" psql -X -v ON_ERROR_STOP=1 -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$1" \
      -f "$2" >/dev/null
  fi
}

pg_restore_run() { # db dumpfile args...
  local db="$1" file="$2"; shift 2
  if [[ "$PG_CLIENT" == "docker" ]]; then
    docker run --rm -i -e PGPASSWORD="$PGPASSWORD" "$PG_DOCKER_IMAGE" \
      pg_restore -h "$(docker_pg_host "$PGHOST")" -p "$PGPORT" -U "$PGUSER" --dbname="$db" "$@" < "$file"
  else
    PGPASSWORD="$PGPASSWORD" pg_restore -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" --dbname="$db" "$@" "$file"
  fi
}

if ((LATEST)); then
  [[ -z "$SRC" ]] || die "--latest 与备份目录参数不能同时使用"
  SRC="$(find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -name '20*' | sort | tail -n 1)"
  [[ -n "$SRC" ]] || die "$BACKUP_ROOT 下没有备份"
fi
[[ -n "$SRC" ]] || die "用法：$0 [--latest] [--yes] <备份目录>"
[[ -d "$SRC" ]] || die "备份目录不存在：$SRC"
MANIFEST="$SRC/manifest.txt"
[[ -f "$MANIFEST" ]] || die "缺少 manifest.txt：$SRC"
[[ ! -f "$SRC/.incomplete" ]] || die "该备份标记为不完整（.incomplete），拒绝恢复"

mval() { sed -n "s/^$1=//p" "$MANIFEST" | head -n 1; }
PLAIN_DB="$(mval PLAIN_DB)"; SECURE_DB="$(mval SECURE_DB)"
PLAIN_DUMP="$(mval PLAIN_DUMP)"; SECURE_DUMP="$(mval SECURE_DUMP)"
GLOBALS_MARK="$(mval GLOBALS)"
HSM_KEYS_MARK="$(mval HSM_KEYS)"
HSM_KEYS_FILE="$(mval HSM_KEYS_FILE)"
HSM_KEYS_SHA_EXPECTED="$(mval HSM_KEYS_SHA256)"
[[ -n "$PLAIN_DB" && -n "$SECURE_DB" && -n "$PLAIN_DUMP" && -n "$SECURE_DUMP" ]] || die "manifest.txt 缺少必要字段"
TARGET_PLAIN="${TARGET_PLAIN:-$PLAIN_DB}"
TARGET_SECURE="${TARGET_SECURE:-$SECURE_DB}"

valid_db() { [[ "$1" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]*$ ]] && ! [[ "$1" =~ ^(postgres|template0|template1)$ ]]; }
valid_db "$TARGET_PLAIN" || die "非法目标库名：$TARGET_PLAIN"
valid_db "$TARGET_SECURE" || die "非法目标库名：$TARGET_SECURE"
[[ "$TARGET_PLAIN" != "$TARGET_SECURE" ]] || die "两个目标库不能同名"

[[ -f "$SRC/$PLAIN_DUMP" && -f "$SRC/$SECURE_DUMP" ]] || die "备份包缺少 dump 文件"
[[ -f "$SRC/SHA256SUMS" ]] || die "缺少 SHA256SUMS，无法校验完整性"
echo "校验备份包完整性..."
(cd "$SRC" && sha256sum -c SHA256SUMS) || die "SHA-256 校验失败，备份可能损坏"
if [[ "$HSM_KEYS_MARK" == "1" ]]; then
  [[ -n "$HSM_KEYS_FILE" && -f "$SRC/$HSM_KEYS_FILE" ]] || die "manifest 声明 HSM 密钥包但文件缺失"
  HSM_KEYS_SHA_ACTUAL="$(sha256sum "$SRC/$HSM_KEYS_FILE" | awk '{print $1}')"
  [[ -z "$HSM_KEYS_SHA_EXPECTED" || "$HSM_KEYS_SHA_EXPECTED" == "$HSM_KEYS_SHA_ACTUAL" ]] \
    || die "HSM 密钥包 SHA-256 不匹配"
  echo "HSM 密钥包 : $HSM_KEYS_FILE（已校验，需离线口令解密）"
else
  echo "HSM 密钥包 : 缺失（警告：密文库历史数据无法独立解密）" >&2
fi

echo "------------------------------------------------------------"
echo "备份目录 : $SRC"
echo "明文库   : $PLAIN_DB  ->  $TARGET_PLAIN"
echo "密文库   : $SECURE_DB ->  $TARGET_SECURE"
echo "目标集群 : $PGHOST:$PGPORT (用户 $PGUSER, 属主 $OWNER)"
echo "PG 客户端: $([[ $PG_CLIENT == docker ]] && echo "docker run $PG_DOCKER_IMAGE (经 $DOCKER_PG_HOST 访问数据库)" || echo 本机)"
echo "方式     : $([[ $KEEP_EXISTING == 1 ]] && echo '保留原库并覆盖对象' || echo '删除并重建目标库')"
echo "------------------------------------------------------------"
if ((!ASSUME_YES)); then
  ans=""
  read -r -p "此操作会覆盖目标库，输入 RESTORE 继续：" ans || true
  [[ "$ans" == "RESTORE" ]] || die "已取消"
fi

# 恢复全局对象（角色等）需要超级用户；失败不静默
if ((DO_GLOBALS)); then
  [[ "$GLOBALS_MARK" == "1" && -f "$SRC/globals.sql" ]] || die "--globals 指定但备份包中没有 globals.sql"
  echo "恢复 globals.sql ..."
  psql_file "$MAINT_DB" "$SRC/globals.sql"
fi

prepare_db() { # name
  local db="$1" exists
  exists="$(q "SELECT 1 FROM pg_database WHERE datname = '$db'")"
  if [[ -n "$exists" ]]; then
    if ((KEEP_EXISTING)); then
      echo "保留现有数据库 $db（--clean 覆盖对象）"
      return 0
    fi
    q "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '$db' AND pid <> pg_backend_pid()" >/dev/null
    q "DROP DATABASE IF EXISTS \"$db\"" >/dev/null
    echo "已删除旧数据库 $db"
  fi
  q "CREATE DATABASE \"$db\" OWNER \"$OWNER\" TEMPLATE template0" >/dev/null
  echo "已创建数据库 $db（owner=$OWNER）"
}

restore_db() { # name dumpfile
  local db="$1" file="$2" t0=$SECONDS
  echo "恢复 $db <- $(basename "$file") ..."
  local args=(--exit-on-error --single-transaction)
  ((KEEP_EXISTING)) && args+=(--clean --if-exists)
  ((NO_OWNER)) && args+=(--no-owner --no-privileges)
  pg_restore_run "$db" "$file" "${args[@]}" || die "pg_restore 恢复 $db 失败"
  echo "$db 恢复完成，耗时 $((SECONDS - t0)) 秒"
}

check_db() { # name
  local db="$1" v
  v="$(qdb "$db" "SELECT CASE WHEN to_regclass('public.schema_migration') IS NULL THEN 'MISSING' ELSE (SELECT count(*)||' migrations, latest='||COALESCE(max(version),'none') FROM schema_migration) END")" \
    || die "恢复后连接 $db 失败"
  echo "  $db 校验：$v"
  [[ "$v" != "MISSING" ]] || echo "  警告：$db 中未找到 schema_migration，请确认这是 MES 数据库" >&2
  if ((ANALYZE_AFTER)); then
    qdb "$db" 'ANALYZE' || echo "  警告：$db ANALYZE 失败（不影响数据）" >&2
  fi
}

prepare_db "$TARGET_PLAIN"
prepare_db "$TARGET_SECURE"
restore_db "$TARGET_PLAIN" "$SRC/$PLAIN_DUMP"
restore_db "$TARGET_SECURE" "$SRC/$SECURE_DUMP"

echo "恢复后校验："
check_db "$TARGET_PLAIN"
check_db "$TARGET_SECURE"

cat >&2 <<'EOF'
------------------------------------------------------------
恢复完成。密文库数据仍是密文，务必确认：
  1) HSM 服务已启动且可访问（/healthz、X-API-Key 有效）；
  2) 若备份包含 hsm-keys.tar.gz.enc，先用离线保管的口令解密并恢复
     tenants.json + token 目录 + PIN；否则密文字段无法解密，业务报 KEY_UNAVAILABLE。
建议随后启动服务并做登录、员工档案解密、报工等冒烟验证。
------------------------------------------------------------
EOF
