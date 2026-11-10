CREATE TABLE node (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,node_type TEXT NOT NULL CHECK(node_type IN('center','edge')),name TEXT NOT NULL,
 certificate_fingerprint TEXT,public_key TEXT,number_segment TEXT,last_seen_at BIGINT,status TEXT NOT NULL DEFAULT 'active',created_at BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE ownership_lease (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,aggregate_type TEXT NOT NULL,aggregate_id TEXT NOT NULL,owner_node_id TEXT NOT NULL,
 ownership_epoch BIGINT NOT NULL CHECK(ownership_epoch>0),valid_from BIGINT NOT NULL,valid_to BIGINT,status TEXT NOT NULL,created_at BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,aggregate_type,aggregate_id,ownership_epoch)
);
CREATE TABLE registration_request (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,invite_code_hash TEXT NOT NULL,name TEXT NOT NULL,phone TEXT,requested_org_id TEXT,
 wechat_unionid TEXT,status TEXT NOT NULL DEFAULT 'pending',reviewed_by TEXT,reviewed_at BIGINT,reason TEXT,created_at BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE employee_skill (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,employee_id TEXT NOT NULL,process_id TEXT NOT NULL,skill_level TEXT NOT NULL,
 certified_at BIGINT,remark TEXT,PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,employee_id,process_id),
 FOREIGN KEY(tenant_id,employee_id) REFERENCES employee(tenant_id,id),FOREIGN KEY(tenant_id,process_id) REFERENCES process_definition(tenant_id,id)
);
CREATE TABLE employee_pay_profile (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,employee_id TEXT NOT NULL,pay_mode TEXT NOT NULL,
 base_salary NUMERIC(20,6) NOT NULL DEFAULT 0,price_tier TEXT,coefficient NUMERIC(20,6) NOT NULL DEFAULT 1,
 valid_from BIGINT NOT NULL,valid_to BIGINT,version BIGINT NOT NULL DEFAULT 1,created_at BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,employee_id) REFERENCES employee(tenant_id,id)
);
CREATE TABLE material_receipt (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,receipt_no TEXT NOT NULL,receipt_date BIGINT NOT NULL,project_id TEXT NOT NULL,
 warehouse_id TEXT NOT NULL,supplier_id TEXT NOT NULL,receiver_employee_id TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'draft',
 created_at BIGINT NOT NULL,created_by TEXT NOT NULL,created_node TEXT NOT NULL,updated_at BIGINT NOT NULL,updated_by TEXT NOT NULL,
 updated_node TEXT NOT NULL,version BIGINT NOT NULL DEFAULT 1,PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,receipt_no)
);
CREATE TABLE material_receipt_line (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,receipt_id TEXT NOT NULL,material_id TEXT NOT NULL,
 bed_no TEXT,color_no TEXT,batch_no TEXT,received_qty NUMERIC(20,6) NOT NULL,pass_qty NUMERIC(20,6) NOT NULL DEFAULT 0,
 reject_qty NUMERIC(20,6) NOT NULL DEFAULT 0,concession_qty NUMERIC(20,6) NOT NULL DEFAULT 0,pending_qty NUMERIC(20,6) NOT NULL DEFAULT 0,
 accepted_qty NUMERIC(20,6) NOT NULL DEFAULT 0,unit_price NUMERIC(20,6) NOT NULL DEFAULT 0,amount NUMERIC(20,6) NOT NULL DEFAULT 0,remark TEXT,
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,receipt_id) REFERENCES material_receipt(tenant_id,id),
 CHECK(received_qty>=0 AND pass_qty>=0 AND reject_qty>=0 AND concession_qty>=0 AND pending_qty>=0 AND accepted_qty>=0)
);
CREATE TABLE iqc_result (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,receipt_line_id TEXT NOT NULL,result TEXT NOT NULL,
 inspector_employee_id TEXT NOT NULL,inspected_at BIGINT NOT NULL,defect_description TEXT,attachment_ids JSONB NOT NULL DEFAULT '[]',
 created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,receipt_line_id) REFERENCES material_receipt_line(tenant_id,id)
);
CREATE TABLE print_template (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,name TEXT NOT NULL,template_type TEXT NOT NULL,
 paper_width NUMERIC(20,6),paper_height NUMERIC(20,6),content JSONB NOT NULL,version BIGINT NOT NULL,status TEXT NOT NULL DEFAULT 'draft',
 created_at BIGINT NOT NULL,created_by TEXT NOT NULL,PRIMARY KEY(tenant_id,id)
);
CREATE TABLE printer (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,name TEXT NOT NULL,model TEXT,connection_type TEXT NOT NULL,
 address TEXT,capabilities JSONB NOT NULL DEFAULT '{}',default_template_id TEXT,status TEXT NOT NULL DEFAULT 'active',last_seen_at BIGINT,
 created_at BIGINT NOT NULL,updated_at BIGINT NOT NULL,version BIGINT NOT NULL DEFAULT 1,PRIMARY KEY(tenant_id,id)
);
CREATE TABLE print_job (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,printer_id TEXT NOT NULL,template_id TEXT NOT NULL,
 owner_type TEXT NOT NULL,owner_id TEXT NOT NULL,original_job_id TEXT,rendered_payload TEXT NOT NULL,copies INTEGER NOT NULL CHECK(copies>0),
 status TEXT NOT NULL DEFAULT 'queued',error TEXT,retry_count INTEGER NOT NULL DEFAULT 0,requested_by TEXT NOT NULL,requested_at BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE final_inspection (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,inspection_no TEXT NOT NULL,work_order_id TEXT NOT NULL,bundle_id TEXT NOT NULL,
 inspected_qty BIGINT NOT NULL CHECK(inspected_qty>0),passed_qty BIGINT NOT NULL DEFAULT 0 CHECK(passed_qty>=0),
 rework_qty BIGINT NOT NULL DEFAULT 0 CHECK(rework_qty>=0),scrapped_qty BIGINT NOT NULL DEFAULT 0 CHECK(scrapped_qty>=0),
 inspector_employee_id TEXT NOT NULL,inspection_date BIGINT NOT NULL,handling_result TEXT,status TEXT NOT NULL DEFAULT 'pending',
 original_inspection_id TEXT,remark TEXT,created_at BIGINT NOT NULL,created_by TEXT NOT NULL,created_node TEXT NOT NULL,version BIGINT NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,inspection_no),CHECK(inspected_qty=passed_qty+rework_qty+scrapped_qty),
 FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id),FOREIGN KEY(tenant_id,bundle_id) REFERENCES bundle(tenant_id,id)
);
CREATE TABLE defect_record (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,owner_type TEXT NOT NULL,owner_id TEXT NOT NULL,defect_code TEXT NOT NULL,
 qty BIGINT NOT NULL CHECK(qty>0),description TEXT,responsibility_process_id TEXT,attachment_ids JSONB NOT NULL DEFAULT '[]',created_at BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE rework_order (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,rework_no TEXT NOT NULL,inspection_id TEXT NOT NULL,defect_record_id TEXT,
 task_id TEXT,responsibility_process_id TEXT,employee_id TEXT,qty BIGINT NOT NULL CHECK(qty>0),due_at BIGINT,status TEXT NOT NULL DEFAULT 'pending',
 reason TEXT NOT NULL,created_at BIGINT NOT NULL,version BIGINT NOT NULL DEFAULT 1,PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,rework_no),
 FOREIGN KEY(tenant_id,inspection_id) REFERENCES final_inspection(tenant_id,id)
);
CREATE TABLE reinspection (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,rework_order_id TEXT NOT NULL,inspected_qty BIGINT NOT NULL,
 passed_qty BIGINT NOT NULL,rework_qty BIGINT NOT NULL,scrapped_qty BIGINT NOT NULL,inspector_employee_id TEXT NOT NULL,
 inspection_id TEXT,remark TEXT,created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id),
 CHECK(inspected_qty=passed_qty+rework_qty+scrapped_qty),FOREIGN KEY(tenant_id,rework_order_id) REFERENCES rework_order(tenant_id,id)
);
CREATE TABLE finished_receipt (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,receipt_no TEXT NOT NULL,work_order_id TEXT NOT NULL,warehouse_id TEXT NOT NULL,
 receipt_date BIGINT NOT NULL,status TEXT NOT NULL DEFAULT 'draft',created_employee_id TEXT NOT NULL,
 quality_confirmed_by TEXT,quality_confirmed_at BIGINT,reviewed_by TEXT,reviewed_at BIGINT,remark TEXT,
 created_at BIGINT NOT NULL,created_by TEXT NOT NULL,created_node TEXT NOT NULL,version BIGINT NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,receipt_no),FOREIGN KEY(tenant_id,work_order_id) REFERENCES work_order(tenant_id,id)
);
CREATE TABLE finished_receipt_line (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,receipt_id TEXT NOT NULL,bundle_id TEXT NOT NULL,inspection_id TEXT NOT NULL,
 color TEXT NOT NULL,size TEXT NOT NULL,qty BIGINT NOT NULL CHECK(qty>0),available_snapshot BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,receipt_id) REFERENCES finished_receipt(tenant_id,id),
 FOREIGN KEY(tenant_id,bundle_id) REFERENCES bundle(tenant_id,id),FOREIGN KEY(tenant_id,inspection_id) REFERENCES final_inspection(tenant_id,id)
);
CREATE TABLE shipment (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,shipment_no TEXT NOT NULL,project_id TEXT NOT NULL,customer_id TEXT NOT NULL,
 shipment_date BIGINT NOT NULL,operator_employee_id TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'draft',remark TEXT,
 created_at BIGINT NOT NULL,created_by TEXT NOT NULL,created_node TEXT NOT NULL,version BIGINT NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,shipment_no),FOREIGN KEY(tenant_id,project_id) REFERENCES project(tenant_id,id)
);
CREATE TABLE shipment_line (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,shipment_id TEXT NOT NULL,work_order_id TEXT NOT NULL,bundle_id TEXT,
 color TEXT NOT NULL,size TEXT NOT NULL,qty BIGINT NOT NULL CHECK(qty>0),PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,shipment_id) REFERENCES shipment(tenant_id,id)
);
CREATE TABLE inventory_ledger (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,warehouse_id TEXT NOT NULL,inventory_type TEXT NOT NULL,
 source_type TEXT NOT NULL,source_id TEXT NOT NULL,material_id TEXT,bundle_id TEXT,qty_delta NUMERIC(20,6) NOT NULL CHECK(qty_delta<>0),
 occurred_at BIGINT NOT NULL,reversal_of_id TEXT,created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,source_type,source_id)
);
CREATE TABLE payroll_sheet (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,employee_id TEXT NOT NULL,payroll_period TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'draft',
 attendance_days NUMERIC(20,6) NOT NULL DEFAULT 0,base_salary NUMERIC(20,6) NOT NULL DEFAULT 0,
 gross_amount NUMERIC(20,6) NOT NULL DEFAULT 0,net_amount NUMERIC(20,6) NOT NULL DEFAULT 0,
 paid_amount NUMERIC(20,6) NOT NULL DEFAULT 0,outstanding_amount NUMERIC(20,6) NOT NULL DEFAULT 0,
 source_snapshot_hash TEXT NOT NULL,confirmed_by TEXT,confirmed_at BIGINT,created_at BIGINT NOT NULL,created_by TEXT NOT NULL,
 created_node TEXT NOT NULL,updated_at BIGINT NOT NULL,updated_by TEXT NOT NULL,updated_node TEXT NOT NULL,version BIGINT NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,employee_id,payroll_period,version)
);
CREATE TABLE payroll_item (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,payroll_sheet_id TEXT NOT NULL,item_type TEXT NOT NULL,
 item_code TEXT NOT NULL,item_name TEXT NOT NULL,qty NUMERIC(20,6),amount NUMERIC(20,6) NOT NULL,source_reference TEXT,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,payroll_sheet_id,item_type,item_code),
 FOREIGN KEY(tenant_id,payroll_sheet_id) REFERENCES payroll_sheet(tenant_id,id)
);
CREATE TABLE payroll_adjustment (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,payroll_sheet_id TEXT NOT NULL,adjustment_type TEXT NOT NULL,
 amount NUMERIC(20,6) NOT NULL,reason TEXT NOT NULL,source_entry_id TEXT,target_period TEXT NOT NULL,operator_id TEXT NOT NULL,
 created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id)
);
CREATE TABLE payment (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,payroll_sheet_id TEXT NOT NULL,amount NUMERIC(20,6) NOT NULL CHECK(amount>0),
 paid_at BIGINT NOT NULL,payment_method TEXT NOT NULL,operator_employee_id TEXT NOT NULL,external_reference TEXT,
 reversal_of_id TEXT,remark TEXT,created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id)
);
CREATE TABLE progress_projection (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,dimension TEXT NOT NULL,dimension_id TEXT NOT NULL,
 planned_qty BIGINT NOT NULL DEFAULT 0,approved_good_qty BIGINT NOT NULL DEFAULT 0,pending_qty BIGINT NOT NULL DEFAULT 0,
 completed_task_count BIGINT NOT NULL DEFAULT 0,task_count BIGINT NOT NULL DEFAULT 0,progress_ratio NUMERIC(20,6) NOT NULL DEFAULT 0,
 last_event_id TEXT,version BIGINT NOT NULL DEFAULT 1,PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,dimension,dimension_id)
);
CREATE TABLE daily_statistic_projection (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,statistic_type TEXT NOT NULL,business_date TEXT NOT NULL,
 group_type TEXT NOT NULL,group_id TEXT NOT NULL,qty NUMERIC(20,6) NOT NULL DEFAULT 0,seconds BIGINT NOT NULL DEFAULT 0,
 amount NUMERIC(20,6) NOT NULL DEFAULT 0,pending_qty BIGINT NOT NULL DEFAULT 0,last_event_id TEXT,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,statistic_type,business_date,group_type,group_id)
);
CREATE TABLE custom_field_definition (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,owner_type TEXT NOT NULL,field_key TEXT NOT NULL,label TEXT NOT NULL,
 data_type TEXT NOT NULL,required BOOLEAN NOT NULL DEFAULT FALSE,options JSONB NOT NULL DEFAULT '[]',display_scope JSONB NOT NULL DEFAULT '[]',
 permission_code TEXT,sort_no INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'active',version BIGINT NOT NULL DEFAULT 1,
 created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,owner_type,field_key)
);
CREATE TABLE custom_field_value (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,definition_id TEXT NOT NULL,owner_id TEXT NOT NULL,
 value_text TEXT,value_number NUMERIC(20,6),value_date BIGINT,value_bool BOOLEAN,version BIGINT NOT NULL DEFAULT 1,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,definition_id,owner_id)
);
CREATE TABLE sync_conflict (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,event_id TEXT NOT NULL,aggregate_type TEXT NOT NULL,aggregate_id TEXT NOT NULL,
 conflict_code TEXT NOT NULL,incoming_value JSONB,current_value JSONB,status TEXT NOT NULL DEFAULT 'open',resolution_type TEXT,
 resolution_value JSONB,resolved_by TEXT,resolved_at BIGINT,created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id)
);
CREATE TABLE sync_snapshot (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,node_id TEXT NOT NULL,schema_version INTEGER NOT NULL,center_seq BIGINT NOT NULL,
 storage_key TEXT NOT NULL,sha256 TEXT NOT NULL,attachment_manifest_key TEXT,status TEXT NOT NULL DEFAULT 'creating',verified_at BIGINT,
 created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id)
);
CREATE TABLE number_allocation (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,number_type TEXT NOT NULL,node_id TEXT NOT NULL,prefix TEXT NOT NULL,
 date_part TEXT NOT NULL,segment_start BIGINT NOT NULL,segment_end BIGINT NOT NULL,next_value BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,number_type,node_id,prefix,date_part,segment_start),CHECK(next_value>=segment_start AND next_value<=segment_end+1)
);
CREATE TABLE export_job (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,job_type TEXT NOT NULL,filter_snapshot JSONB NOT NULL,
 requested_by TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'queued',progress INTEGER NOT NULL DEFAULT 0,storage_key TEXT,expires_at BIGINT,
 error TEXT,created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id)
);
CREATE TABLE import_job (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,job_type TEXT NOT NULL,source_attachment_id TEXT NOT NULL,
 requested_by TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'uploaded',total_rows INTEGER NOT NULL DEFAULT 0,success_rows INTEGER NOT NULL DEFAULT 0,
 error_rows INTEGER NOT NULL DEFAULT 0,result_attachment_id TEXT,created_at BIGINT NOT NULL,PRIMARY KEY(tenant_id,id)
);
CREATE TABLE import_job_error (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,import_job_id TEXT NOT NULL,row_no INTEGER NOT NULL,field_key TEXT,
 error_code TEXT NOT NULL,message TEXT NOT NULL,raw_value TEXT,PRIMARY KEY(tenant_id,id)
);
CREATE TABLE backup_job (
 tenant_id TEXT NOT NULL REFERENCES tenant(id),id TEXT NOT NULL,node_id TEXT NOT NULL,backup_type TEXT NOT NULL,storage_key TEXT,
 manifest_sha256 TEXT,includes_attachments BOOLEAN NOT NULL DEFAULT TRUE,status TEXT NOT NULL DEFAULT 'running',
 started_at BIGINT NOT NULL,finished_at BIGINT,error TEXT,PRIMARY KEY(tenant_id,id)
);
CREATE INDEX ix_final_inspection_bundle ON final_inspection(tenant_id,bundle_id,inspection_date,id);
CREATE INDEX ix_finished_receipt_order ON finished_receipt(tenant_id,work_order_id,status,id);
CREATE INDEX ix_shipment_project ON shipment(tenant_id,project_id,shipment_date,id);
CREATE INDEX ix_ledger_stock ON inventory_ledger(tenant_id,inventory_type,material_id,bundle_id,occurred_at);
CREATE INDEX ix_piece_employee_period ON piecework_entry(tenant_id,employee_id,payroll_period,id);
CREATE INDEX ix_payroll_period ON payroll_sheet(tenant_id,payroll_period,status,id);
CREATE TRIGGER trg_ledger_append_only BEFORE UPDATE OR DELETE ON inventory_ledger FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER trg_payment_append_only BEFORE UPDATE OR DELETE ON payment FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER trg_adjustment_append_only BEFORE UPDATE OR DELETE ON payroll_adjustment FOR EACH ROW EXECUTE FUNCTION reject_mutation();
CREATE TRIGGER trg_inspection_no_delete BEFORE DELETE ON final_inspection FOR EACH ROW EXECUTE FUNCTION reject_mutation();
