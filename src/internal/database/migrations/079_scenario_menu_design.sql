-- 业务场景分类与场景化菜单（设计 v0.02-002：4.19 场景字段、4.19.1 裁床单两级录入、6.12 菜单与角色入口）
-- 目标：project/work_order 增加场景维度；裁床单采用「主表 → 床次行 → 扎号明细」两级录入；
--       草稿与正式记录分离（每分钟自动保存只写草稿，正式保存才落业务明细并可发布到工单）。
-- 不改动既有 cutting_order/cutting_detail/bundle/ticket 链路，新表为并行目标结构，发布时再同步到既有链路。
-- 幂等：全部 IF NOT EXISTS / ADD COLUMN IF NOT EXISTS / ON CONFLICT DO NOTHING。

/* ========== 1. 业务场景字段（4.19） ========== */

ALTER TABLE project ADD COLUMN IF NOT EXISTS scene_type TEXT NOT NULL DEFAULT 'A';
ALTER TABLE project ADD COLUMN IF NOT EXISTS scene_sub TEXT NOT NULL DEFAULT 'A1';
ALTER TABLE work_order ADD COLUMN IF NOT EXISTS scene_type TEXT NOT NULL DEFAULT 'A';
ALTER TABLE work_order ADD COLUMN IF NOT EXISTS scene_sub TEXT NOT NULL DEFAULT 'A1';

