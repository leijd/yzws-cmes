INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-pt-201', t.id, 'task:read', 1 FROM platform_role_template t WHERE t.code='process_technician'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-pt-202', t.id, 'report:create', 1 FROM platform_role_template t WHERE t.code='process_technician'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||replace(p.code,':','-'),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN (VALUES ('task:read'),('report:create')) p(code)
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||replace(p.code,':','-'),r.id,p.id
FROM role r JOIN platform_role_template t ON t.code=r.code AND r.source='platform'
JOIN platform_role_template_permission tp ON tp.template_id=t.id
JOIN permission p ON p.tenant_id=r.tenant_id AND p.code=tp.permission_code
WHERE r.code='process_technician'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
