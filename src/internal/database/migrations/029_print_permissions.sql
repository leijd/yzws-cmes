-- v0.0.3.1：菲票打印与补打的权限点

INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('print:read','菲票打印','查看打印机、模板与打印任务','read','查看打印机、模板、打印任务及回执',FALSE,55),
('print:write','菲票打印','发起打印、补打与登记回执','write','发起菲票打印、登记设备回执、单张补打',FALSE,56)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-'||template_id||'-print-read', template_id, 'print:read', 1
FROM platform_role_template_permission WHERE permission_code='work_order:read'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-'||template_id||'-print-write', template_id, 'print:write', 1
FROM platform_role_template_permission WHERE permission_code='work_order:write'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-print-read','print:read','print','read' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-print-write','print:write','print','write' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT rp.tenant_id,'rp-'||rp.role_id||'-print-read',rp.role_id,np.id
FROM role_permission rp
JOIN permission op ON op.tenant_id=rp.tenant_id AND op.id=rp.permission_id AND op.code='work_order:read'
JOIN permission np ON np.tenant_id=rp.tenant_id AND np.code='print:read'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT rp.tenant_id,'rp-'||rp.role_id||'-print-write',rp.role_id,np.id
FROM role_permission rp
JOIN permission op ON op.tenant_id=rp.tenant_id AND op.id=rp.permission_id AND op.code='work_order:write'
JOIN permission np ON np.tenant_id=rp.tenant_id AND np.code='print:write'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-print-read',r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='print:read'
WHERE r.is_system
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-print-write',r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='print:write'
WHERE r.is_system
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
