CREATE TABLE platform_admin (
  id TEXT PRIMARY KEY,
  login_name TEXT NOT NULL UNIQUE,
  password_verifier TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN('active','disabled','locked')),
  version BIGINT NOT NULL DEFAULT 1,
  last_login_at BIGINT,
  created_at BIGINT NOT NULL
);
CREATE TABLE platform_session (
  id TEXT PRIMARY KEY,
  admin_id TEXT NOT NULL REFERENCES platform_admin(id),
  expires_at BIGINT NOT NULL,
  revoked_at BIGINT,
  device_info JSONB NOT NULL DEFAULT '{}',
  created_at BIGINT NOT NULL
);
CREATE INDEX ix_platform_session_admin ON platform_session(admin_id,expires_at);
CREATE TABLE platform_setting (
  id TEXT PRIMARY KEY,
  setting_key TEXT NOT NULL,
  setting_value JSONB NOT NULL,
  version BIGINT NOT NULL,
  updated_by TEXT NOT NULL REFERENCES platform_admin(id),
  change_reason TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  UNIQUE(setting_key,version)
);
CREATE TABLE platform_audit_log (
  id TEXT PRIMARY KEY,
  request_id TEXT NOT NULL,
  actor_id TEXT,
  action TEXT NOT NULL,
  owner_type TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  before_values JSONB,
  after_values JSONB,
  reason TEXT,
  created_at BIGINT NOT NULL
);
CREATE INDEX ix_platform_audit_owner ON platform_audit_log(owner_type,owner_id,created_at);
CREATE TRIGGER trg_platform_audit_append_only BEFORE UPDATE OR DELETE ON platform_audit_log FOR EACH ROW EXECUTE FUNCTION reject_mutation();
COMMENT ON TABLE platform_admin IS '独立平台管理员，不属于任何租户';
COMMENT ON TABLE platform_session IS '平台管理员独立会话，与租户会话隔离';
COMMENT ON TABLE platform_setting IS '平台参数只追加版本';
COMMENT ON TABLE platform_audit_log IS '平台登录、租户生命周期和参数操作审计';
COMMENT ON COLUMN platform_admin.login_name IS '平台管理员全局登录名';
COMMENT ON COLUMN platform_admin.password_verifier IS 'Argon2id密码验证器，不明文保存';
COMMENT ON COLUMN platform_setting.setting_key IS '平台参数键';
COMMENT ON COLUMN platform_setting.setting_value IS '平台参数值';
COMMENT ON COLUMN platform_setting.change_reason IS '平台参数变更原因';
