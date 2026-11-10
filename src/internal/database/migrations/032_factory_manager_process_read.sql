-- v0.0.3.1：为存量“厂长”角色补齐模板中已有的 process:read
-- 背景：员工档案的“工序专长”需要读取工序库；旧租户的 factory_manager 角色可能是在模板加入该权限之前创建的。

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-process-read','process:read','process','read' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-process-read',r.id,p.id
FROM role r
JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='process:read'
WHERE r.code='factory_manager'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-fm-process-read','prt-factory-manager','process:read',1
WHERE NOT EXISTS (
  SELECT 1 FROM platform_role_template_permission
  WHERE template_id='prt-factory-manager' AND permission_code='process:read'
);
