/* 生产工单：裁床单改由工单直接分配；生产线员工自动作为工单参与者 */
CREATE TABLE work_order_cutting_sheet (
  tenant_id        TEXT NOT NULL,
  work_order_id    TEXT NOT NULL,
  cutting_sheet_id TEXT NOT NULL,
  created_at       BIGINT NOT NULL,
  created_by       TEXT NOT NULL,
  PRIMARY KEY (tenant_id, work_order_id, cutting_sheet_id),
  FOREIGN KEY (tenant_id, work_order_id) REFERENCES work_order(tenant_id, id) ON DELETE CASCADE,
  FOREIGN KEY (tenant_id, cutting_sheet_id) REFERENCES cutting_sheet(tenant_id, id)
);
CREATE INDEX ix_wocs_sheet ON work_order_cutting_sheet(tenant_id, cutting_sheet_id);
COMMENT ON TABLE work_order_cutting_sheet IS '生产工单分配的裁床单（按产品订单项目号过滤）';
