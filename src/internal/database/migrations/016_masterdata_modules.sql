INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-menu-masterdata',NULL,'menu','menu.masterdata','员工与客户','/masterdata/B06','masterdata:read',85,1),
('mod-masterdata-write','mod-menu-masterdata','button','button.masterdata.write','维护基础资料',NULL,'masterdata:write',86,1)
ON CONFLICT(code) DO NOTHING;
