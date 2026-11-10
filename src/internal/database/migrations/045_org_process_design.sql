-- v0.0.4.2：厂内业务域设计落地（docx 第 6/8 章）
-- 范围：org_unit 业务属性扩展、生产线资源配置（line_resource）、管理级别（mgmt_level）、
--       用户资料模板（user_profile_template）、生产标准工序表四表（process_standard*）。
-- 预留：capacity_profile / ai_profile 本期不建表（docx 第 10 章），仅锁定设计口径。
-- 约定：主键 TEXT(UUIDv7)；时间 BIGINT(UTC 微秒)；金额/工时 NUMERIC(20,6)；
--       全部表含 tenant_id 隔离键；外键一律 (tenant_id, 从表ID)→(tenant_id, 主表ID) 复合外键。

-- ===== 6.1 org_unit 车间/产线/班组组织表（扩展）=====
ALTER TABLE org_unit ADD COLUMN workshop_nature TEXT NOT NULL DEFAULT 'own';
ALTER TABLE org_unit ADD COLUMN outsourced_partner TEXT;
ALTER TABLE org_unit ADD COLUMN line_nature TEXT NOT NULL DEFAULT 'fixed';
ALTER TABLE org_unit ADD COLUMN established_at BIGINT;
ALTER TABLE org_unit ADD COLUMN capacity_note TEXT;
COMMENT ON COLUMN org_unit.workshop_nature IS '仅 workshop 有效：own 自有车间 / outsourced 外包车间';
COMMENT ON COLUMN org_unit.outsourced_partner IS '外包方名称（外包车间必填）';
COMMENT ON COLUMN org_unit.line_nature IS '仅 line 有效：fixed 固定产线 / dynamic 动态产线（小微厂按订单/工序表动态配置）';
COMMENT ON COLUMN org_unit.established_at IS '车间/产线设立时间（UTC 微秒）';
COMMENT ON COLUMN org_unit.capacity_note IS '产能说明（班组人数、班次等简述；正式产能评估见预留表 capacity_profile）';

-- ===== 6.2 line_resource 生产线资源配置表 =====
CREATE TABLE line_resource (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  line_org_id TEXT NOT NULL,
  config_type TEXT NOT NULL DEFAULT 'fixed' CHECK(config_type IN ('fixed','dynamic')),
  work_order_id TEXT,
  manager_employee_id TEXT,
  member_employee_ids JSONB NOT NULL DEFAULT '[]',
  equipment_ids JSONB NOT NULL DEFAULT '[]',
  process_standard_id TEXT,
  quality_ref JSONB NOT NULL DEFAULT '{}',
  delivery_ref JSONB NOT NULL DEFAULT '{}',
  valid_from BIGINT,
  valid_to BIGINT,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','expired')),
  remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id),
  FOREIGN KEY(tenant_id,line_org_id) REFERENCES org_unit(tenant_id,id)
);
CREATE INDEX ix_line_resource_line ON line_resource(tenant_id,line_org_id,status);
CREATE INDEX ix_line_resource_order ON line_resource(tenant_id,work_order_id);
COMMENT ON TABLE line_resource IS '生产线资源配置表：一条线对应的人员/设备/生产技术；小微厂动态配置（一单一配）';
COMMENT ON COLUMN line_resource.line_org_id IS '所属生产线（org_type=line，必须同厂）';
COMMENT ON COLUMN line_resource.config_type IS 'fixed 固定配置 / dynamic 按订单动态配置（work_order_id 必填）';
COMMENT ON COLUMN line_resource.manager_employee_id IS '管理人员（线长/组长）';
COMMENT ON COLUMN line_resource.member_employee_ids IS '生产人员清单（employee ID 数组，含线外工、后整人员归属）';
COMMENT ON COLUMN line_resource.equipment_ids IS '设备清单（编号数组；设备台账随设备管理模块另行建表）';
COMMENT ON COLUMN line_resource.process_standard_id IS '生产技术·工序表：本配置使用的生产标准工序表';
COMMENT ON COLUMN line_resource.quality_ref IS '生产技术·质量表：质检/抽查记录查询条件快照';
COMMENT ON COLUMN line_resource.delivery_ref IS '生产技术·货期完成情况表：计划 vs 实际货期数据来源快照';
COMMENT ON COLUMN line_resource.valid_from IS '配置生效区间起；dynamic 配置随订单完工失效';

-- ===== 6.3 mgmt_level 管理级别表 =====
CREATE TABLE mgmt_level (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  level_code TEXT NOT NULL,
  level_name TEXT NOT NULL,
  description TEXT,
  sort_no INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,level_code)
);
COMMENT ON TABLE mgmt_level IS '管理级别表：角色之外的纵向层级（L1 厂级→L4 个人级），决定"能看多广"；角色决定"能做什么"';
COMMENT ON COLUMN mgmt_level.level_code IS 'L1_FACTORY 厂级 / L2_WORKSHOP 车间级 / L3_LINE 产线级 / L4_SELF 个人级';

