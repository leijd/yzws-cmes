-- 修复车工报工权限：报工页需要读取工单/扎信息（模式 A 按扎报工、模式 B 显示工单上下文）
INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-wk-101', t.id, 'work_order:read', 1
FROM platform_role_template t
WHERE t.code='worker'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-work-order-read','work_order:read','work_order','read' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-work-order-read',r.id,p.id
FROM role r
JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='work_order:read'
WHERE r.source='platform' AND r.code='worker'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
