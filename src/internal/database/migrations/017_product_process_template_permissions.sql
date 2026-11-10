INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-fm-007','prt-factory-manager','product:read',1),
('prtp-fm-008','prt-factory-manager','product:write',1),
('prtp-fm-009','prt-factory-manager','process:read',1),
('prtp-fm-010','prt-factory-manager','process:write',1),
('prtp-ps-006','prt-production-supervisor','product:read',1),
('prtp-ps-007','prt-production-supervisor','process:read',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-product-read','product:read','product','read' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;
INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-product-write','product:write','product','write' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;
INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-process-read','process:read','process','read' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;
INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-process-write','process:write','process','write' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-product-read',r.id,p.id FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='product:read'
WHERE r.source='platform' AND r.code='factory_manager'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-product-write',r.id,p.id FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='product:write'
WHERE r.source='platform' AND r.code='factory_manager'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-process-read',r.id,p.id FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='process:read'
WHERE r.source='platform' AND r.code='factory_manager'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-process-write',r.id,p.id FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='process:write'
WHERE r.source='platform' AND r.code='factory_manager'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-product-read',r.id,p.id FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='product:read'
WHERE r.source='platform' AND r.code='production_supervisor'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-process-read',r.id,p.id FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='process:read'
WHERE r.source='platform' AND r.code='production_supervisor'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