-- scene 是业务视角分类，order_source 是数据归属标记（A→own，B/C→contracted），二者并存
DO $$ BEGIN
  ALTER TABLE project ADD CONSTRAINT ck_project_scene_type CHECK(scene_type IN ('A','B','C'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
  ALTER TABLE project ADD CONSTRAINT ck_project_scene_sub CHECK(scene_sub IN ('A1','A2','A3','B1','B2','C1'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
  ALTER TABLE work_order ADD CONSTRAINT ck_wo_scene_type CHECK(scene_type IN ('A','B','C'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
DO $$ BEGIN
  ALTER TABLE work_order ADD CONSTRAINT ck_wo_scene_sub CHECK(scene_sub IN ('A1','A2','A3','B1','B2','C1'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

CREATE INDEX IF NOT EXISTS ix_project_scene ON project(tenant_id,scene_type,scene_sub);
CREATE INDEX IF NOT EXISTS ix_wo_scene ON work_order(tenant_id,scene_type,scene_sub);

-- C 接外包工序允许无产品信息（6.12.7 允许无记录）；放宽 NOT NULL，外键在 NULL 时不生效
ALTER TABLE project ALTER COLUMN product_id DROP NOT NULL;
ALTER TABLE work_order ALTER COLUMN product_id DROP NOT NULL;

/* ========== 2. 裁床单两级录入（4.19.1） ========== */

-- 2.1 裁床单主表：一单一号，可含多个床次行
CREATE TABLE IF NOT EXISTS cutting_sheet (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  cutting_no TEXT NOT NULL,
  scene_type TEXT NOT NULL DEFAULT 'A' CHECK(scene_type IN ('A','B','C')),
  scene_sub TEXT NOT NULL DEFAULT 'A1' CHECK(scene_sub IN ('A1','A2','A3','B1','B2','C1')),
  customer_id TEXT,
  cutting_date BIGINT NOT NULL,
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','saved','published','cancelled')),
  remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,cutting_no)
);
CREATE INDEX IF NOT EXISTS ix_csheet_scene ON cutting_sheet(tenant_id,scene_type,status);

-- 2.2 床次行（上级表格行）：唛架允许空；工单/项目可空以支持「先录裁床资料、后建工单」的流程顺序
CREATE TABLE IF NOT EXISTS cutting_bed (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  sheet_id TEXT NOT NULL,
  work_order_id TEXT, project_id TEXT,
  style_no TEXT NOT NULL, style_name TEXT,
  customer_id TEXT,
  color TEXT NOT NULL,
  bed_no TEXT NOT NULL,
  marker_no TEXT,                       -- 非必填，允许空（4.19.1）
  fabric_layers BIGINT NOT NULL DEFAULT 0,
  cut_qty BIGINT NOT NULL DEFAULT 0 CHECK(cut_qty>=0),
  bundle_range TEXT,                    -- 下级明细汇总回写，如 1~24 / 1~10,12
  size_ratio JSONB NOT NULL DEFAULT '[]',  -- 码数明细汇总回写，如 [{"size":"S","qty":24}]
  cutter_id TEXT, reviewer_id TEXT,
  remark TEXT,
  sort_no INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','saved','published','locked')),
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id),
  FOREIGN KEY(tenant_id,sheet_id) REFERENCES cutting_sheet(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS ix_cbed_sheet ON cutting_bed(tenant_id,sheet_id,sort_no);
CREATE INDEX IF NOT EXISTS ix_cbed_wo ON cutting_bed(tenant_id,work_order_id);

-- 2.3 扎号明细（下级表格行，正式记录）：来自上级的固定字段 + 可编辑字段 + 动态部位列
CREATE TABLE IF NOT EXISTS cutting_bundle_detail (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  bed_id TEXT NOT NULL,
  work_order_no TEXT,                   -- 来自上级，内容固定
  style_no TEXT,                        -- 来自上级，内容固定
  color TEXT NOT NULL,                  -- 来自上级，内容固定
  bed_no TEXT NOT NULL,                 -- 来自上级，内容固定
  size TEXT NOT NULL,
  bundle_no TEXT NOT NULL,
  piece_qty BIGINT NOT NULL CHECK(piece_qty>0),
  parts JSONB NOT NULL DEFAULT '[]',    -- 部位 1~N 动态列，按部位模板配置
  sort_no INTEGER NOT NULL DEFAULT 1,
  locked BOOLEAN NOT NULL DEFAULT FALSE,  -- 已报工/质检/入库后锁定（CUTTING_BUNDLE_LOCKED）
  bundle_id TEXT,                       -- 发布后关联既有 bundle
  ticket_id TEXT,                       -- 发布后关联既有 ticket
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id),
  UNIQUE(tenant_id,bed_id,bundle_no),
  FOREIGN KEY(tenant_id,bed_id) REFERENCES cutting_bed(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS ix_cbdetail_bed ON cutting_bundle_detail(tenant_id,bed_id,sort_no);

-- 2.4 自动保存草稿：每床次行一份，仅编辑态数据，不产生进度/菲票/业务事实
CREATE TABLE IF NOT EXISTS cutting_bed_draft (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  bed_id TEXT NOT NULL,
  payload JSONB NOT NULL,               -- 下级明细行数组 + 汇总草稿（bundle_range/size_ratio）
  saved_at BIGINT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,bed_id),
  FOREIGN KEY(tenant_id,bed_id) REFERENCES cutting_bed(tenant_id,id)
);

/* ========== 2.5 直播取证会话（6.12.8） ========== */

-- 检验/验收环节发起的实时直播（固定摄像头流或手机实时画面），授权人员远程观看；
-- 直播结束时可按时段同步封存片段为 evidence_clip（复用 4.26 哈希与封存机制）。
CREATE TABLE IF NOT EXISTS evidence_live_session (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  biz_type TEXT NOT NULL,                  -- line_inspection/final_inspection/report_review/finished_receipt/shipment/outsource_return
  biz_id TEXT NOT NULL,
  scene_type TEXT, scene_sub TEXT,
  source_type TEXT NOT NULL DEFAULT 'camera' CHECK(source_type IN ('camera','mobile')),
  camera_id TEXT,
  started_by TEXT NOT NULL, started_at BIGINT NOT NULL,
  ended_at BIGINT,
  status TEXT NOT NULL DEFAULT 'live' CHECK(status IN ('live','ended','sealed')),
  watchers JSONB NOT NULL DEFAULT '[]',    -- 经 5.7 数据范围过滤的授权观看人
  evidence_clip_id TEXT,                   -- 封存后关联的证据片段
  live_ticket TEXT,                        -- 短时播放票据引用（不含凭据本体）
  remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS ix_evidence_live_biz ON evidence_live_session(tenant_id,biz_type,biz_id,status);

/* ========== 3. 权限点注册（5.17、6.12） ========== */

-- 裁床两级录入复用既有 cutting:read / cutting:write；新增发货、员工薪水分析、场景向导权限点
INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('shipment:read','发货','查看发货','read','浏览发货单与回交记录',FALSE,120),
('shipment:write','发货','维护发货','write','登记发货/回交与作废',FALSE,121),
('salary:read','员工薪水分析','查看员工薪水分析','read','按数据范围查看薪水情况与工作量情况',FALSE,122),
('intake:write','业务受理','发起场景向导','write','按业务场景新建项目与工单向导',FALSE,123)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-cutting-sheet',NULL,'menu','menu.cutting.sheet','裁床单两级录入','/cutting/sheets','cutting:read',120,1),
('mod-cutting-sheet-write','mod-menu-cutting-sheet','button','button.cutting.sheet.write','维护裁床单',NULL,'cutting:write',121,1),
('mod-menu-intake',NULL,'menu','menu.intake','新建项目向导','/intake/new','intake:write',122,1),
('mod-menu-shipment',NULL,'menu','menu.shipment','发货','/shipments/E04','shipment:read',123,1),
('mod-shipment-write','mod-menu-shipment','button','button.shipment.write','登记发货',NULL,'shipment:write',124,1),
('mod-menu-salary',NULL,'menu','menu.salary','员工薪水分析','/analysis/salary','salary:read',125,1),
('mod-menu-process-standard',NULL,'menu','menu.process.standard','标准工序','/process-standards','process:read',126,1)
ON CONFLICT(code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||substr(md5(p.code),1,8),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN platform_permission p
WHERE p.code IN ('shipment:read','shipment:write','salary:read','intake:write')
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||substr(md5(p.id),1,8),r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id
WHERE r.code='tenant_admin'
  AND p.code IN ('shipment:read','shipment:write','salary:read','intake:write')
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-fm-sc1','prt-factory-manager','shipment:read',1),
('prtp-fm-sc2','prt-factory-manager','shipment:write',1),
('prtp-fm-sc3','prt-factory-manager','salary:read',1),
('prtp-fm-sc4','prt-factory-manager','intake:write',1),
('prtp-ps-sc1','prt-production-supervisor','intake:write',1),
('prtp-wh-sc1','prt-warehouse','shipment:read',1),
('prtp-wh-sc2','prt-warehouse','shipment:write',1),
('prtp-pf-sc1','prt-payroll-finance','salary:read',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;

-- 存量租户按角色模板回填权限（参照 023_factory_manager_payroll_read.sql）：
-- 新租户由 tenant seed 依模板落权，但存量租户的 role_permission 不会自动跟随模板更新，
-- 否则新菜单对已有的厂长/生产主管/仓管/工资财务角色不可见。
INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-sc-'||substr(md5(p.code),1,8),r.id,p.id
FROM role r
JOIN platform_role_template tpl ON tpl.code=r.code
JOIN platform_role_template_permission tpp ON tpp.template_id=tpl.id
JOIN permission p ON p.tenant_id=r.tenant_id AND p.code=tpp.permission_code
WHERE r.source='platform'
  AND tpp.permission_code IN ('shipment:read','shipment:write','salary:read','intake:write')
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

-- 裁床两级录入需「cutting:read + 打印权限」，而裁床负责人模板仅有 cutting:read/write，
-- 未随 029 的 work_order:read 回填 print:read。此处按 cutting:read 补打印权限，避免误挡。
INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-'||template_id||'-sc-print', template_id, 'print:read', 1
FROM platform_role_template_permission WHERE permission_code='cutting:read'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT rp.tenant_id,'rp-'||rp.role_id||'-sc-print',rp.role_id,np.id
FROM role_permission rp
JOIN permission op ON op.tenant_id=rp.tenant_id AND op.id=rp.permission_id AND op.code='cutting:read'
JOIN permission np ON np.tenant_id=rp.tenant_id AND np.code='print:read'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

-- 6.12.8：线检/总检/审核报工/入库/发货等环节内嵌取证入口，
-- 077 只给厂长与仓管模板授了证据权限，此处补生产主管、组长、裁床负责人、工资财务。
INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-ps-ev1','prt-production-supervisor','video:view',1),
('prtp-ps-ev2','prt-production-supervisor','evidence:seal',1),
('prtp-gl-ev1','prt-group-leader','video:view',1),
('prtp-gl-ev2','prt-group-leader','evidence:seal',1),
('prtp-cl-ev1','prt-cutting-lead','video:view',1),
('prtp-cl-ev2','prt-cutting-lead','evidence:seal',1),
('prtp-pf-ev1','prt-payroll-finance','video:view',1),
('prtp-pf-ev2','prt-payroll-finance','evidence:seal',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-sc-'||substr(md5(p.code),1,8),r.id,p.id
FROM role r
JOIN platform_role_template tpl ON tpl.code=r.code
JOIN platform_role_template_permission tpp ON tpp.template_id=tpl.id
JOIN permission p ON p.tenant_id=r.tenant_id AND p.code=tpp.permission_code
WHERE r.source='platform'
  AND tpp.permission_code IN ('video:view','evidence:seal')
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
