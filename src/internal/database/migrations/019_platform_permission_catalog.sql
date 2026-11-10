CREATE TABLE platform_permission (
  code TEXT PRIMARY KEY,
  module TEXT NOT NULL,
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK(kind IN('read','write','action')),
  description TEXT,
  admin_only BOOLEAN NOT NULL DEFAULT FALSE,
  sort_no INTEGER NOT NULL DEFAULT 0
);
COMMENT ON TABLE platform_permission IS '平台统一权限点目录：每项业务功能的读、写、操作权限，中文名称供租户角色分配界面展示';

INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('workbench:read','工作台','查看工作台','read','进入生产工作台，查看待办与进度',FALSE,10),
('product:read','产品与工序','查看产品与工序','read','浏览产品资料、工序路线与产品图片',FALSE,20),
('product:write','产品与工序','维护产品与工序','write','新建、编辑、隐藏、删除产品并配置产品工序路线',FALSE,21),
('attachment:write','产品与工序','上传产品图片','write','上传和管理产品图片附件',FALSE,22),
('process:read','工序库','查看工序库','read','浏览工序定义',FALSE,25),
('process:write','工序库','维护工序库','write','新建、编辑、删除工序定义',FALSE,26),
('work_order:read','项目与工单','查看项目与工单','read','浏览客户项目、工单与生产进度',FALSE,30),
('work_order:write','项目与工单','维护工单','write','新建工单，编辑和操作工单',FALSE,31),
('work_order:release','项目与工单','工单下发','action','下发工单，开始生产',FALSE,32),
('project:read','项目与工单','查看客户项目','read','浏览客户项目',FALSE,33),
('project:write','项目与工单','维护客户项目','write','新建、编辑客户项目',FALSE,34),
('cutting:read','裁床与分扎','查看裁床与分扎','read','浏览裁床单与分扎信息',FALSE,40),
('cutting:write','裁床与分扎','维护裁床','write','新建裁床单并确认裁床结果',FALSE,41),
('task:read','任务与派工','查看任务与派工','read','浏览生产任务',FALSE,50),
('task:assign','任务与派工','任务派工','action','派工，调整任务人员与数量',FALSE,51),
('task:transfer','任务与派工','任务转工','action','把任务转交给其他人员',FALSE,52),
('report:read','报工与审批','查看报工与审批','read','浏览报工记录与审批状态',FALSE,60),
('report:create','报工与审批','提交报工','write','提交和撤回报工',FALSE,61),
('review:approve','报工与审批','审批报工','action','通过或驳回报工',FALSE,62),
('review:correct','报工与审批','纠错审批','action','修改报工数量、工价并审批',FALSE,63),
('report:reverse','报工与审批','冲销报工','action','冲销已审批的报工',FALSE,64),
('progress:read','进度与统计','查看进度与统计','read','浏览生产进度与统计',FALSE,70),
('statistics:read','进度与统计','查看统计矩阵','read','查看多维统计矩阵',FALSE,71),
('payroll:read','工资单管理','查看工资单','read','浏览工资单',FALSE,80),
('payroll:confirm','工资单管理','确认工资','action','生成工资草稿并确认工资单',FALSE,81),
('payroll:pay','工资单管理','工资付款','action','登记付款流水',FALSE,82),
('payroll:export','工资单管理','导出工资','action','导出工资数据',FALSE,83),
('masterdata:read','员工与客户','查看员工与客户','read','浏览员工、客户与仓库资料',FALSE,90),
('masterdata:write','员工与客户','维护基础资料','write','新建、编辑员工、客户和仓库',FALSE,91),
('identity:read','用户角色权限','查看用户角色权限','read','浏览用户、角色与权限',TRUE,100),
('identity:write','用户角色权限','管理用户角色权限','write','新建用户和角色，分配功能权限',TRUE,101),
('settings:read','功能设置','查看功能设置','read','浏览系统参数',FALSE,110),
('settings:write','功能设置','修改功能设置','write','修改报工规则等功能设置',FALSE,111)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-attachment-write','mod-menu-products','button','button.attachment.write','上传产品图片',NULL,'attachment:write',25,1)
ON CONFLICT(code) DO NOTHING;
