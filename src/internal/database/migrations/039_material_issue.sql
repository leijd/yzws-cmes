-- 生产领料：把物料库存与生产工单显式关联，领料时写负向库存流水
CREATE TABLE material_issue (
  tenant_id TEXT NOT NULL REFERENCES tenant(id),
  id TEXT NOT NULL,
  issue_no TEXT NOT NULL,
  issue_date TEXT NOT NULL,
  work_order_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  warehouse_id TEXT NOT NULL,
  material_id TEXT NOT NULL,
  qty NUMERIC(20,6) NOT NULL CHECK(qty > 0),
  unit TEXT NOT NULL,
  purpose TEXT,
  operator_employee_id TEXT,
  status TEXT NOT NULL DEFAULT 'posted' CHECK(status IN ('posted','reversed')),
  remark TEXT,
  version BIGINT NOT NULL DEFAULT 1,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL,
  PRIMARY KEY(tenant_id,id),
  UNIQUE(tenant_id,issue_no),
  FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id),
  FOREIGN KEY(tenant_id,project_id) REFERENCES project(tenant_id,id),
  FOREIGN KEY(tenant_id,warehouse_id) REFERENCES warehouse(tenant_id,id),
  FOREIGN KEY(tenant_id,material_id) REFERENCES material(tenant_id,id),
  FOREIGN KEY(tenant_id,operator_employee_id) REFERENCES employee(tenant_id,id)
);
COMMENT ON TABLE material_issue IS '生产领料单：关联工单、项目、仓库和物料，提交后扣减物料库存';
COMMENT ON COLUMN material_issue.status IS 'posted 已领料；reversed 已冲销';
CREATE INDEX ix_material_issue_wo ON material_issue(tenant_id,work_order_id,status,id);
CREATE INDEX ix_material_issue_material ON material_issue(tenant_id,material_id,warehouse_id,status,id);
