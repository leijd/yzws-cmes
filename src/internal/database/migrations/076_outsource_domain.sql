-- 外协管理域（委外加工，设计 4.25）：外协厂商档案、跨租户协作授权、委外单/明细、
-- 委外物料流水、进度投影、委外结算；work_order 扩展受托来源标记。
-- 建模原则：业务单据单一租户归属，跨租户关系仅经 tenant_collaboration 显式授权，
-- 对方可见数据只走白名单投影（不含工价/成本/人员）。
-- 幂等：全部 IF NOT EXISTS / ON CONFLICT DO NOTHING，可重复执行。

/* ========== 表结构 ========== */

-- 跨租户协作授权表（平台级链接表，无 tenant_id；两侧租户 ID 为普通引用键）
CREATE TABLE IF NOT EXISTS tenant_collaboration (
  id TEXT PRIMARY KEY,
  consignor_tenant_id TEXT NOT NULL REFERENCES tenant(id),
  contractor_tenant_id TEXT NOT NULL REFERENCES tenant(id),
  partner_id TEXT,
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','active','suspended','terminated')),
  proposed_tenant_id TEXT NOT NULL,
  proposed_by TEXT NOT NULL, proposed_at BIGINT NOT NULL,
  confirmed_by TEXT, confirmed_at BIGINT,
  effective_at BIGINT, expires_at BIGINT,
  scope_note TEXT, terminated_reason TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  CHECK(consignor_tenant_id<>contractor_tenant_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_collab_pair_active ON tenant_collaboration(consignor_tenant_id,contractor_tenant_id)
  WHERE status IN ('pending','active','suspended');
CREATE INDEX IF NOT EXISTS ix_collab_consignor ON tenant_collaboration(consignor_tenant_id,status);
CREATE INDEX IF NOT EXISTS ix_collab_contractor ON tenant_collaboration(contractor_tenant_id,status);

-- 外协厂商档案表（partner_tenant_id 为空表示非系统租户的外部厂商）
CREATE TABLE IF NOT EXISTS outsourcing_partner (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  code TEXT NOT NULL, name TEXT NOT NULL,
  partner_tenant_id TEXT REFERENCES tenant(id),
  contact_name TEXT, contact_phone TEXT, address TEXT,
  capable_segments JSONB, qualification_note TEXT,
  quality_rating NUMERIC(5,2),
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','disabled')),
  remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_outpartner_tenant ON outsourcing_partner(tenant_id,partner_tenant_id)
  WHERE partner_tenant_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS ix_outpartner_name ON outsourcing_partner(tenant_id,name);

-- 委外单主表（归属委托方租户）
CREATE TABLE IF NOT EXISTS outsourcing_order (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  order_no TEXT NOT NULL,
  partner_id TEXT NOT NULL,
  collaboration_id TEXT,
  project_id TEXT, work_order_id TEXT,
  scope_type TEXT NOT NULL CHECK(scope_type IN ('order','style','processes')),
  pricing_type TEXT NOT NULL CHECK(pricing_type IN ('piece','lump')),
  total_qty NUMERIC(20,6) NOT NULL CHECK(total_qty>0),
  agreed_due_date TEXT,
  status TEXT NOT NULL DEFAULT 'draft'
    CHECK(status IN ('draft','issued','in_transit','processing','partially_returned','completed','settled','cancelled')),
  issued_at BIGINT, closed_at BIGINT, remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,order_no),
  FOREIGN KEY(tenant_id,partner_id) REFERENCES outsourcing_partner(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS ix_outorder_partner ON outsourcing_order(tenant_id,partner_id,status);
CREATE INDEX IF NOT EXISTS ix_outorder_wo ON outsourcing_order(tenant_id,work_order_id);

-- 委外明细表（数量守恒：returned+rework+defect ≤ qty）
CREATE TABLE IF NOT EXISTS outsourcing_order_item (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  outsourcing_order_id TEXT NOT NULL,
  process_id TEXT, segment TEXT, bundle_range TEXT,
  qty NUMERIC(20,6) NOT NULL CHECK(qty>0),
  unit_price NUMERIC(20,6),
  returned_qty NUMERIC(20,6) NOT NULL DEFAULT 0,
  rework_qty NUMERIC(20,6) NOT NULL DEFAULT 0,
  defect_qty NUMERIC(20,6) NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id),
  CHECK(returned_qty+rework_qty+defect_qty<=qty),
  FOREIGN KEY(tenant_id,outsourcing_order_id) REFERENCES outsourcing_order(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS ix_outitem_order ON outsourcing_order_item(tenant_id,outsourcing_order_id);

-- 委外物料流水表（客供料：所有权属委托方，保管权在外协方；双方各自记账按 linked_ref 对账）
CREATE TABLE IF NOT EXISTS outsourcing_material_flow (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  outsourcing_order_id TEXT NOT NULL,
  item_id TEXT,
  flow_type TEXT NOT NULL CHECK(flow_type IN ('issue','return')),
  material_id TEXT, material_name TEXT NOT NULL,
  qty NUMERIC(20,6) NOT NULL CHECK(qty>0),
  from_warehouse_id TEXT, to_warehouse_id TEXT,
  flow_date TEXT NOT NULL, voucher_no TEXT, linked_ref TEXT,
  status TEXT NOT NULL DEFAULT 'posted' CHECK(status IN ('posted','cancelled')),
  remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id),
  FOREIGN KEY(tenant_id,outsourcing_order_id) REFERENCES outsourcing_order(tenant_id,id),
  FOREIGN KEY(tenant_id,item_id) REFERENCES outsourcing_order_item(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS ix_outflow_order ON outsourcing_material_flow(tenant_id,outsourcing_order_id,flow_type);

-- 委外进度投影表（委托方侧只读副本，非业务事实源）
CREATE TABLE IF NOT EXISTS outsourcing_progress_projection (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  outsourcing_order_id TEXT NOT NULL,
  contractor_tenant_id TEXT,
  reported_at BIGINT NOT NULL,
  completed_qty NUMERIC(20,6) NOT NULL DEFAULT 0,
  in_process_qty NUMERIC(20,6) NOT NULL DEFAULT 0,
  quality_pass_qty NUMERIC(20,6) NOT NULL DEFAULT 0,
  stage_note TEXT, snapshot JSONB, source_ref TEXT, reported_by TEXT,
  created_at BIGINT NOT NULL, updated_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id),
  FOREIGN KEY(tenant_id,outsourcing_order_id) REFERENCES outsourcing_order(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS ix_outproj_order ON outsourcing_progress_projection(tenant_id,outsourcing_order_id,reported_at);

-- 委外结算单（direction=payable 本厂委托应付 / receivable 本厂受托应收）
CREATE TABLE IF NOT EXISTS outsourcing_settlement (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  settlement_no TEXT NOT NULL,
  outsourcing_order_id TEXT, work_order_id TEXT,
  partner_id TEXT, direction TEXT NOT NULL DEFAULT 'payable' CHECK(direction IN ('payable','receivable')),
  period_start TEXT, period_end TEXT,
  qualified_qty NUMERIC(20,6) NOT NULL DEFAULT 0,
  unit_price NUMERIC(20,6) NOT NULL DEFAULT 0 CHECK(unit_price>=0),
  amount NUMERIC(20,6) NOT NULL DEFAULT 0 CHECK(amount>=0),
  deduction_amount NUMERIC(20,6) NOT NULL DEFAULT 0 CHECK(deduction_amount>=0),
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','confirmed','invoiced','paid')),
  confirmed_by TEXT, confirmed_at BIGINT, paid_at BIGINT, remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,settlement_no)
);
CREATE INDEX IF NOT EXISTS ix_outsettle_order ON outsourcing_settlement(tenant_id,outsourcing_order_id);

-- 受托侧：work_order 扩展来源标记（order_source=contracted 时本厂为受托方）
ALTER TABLE work_order ADD COLUMN IF NOT EXISTS order_source TEXT NOT NULL DEFAULT 'own';
ALTER TABLE work_order ADD COLUMN IF NOT EXISTS source_collaboration_id TEXT;
ALTER TABLE work_order ADD COLUMN IF NOT EXISTS source_order_ref TEXT;
CREATE INDEX IF NOT EXISTS ix_wo_source ON work_order(tenant_id,order_source,source_order_ref);

/* ========== 权限点注册（与 071 口径一致） ========== */

INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('outsource:read','外协管理','查看外协','read','浏览外协厂商、协作关系、委外单与进度',FALSE,110),
('outsource:write','外协管理','维护外协','write','维护厂商档案、发起/确认协作、委外单发出与回货、进度填报',FALSE,111),
('outsource:settle','外协管理','委外结算','write','创建、确认、登记委外结算与收付款',FALSE,112)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-outsource',NULL,'menu','menu.outsource','外协管理','/outsource/orders','outsource:read',110,1),
('mod-outsource-write','mod-menu-outsource','button','button.outsource.write','维护外协业务',NULL,'outsource:write',111,1),
('mod-outsource-settle','mod-menu-outsource','button','button.outsource.settle','委外结算',NULL,'outsource:settle',112,1)
ON CONFLICT(code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||substr(md5(p.code),1,8),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN platform_permission p
WHERE p.code IN ('outsource:read','outsource:write','outsource:settle')
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||substr(md5(p.id),1,8),r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id
WHERE r.code='tenant_admin'
  AND p.code IN ('outsource:read','outsource:write','outsource:settle')
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-fm-os1','prt-factory-manager','outsource:read',1),
('prtp-fm-os2','prt-factory-manager','outsource:write',1),
('prtp-fm-os3','prt-factory-manager','outsource:settle',1),
('prtp-ps-os1','prt-production-supervisor','outsource:read',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;
