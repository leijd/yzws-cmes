UPDATE permission SET code='identity:read' WHERE code='identity' AND resource='identity' AND action='read';

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-identity-write','identity:write','identity','write' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-identity-read',r.id,p.id FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='identity:read' WHERE r.code='tenant_admin'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-identity-write',r.id,p.id FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='identity:write' WHERE r.code='tenant_admin'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

DELETE FROM role_permission rp
USING role r, permission p
WHERE rp.tenant_id=r.tenant_id AND rp.role_id=r.id AND rp.tenant_id=p.tenant_id AND rp.permission_id=p.id
  AND r.code='tenant_admin' AND p.code NOT IN ('identity:read','identity:write');
