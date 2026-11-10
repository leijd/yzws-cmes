-- 出货退货：支持整单/部分退货，退货写正向成品库存流水
CREATE TABLE shipment_return (
  tenant_id TEXT NOT NULL REFERENCES tenant(id),
  id TEXT NOT NULL,
  return_no TEXT NOT NULL,
  shipment_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  work_order_id TEXT NOT NULL,
  bundle_id TEXT NOT NULL,
  qty BIGINT NOT NULL CHECK(qty>0),
  reason TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'posted' CHECK(status IN ('posted','reversed')),
  operator_employee_id TEXT NOT NULL,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id),
  UNIQUE(tenant_id,return_no),
  FOREIGN KEY(tenant_id,shipment_id) REFERENCES shipment(tenant_id,id),
  FOREIGN KEY(tenant_id,project_id) REFERENCES project(tenant_id,id),
  FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id),
  FOREIGN KEY(tenant_id,bundle_id) REFERENCES bundle(tenant_id,id)
);
COMMENT ON TABLE shipment_return IS '出货退货单：按出货单和扎退回成品，支持部分或全部退货';
CREATE INDEX ix_shipment_return_shipment ON shipment_return(tenant_id,shipment_id,status,id);
CREATE INDEX ix_shipment_return_project ON shipment_return(tenant_id,project_id,status,id);
