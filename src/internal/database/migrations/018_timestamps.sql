CREATE TABLE deletion_log (
  id TEXT PRIMARY KEY,
  table_name TEXT NOT NULL,
  tenant_id TEXT,
  record_id TEXT,
  deleted_at BIGINT NOT NULL,
  snapshot JSONB NOT NULL
);
CREATE INDEX ix_deletion_log_table_time ON deletion_log(table_name, deleted_at);
CREATE INDEX ix_deletion_log_record ON deletion_log(tenant_id, record_id);
COMMENT ON TABLE deletion_log IS '删除留痕：业务数据物理删除时由触发器记录整行快照与删除时间戳';

ALTER TABLE attachment ADD COLUMN updated_at BIGINT;
ALTER TABLE attachment_link ADD COLUMN created_at BIGINT;
ALTER TABLE attachment_link ADD COLUMN updated_at BIGINT;
ALTER TABLE bundle ADD COLUMN updated_at BIGINT;
ALTER TABLE custom_field_definition ADD COLUMN updated_at BIGINT;
ALTER TABLE custom_field_value ADD COLUMN created_at BIGINT;
ALTER TABLE custom_field_value ADD COLUMN updated_at BIGINT;
ALTER TABLE cutting_detail ADD COLUMN created_at BIGINT;
ALTER TABLE cutting_detail ADD COLUMN updated_at BIGINT;
ALTER TABLE defect_record ADD COLUMN updated_at BIGINT;
ALTER TABLE employee_pay_profile ADD COLUMN updated_at BIGINT;
ALTER TABLE employee_skill ADD COLUMN created_at BIGINT;
ALTER TABLE employee_skill ADD COLUMN updated_at BIGINT;
ALTER TABLE export_job ADD COLUMN updated_at BIGINT;
ALTER TABLE feature_setting ADD COLUMN updated_at BIGINT;
ALTER TABLE final_inspection ADD COLUMN updated_at BIGINT;
ALTER TABLE finished_receipt ADD COLUMN updated_at BIGINT;
ALTER TABLE finished_receipt_line ADD COLUMN created_at BIGINT;
ALTER TABLE finished_receipt_line ADD COLUMN updated_at BIGINT;
ALTER TABLE import_job ADD COLUMN updated_at BIGINT;
ALTER TABLE inventory_ledger ADD COLUMN updated_at BIGINT;
ALTER TABLE iqc_result ADD COLUMN updated_at BIGINT;
ALTER TABLE material_receipt_line ADD COLUMN created_at BIGINT;
ALTER TABLE material_receipt_line ADD COLUMN updated_at BIGINT;
ALTER TABLE node ADD COLUMN updated_at BIGINT;
ALTER TABLE notification ADD COLUMN updated_at BIGINT;
ALTER TABLE ownership_lease ADD COLUMN updated_at BIGINT;
ALTER TABLE payment ADD COLUMN updated_at BIGINT;
ALTER TABLE payroll_adjustment ADD COLUMN updated_at BIGINT;
ALTER TABLE payroll_item ADD COLUMN created_at BIGINT;
ALTER TABLE payroll_item ADD COLUMN updated_at BIGINT;
ALTER TABLE permission ADD COLUMN created_at BIGINT;
ALTER TABLE permission ADD COLUMN updated_at BIGINT;
ALTER TABLE piecework_entry ADD COLUMN updated_at BIGINT;
ALTER TABLE platform_admin ADD COLUMN updated_at BIGINT;
ALTER TABLE platform_module ADD COLUMN updated_at BIGINT;
ALTER TABLE platform_role_template ADD COLUMN updated_at BIGINT;
ALTER TABLE platform_role_template_permission ADD COLUMN updated_at BIGINT;
ALTER TABLE platform_setting ADD COLUMN updated_at BIGINT;
ALTER TABLE print_job ADD COLUMN created_at BIGINT;
ALTER TABLE print_job ADD COLUMN updated_at BIGINT;
ALTER TABLE print_template ADD COLUMN updated_at BIGINT;
ALTER TABLE product_process ADD COLUMN created_at BIGINT;
ALTER TABLE product_process ADD COLUMN updated_at BIGINT;
ALTER TABLE registration_request ADD COLUMN updated_at BIGINT;
ALTER TABLE reinspection ADD COLUMN updated_at BIGINT;
ALTER TABLE report_review ADD COLUMN updated_at BIGINT;
ALTER TABLE rework_order ADD COLUMN updated_at BIGINT;
ALTER TABLE role_permission ADD COLUMN created_at BIGINT;
ALTER TABLE role_permission ADD COLUMN updated_at BIGINT;
ALTER TABLE shipment ADD COLUMN updated_at BIGINT;
ALTER TABLE shipment_line ADD COLUMN created_at BIGINT;
ALTER TABLE shipment_line ADD COLUMN updated_at BIGINT;
ALTER TABLE task_transfer ADD COLUMN updated_at BIGINT;
ALTER TABLE tenant ADD COLUMN updated_at BIGINT;
ALTER TABLE ticket ADD COLUMN updated_at BIGINT;
ALTER TABLE user_role ADD COLUMN created_at BIGINT;
ALTER TABLE user_role ADD COLUMN updated_at BIGINT;
ALTER TABLE user_scope ADD COLUMN created_at BIGINT;
ALTER TABLE user_scope ADD COLUMN updated_at BIGINT;
ALTER TABLE work_order_process_snapshot ADD COLUMN updated_at BIGINT;
ALTER TABLE work_report ADD COLUMN updated_at BIGINT;

