-- 组织、人员、归属、登录域扩展（设计 4.3-4.9 / 9.2 迁移 055 合并实施）。
-- 加密口径：phone_hash 列 055 已建；employee 敏感列沿用现有列，密文库由 DAO 转换。

-- ===== org_unit 扩展（设计 4.3） =====
ALTER TABLE org_unit ADD COLUMN IF NOT EXISTS workshop_nature TEXT NOT NULL DEFAULT 'own';      -- own/outsourced，仅车间
ALTER TABLE org_unit ADD COLUMN IF NOT EXISTS outsourced_partner TEXT;                           -- 外包方名称
ALTER TABLE org_unit ADD COLUMN IF NOT EXISTS line_nature TEXT NOT NULL DEFAULT 'fixed';         -- fixed/dynamic，仅产线
ALTER TABLE org_unit ADD COLUMN IF NOT EXISTS default_process_segment TEXT;                      -- 默认工序段（字典引用）
ALTER TABLE org_unit ADD COLUMN IF NOT EXISTS established_at BIGINT;                             -- 设立时间
ALTER TABLE org_unit ADD COLUMN IF NOT EXISTS capacity_note TEXT;                                -- 产能说明
ALTER TABLE org_unit ADD COLUMN IF NOT EXISTS remark TEXT;                                       -- 备注
COMMENT ON COLUMN org_unit.workshop_nature IS '车间性质：own 自营 / outsourced 外包（外包时 outsourced_partner 必填）';
COMMENT ON COLUMN org_unit.line_nature IS '产线性质：fixed 固定 / dynamic 按工单动态配置';

-- ===== employee 扩展（设计 4.5） =====
ALTER TABLE employee ADD COLUMN IF NOT EXISTS person_type TEXT NOT NULL DEFAULT 'worker';        -- manager/worker
ALTER TABLE employee ADD COLUMN IF NOT EXISTS workshop_org_id TEXT;                              -- 当前主归属车间
ALTER TABLE employee ADD COLUMN IF NOT EXISTS line_org_id TEXT;                                  -- 当前主归属产线
ALTER TABLE employee ADD COLUMN IF NOT EXISTS team_org_id TEXT;                                  -- 当前班组（可空）
CREATE INDEX IF NOT EXISTS ix_employee_org_new ON employee(tenant_id,workshop_org_id,line_org_id,team_org_id,person_type,status,id);

/* person_type 回填：有登录账号的人员视为管理人员（设计 4.5 迁移口径） */
UPDATE employee SET person_type='manager' WHERE id IN (SELECT employee_id FROM user_account);

/* 结构化归属回填：由旧 org_unit_id（班组/产线/车间）推导三级归属 */
UPDATE employee e SET
  workshop_org_id = CASE ou.org_type
    WHEN 'workshop' THEN ou.id
    WHEN 'line' THEN ou.parent_id
    WHEN 'team' THEN (SELECT p.parent_id FROM org_unit p WHERE p.tenant_id=ou.tenant_id AND p.id=ou.parent_id)
    ELSE NULL END,
  line_org_id = CASE ou.org_type
    WHEN 'line' THEN ou.id
    WHEN 'team' THEN ou.parent_id
    ELSE NULL END,
  team_org_id = CASE ou.org_type WHEN 'team' THEN ou.id ELSE NULL END
FROM org_unit ou
WHERE ou.tenant_id=e.tenant_id AND ou.id=e.org_unit_id;

