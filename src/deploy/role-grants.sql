-- 全部租户全部角色的授权数量（看租户管理员是否普遍缺权）
SELECT t.code AS tenant, r.name AS role, COUNT(rp.id) AS grants
FROM role r JOIN tenant t ON t.id = r.tenant_id
LEFT JOIN role_permission rp ON rp.tenant_id = r.tenant_id AND rp.role_id = r.id
GROUP BY t.code, r.name ORDER BY t.code, grants DESC;
-- 租户管理员角色具体持有哪些权限
SELECT t.code, p.code AS perm
FROM role r JOIN tenant t ON t.id = r.tenant_id
JOIN role_permission rp ON rp.tenant_id = r.tenant_id AND rp.role_id = r.id
JOIN permission p ON p.id = rp.permission_id
WHERE r.name = '租户管理员' ORDER BY t.code, p.code LIMIT 40;