CREATE FUNCTION stamp_created_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (to_jsonb(NEW) ->> 'created_at') IS NULL THEN
    NEW := jsonb_populate_record(NEW, jsonb_build_object('created_at', (extract(epoch FROM clock_timestamp()) * 1000000)::bigint));
  END IF;
  RETURN NEW;
END $$;

CREATE FUNCTION stamp_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (to_jsonb(NEW) ->> 'updated_at') IS NULL OR (to_jsonb(NEW) ->> 'updated_at') = (to_jsonb(OLD) ->> 'updated_at') THEN
    NEW := jsonb_populate_record(NEW, jsonb_build_object('updated_at', (extract(epoch FROM clock_timestamp()) * 1000000)::bigint));
  END IF;
  RETURN NEW;
END $$;

CREATE FUNCTION log_deletion() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO deletion_log(id, table_name, tenant_id, record_id, deleted_at, snapshot)
  VALUES ('del-' || md5(clock_timestamp()::text || random()::text || txid_current()::text),
    TG_TABLE_NAME,
    to_jsonb(OLD) ->> 'tenant_id',
    to_jsonb(OLD) ->> 'id',
    (extract(epoch FROM clock_timestamp()) * 1000000)::bigint,
    to_jsonb(OLD));
  RETURN OLD;
END $$;

DO $$
DECLARE
  t TEXT;
  tables TEXT[] := ARRAY[
    'org_unit','employee','user_account','role','permission','user_role','role_permission','user_scope',
    'customer','supplier','warehouse','material','dictionary_item','product','process_definition','product_process',
    'project','work_order','work_order_process_snapshot','cutting_order','cutting_detail','bundle','ticket',
    'production_task','task_transfer','work_report','report_review','piecework_entry',
    'attachment','attachment_link','inventory_ledger','iqc_result','final_inspection','reinspection','defect_record',
    'rework_order','material_receipt','material_receipt_line','finished_receipt','finished_receipt_line',
    'shipment','shipment_line','payment','payroll_sheet','payroll_item','payroll_adjustment',
    'feature_setting','notification','custom_field_definition','custom_field_value','employee_pay_profile','employee_skill',
    'print_template','print_job','printer','node','tenant','registration_request','ownership_lease','export_job','import_job',
    'platform_admin','platform_module','platform_role_template','platform_role_template_permission','platform_setting'
  ];
BEGIN
  FOREACH t IN ARRAY tables LOOP
    EXECUTE format('DROP TRIGGER IF EXISTS trg_stamp_insert ON %I', t);
    EXECUTE format('CREATE TRIGGER trg_stamp_insert BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION stamp_created_at()', t);
    EXECUTE format('DROP TRIGGER IF EXISTS trg_stamp_update ON %I', t);
    EXECUTE format('CREATE TRIGGER trg_stamp_update BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION stamp_updated_at()', t);
    EXECUTE format('DROP TRIGGER IF EXISTS trg_log_deletion ON %I', t);
    EXECUTE format('CREATE TRIGGER trg_log_deletion AFTER DELETE ON %I FOR EACH ROW EXECUTE FUNCTION log_deletion()', t);
  END LOOP;
END $$;
