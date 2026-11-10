-- 制衣厂门户：各厂自己的 logo 与欢迎内容（未设置时使用平台默认内容）
CREATE TABLE IF NOT EXISTS tenant_portal (
  tenant_id TEXT PRIMARY KEY REFERENCES tenant(id),
  logo_url TEXT,
  welcome_title TEXT,
  welcome_text TEXT,
  updated_at BIGINT,
  updated_by TEXT,
  version BIGINT NOT NULL DEFAULT 1
);
