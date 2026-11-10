-- v0.0.3.1：内置“租户管理员”默认拥有全部功能权限
-- 说明：tenant_admin 为固定角色不可编辑；此前只有身份类权限，导致租户管理员看不到业务菜单。

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||substr(md5(p.code),1,8),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN platform_permission p
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||substr(md5(p.id),1,8),r.id,p.id
FROM role r
JOIN permission p ON p.tenant_id=r.tenant_id
WHERE r.code='tenant_admin'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
