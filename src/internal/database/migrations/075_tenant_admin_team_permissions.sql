-- 生产组管理页面改由「组织资源」权限点控制（菜单=team:read，配置组员=team:write），
-- 不再依赖 identity:read/identity:write。063 只登记了权限点目录与角色模板，
-- 未给存量租户的 tenant_admin 角色补授权（071 有同样补授权动作，063 漏了），
-- 不补的话厂管理员反而看不到「生产组管理」菜单。本迁移补齐（幂等）。

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||substr(md5(p.id),1,8),r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id
WHERE r.code='tenant_admin'
  AND p.code IN ('team:read','team:write')
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
