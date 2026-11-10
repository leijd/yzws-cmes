-- v0.0.3.1：工序明细 F11 的数据范围权限点（全部 / 本组 / 本人）

INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('progress:detail_all','工序进度','查看全厂工序明细','read','工序明细不受组织限制，可溯源全部扎×工序×人员',FALSE,27),
('progress:detail_team','工序进度','查看本组工序明细','read','工序明细限本组员工（直属下级及同部门员工）',FALSE,28)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-'||template_id||'-detail-all', template_id, 'progress:detail_all', 1
FROM platform_role_template_permission WHERE permission_code='progress:read' AND template_id IN ('prt-factory-manager','prt-production-supervisor')
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-'||template_id||'-detail-team', template_id, 'progress:detail_team', 1
FROM platform_role_template_permission WHERE permission_code='progress:read' AND template_id='prt-group-leader'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-progress-detail-all','progress:detail_all','progress','detail_all' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-progress-detail-team','progress:detail_team','progress','detail_team' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-progress-detail-all',r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='progress:detail_all'
WHERE r.is_system OR r.code IN ('factory_manager','production_supervisor')
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-progress-detail-team',r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='progress:detail_team'
WHERE r.code='group_leader'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
