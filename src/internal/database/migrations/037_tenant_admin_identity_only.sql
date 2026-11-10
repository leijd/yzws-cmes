-- v0.0.3.1：租户管理员仅保留“用户/角色/权限”管理，不再拥有业务功能权限
-- 说明：036 曾为 tenant_admin 授予全部权限，本迁移将其收敛回身份类权限。

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-identity-read','identity:read','identity','read' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-identity-write','identity:write','identity','write' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-identity-read',r.id,p.id
FROM role r
JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='identity:read'
WHERE r.code='tenant_admin'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-identity-write',r.id,p.id
FROM role r
JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='identity:write'
WHERE r.code='tenant_admin'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

DELETE FROM role_permission rp
USING role r, permission p
WHERE rp.tenant_id=r.tenant_id AND rp.role_id=r.id
  AND p.tenant_id=r.tenant_id AND p.id=rp.permission_id
  AND r.code='tenant_admin'
  AND p.code NOT IN ('identity:read','identity:write');
