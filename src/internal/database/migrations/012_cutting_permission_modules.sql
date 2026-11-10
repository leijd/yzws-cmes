INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-cutting','mod-menu-orders','menu','menu.cutting','裁床与分扎','/orders/C03','cutting:read',33,1),
('mod-cutting-write','mod-menu-cutting','button','button.cutting.write','维护裁床',NULL,'cutting:write',34,1)
ON CONFLICT(code) DO NOTHING;
