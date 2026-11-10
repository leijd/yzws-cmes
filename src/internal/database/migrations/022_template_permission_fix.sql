INSERT INTO platform_role_template(id,code,name,description,sort_no,created_at) VALUES
('prt-process-technician','process_technician','生产工艺员','维护工序库和产品工序资料，上传产品图片',45,1)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-pt-'||replace(v.code,':','-'), t.id, v.code, 1
FROM platform_role_template t CROSS JOIN (VALUES ('product:read'),('process:read'),('process:write'),('attachment:read'),('attachment:write')) v(code)
WHERE t.code='process_technician'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-fm-101','prt-factory-manager','project:read',1),
('prtp-fm-102','prt-factory-manager','project:write',1),
('prtp-fm-103','prt-factory-manager','cutting:read',1),
('prtp-fm-104','prt-factory-manager','cutting:write',1),
('prtp-fm-105','prt-factory-manager','work_order:release',1),
('prtp-fm-106','prt-factory-manager','task:assign',1),
('prtp-fm-107','prt-factory-manager','review:approve',1),
('prtp-fm-108','prt-factory-manager','review:correct',1),
('prtp-fm-109','prt-factory-manager','report:reverse',1),
('prtp-fm-110','prt-factory-manager','statistics:read',1),
('prtp-fm-111','prt-factory-manager','masterdata:read',1),
('prtp-fm-112','prt-factory-manager','masterdata:write',1),
('prtp-fm-113','prt-factory-manager','attachment:read',1),
('prtp-fm-114','prt-factory-manager','attachment:write',1),
('prtp-ps-101','prt-production-supervisor','workbench:read',1),
('prtp-ps-102','prt-production-supervisor','task:transfer',1),
('prtp-ps-103','prt-production-supervisor','statistics:read',1),
('prtp-ps-104','prt-production-supervisor','masterdata:read',1),
('prtp-ps-105','prt-production-supervisor','cutting:read',1),
('prtp-gl-101','prt-group-leader','task:transfer',1),
('prtp-gl-102','prt-group-leader','masterdata:read',1),
('prtp-pf-101','prt-payroll-finance','statistics:read',1),
('prtp-pf-102','prt-payroll-finance','masterdata:read',1),
('prtp-wh-101','prt-warehouse','masterdata:read',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||replace(p.code,':','-'),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN platform_permission p
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||replace(pm.code,':','-'),r.id,pm.id
FROM role r
JOIN platform_role_template t ON t.code=r.code AND r.source='platform'
JOIN platform_role_template_permission tp ON tp.template_id=t.id
JOIN permission pm ON pm.tenant_id=r.tenant_id AND pm.code=tp.permission_code
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
