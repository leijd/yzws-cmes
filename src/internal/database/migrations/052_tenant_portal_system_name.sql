-- 制衣厂系统名称：显示在工作页面 logo 旁边；留空则使用平台默认名称。
ALTER TABLE tenant_portal ADD COLUMN IF NOT EXISTS system_name TEXT;
COMMENT ON COLUMN tenant_portal.system_name IS '制衣厂系统名称（工作页面 logo 旁显示），留空使用平台默认名称';