-- ===== employee_org_binding 人员组织归属历史（设计 4.6，新增表） =====
CREATE TABLE IF NOT EXISTS employee_org_binding (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  employee_id TEXT NOT NULL,
  workshop_org_id TEXT NOT NULL,
  line_org_id TEXT NOT NULL,
  team_org_id TEXT,
  is_primary BOOLEAN NOT NULL DEFAULT TRUE,
  valid_from BIGINT NOT NULL,
  valid_to BIGINT,
  change_reason TEXT,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  created_node TEXT NOT NULL,
  PRIMARY KEY(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS ix_binding_employee_current ON employee_org_binding(tenant_id,employee_id,is_primary,valid_to);
CREATE INDEX IF NOT EXISTS ix_binding_line_current ON employee_org_binding(tenant_id,line_org_id,is_primary,valid_to,employee_id);
COMMENT ON TABLE employee_org_binding IS '人员组织归属历史：当前主归属 is_primary=true 且 valid_to IS NULL，每人同时至多一条（换线事务保证）';

/* 为在职且已有结构化归属的员工补当前主归属历史 */
INSERT INTO employee_org_binding(tenant_id,id,employee_id,workshop_org_id,line_org_id,team_org_id,is_primary,valid_from,change_reason,created_at,created_by,created_node)
SELECT e.tenant_id, 'bnd-'||e.id, e.id, e.workshop_org_id, e.line_org_id, e.team_org_id, TRUE,
       COALESCE(e.created_at,extract(epoch from now())*1000000)::BIGINT, '迁移回填',
       extract(epoch from now())*1000000::BIGINT, 'system', 'migration'
FROM employee e
WHERE e.status='active' AND e.line_org_id IS NOT NULL
  AND NOT EXISTS(SELECT 1 FROM employee_org_binding b WHERE b.tenant_id=e.tenant_id AND b.employee_id=e.id AND b.is_primary=TRUE AND b.valid_to IS NULL);

-- ===== user_account 扩展（设计 4.7） =====
ALTER TABLE user_account ADD COLUMN IF NOT EXISTS client_scope TEXT NOT NULL DEFAULT 'all';      -- all/pc/mobile
ALTER TABLE user_account ADD COLUMN IF NOT EXISTS mgmt_level_id TEXT;                            -- L1~L4 数据范围

-- ===== role 扩展（设计 4.8） =====
ALTER TABLE role ADD COLUMN IF NOT EXISTS default_data_scope TEXT;                               -- factory/workshop/line/team/self/...

-- ===== auth_login_log 登录日志（设计 4.9，新增表；只允许 INSERT） =====
CREATE TABLE IF NOT EXISTS auth_login_log (
  id TEXT PRIMARY KEY,
  tenant_id TEXT,
  user_id TEXT,
  login_identifier_masked TEXT,
  login_identifier_hash TEXT,
  login_method TEXT NOT NULL,               -- employee_no_password/phone_password/phone_sms/unified
  client_type TEXT NOT NULL,                -- pc/mobile
  device_info JSONB NOT NULL DEFAULT '{}',
  ip TEXT,
  result TEXT NOT NULL,                     -- success/failure/denied
  failure_code TEXT,
  failure_reason TEXT,
  request_id TEXT NOT NULL,
  session_id TEXT,
  occurred_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS ix_login_log_tenant_time ON auth_login_log(tenant_id,occurred_at,id);
CREATE INDEX IF NOT EXISTS ix_login_log_user ON auth_login_log(user_id,occurred_at);
CREATE INDEX IF NOT EXISTS ix_login_log_failure ON auth_login_log(failure_code,occurred_at);
COMMENT ON TABLE auth_login_log IS '登录日志：只允许 INSERT（触发器强制）；不存完整手机号/密码/token';

CREATE OR REPLACE FUNCTION auth_login_log_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'auth_login_log is insert-only';
END $$;
DROP TRIGGER IF EXISTS trg_auth_login_log_immutable ON auth_login_log;
CREATE TRIGGER trg_auth_login_log_immutable BEFORE UPDATE OR DELETE ON auth_login_log
  FOR EACH ROW EXECUTE FUNCTION auth_login_log_immutable();

-- ===== 新权限点（为所有现有租户补齐，设计 5.2/5.4） =====
INSERT INTO permission(tenant_id,id,code,resource,action,sensitivity,description)
SELECT t.id, 'perm-'||t.id||'-workshop-read', 'workshop:read', 'workshop', 'read', 'normal', '车间查看' FROM tenant t
ON CONFLICT DO NOTHING;
INSERT INTO permission(tenant_id,id,code,resource,action,sensitivity,description)
SELECT t.id, 'perm-'||t.id||'-workshop-write', 'workshop:write', 'workshop', 'write', 'normal', '车间维护' FROM tenant t
ON CONFLICT DO NOTHING;
INSERT INTO permission(tenant_id,id,code,resource,action,sensitivity,description)
SELECT t.id, 'perm-'||t.id||'-line-read', 'line:read', 'line', 'read', 'normal', '产线查看' FROM tenant t
ON CONFLICT DO NOTHING;
INSERT INTO permission(tenant_id,id,code,resource,action,sensitivity,description)
SELECT t.id, 'perm-'||t.id||'-line-write', 'line:write', 'line', 'write', 'normal', '产线维护' FROM tenant t
ON CONFLICT DO NOTHING;
INSERT INTO permission(tenant_id,id,code,resource,action,sensitivity,description)
SELECT t.id, 'perm-'||t.id||'-sensitive-read', 'employee:sensitive:read', 'employee', 'read', 'sensitive', '员工敏感字段查看' FROM tenant t
ON CONFLICT DO NOTHING;
