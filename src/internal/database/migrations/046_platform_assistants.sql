-- 平台辅助人员：由平台管理员统一创建与分配权限；
-- 辅助人员不能再创建/管理平台管理员与辅助人员（assistants.manage 仅超管），其余按分配的权限点访问平台接口。
ALTER TABLE platform_admin ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'super';
ALTER TABLE platform_admin ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN platform_admin.role IS '平台账号角色：super=平台管理员（全部权限）；assistant=辅助人员（按 platform_admin_permission 分配）';
COMMENT ON COLUMN platform_admin.name IS '平台账号显示名称';

CREATE TABLE IF NOT EXISTS platform_admin_permission (
  admin_id TEXT NOT NULL REFERENCES platform_admin(id),
  permission_code TEXT NOT NULL,
  PRIMARY KEY(admin_id, permission_code)
);
COMMENT ON TABLE platform_admin_permission IS '平台辅助人员的权限点分配（仅 role=assistant 生效）';
