-- 制衣厂门户 logo 文件：管理员上传的 PNG/GIF 直接存库，经公开接口按厂编码输出，
-- 避免依赖静态文件目录；未上传时仍使用 tenant_portal.logo_url（图片网址）或平台默认 logo。
ALTER TABLE tenant_portal ADD COLUMN IF NOT EXISTS logo_data BYTEA;
ALTER TABLE tenant_portal ADD COLUMN IF NOT EXISTS logo_content_type TEXT;
COMMENT ON COLUMN tenant_portal.logo_data IS '本厂上传的 logo 图片二进制（PNG/GIF，≤500KB）';
COMMENT ON COLUMN tenant_portal.logo_content_type IS '本厂上传 logo 的 MIME 类型（image/png 或 image/gif）';