-- 存量制衣厂预置标准四级（id 确定性生成，保证幂等）
INSERT INTO mgmt_level(tenant_id,id,level_code,level_name,description,sort_no,status,created_at,created_by,created_node,updated_at,updated_by,updated_node)
SELECT t.id, 'mlvl-'||t.id||'-1','L1_FACTORY','厂级','全厂数据范围',1,'active',
       (EXTRACT(EPOCH FROM now())*1000000)::BIGINT,'system',t.id,
       (EXTRACT(EPOCH FROM now())*1000000)::BIGINT,'system',t.id FROM tenant t
ON CONFLICT (tenant_id,level_code) DO NOTHING;
INSERT INTO mgmt_level(tenant_id,id,level_code,level_name,description,sort_no,status,created_at,created_by,created_node,updated_at,updated_by,updated_node)
SELECT t.id, 'mlvl-'||t.id||'-2','L2_WORKSHOP','车间级','本车间数据范围',2,'active',
       (EXTRACT(EPOCH FROM now())*1000000)::BIGINT,'system',t.id,
       (EXTRACT(EPOCH FROM now())*1000000)::BIGINT,'system',t.id FROM tenant t
ON CONFLICT (tenant_id,level_code) DO NOTHING;
INSERT INTO mgmt_level(tenant_id,id,level_code,level_name,description,sort_no,status,created_at,created_by,created_node,updated_at,updated_by,updated_node)
SELECT t.id, 'mlvl-'||t.id||'-3','L3_LINE','产线级','本产线数据范围',3,'active',
       (EXTRACT(EPOCH FROM now())*1000000)::BIGINT,'system',t.id,
       (EXTRACT(EPOCH FROM now())*1000000)::BIGINT,'system',t.id FROM tenant t
ON CONFLICT (tenant_id,level_code) DO NOTHING;
INSERT INTO mgmt_level(tenant_id,id,level_code,level_name,description,sort_no,status,created_at,created_by,created_node,updated_at,updated_by,updated_node)
SELECT t.id, 'mlvl-'||t.id||'-4','L4_SELF','个人级','本人数据范围',4,'active',
       (EXTRACT(EPOCH FROM now())*1000000)::BIGINT,'system',t.id,
       (EXTRACT(EPOCH FROM now())*1000000)::BIGINT,'system',t.id FROM tenant t
ON CONFLICT (tenant_id,level_code) DO NOTHING;

-- 用户账号关联管理级别（同厂，可空；空表示仅按角色权限）
ALTER TABLE user_account ADD COLUMN mgmt_level_id TEXT;
ALTER TABLE user_account ADD CONSTRAINT fk_user_account_mgmt_level
  FOREIGN KEY(tenant_id,mgmt_level_id) REFERENCES mgmt_level(tenant_id,id);
COMMENT ON COLUMN user_account.mgmt_level_id IS '管理级别（同厂，可空；与角色共同决定数据可见范围，见 mgmt_level 表）';

-- ===== 6.4 user_profile_template 用户资料模板表 =====
CREATE TABLE user_profile_template (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  template_type TEXT NOT NULL CHECK(template_type IN ('standard','custom')),
  name TEXT NOT NULL,
  fields JSONB NOT NULL DEFAULT '[]',
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  applied_count INTEGER NOT NULL DEFAULT 0,
  remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,template_type)
);
COMMENT ON TABLE user_profile_template IS '用户资料模板表：员工信息标准板一次全自动设定，厂管理账户可修改保存为自定义模板';
COMMENT ON COLUMN user_profile_template.fields IS '字段定义数组 [{field_code,field_name,required,encrypted,visible_level,dict_type,sort_no,remark}]；visible_level 引用 mgmt_level';
COMMENT ON COLUMN user_profile_template.is_active IS '是否生效；标准板停用后回退系统内置默认定义';

-- ===== 8.1 process_standard 生产标准工序表主表 =====
CREATE TABLE process_standard (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  sheet_no TEXT NOT NULL,
  product_ref TEXT,
  project_ref TEXT,
  version_no BIGINT NOT NULL DEFAULT 1,
  revision BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','pending_confirm','standard','revising','superseded')),
  created_by TEXT NOT NULL,
  copied_from_id TEXT,
  confirmed_round INTEGER NOT NULL DEFAULT 0,
  effective_at BIGINT,
  superseded_by TEXT,
  remark TEXT,
  created_at BIGINT NOT NULL,
  created_node TEXT NOT NULL DEFAULT 'center',
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,sheet_no),
  FOREIGN KEY(tenant_id,copied_from_id) REFERENCES process_standard(tenant_id,id),
  FOREIGN KEY(tenant_id,superseded_by) REFERENCES process_standard(tenant_id,id)
);
CREATE INDEX ix_sheet_product ON process_standard(tenant_id,product_ref,status);
CREATE INDEX ix_sheet_project ON process_standard(tenant_id,project_ref);
COMMENT ON TABLE process_standard IS '生产标准工序表主表：草稿→待确认→生产标准；修订走 revising 重新两确认；内容为厂技术内核';
COMMENT ON COLUMN process_standard.sheet_no IS '工序表编号：GZB+年月+三位流水（同厂唯一）';
COMMENT ON COLUMN process_standard.created_by IS '制订人（工艺员，user_account.id）';
COMMENT ON COLUMN process_standard.copied_from_id IS '模板来源工序表（允许拿旧款标准表做模板增删改后另存）';
COMMENT ON COLUMN process_standard.confirmed_round IS '已完成确认轮次（每轮两确认：工艺员提交+线长确认）';
COMMENT ON COLUMN process_standard.superseded_by IS '替代本表的新表 ID';

