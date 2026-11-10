-- 过程巡检与巡检返工：巡检按工单工序+扎记录，返工单可来源于总检或巡检
CREATE TABLE process_inspection (
  tenant_id TEXT NOT NULL REFERENCES tenant(id),
  id TEXT NOT NULL,
  inspection_no TEXT NOT NULL,
  work_order_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  product_id TEXT NOT NULL,
  process_snapshot_id TEXT NOT NULL,
  process_code TEXT NOT NULL,
  process_name TEXT NOT NULL,
  bundle_id TEXT NOT NULL,
  inspected_qty BIGINT NOT NULL CHECK(inspected_qty>0),
  passed_qty BIGINT NOT NULL DEFAULT 0 CHECK(passed_qty>=0),
  rework_qty BIGINT NOT NULL DEFAULT 0 CHECK(rework_qty>=0),
  scrapped_qty BIGINT NOT NULL DEFAULT 0 CHECK(scrapped_qty>=0),
  inspector_employee_id TEXT NOT NULL,
  inspection_date BIGINT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','confirmed')),
  remark TEXT,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id),
  UNIQUE(tenant_id,inspection_no),
  CHECK(inspected_qty=passed_qty+rework_qty+scrapped_qty),
  FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id),
  FOREIGN KEY(tenant_id,project_id) REFERENCES project(tenant_id,id),
  FOREIGN KEY(tenant_id,product_id) REFERENCES product(tenant_id,id),
  FOREIGN KEY(tenant_id,bundle_id) REFERENCES bundle(tenant_id,id)
);
COMMENT ON TABLE process_inspection IS '过程巡检：按工单、工序快照和扎记录巡检结果';
CREATE INDEX ix_process_inspection_wo ON process_inspection(tenant_id,work_order_id,process_snapshot_id,status,id);

ALTER TABLE rework_order ADD COLUMN source_type TEXT NOT NULL DEFAULT 'final_inspection' CHECK(source_type IN ('final_inspection','process_inspection'));
ALTER TABLE rework_order ADD COLUMN process_inspection_id TEXT;
ALTER TABLE rework_order ADD COLUMN work_order_id TEXT;
ALTER TABLE rework_order ADD COLUMN bundle_id TEXT;
ALTER TABLE rework_order ADD COLUMN process_snapshot_id TEXT;
ALTER TABLE rework_order ADD COLUMN process_code TEXT;
ALTER TABLE rework_order ADD COLUMN process_name TEXT;
ALTER TABLE rework_order ADD COLUMN reported_qty BIGINT NOT NULL DEFAULT 0;
ALTER TABLE rework_order ADD COLUMN reported_at BIGINT;
ALTER TABLE rework_order ADD COLUMN reported_by TEXT;
ALTER TABLE rework_order ALTER COLUMN inspection_id DROP NOT NULL;
COMMENT ON COLUMN rework_order.source_type IS '返工来源：final_inspection 总检；process_inspection 过程巡检';
COMMENT ON COLUMN rework_order.reported_qty IS '返工报工数量；复检前由返工人员提交';
