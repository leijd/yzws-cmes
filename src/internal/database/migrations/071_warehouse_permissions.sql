-- 仓库管理（物料出入库）权限点：此前物料/仓库基础资料与出入库接口复用
-- masterdata:read/write、work_order:read/write，角色权限配置界面选不到「仓库管理」模块，
-- 也无法单独授予/收回出入库权限。本迁移补齐目录、功能模块、模板与存量租户授权。
-- 与 019/063 口径一致：权限码形如 资源:动作，kind 限 read/write/action。

INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('warehouse:read','仓库管理','查看仓库与物料','read','浏览仓库、物料资料与库存台账',FALSE,94),
('warehouse:write','仓库管理','维护仓库与物料','write','新建、编辑、停用仓库与物料，设置安全库存',FALSE,95),
('inventory:read','仓库管理','查看库存与出入库','read','浏览库存汇总、库存流水与物料出入库单据',FALSE,96),
('inventory:receipt','仓库管理','物料入库','write','登记、提交、冲销物料入库单与来料质检',FALSE,97),
('inventory:issue','仓库管理','物料出库','write','登记、发放、冲销物料出库单',FALSE,98)
ON CONFLICT(code) DO NOTHING;

-- 角色/模板保存权限时按 platform_module 校验权限点（status='active' 且 is_admin_only=FALSE），
-- 每个新权限点至少注册一条记录，否则勾选后保存会报 INVALID_PERMISSION（见 064 补漏记录）。
INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-warehouse',NULL,'menu','menu.warehouse','仓库管理','/materials/master','warehouse:read',94,1),
('mod-warehouse-write','mod-menu-warehouse','button','button.warehouse.write','维护仓库与物料',NULL,'warehouse:write',95,1),
('mod-inventory-read','mod-menu-warehouse','api','api.inventory.read','查看库存与出入库',NULL,'inventory:read',96,1),
('mod-inventory-receipt','mod-menu-warehouse','api','api.inventory.receipt','物料入库',NULL,'inventory:receipt',97,1),
('mod-inventory-issue','mod-menu-warehouse','api','api.inventory.issue','物料出库',NULL,'inventory:issue',98,1)
ON CONFLICT(code) DO NOTHING;

/* 存量租户补齐 permission 记录（可被角色分配引用） */
INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||substr(md5(p.code),1,8),p.code,split_part(p.code,':',1),split_part(p.code,':',2)
FROM tenant t CROSS JOIN platform_permission p
WHERE p.code IN ('warehouse:read','warehouse:write','inventory:read','inventory:receipt','inventory:issue')
ON CONFLICT(tenant_id,code) DO NOTHING;

/* 平滑过渡：原来靠 masterdata/work_order 权限访问仓库功能的角色，自动获得对应的新权限，
 * 避免升级后仓管角色突然打不开物料页面。租户管理员补齐全部新权限。 */
WITH map(old_code,new_code) AS (VALUES
  ('masterdata:read','warehouse:read'),
  ('masterdata:read','inventory:read'),
  ('masterdata:write','warehouse:write'),
  ('work_order:read','inventory:read'),
  ('work_order:write','inventory:receipt'),
  ('work_order:write','inventory:issue')
)
INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT DISTINCT rp.tenant_id,'rp-'||rp.role_id||'-'||substr(md5(np.code),1,8),rp.role_id,np.id
FROM role_permission rp
JOIN permission op ON op.tenant_id=rp.tenant_id AND op.id=rp.permission_id
JOIN map m ON m.old_code=op.code
JOIN permission np ON np.tenant_id=rp.tenant_id AND np.code=m.new_code
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||substr(md5(p.id),1,8),r.id,p.id
FROM role r JOIN permission p ON p.tenant_id=r.tenant_id
WHERE r.code='tenant_admin'
  AND p.code IN ('warehouse:read','warehouse:write','inventory:read','inventory:receipt','inventory:issue')
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;

/* 平台角色模板：仓管/质检默认具备完整仓库权限，厂长只读仓库与库存 */
INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-wh-wr','prt-warehouse','warehouse:read',1),
('prtp-wh-ww','prt-warehouse','warehouse:write',1),
('prtp-wh-ir','prt-warehouse','inventory:read',1),
('prtp-wh-rec','prt-warehouse','inventory:receipt',1),
('prtp-wh-iss','prt-warehouse','inventory:issue',1),
('prtp-fm-wr','prt-factory-manager','warehouse:read',1),
('prtp-fm-ir','prt-factory-manager','inventory:read',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;
