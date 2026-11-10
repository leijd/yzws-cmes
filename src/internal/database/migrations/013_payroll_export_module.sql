INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-payroll-export','mod-menu-payroll','button','button.payroll.export','导出工资',NULL,'payroll:export',83,1)
ON CONFLICT(code) DO NOTHING;
