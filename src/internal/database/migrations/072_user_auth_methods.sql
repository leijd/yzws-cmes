-- 登录验证方式（按用户账号配置）：短信验证码为基础验证，恒开且不允许关闭；
-- 「密码验证」「微信设备快捷登录」由本人或制衣厂管理员按账号开通/关闭。
-- 默认只保留短信验证：首次登录不再需要厂管理员提供的初始密码。
-- 两库同结构，存量账号由默认值补齐（PostgreSQL 快速默认值，不重写表）。

ALTER TABLE user_account ADD COLUMN IF NOT EXISTS auth_methods JSONB NOT NULL DEFAULT '{"sms":true,"password":false,"wechat":false}'::jsonb;

COMMENT ON COLUMN user_account.auth_methods IS '登录验证方式开关：sms 恒开（不可关闭）；password 密码验证；wechat 微信设备快捷登录。本人或厂管理员可调整。';

UPDATE user_account SET auth_methods = '{"sms":true,"password":false,"wechat":false}'::jsonb
WHERE auth_methods IS NULL OR auth_methods = '{}'::jsonb;

-- 微信设备快捷登录默认关闭：清理历史设备绑定，避免关闭后仍可凭已绑定设备扫码免短信登录。
-- 需要该方式的用户可自行或由厂管理员开通后重新绑定设备。
UPDATE user_account SET wechat_unionid = ''
WHERE COALESCE(wechat_unionid, '') <> ''
  AND NOT COALESCE((auth_methods->>'wechat')::boolean, FALSE);
