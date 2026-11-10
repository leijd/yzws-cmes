-- v0.0.3.1 数据与界面补充设计：唛架、A/B 派工、扎部位、菲票和进度投影

CREATE TABLE marker (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  marker_no TEXT NOT NULL,
  marker_date TEXT NOT NULL,
  work_order_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  product_id TEXT NOT NULL,
  product_code TEXT NOT NULL,
  product_name TEXT NOT NULL,
  color TEXT NOT NULL,
  material_id TEXT NOT NULL,
  material_code TEXT NOT NULL,
  material_name TEXT NOT NULL,
  material_specification TEXT,
  fabric_width_cm NUMERIC(12,2),
  pieces_per_marker BIGINT NOT NULL CHECK (pieces_per_marker > 0),
  fabric_layers BIGINT NOT NULL CHECK (fabric_layers > 0),
  cut_qty BIGINT NOT NULL CHECK (cut_qty > 0),
  marker_length_m NUMERIC(12,2) NOT NULL CHECK (marker_length_m > 0),
  estimated_fabric_m NUMERIC(14,2),
  utilization_rate NUMERIC(8,6),
  drawing_attachment_id TEXT,
  layout_employee_id TEXT NOT NULL,
  reviewer_employee_id TEXT,
  review_status TEXT NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending','approved','rejected')),
  review_reason TEXT,
  reviewed_at BIGINT,
  remark TEXT,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY (tenant_id,id),
  UNIQUE (tenant_id,marker_no),
  FOREIGN KEY (tenant_id,work_order_id) REFERENCES work_order(tenant_id,id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES project(tenant_id,id),
  FOREIGN KEY (tenant_id,product_id) REFERENCES product(tenant_id,id),
  FOREIGN KEY (tenant_id,material_id) REFERENCES material(tenant_id,id),
  FOREIGN KEY (tenant_id,drawing_attachment_id) REFERENCES attachment(tenant_id,id),
  FOREIGN KEY (tenant_id,layout_employee_id) REFERENCES employee(tenant_id,id),
  FOREIGN KEY (tenant_id,reviewer_employee_id) REFERENCES employee(tenant_id,id)
);
COMMENT ON TABLE marker IS '唛架主表：排料图、配比、层数、用料估算与审核状态';
COMMENT ON COLUMN marker.marker_no IS '唛架编号 MK-款号-两位顺序号，如 MK-S26088-01';
COMMENT ON COLUMN marker.pieces_per_marker IS '每唛件数，必须等于尺码配比合计';
COMMENT ON COLUMN marker.cut_qty IS '本唛架裁片数=每唛件数×拉布层数';
COMMENT ON COLUMN marker.estimated_fabric_m IS '估算用布总长=唛架长度×拉布层数';
COMMENT ON COLUMN marker.utilization_rate IS '排版软件导出的面料利用率，0～1';

CREATE INDEX ix_marker_wo ON marker(tenant_id,work_order_id,review_status,id);
CREATE INDEX ix_marker_material ON marker(tenant_id,material_id,color,id);

CREATE TABLE marker_size_ratio (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  marker_id TEXT NOT NULL,
  size TEXT NOT NULL,
  ratio_qty BIGINT NOT NULL CHECK (ratio_qty > 0),
  sort_no INTEGER NOT NULL,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY (tenant_id,id),
  UNIQUE (tenant_id,marker_id,size),
  FOREIGN KEY (tenant_id,marker_id) REFERENCES marker(tenant_id,id) ON DELETE CASCADE
);
COMMENT ON TABLE marker_size_ratio IS '唛架尺码配比明细：一唛架可混码，分扎时按尺码拆分';

CREATE INDEX ix_marker_ratio ON marker_size_ratio(tenant_id,marker_id,sort_no);

ALTER TABLE project ADD COLUMN workshop_org_unit_id TEXT;
ALTER TABLE project ADD COLUMN line_org_unit_id TEXT;
COMMENT ON COLUMN project.workshop_org_unit_id IS '项目责任车间，独立一列';
COMMENT ON COLUMN project.line_org_unit_id IS '项目责任生产线，独立一列；改车间后须重新选择';

ALTER TABLE work_order ADD COLUMN issue_date TEXT;
ALTER TABLE work_order ADD COLUMN color TEXT;
ALTER TABLE work_order ADD COLUMN workshop_org_unit_id TEXT;
ALTER TABLE work_order ADD COLUMN line_org_unit_id TEXT;
ALTER TABLE work_order ADD COLUMN report_mode TEXT NOT NULL DEFAULT 'B' CHECK (report_mode IN ('A','B'));
ALTER TABLE work_order ADD COLUMN review_status TEXT NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending','approved','rejected'));
ALTER TABLE work_order ADD COLUMN reviewer_employee_id TEXT;
ALTER TABLE work_order ADD COLUMN reviewed_at BIGINT;
ALTER TABLE work_order ADD COLUMN review_reason TEXT;
ALTER TABLE work_order ADD COLUMN process_plan_version BIGINT NOT NULL DEFAULT 1;
ALTER TABLE work_order ADD COLUMN process_plan_locked BOOLEAN NOT NULL DEFAULT FALSE;
COMMENT ON COLUMN work_order.issue_date IS '开单日期，默认当天可改';
COMMENT ON COLUMN work_order.color IS '本工单投产颜色，同款不同色拆不同工单';
COMMENT ON COLUMN work_order.report_mode IS '报工模式：A 派工受控 / B 自主选工序，互斥';
COMMENT ON COLUMN work_order.process_plan_locked IS '已有报工后锁定工序方案和报工模式';

ALTER TABLE process_definition ADD COLUMN process_category TEXT;
ALTER TABLE process_definition ADD COLUMN work_instruction TEXT;
COMMENT ON COLUMN process_definition.process_category IS '工序大类：车缝/锁钉/后整/包装';
COMMENT ON COLUMN process_definition.work_instruction IS '作业说明/工艺要求';

ALTER TABLE product ADD COLUMN part_template JSONB NOT NULL DEFAULT '[]';
COMMENT ON COLUMN product.part_template IS '款式部位模板，按 sort_no 生成扎部位与部位级菲票';

ALTER TABLE product_process ADD COLUMN process_category TEXT;
ALTER TABLE product_process ADD COLUMN equipment_name TEXT;
ALTER TABLE product_process ADD COLUMN work_instruction TEXT;
COMMENT ON COLUMN product_process.process_category IS '项目工序路线中的工序大类覆盖值';
COMMENT ON COLUMN product_process.equipment_name IS '项目工序路线中的所需设备覆盖值';
COMMENT ON COLUMN product_process.work_instruction IS '项目工序路线中的作业说明覆盖值';

ALTER TABLE work_order_process_snapshot ADD COLUMN process_category TEXT;
ALTER TABLE work_order_process_snapshot ADD COLUMN equipment_name TEXT;
ALTER TABLE work_order_process_snapshot ADD COLUMN work_instruction TEXT;
COMMENT ON COLUMN work_order_process_snapshot.process_category IS '下发时工序大类快照';
COMMENT ON COLUMN work_order_process_snapshot.equipment_name IS '下发时所需设备快照';

ALTER TABLE cutting_order ADD COLUMN marker_id TEXT;
COMMENT ON COLUMN cutting_order.marker_id IS '关联已审核通过的 marker.id；历史 marker_no 保留作快照';

CREATE TABLE task_bundle_assignment (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  task_id TEXT NOT NULL,
  bundle_id TEXT NOT NULL,
  employee_id TEXT NOT NULL,
  planned_qty BIGINT NOT NULL CHECK (planned_qty > 0),
  assigned_by TEXT NOT NULL,
  assigned_at BIGINT NOT NULL,
  revoked_by TEXT,
  revoked_at BIGINT,
  revoked_reason TEXT,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked','completed')),
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY (tenant_id,id),
  FOREIGN KEY (tenant_id,task_id) REFERENCES production_task(tenant_id,id),
  FOREIGN KEY (tenant_id,bundle_id) REFERENCES bundle(tenant_id,id),
  FOREIGN KEY (tenant_id,employee_id) REFERENCES employee(tenant_id,id)
);
COMMENT ON TABLE task_bundle_assignment IS '模式A派工受控：扎×工序×人员归属，模式B不使用该表';

CREATE INDEX ix_tba_employee ON task_bundle_assignment(tenant_id,employee_id,status,id);
CREATE INDEX ix_tba_bundle ON task_bundle_assignment(tenant_id,bundle_id,task_id,status);
CREATE UNIQUE INDEX uk_tba_active ON task_bundle_assignment(tenant_id,task_id,bundle_id,employee_id) WHERE status='active';

ALTER TABLE work_report ADD COLUMN bundle_id TEXT;
ALTER TABLE work_report ADD COLUMN bundle_part_id TEXT;
ALTER TABLE work_report ADD COLUMN ticket_id TEXT;
ALTER TABLE work_report ADD COLUMN process_snapshot_id TEXT;
ALTER TABLE work_report ADD COLUMN device_code TEXT;
ALTER TABLE work_report ADD COLUMN project_id TEXT;
ALTER TABLE work_report ADD COLUMN work_order_id TEXT;
ALTER TABLE work_report ADD COLUMN product_code TEXT;
ALTER TABLE work_report ADD COLUMN bundle_no TEXT;
ALTER TABLE work_report ADD COLUMN part_code TEXT;
ALTER TABLE work_report ADD COLUMN part_name TEXT;
ALTER TABLE work_report ADD COLUMN color TEXT;
ALTER TABLE work_report ADD COLUMN size TEXT;
ALTER TABLE work_report ADD COLUMN original_employee_id TEXT;
ALTER TABLE work_report ADD COLUMN original_unit_price NUMERIC(20,6);
ALTER TABLE work_report ADD COLUMN client_occurred_at BIGINT;
COMMENT ON COLUMN work_report.bundle_id IS '报工扎；重复报工、超数和扎顺序校验的最小单元';
COMMENT ON COLUMN work_report.bundle_part_id IS '报工部位，可空表示整件/历史数据';
COMMENT ON COLUMN work_report.device_code IS '报工设备编号，如 平车08';
COMMENT ON COLUMN work_report.original_employee_id IS '审批换人前的原报工人';
ALTER TABLE work_report ADD CONSTRAINT fk_work_report_bundle FOREIGN KEY (tenant_id,bundle_id) REFERENCES bundle(tenant_id,id);
ALTER TABLE work_report ADD CONSTRAINT fk_work_report_work_order FOREIGN KEY (tenant_id,work_order_id) REFERENCES work_order(tenant_id,id);

CREATE TABLE bundle_part (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  bundle_id TEXT NOT NULL,
  part_code TEXT NOT NULL,
  part_name TEXT NOT NULL,
  sort_no INTEGER NOT NULL,
  qty BIGINT NOT NULL CHECK (qty > 0),
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','changed','closed')),
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY (tenant_id,id),
  UNIQUE (tenant_id,bundle_id,part_code),
  UNIQUE (tenant_id,bundle_id,sort_no),
  FOREIGN KEY (tenant_id,bundle_id) REFERENCES bundle(tenant_id,id)
);
COMMENT ON TABLE bundle_part IS '扎部位明细：一扎可拆多个部位，每个启用部位生成一张菲票';

ALTER TABLE ticket ADD COLUMN bundle_part_id TEXT;
ALTER TABLE ticket ADD COLUMN part_code TEXT;
ALTER TABLE ticket ADD COLUMN part_name TEXT;
ALTER TABLE ticket ADD COLUMN print_status TEXT NOT NULL DEFAULT 'unprinted' CHECK (print_status IN ('unprinted','printed','failed','voided'));
ALTER TABLE ticket ADD COLUMN print_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE ticket ADD COLUMN print_revision BIGINT NOT NULL DEFAULT 1;
ALTER TABLE ticket ADD COLUMN last_print_job_id TEXT;
ALTER TABLE ticket ADD COLUMN voided_at BIGINT;
ALTER TABLE ticket ADD COLUMN voided_reason TEXT;
COMMENT ON COLUMN ticket.print_revision IS '补打保留 ticket_id，递增打印版本；旧物理票作废留痕';
ALTER TABLE ticket ADD CONSTRAINT fk_ticket_bundle_part FOREIGN KEY (tenant_id,bundle_part_id) REFERENCES bundle_part(tenant_id,id);

CREATE TABLE style_progress_projection (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  order_qty BIGINT NOT NULL DEFAULT 0,
  cut_qty BIGINT NOT NULL DEFAULT 0,
  wip_qty BIGINT NOT NULL DEFAULT 0,
  received_qty BIGINT NOT NULL DEFAULT 0,
  shipped_qty BIGINT NOT NULL DEFAULT 0,
  progress_ratio NUMERIC,
  current_node_text TEXT,
  workshop_org_unit_id TEXT,
  line_org_unit_id TEXT,
  planned_delivery_date TEXT,
  remaining_days INTEGER,
  avg_receipt_qty_4d NUMERIC,
  estimated_days NUMERIC,
  warning_level TEXT CHECK (warning_level IN ('normal','warning','urgent','none')),
  business_status TEXT,
  last_event_id TEXT,
  data_version BIGINT NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id,id),
  UNIQUE (tenant_id,project_id),
  FOREIGN KEY (tenant_id,project_id) REFERENCES project(tenant_id,id)
);
COMMENT ON TABLE style_progress_projection IS '款式进度17列投影：裁片/WIP/入库/整单进度/当前节点/交期预警';

