CREATE TABLE platform_role_template (
  id TEXT PRIMARY KEY,
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  description TEXT,
  sort_no INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN('active','disabled')),
  created_at BIGINT NOT NULL
);
COMMENT ON TABLE platform_role_template IS '平台预定义租户用户角色模板';
COMMENT ON COLUMN platform_role_template.code IS '角色模板全局唯一编码';
COMMENT ON COLUMN platform_role_template.name IS '角色模板显示名称';
COMMENT ON COLUMN platform_role_template.description IS '角色模板说明';
COMMENT ON COLUMN platform_role_template.sort_no IS '显示顺序';
COMMENT ON COLUMN platform_role_template.status IS '模板启停用状态';
COMMENT ON COLUMN platform_role_template.created_at IS '模板创建时间，UTC微秒';

CREATE TABLE platform_role_template_permission (
  id TEXT PRIMARY KEY,
  template_id TEXT NOT NULL REFERENCES platform_role_template(id),
  permission_code TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  UNIQUE(template_id,permission_code)
);
COMMENT ON TABLE platform_role_template_permission IS '角色模板对应平台权限点';
COMMENT ON COLUMN platform_role_template_permission.template_id IS '角色模板ID';
COMMENT ON COLUMN platform_role_template_permission.permission_code IS '平台功能模块权限点编码';

INSERT INTO platform_role_template(id,code,name,description,sort_no,created_at) VALUES
('prt-factory-manager','factory_manager','厂长/生产经理','查看生产、工单、任务、报工和进度',10,1),
('prt-production-supervisor','production_supervisor','生产主管','查看并派工所辖生产任务',20,1),
('prt-cutting-lead','cutting_lead','裁床负责人','裁床维护及工单下发',30,1),
('prt-group-leader','group_leader','组长','本组派工、报工和审批',40,1),
('prt-warehouse','warehouse','仓管/质检','裁床、质检、库存相关操作',50,1),
('prt-payroll-finance','payroll_finance','工资财务','工资查看、确认、付款和导出',60,1),
('prt-worker','worker','车工','本人任务和扫码报工',70,1)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-fm-001','prt-factory-manager','workbench:read',1),('prtp-fm-002','prt-factory-manager','work_order:read',1),('prtp-fm-003','prt-factory-manager','work_order:write',1),('prtp-fm-004','prt-factory-manager','task:read',1),('prtp-fm-005','prt-factory-manager','report:read',1),('prtp-fm-006','prt-factory-manager','progress:read',1),
('prtp-ps-001','prt-production-supervisor','work_order:read',1),('prtp-ps-002','prt-production-supervisor','task:read',1),('prtp-ps-003','prt-production-supervisor','task:assign',1),('prtp-ps-004','prt-production-supervisor','report:read',1),('prtp-ps-005','prt-production-supervisor','progress:read',1),
('prtp-cl-001','prt-cutting-lead','work_order:read',1),('prtp-cl-002','prt-cutting-lead','work_order:release',1),('prtp-cl-003','prt-cutting-lead','cutting:read',1),('prtp-cl-004','prt-cutting-lead','cutting:write',1),
('prtp-gl-001','prt-group-leader','task:read',1),('prtp-gl-002','prt-group-leader','task:assign',1),('prtp-gl-003','prt-group-leader','report:read',1),('prtp-gl-004','prt-group-leader','report:create',1),('prtp-gl-005','prt-group-leader','review:approve',1),('prtp-gl-006','prt-group-leader','progress:read',1),
('prtp-wh-001','prt-warehouse','work_order:read',1),('prtp-wh-002','prt-warehouse','work_order:write',1),('prtp-wh-003','prt-warehouse','cutting:read',1),('prtp-wh-004','prt-warehouse','cutting:write',1),
('prtp-pf-001','prt-payroll-finance','payroll:read',1),('prtp-pf-002','prt-payroll-finance','payroll:pay',1),('prtp-pf-003','prt-payroll-finance','payroll:export',1),('prtp-pf-004','prt-payroll-finance','progress:read',1),
('prtp-wk-001','prt-worker','task:read',1),('prtp-wk-002','prt-worker','report:create',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;