-- ===== 8.2 process_standard_line 生产标准工序表明细 =====
CREATE TABLE process_standard_line (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  sheet_id TEXT NOT NULL,
  seq_no INTEGER NOT NULL,
  process_id TEXT,
  process_code TEXT NOT NULL,
  process_name TEXT NOT NULL,
  category TEXT,
  work_seconds NUMERIC(20,6),
  unit_price NUMERIC(20,6),
  equipment TEXT,
  instruction TEXT,
  remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,sheet_id,seq_no),
  FOREIGN KEY(tenant_id,sheet_id) REFERENCES process_standard(tenant_id,id),
  FOREIGN KEY(tenant_id,process_id) REFERENCES process_definition(tenant_id,id)
);
CREATE INDEX ix_sheet_line_process ON process_standard_line(tenant_id,process_id);
COMMENT ON TABLE process_standard_line IS '生产标准工序表明细：车间流转顺序即 seq_no 顺序；业务字段整行加密存储（技术内核，密文以 enc: 前缀区分）';
COMMENT ON COLUMN process_standard_line.process_id IS '公共工序库工序引用（process_definition，同厂）；编码与名称快照落在本行';
COMMENT ON COLUMN process_standard_line.work_seconds IS '标准工时（秒，加密存储）';
COMMENT ON COLUMN process_standard_line.unit_price IS '工序单价 元/件（加密存储）';
COMMENT ON COLUMN process_standard_line.instruction IS '作业说明/工艺要求（加密存储）';

-- ===== 8.3 process_standard_confirm 工序表两确认记录表 =====
CREATE TABLE process_standard_confirm (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  sheet_id TEXT NOT NULL,
  revision BIGINT NOT NULL,
  round_no INTEGER NOT NULL,
  confirm_seq INTEGER NOT NULL CHECK(confirm_seq IN (1,2)),
  confirmer_id TEXT NOT NULL,
  confirmer_role TEXT NOT NULL CHECK(confirmer_role IN ('process_engineer','group_leader')),
  action TEXT NOT NULL CHECK(action IN ('submit','confirm','reject')),
  opinion TEXT,
  created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id),
  FOREIGN KEY(tenant_id,sheet_id) REFERENCES process_standard(tenant_id,id)
);
CREATE INDEX ix_confirm_sheet ON process_standard_confirm(tenant_id,sheet_id,revision,round_no,confirm_seq);
COMMENT ON TABLE process_standard_confirm IS '工序表两确认记录表：首次制订与每次修改均须一轮两确认（工艺员提交→线长确认）';
COMMENT ON COLUMN process_standard_confirm.round_no IS '确认轮次：1 首次制订，2 起为第 N 次修改';
COMMENT ON COLUMN process_standard_confirm.confirm_seq IS '确认顺序：1 工艺员提交 / 2 线长确认';

-- ===== 8.4 process_standard_grant 工序表查看授权表 =====
CREATE TABLE process_standard_grant (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  sheet_id TEXT NOT NULL,
  grantee_user_id TEXT NOT NULL,
  grantee_role TEXT,
  granted_by TEXT NOT NULL,
  scope TEXT NOT NULL DEFAULT 'view' CHECK(scope IN ('view')),
  valid_from BIGINT,
  valid_to BIGINT,
  reason TEXT,
  revoked_at BIGINT,
  revoked_by TEXT,
  created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id),
  FOREIGN KEY(tenant_id,sheet_id) REFERENCES process_standard(tenant_id,id),
  FOREIGN KEY(tenant_id,grantee_user_id) REFERENCES user_account(tenant_id,id),
  FOREIGN KEY(tenant_id,granted_by) REFERENCES user_account(tenant_id,id)
);
CREATE INDEX ix_grant_sheet ON process_standard_grant(tenant_id,sheet_id,grantee_user_id);
CREATE INDEX ix_grant_user ON process_standard_grant(tenant_id,grantee_user_id);
COMMENT ON TABLE process_standard_grant IS '工序表查看授权表：查看他人指定的工序表须厂长/经理授权（仅只读，按人按表按有效期）';
COMMENT ON COLUMN process_standard_grant.granted_by IS '授权人：仅厂长/经理（factory_head/manager，事务保证）';
