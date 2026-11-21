/* 生产工单管理：工单下的生产组（车间/产线/线长/组长/组员）与裁床单分配 */
CREATE TABLE work_order_group (
  tenant_id            TEXT NOT NULL,
  id                   TEXT NOT NULL,
  work_order_id        TEXT NOT NULL,
  workshop_org_unit_id TEXT,
  line_org_unit_id     TEXT,
  name                 TEXT NOT NULL DEFAULT '',
  leader_employee_id   TEXT,
  created_at           BIGINT NOT NULL,
  created_by           TEXT NOT NULL,
  created_node         TEXT NOT NULL,
  updated_at           BIGINT NOT NULL,
  updated_by           TEXT NOT NULL,
  updated_node         TEXT NOT NULL,
  PRIMARY KEY (tenant_id, id),
  FOREIGN KEY (tenant_id, work_order_id) REFERENCES work_order(tenant_id, id)
);
CREATE INDEX ix_wog_order ON work_order_group(tenant_id, work_order_id);
COMMENT ON TABLE work_order_group IS '生产工单下组建的生产组（绑定车间/产线/组长，组员与裁床单分配）';

CREATE TABLE work_order_group_member (
  tenant_id   TEXT NOT NULL,
  group_id    TEXT NOT NULL,
  employee_id TEXT NOT NULL,
  created_at  BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, group_id, employee_id),
  FOREIGN KEY (tenant_id, group_id) REFERENCES work_order_group(tenant_id, id) ON DELETE CASCADE
);
COMMENT ON TABLE work_order_group_member IS '工单生产组组员';

CREATE TABLE work_order_group_sheet (
  tenant_id        TEXT NOT NULL,
  group_id         TEXT NOT NULL,
  cutting_sheet_id TEXT NOT NULL,
  created_at       BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, group_id, cutting_sheet_id),
  FOREIGN KEY (tenant_id, group_id) REFERENCES work_order_group(tenant_id, id) ON DELETE CASCADE
);
COMMENT ON TABLE work_order_group_sheet IS '工单生产组分配的裁床单';

UPDATE platform_module SET name='生产工单管理' WHERE code='menu.tasks';
