INSERT INTO platform_permission(code,module,name,kind,description,admin_only,sort_no) VALUES
('attachment:read','产品与工序','查看产品图片','read','浏览和下载产品图片等附件',FALSE,22)
ON CONFLICT(code) DO NOTHING;

INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,created_at) VALUES
('mod-attachment-read','mod-menu-products','button','button.attachment.read','查看产品图片',NULL,'attachment:read',26,1)
ON CONFLICT(code) DO NOTHING;
