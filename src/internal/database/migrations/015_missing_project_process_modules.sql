INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-process','mod-menu-products','menu','menu.process','工序库','/products/B01','process:read',23,1),
('mod-process-write','mod-menu-process','button','button.process.write','维护工序',NULL,'process:write',24,1),
('mod-menu-project','mod-menu-orders','menu','menu.project','客户项目','/orders/C03','project:read',29,1),
('mod-project-write','mod-menu-project','button','button.project.write','维护项目',NULL,'project:write',35,1)
ON CONFLICT(code) DO NOTHING;
