-- 简单考勤域（设计 4.27）：扫描考勤屏二维码登记上下班。
-- 先刷新后扫、一码一次、服务端时间为准；票据哈希存储防枚举。
-- 幂等：全部 IF NOT EXISTS / ON CONFLICT DO NOTHING。

CREATE TABLE IF NOT EXISTS attendance_qr_ticket (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  ticket_hash TEXT NOT NULL,
  att_point TEXT NOT NULL,
  display_label TEXT,
  created_at BIGINT NOT NULL, expires_at BIGINT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','consumed','expired','superseded')),
  consumed_by TEXT, consumed_at BIGINT,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,ticket_hash)
);
CREATE INDEX IF NOT EXISTS ix_att_ticket_point ON attendance_qr_ticket(tenant_id,att_point,status);

CREATE TABLE IF NOT EXISTS attendance_record (
  tenant_id TEXT NOT NULL REFERENCES tenant(id), id TEXT NOT NULL,
  employee_id TEXT NOT NULL,
  att_type TEXT NOT NULL CHECK(att_type IN ('in','out')),
  att_at BIGINT NOT NULL,
  ticket_id TEXT NOT NULL,
  att_point TEXT NOT NULL,
  device_info TEXT,
  status TEXT NOT NULL DEFAULT 'normal' CHECK(status IN ('normal','void')),
  remark TEXT,
  created_at BIGINT NOT NULL, created_by TEXT NOT NULL, created_node TEXT NOT NULL,
  updated_at BIGINT NOT NULL, updated_by TEXT NOT NULL, updated_node TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,ticket_id),
  FOREIGN KEY(tenant_id,ticket_id) REFERENCES attendance_qr_ticket(tenant_id,id)
);
CREATE INDEX IF NOT EXISTS ix_att_emp ON attendance_record(tenant_id,employee_id,att_at);
CREATE INDEX IF NOT EXISTS ix_att_point ON attendance_record(tenant_id,att_point,att_at);

INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('attendance:record','简单考勤','扫码考勤','write','本人扫描考勤二维码登记上下班',FALSE,117),
('attendance:screen','简单考勤','考勤屏','action','考勤屏展示与刷新考勤二维码',FALSE,118),
('attendance:manage','简单考勤','考勤管理','read','查询、导出与作废考勤记录',FALSE,119)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-attendance',NULL,'menu','menu.attendance','简单考勤','/attendance/records','attendance:manage',117,1),
('mod-attendance-screen','mod-menu-attendance','api','api.attendance.screen','考勤屏','/attendance/screen','attendance:screen',118,1),
('mod-attendance-record','mod-menu-attendance','button','button.attendance.record','扫码考勤',NULL,'attendance:record',119,1)
ON CONFLICT(code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||substr(md5(p.code),1,8),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN platform_permission p
WHERE p.code IN ('attendance:record','attendance:screen','attendance:manage')
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||substr(md5(p.id),1,8),r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id
WHERE r.code='tenant_admin'
  AND p.code IN ('attendance:record','attendance:screen','attendance:manage')
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-wk-at1','prt-worker','attendance:record',1),
('prtp-fm-at1','prt-factory-manager','attendance:record',1),
('prtp-fm-at2','prt-factory-manager','attendance:manage',1),
('prtp-fm-at3','prt-factory-manager','attendance:screen',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;
