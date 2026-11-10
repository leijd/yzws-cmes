-- v0.0.3.1：唛架 C11 与工单审核/A-B 模式的权限点、菜单与历史数据回填

INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('marker:read','物料唛架','查看唛架表','read','查看唛架、配比、用料估算和引用的裁床单',FALSE,45),
('marker:write','物料唛架','维护唛架表','write','新增、编辑、提交审核和删除待审核唛架',FALSE,46),
('marker:review','物料唛架','审核唛架表','action','审核通过或驳回唛架，通过后方可被裁床单引用',FALSE,47),
('work_order:review','项目与工单','审核工单','action','工单审核通过或驳回，通过后方可排产和报工',FALSE,31)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-marker','mod-menu-cutting','menu','menu.marker','唛架表','/orders/C11','marker:read',36,1),
('mod-marker-write','mod-menu-marker','button','button.marker.write','维护唛架',NULL,'marker:write',37,1),
('mod-marker-review','mod-menu-marker','button','button.marker.review','审核唛架',NULL,'marker:review',38,1),
('mod-work-order-review','mod-menu-orders','button','button.work_order.review','审核工单',NULL,'work_order:review',31,1)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-'||template_id||'-marker-read', template_id, 'marker:read', 1
FROM platform_role_template_permission WHERE permission_code='cutting:read'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-'||template_id||'-marker-write', template_id, 'marker:write', 1
FROM platform_role_template_permission WHERE permission_code='cutting:write'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-'||template_id||'-marker-review', template_id, 'marker:review', 1
FROM platform_role_template_permission WHERE permission_code='cutting:write'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at)
SELECT 'prtp-'||template_id||'-work-order-review', template_id, 'work_order:review', 1
FROM platform_role_template_permission WHERE permission_code='work_order:release'
ON CONFLICT(template_id,permission_code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-marker-read','marker:read','marker','read' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-marker-write','marker:write','marker','write' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-marker-review','marker:review','marker','review' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-work-order-review','work_order:review','work_order','review' FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT rp.tenant_id,'rp-'||rp.role_id||'-marker-read',rp.role_id,np.id
FROM role_permission rp
JOIN permission op ON op.tenant_id=rp.tenant_id AND op.id=rp.permission_id AND op.code='cutting:read'
JOIN permission np ON np.tenant_id=rp.tenant_id AND np.code='marker:read'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT rp.tenant_id,'rp-'||rp.role_id||'-marker-write',rp.role_id,np.id
FROM role_permission rp
JOIN permission op ON op.tenant_id=rp.tenant_id AND op.id=rp.permission_id AND op.code='cutting:write'
JOIN permission np ON np.tenant_id=rp.tenant_id AND np.code='marker:write'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT rp.tenant_id,'rp-'||rp.role_id||'-marker-review',rp.role_id,np.id
FROM role_permission rp
JOIN permission op ON op.tenant_id=rp.tenant_id AND op.id=rp.permission_id AND op.code='cutting:write'
JOIN permission np ON np.tenant_id=rp.tenant_id AND np.code='marker:review'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT rp.tenant_id,'rp-'||rp.role_id||'-work-order-review',rp.role_id,np.id
FROM role_permission rp
JOIN permission op ON op.tenant_id=rp.tenant_id AND op.id=rp.permission_id AND op.code='work_order:release'
JOIN permission np ON np.tenant_id=rp.tenant_id AND np.code='work_order:review'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-marker-read',r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='marker:read'
WHERE r.is_system
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-marker-write',r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='marker:write'
WHERE r.is_system
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-marker-review',r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='marker:review'
WHERE r.is_system
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-work-order-review',r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='work_order:review'
WHERE r.is_system
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

UPDATE work_order SET review_status='approved', reviewed_at=COALESCE(released_at,updated_at)
WHERE status<>'pending_release' AND review_status='pending';

UPDATE work_order w SET process_plan_locked=TRUE
WHERE EXISTS (
  SELECT 1 FROM production_task t JOIN work_report wr ON wr.tenant_id=t.tenant_id AND wr.task_id=t.id
  WHERE t.tenant_id=w.tenant_id AND t.work_order_id=w.id AND wr.report_kind<>'reversal'
);
