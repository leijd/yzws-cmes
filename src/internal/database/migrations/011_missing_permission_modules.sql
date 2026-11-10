INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-order-edit','mod-menu-orders','button','button.order.edit','编辑工单',NULL,'work_order:write',32,1),
('mod-task-transfer','mod-menu-tasks','button','button.task.transfer','任务转工',NULL,'task:transfer',43,1),
('mod-report-reverse','mod-menu-reports','button','button.report.reverse','冲销报工',NULL,'report:reverse',53,1),
('mod-review-correct','mod-menu-reports','button','button.review.correct','纠错审批',NULL,'review:correct',54,1),
('mod-payroll-confirm','mod-menu-payroll','button','button.payroll.confirm','确认工资',NULL,'payroll:confirm',82,1),
('mod-menu-statistics',NULL,'menu','menu.statistics','统计矩阵','/progress/F01','statistics:read',71,1)
ON CONFLICT(code) DO NOTHING;
