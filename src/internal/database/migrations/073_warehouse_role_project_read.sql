-- 仓管/质检角色预置「查看客户项目」(project:read)。
-- 物料入库、质检与库存两个模块首屏都会读取 GET /api/v1/projects 作为项目下拉，
-- 该接口要求 project:read；角色缺少该权限时整页 Promise.all 失败并提示「没有执行此操作的权限」。
-- 本迁移只补齐角色授权数据，不改动页面与接口逻辑（维持原有代码）。
-- 两库同结构；全部语句幂等。

/* 每个租户补齐权限点记录（可被角色引用） */
INSERT INTO permission(tenant_id,id,code,resource,action)
SELECT t.id,'perm-'||t.id||'-'||substr(md5('project:read'),1,8),'project:read','project','read'
FROM tenant t
ON CONFLICT(tenant_id,code) DO NOTHING;

/* 平台角色模板：新租户初始化仓管/质检角色时即带上该权限 */
INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES
('prtp-wh-project-read','prt-warehouse','project:read',1)
ON CONFLICT(template_id,permission_code) DO NOTHING;

/* 存量租户：按模板编码或角色名称识别仓管/质检（库检）角色并补齐 */
INSERT INTO role_permission(tenant_id,id,role_id,permission_id)
SELECT r.tenant_id,'rp-'||r.id||'-'||substr(md5(p.code),1,8),r.id,p.id
FROM role r
JOIN permission p ON p.tenant_id=r.tenant_id AND p.code='project:read'
WHERE r.code IN ('warehouse','quality','quality_inspector')
   OR r.name LIKE '%仓管%' OR r.name LIKE '%质检%' OR r.name LIKE '%库检%' OR r.name LIKE '%仓库%'
ON CONFLICT(tenant_id,role_id,permission_id) DO NOTHING;
