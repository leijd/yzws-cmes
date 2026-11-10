CREATE TABLE tenant (
  id TEXT PRIMARY KEY, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
  timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai', status TEXT NOT NULL CHECK(status IN ('active','disabled')),
  settings JSONB NOT NULL DEFAULT '{}', version BIGINT NOT NULL DEFAULT 1, created_at BIGINT NOT NULL
);
CREATE TABLE org_unit (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, parent_id TEXT,
  org_type TEXT NOT NULL CHECK(org_type IN ('department','workshop','line','team')),
  code TEXT NOT NULL, name TEXT NOT NULL, manager_employee_id TEXT, path TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active', created_at BIGINT NOT NULL, created_by TEXT NOT NULL,
  created_node TEXT NOT NULL, updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE TABLE employee (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, employee_no TEXT NOT NULL,
  name TEXT NOT NULL, gender TEXT, birth_date TEXT, phone TEXT, id_card_no TEXT, address TEXT,
  nation TEXT, native_place TEXT, hire_date TEXT, status TEXT NOT NULL DEFAULT 'active',
  org_unit_id TEXT, manager_employee_id TEXT, position TEXT, bank_card TEXT, bank_name TEXT,
  emergency_contact TEXT, emergency_phone TEXT, remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,employee_no),
  FOREIGN KEY(tenant_id,org_unit_id) REFERENCES org_unit(tenant_id,id)
);
CREATE TABLE user_account (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, employee_id TEXT,
  login_name TEXT NOT NULL, password_verifier TEXT NOT NULL, wechat_unionid TEXT,
  status TEXT NOT NULL DEFAULT 'active', offline_policy JSONB NOT NULL DEFAULT '{}', last_login_at BIGINT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,login_name),
  FOREIGN KEY(tenant_id,employee_id) REFERENCES employee(tenant_id,id)
);
CREATE TABLE role (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, code TEXT NOT NULL,
  name TEXT NOT NULL, description TEXT, is_system BOOLEAN NOT NULL DEFAULT FALSE,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE TABLE permission (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, code TEXT NOT NULL,
  resource TEXT NOT NULL, action TEXT NOT NULL, sensitivity TEXT NOT NULL DEFAULT 'normal', description TEXT,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE TABLE user_role (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, user_id TEXT NOT NULL,
  role_id TEXT NOT NULL, valid_from BIGINT NOT NULL, valid_to BIGINT,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,user_id,role_id,valid_from),
  FOREIGN KEY(tenant_id,user_id) REFERENCES user_account(tenant_id,id),
  FOREIGN KEY(tenant_id,role_id) REFERENCES role(tenant_id,id)
);
CREATE TABLE role_permission (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, role_id TEXT NOT NULL,
  permission_id TEXT NOT NULL, PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,role_id,permission_id),
  FOREIGN KEY(tenant_id,role_id) REFERENCES role(tenant_id,id),
  FOREIGN KEY(tenant_id,permission_id) REFERENCES permission(tenant_id,id)
);
CREATE TABLE user_scope (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, user_id TEXT NOT NULL,
  scope_type TEXT NOT NULL, scope_value TEXT NOT NULL, effect TEXT NOT NULL DEFAULT 'allow',
  PRIMARY KEY(tenant_id,id), FOREIGN KEY(tenant_id,user_id) REFERENCES user_account(tenant_id,id)
);
CREATE TABLE auth_session (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, user_id TEXT NOT NULL,
  node_id TEXT NOT NULL, refresh_token_hash TEXT NOT NULL, permission_snapshot_version BIGINT NOT NULL DEFAULT 1,
  expires_at BIGINT NOT NULL, offline_expires_at BIGINT NOT NULL, revoked_at BIGINT, reason TEXT,
  device_info JSONB NOT NULL DEFAULT '{}', created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,refresh_token_hash),
  FOREIGN KEY(tenant_id,user_id) REFERENCES user_account(tenant_id,id)
);
CREATE TABLE customer (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL,
  contact_name TEXT, phone TEXT, address TEXT, remark TEXT, status TEXT NOT NULL DEFAULT 'active',
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE TABLE supplier (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL,
  contact_name TEXT, phone TEXT, address TEXT, remark TEXT, material_categories JSONB NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'active', created_at BIGINT NOT NULL, created_by TEXT NOT NULL,
  created_node TEXT NOT NULL, updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE TABLE warehouse (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL,
  warehouse_type TEXT NOT NULL, org_unit_id TEXT, manager_employee_id TEXT, status TEXT NOT NULL DEFAULT 'active',
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE TABLE material (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL,
  category TEXT, specification TEXT, unit TEXT NOT NULL, unit_scale BIGINT NOT NULL DEFAULT 1,
  default_supplier_id TEXT, batch_rule JSONB NOT NULL DEFAULT '{}', status TEXT NOT NULL DEFAULT 'active',
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE TABLE dictionary_item (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, dictionary_type TEXT NOT NULL,
  code TEXT NOT NULL, name TEXT NOT NULL, parent_id TEXT, sort_no INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active', created_at BIGINT NOT NULL, created_by TEXT NOT NULL,
  created_node TEXT NOT NULL, updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,dictionary_type,code)
);
CREATE TABLE product (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, product_code TEXT NOT NULL,
  name TEXT NOT NULL, spec TEXT, unit TEXT NOT NULL, category_id TEXT, accessory_combination TEXT,
  customer_id TEXT, development_date TEXT, sale_price NUMERIC(20,6), remark TEXT,
  image_attachment_id TEXT, drawing_attachment_id TEXT, status TEXT NOT NULL DEFAULT 'draft',
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,product_code)
);
CREATE TABLE process_definition (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, process_code TEXT NOT NULL,
  name TEXT NOT NULL, default_unit_price NUMERIC(20,6) NOT NULL DEFAULT 0 CHECK(default_unit_price>=0),
  default_report_ratio NUMERIC(20,6) NOT NULL DEFAULT 1 CHECK(default_report_ratio>0),
  default_department_id TEXT, standard_work_seconds BIGINT NOT NULL DEFAULT 0 CHECK(standard_work_seconds>=0),
  defect_config JSONB NOT NULL DEFAULT '[]', equipment TEXT, remark TEXT, status TEXT NOT NULL DEFAULT 'active',
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,process_code)
);
CREATE TABLE product_process (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, product_id TEXT NOT NULL,
  product_version BIGINT NOT NULL, process_id TEXT NOT NULL, sequence_no INTEGER NOT NULL CHECK(sequence_no>0),
  unit_price NUMERIC(20,6) NOT NULL CHECK(unit_price>=0), report_ratio NUMERIC(20,6) NOT NULL CHECK(report_ratio>0),
  department_id TEXT, standard_work_seconds BIGINT NOT NULL DEFAULT 0,
  is_parallel BOOLEAN NOT NULL DEFAULT FALSE, is_critical BOOLEAN NOT NULL DEFAULT FALSE,
  is_required BOOLEAN NOT NULL DEFAULT TRUE, defect_config JSONB NOT NULL DEFAULT '[]', remark TEXT,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,product_id,product_version,process_id),
  UNIQUE(tenant_id,product_id,product_version,sequence_no),
  FOREIGN KEY(tenant_id,product_id) REFERENCES product(tenant_id,id),
  FOREIGN KEY(tenant_id,process_id) REFERENCES process_definition(tenant_id,id)
);
CREATE TABLE project (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, project_code TEXT NOT NULL,
  customer_id TEXT NOT NULL, product_id TEXT NOT NULL, received_date BIGINT,
  order_qty BIGINT NOT NULL CHECK(order_qty>0), customer_unit_price NUMERIC(20,6) NOT NULL DEFAULT 0,
  external_unit_price NUMERIC(20,6) NOT NULL DEFAULT 0, material_owner TEXT,
  owner_employee_id TEXT, qc_employee_id TEXT, required_delivery_date TEXT, status TEXT NOT NULL DEFAULT 'draft',
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,project_code),
  FOREIGN KEY(tenant_id,customer_id) REFERENCES customer(tenant_id,id),
  FOREIGN KEY(tenant_id,product_id) REFERENCES product(tenant_id,id)
);
CREATE TABLE work_order (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, work_order_code TEXT NOT NULL,
  project_id TEXT NOT NULL, product_id TEXT NOT NULL, product_version BIGINT NOT NULL,
  planned_product_qty BIGINT NOT NULL CHECK(planned_product_qty>0),
  status TEXT NOT NULL CHECK(status IN ('draft','pending_release','in_production','paused','completed','cancelled')),
  priority TEXT NOT NULL DEFAULT 'normal', planned_start_at BIGINT, planned_end_at BIGINT,
  actual_start_at BIGINT, actual_end_at BIGINT, released_by TEXT, released_at BIGINT,
  completion_policy TEXT NOT NULL DEFAULT 'all_required', custom_values JSONB NOT NULL DEFAULT '{}',
  work_order_category TEXT, packaging_requirement TEXT, special_requirement TEXT, release_snapshot_hash TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,work_order_code),
  FOREIGN KEY(tenant_id,project_id) REFERENCES project(tenant_id,id),
  FOREIGN KEY(tenant_id,product_id) REFERENCES product(tenant_id,id)
);
CREATE TABLE work_order_process_snapshot (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, work_order_id TEXT NOT NULL,
  product_process_id TEXT NOT NULL, sequence_no INTEGER NOT NULL, process_code TEXT NOT NULL,
  process_name TEXT NOT NULL, planned_qty BIGINT NOT NULL CHECK(planned_qty>0),
  unit_price NUMERIC(20,6) NOT NULL, report_ratio NUMERIC(20,6) NOT NULL,
  department_id TEXT, standard_work_seconds BIGINT NOT NULL DEFAULT 0,
  route_flags JSONB NOT NULL DEFAULT '{}', defect_config JSONB NOT NULL DEFAULT '[]', created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,work_order_id,product_process_id),
  FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id)
);
CREATE TABLE cutting_order (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, cutting_no TEXT NOT NULL,
  work_order_id TEXT NOT NULL, cutting_date BIGINT NOT NULL, bed_no TEXT, marker_no TEXT,
  fabric_layers BIGINT NOT NULL DEFAULT 0, cut_qty BIGINT NOT NULL CHECK(cut_qty>0),
  bundle_range TEXT, size_ratio JSONB NOT NULL DEFAULT '[]', cutter_id TEXT, reviewer_id TEXT,
  status TEXT NOT NULL DEFAULT 'draft', created_at BIGINT NOT NULL, created_by TEXT NOT NULL,
  created_node TEXT NOT NULL, updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL,
  updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,cutting_no), UNIQUE(tenant_id,work_order_id),
  FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id)
);
CREATE TABLE cutting_detail (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, cutting_order_id TEXT NOT NULL,
  color TEXT NOT NULL, size TEXT NOT NULL, bundle_count BIGINT NOT NULL DEFAULT 1 CHECK(bundle_count>0),
  piece_qty BIGINT NOT NULL CHECK(piece_qty>0), sort_no INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), FOREIGN KEY(tenant_id,cutting_order_id) REFERENCES cutting_order(tenant_id,id)
);
CREATE TABLE bundle (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, work_order_id TEXT NOT NULL,
  cutting_detail_id TEXT NOT NULL, bundle_no TEXT NOT NULL, bed_no TEXT, color TEXT NOT NULL, size TEXT NOT NULL,
  original_qty BIGINT NOT NULL CHECK(original_qty>0), current_qty BIGINT NOT NULL CHECK(current_qty>=0),
  status TEXT NOT NULL DEFAULT 'active', created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,work_order_id,bundle_no),
  FOREIGN KEY(tenant_id,cutting_detail_id) REFERENCES cutting_detail(tenant_id,id)
);
CREATE TABLE ticket (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, bundle_id TEXT NOT NULL,
  public_code TEXT NOT NULL, signature_key_id TEXT NOT NULL, payload_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active', printed_at BIGINT, created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,public_code),
  FOREIGN KEY(tenant_id,bundle_id) REFERENCES bundle(tenant_id,id)
);
CREATE TABLE production_task (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, work_order_id TEXT NOT NULL,
  product_process_id TEXT NOT NULL, parent_task_id TEXT, source_task_id TEXT, department_id TEXT,
  employee_id TEXT, planned_qty BIGINT NOT NULL CHECK(planned_qty>0),
  reported_qty BIGINT NOT NULL DEFAULT 0 CHECK(reported_qty>=0),
  approved_good_qty BIGINT NOT NULL DEFAULT 0 CHECK(approved_good_qty>=0),
  pending_qty BIGINT NOT NULL DEFAULT 0 CHECK(pending_qty>=0), unit_price NUMERIC(20,6) NOT NULL,
  report_ratio NUMERIC(20,6) NOT NULL CHECK(report_ratio>0), standard_work_seconds BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL CHECK(status IN ('unassigned','assigned','in_progress','paused','completed','cancelled')),
  planned_start_at BIGINT, planned_end_at BIGINT, actual_start_at BIGINT, actual_end_at BIGINT, remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL, version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), CHECK(reported_qty<=planned_qty), CHECK(approved_good_qty<=reported_qty),
  FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id)
);
CREATE TABLE task_transfer (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, source_task_id TEXT NOT NULL,
  target_task_id TEXT NOT NULL, from_employee_id TEXT, to_employee_id TEXT NOT NULL,
  transfer_qty BIGINT NOT NULL CHECK(transfer_qty>0), reason TEXT NOT NULL, operator_id TEXT NOT NULL,
  created_at BIGINT NOT NULL, PRIMARY KEY(tenant_id,id)
);
CREATE TABLE work_report (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, task_id TEXT NOT NULL,
  report_kind TEXT NOT NULL CHECK(report_kind IN ('normal','rework','reversal')),
  good_qty BIGINT NOT NULL DEFAULT 0 CHECK(good_qty>=0), defect_qty BIGINT NOT NULL DEFAULT 0 CHECK(defect_qty>=0),
  defect_items JSONB NOT NULL DEFAULT '[]', original_report_id TEXT, employee_id TEXT NOT NULL,
  unit_price NUMERIC(20,6) NOT NULL CHECK(unit_price>=0), coefficient NUMERIC(20,6) NOT NULL DEFAULT 1,
  standard_work_seconds BIGINT NOT NULL DEFAULT 0, actual_work_seconds BIGINT, remark TEXT,
  review_status TEXT NOT NULL CHECK(review_status IN ('pending','approved','rejected')),
  auto_rule_version BIGINT, idempotency_key TEXT NOT NULL, occurred_at BIGINT NOT NULL,
  created_node TEXT NOT NULL, created_at BIGINT NOT NULL, PRIMARY KEY(tenant_id,id),
  UNIQUE(tenant_id,idempotency_key), FOREIGN KEY(tenant_id,task_id) REFERENCES production_task(tenant_id,id)
);
CREATE TABLE report_review (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, report_id TEXT NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('approve','reject','correct_approve','withdraw','reverse')),
  actor_type TEXT NOT NULL CHECK(actor_type IN ('user','system')), actor_id TEXT, reason TEXT,
  before_values JSONB, after_values JSONB, rule_version BIGINT, created_node TEXT NOT NULL,
  created_at BIGINT NOT NULL, PRIMARY KEY(tenant_id,id),
  FOREIGN KEY(tenant_id,report_id) REFERENCES work_report(tenant_id,id)
);
CREATE TABLE piecework_entry (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, report_id TEXT NOT NULL,
  employee_id TEXT NOT NULL, entry_kind TEXT NOT NULL CHECK(entry_kind IN ('estimated','confirmed','reversal','quality_adjustment','manual_adjustment')),
  qty BIGINT NOT NULL, unit_price NUMERIC(20,6) NOT NULL, coefficient NUMERIC(20,6) NOT NULL,
  amount NUMERIC(20,6) NOT NULL, payroll_period TEXT NOT NULL, source_entry_id TEXT, created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id), FOREIGN KEY(tenant_id,report_id) REFERENCES work_report(tenant_id,id)
);
CREATE TABLE attachment (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, sha256 TEXT NOT NULL,
  mime_type TEXT NOT NULL, size_bytes BIGINT NOT NULL CHECK(size_bytes>=0), duration_ms BIGINT,
  width INTEGER, height INTEGER, storage_key TEXT NOT NULL,
  process_status TEXT NOT NULL DEFAULT 'uploading', sync_status TEXT NOT NULL DEFAULT 'local',
  created_by TEXT NOT NULL, created_node TEXT NOT NULL, created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,sha256,size_bytes)
);
CREATE TABLE attachment_link (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, attachment_id TEXT NOT NULL,
  owner_type TEXT NOT NULL, owner_id TEXT NOT NULL, usage_type TEXT NOT NULL,
  sort_no INTEGER NOT NULL DEFAULT 0, is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
  PRIMARY KEY(tenant_id,id), FOREIGN KEY(tenant_id,attachment_id) REFERENCES attachment(tenant_id,id)
);
CREATE TABLE outbox_event (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), event_id TEXT NOT NULL, origin_node_id TEXT NOT NULL,
  origin_seq BIGINT NOT NULL, ownership_epoch BIGINT NOT NULL DEFAULT 1, aggregate_type TEXT NOT NULL,
  aggregate_id TEXT NOT NULL, event_type TEXT NOT NULL, schema_version INTEGER NOT NULL DEFAULT 1,
  occurred_at BIGINT NOT NULL, payload TEXT NOT NULL, payload_sha256 TEXT NOT NULL,
  push_status TEXT NOT NULL DEFAULT 'pending', last_error TEXT, next_retry_at BIGINT, created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,event_id), UNIQUE(tenant_id,origin_node_id,origin_seq)
);
CREATE TABLE inbox_event (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), event_id TEXT NOT NULL, origin_node_id TEXT NOT NULL,
  origin_seq BIGINT NOT NULL, process_result TEXT NOT NULL, result_payload TEXT, received_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,event_id), UNIQUE(tenant_id,origin_node_id,origin_seq,process_result)
);
CREATE TABLE sync_cursor (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, node_id TEXT NOT NULL,
  peer_node_id TEXT NOT NULL, direction TEXT NOT NULL, subscription TEXT NOT NULL,
  cursor_value TEXT NOT NULL, updated_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,node_id,peer_node_id,direction,subscription)
);
CREATE TABLE feature_setting (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, setting_domain TEXT NOT NULL,
  setting_key TEXT NOT NULL, setting_value JSONB NOT NULL, version BIGINT NOT NULL,
  effective_at BIGINT NOT NULL, updated_by TEXT NOT NULL, change_reason TEXT, created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,setting_domain,setting_key,version)
);
CREATE TABLE notification (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, user_id TEXT NOT NULL,
  notification_type TEXT NOT NULL, owner_type TEXT NOT NULL, owner_id TEXT NOT NULL,
  title TEXT NOT NULL, body TEXT NOT NULL, read_at BIGINT, created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id)
);
CREATE TABLE audit_log (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, request_id TEXT NOT NULL,
  actor_type TEXT NOT NULL, actor_id TEXT, action TEXT NOT NULL, owner_type TEXT NOT NULL,
  owner_id TEXT NOT NULL, before_values JSONB, after_values JSONB, reason TEXT, ip TEXT,
  device_info JSONB, created_node TEXT NOT NULL, created_at BIGINT NOT NULL, PRIMARY KEY(tenant_id,id)
);
CREATE TABLE command_request (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL, idempotency_key TEXT NOT NULL,
  command_type TEXT NOT NULL, aggregate_id TEXT NOT NULL, request_hash TEXT NOT NULL,
  status TEXT NOT NULL, response_payload JSONB, expires_at BIGINT NOT NULL, created_at BIGINT NOT NULL,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,idempotency_key)
);
CREATE INDEX ix_product_list ON product(tenant_id,category_id,status,id);
CREATE INDEX ix_pp_version ON product_process(tenant_id,product_id,product_version,sequence_no);
CREATE INDEX ix_wo_list ON work_order(tenant_id,status,planned_end_at,id);
CREATE INDEX ix_task_wo ON production_task(tenant_id,work_order_id,status,id);
CREATE INDEX ix_task_employee ON production_task(tenant_id,employee_id,status,planned_end_at,id);
CREATE INDEX ix_task_parent ON production_task(tenant_id,parent_task_id);
CREATE INDEX ix_report_task ON work_report(tenant_id,task_id,review_status,occurred_at,id);
CREATE INDEX ix_report_employee ON work_report(tenant_id,employee_id,occurred_at,id);
CREATE INDEX ix_outbox_pending ON outbox_event(tenant_id,push_status,origin_seq);
CREATE INDEX ix_audit_owner ON audit_log(tenant_id,owner_type,owner_id,created_at);

CREATE FUNCTION reject_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'append_only_table'; END; $$;
CREATE TRIGGER trg_report_no_delete BEFORE DELETE ON work_report FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER trg_review_append_only BEFORE UPDATE OR DELETE ON report_review FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER trg_piecework_append_only BEFORE UPDATE OR DELETE ON piecework_entry FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER trg_audit_append_only BEFORE UPDATE OR DELETE ON audit_log FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER trg_outbox_no_delete BEFORE DELETE ON outbox_event FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER trg_snapshot_immutable BEFORE UPDATE OR DELETE ON work_order_process_snapshot FOR EACH ROW EXECUTE FUNCTION reject_mutation();
