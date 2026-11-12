-- 063 补漏：给角色分配权限时，后端校验权限点必须存在于 platform_module
-- （production/identity.go：status='active' 且 is_admin_only=FALSE）。
-- 063 把 team:read / equipment:read 只写进了 platform_permission 权限点目录，
-- 漏注册 platform_module，导致给角色勾选“查看生产组/查看设备”保存时报
-- INVALID_PERMISSION（权限点不存在、已停用或仅限固定租户管理员使用）。本迁移补齐（幂等）。

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-team-read','mod-menu-production-line','button','button.team.read','查看生产组',NULL,'team:read',92,1),
('mod-equipment-read','mod-menu-workshop','button','button.equipment.read','查看设备',NULL,'equipment:read',93,1)
ON CONFLICT(code) DO NOTHING;
