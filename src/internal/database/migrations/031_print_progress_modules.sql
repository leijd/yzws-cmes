-- v0.0.3.1：打印与工序明细权限点的功能模块登记，使角色可以分配这些权限

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-print-read','mod-menu-orders','button','button.print.read','查看菲票打印',NULL,'print:read',39,1),
('mod-print-write','mod-menu-orders','button','button.print.write','菲票打印与补打',NULL,'print:write',40,1),
('mod-progress-detail-all','mod-menu-orders','button','button.progress.detail_all','查看全厂工序明细',NULL,'progress:detail_all',41,1),
('mod-progress-detail-team','mod-menu-orders','button','button.progress.detail_team','查看本组工序明细',NULL,'progress:detail_team',42,1)
ON CONFLICT(code) DO NOTHING;