CREATE TABLE process_progress_projection (
  tenant_id TEXT NOT NULL,
  id TEXT NOT NULL,
  work_order_id TEXT NOT NULL,
  process_snapshot_id TEXT NOT NULL,
  sequence_no INTEGER NOT NULL,
  process_code TEXT NOT NULL,
  process_name TEXT NOT NULL,
  process_category TEXT,
  equipment_name TEXT,
  planned_qty BIGINT NOT NULL DEFAULT 0,
  approved_good_qty BIGINT NOT NULL DEFAULT 0,
  progress_ratio NUMERIC,
  in_progress_bundle_count BIGINT NOT NULL DEFAULT 0,
  pending_bundle_count BIGINT NOT NULL DEFAULT 0,
  current_employee_ids JSONB NOT NULL DEFAULT '[]',
  business_status TEXT CHECK (business_status IN ('not_started','in_progress','completed')),
  over_qty_flag BOOLEAN NOT NULL DEFAULT FALSE,
  previous_incomplete_flag BOOLEAN NOT NULL DEFAULT FALSE,
  remark TEXT,
  last_event_id TEXT,
  data_version BIGINT NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id,id),
  UNIQUE (tenant_id,work_order_id,process_snapshot_id),
  FOREIGN KEY (tenant_id,work_order_id) REFERENCES work_order(tenant_id,id),
  FOREIGN KEY (tenant_id,process_snapshot_id) REFERENCES work_order_process_snapshot(tenant_id,id)
);
COMMENT ON TABLE process_progress_projection IS '工序进度13列投影：应做/完成/完成率/进行中扎/待做扎/作业人/异常';

CREATE INDEX ix_process_progress_order ON process_progress_projection(tenant_id,work_order_id,sequence_no);
