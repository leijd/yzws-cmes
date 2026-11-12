-- 组织资源模块权限点补全（设计 4.5）：从属关系——车间(1)有多个生产线、车间(1)有多个设备；
-- 生产线(1)有多个生产组（班组）。后端 API（internal/orgapp/workshops.go）已使用
-- workshop:/line: 权限点，但平台权限目录、platform_module 菜单与租户 permission 记录缺失，
-- 角色分配界面选不到这些模块。本迁移补齐目录与可分配记录；授予哪些角色由租户管理员决定。
-- equipment 表/API 尚未落地，仅注册权限点（预留）。

INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('workshop:read','组织资源','查看车间','read','浏览车间列表与详情',FALSE,84),
('workshop:write','组织资源','维护车间','write','新建、编辑、停用车间（产线与设备挂在车间下）',FALSE,85),
('line:read','组织资源','查看生产线','read','浏览产线列表、成员与生产组',FALSE,86),
('line:write','组织资源','维护生产线','write','新建、编辑、停用产线及其生产组（生产组挂在产线下）',FALSE,87),
('team:read','组织资源','查看生产组','read','浏览生产组（班组，挂在产线下）',FALSE,88),
('team:write','组织资源','维护生产组','write','新建、编辑生产组（班组）',FALSE,89),
('equipment:read','组织资源','查看设备','read','浏览设备（挂在车间下，预留）',FALSE,92),
('equipment:write','组织资源','维护设备','write','新建、编辑设备（挂在车间下，预留）',FALSE,93)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-workshop',NULL,'menu','menu.workshop','车间管理','/settings/workshops','workshop:read',86,1),
('mod-workshop-write','mod-menu-workshop','button','button.workshop.write','维护车间',NULL,'workshop:write',87,1),
('mod-equipment-write','mod-menu-workshop','button','button.equipment.write','维护设备',NULL,'equipment:write',88,1),
('mod-menu-production-line',NULL,'menu','menu.production.line','生产线管理','/settings/lines','line:read',89,1),
('mod-line-write','mod-menu-production-line','button','button.line.write','维护生产线',NULL,'line:write',90,1),
('mod-team-write','mod-menu-production-line','button','button.team.write','维护生产组',NULL,'team:write',91,1)
ON CONFLICT(code) DO NOTHING;

/* 每个已有租户补权限点记录（可被角色分配引用） */
INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||substr(md5(p.code),1,8),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN platform_permission p
WHERE p.code IN ('workshop:read','workshop:write','line:read','line:write','team:read','team:write','equipment:read','equipment:write')
ON CONFLICT(tenant_id,code) DO NOTHING;

/* 角色模板默认带组织资源读权限：厂长（全厂管理）可维护，生产主管只读 */
INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-fm-ws1','prt-factory-manager','workshop:read',1),('prtp-fm-ws2','prt-factory-manager','workshop:write',1),
('prtp-fm-ln1','prt-factory-manager','line:read',1),('prtp-fm-ln2','prt-factory-manager','line:write',1),
('prtp-fm-tm1','prt-factory-manager','team:read',1),('prtp-fm-tm2','prt-factory-manager','team:write',1),
('prtp-fm-eq1','prt-factory-manager','equipment:read',1),('prtp-fm-eq2','prt-factory-manager','equipment:write',1),
('prtp-ps-ws1','prt-production-supervisor','workshop:read',1),
('prtp-ps-ln1','prt-production-supervisor','line:read',1),('prtp-ps-ln2','prt-production-supervisor','line:write',1),
('prtp-ps-tm1','prt-production-supervisor','team:read',1),('prtp-ps-tm2','prt-production-supervisor','team:write',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;
