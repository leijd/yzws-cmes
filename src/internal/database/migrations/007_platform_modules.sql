CREATE TABLE platform_module (
  id TEXT PRIMARY KEY,
  parent_id TEXT REFERENCES platform_module(id),
  module_type TEXT NOT NULL CHECK(module_type IN('menu','button','api')),
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  path TEXT,
  permission_code TEXT NOT NULL,
  sort_no INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN('active','disabled')),
  created_at BIGINT NOT NULL
);
CREATE INDEX ix_platform_module_parent ON platform_module(parent_id,sort_no);
COMMENT ON TABLE platform_module IS '平台统一菜单、按钮和API功能模块定义，用于租户权限分配';
COMMENT ON COLUMN platform_module.parent_id IS '父级功能模块ID';
COMMENT ON COLUMN platform_module.module_type IS '模块类型：菜单、按钮或API';
COMMENT ON COLUMN platform_module.code IS '功能模块全局编码';
COMMENT ON COLUMN platform_module.name IS '功能模块显示名称';
COMMENT ON COLUMN platform_module.path IS '菜单路由或API路径';
COMMENT ON COLUMN platform_module.permission_code IS '租户权限分配使用的统一权限点编码';
COMMENT ON COLUMN platform_module.sort_no IS '同级显示顺序';
COMMENT ON COLUMN platform_module.status IS '模块启停用状态';
COMMENT ON COLUMN platform_module.created_at IS '模块创建时间，UTC微秒';

INSERT INTO platform_module(id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-workbench','menu','menu.workbench','工作台','/workbench/A03','workbench:read',10,1),
('mod-menu-products','menu','menu.products','产品与工序','/products/B01','product:read',20,1),
('mod-product-create','button','button.product.create','新增产品',NULL,'product:write',21,1),
('mod-product-edit','button','button.product.edit','编辑产品',NULL,'product:write',22,1),
('mod-menu-orders','menu','menu.orders','项目与工单','/orders/C03','work_order:read',30,1),
('mod-order-release','button','button.order.release','工单下发',NULL,'work_order:release',31,1),
('mod-menu-tasks','menu','menu.tasks','任务与派工','/tasks/D01','task:read',40,1),
('mod-task-assign','button','button.task.assign','多人派工',NULL,'task:assign',41,1),
('mod-report-create','button','button.report.create','提交报工',NULL,'report:create',42,1),
('mod-menu-reports','menu','menu.reports','报工与审批','/reports/D06','report:read',50,1),
('mod-review-approve','button','button.review.approve','审批报工',NULL,'review:approve',51,1),
('mod-menu-quality','menu','menu.quality','质检与库存','/quality/E01','work_order:read',60,1),
('mod-menu-progress','menu','menu.progress','进度与统计','/progress/F01','progress:read',70,1),
('mod-menu-payroll','menu','menu.payroll','工资单管理','/payroll/F09','payroll:read',80,1),
('mod-payroll-pay','button','button.payroll.pay','工资付款',NULL,'payroll:pay',81,1),
('mod-menu-identity','menu','menu.identity','用户角色权限','/identity/G05','masterdata:read',90,1),
('mod-identity-write','button','button.identity.write','配置用户角色权限',NULL,'masterdata:write',91,1),
('mod-menu-settings','menu','menu.settings','功能设置','/settings/G01','settings:read',100,1),
('mod-settings-write','button','button.settings.write','修改功能设置',NULL,'settings:write',101,1)
ON CONFLICT(code) DO NOTHING;
