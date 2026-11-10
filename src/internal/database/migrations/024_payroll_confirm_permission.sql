INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-pf-201','prt-payroll-finance','payroll:confirm',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-payroll-confirm','payroll:confirm','payroll','confirm' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-payroll-confirm',r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='payroll:confirm'
WHERE r.source='platform' AND r.code='payroll_finance'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
