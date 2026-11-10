-- 登录时验证过的手机号（短信/微信）：切换制衣厂门户时沿用同一手机号，
-- 避免切换到某厂后因该厂管理员档案手机号不同而丢失名下其他制衣厂。
ALTER TABLE auth_session ADD COLUMN IF NOT EXISTS login_phone TEXT;
COMMENT ON COLUMN auth_session.login_phone IS '登录时验证过的手机号（短信/微信），用于门户切换制衣厂';
