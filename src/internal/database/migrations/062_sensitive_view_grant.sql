-- 敏感信息查看授权码（设计 2.4.3/6.4）：加密字段默认 *** 占位，持有效授权码可经
-- /reveal 接口查看明文；每次查看记审计日志，授权码高熵随机串以 SHA-256 哈希存储。
-- 表名白名单由服务端校验（当前仅 employee）；row_id 为空表示该表全部行。
-- 两库同结构（密文库/明文库各建一份）。

CREATE TABLE IF NOT EXISTS sensitive_view_grant (
  id           TEXT PRIMARY KEY,
  tenant_id    TEXT NOT NULL,
  table_name   TEXT NOT NULL,               -- 服务端白名单校验（当前: employee）
  row_id       TEXT NOT NULL DEFAULT '',    -- '' 表示该表全部行
  code_hash    TEXT NOT NULL,               -- sha256hex(授权码明文)，明文仅此一次返回（生成接口）
  status       TEXT NOT NULL DEFAULT 'active', -- active / revoked
  expires_at   BIGINT NOT NULL,             -- 过期时间（微秒时间戳）
  created_by   TEXT NOT NULL,
  created_at   BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS ix_svg_lookup ON sensitive_view_grant(tenant_id,table_name,status,expires_at);
