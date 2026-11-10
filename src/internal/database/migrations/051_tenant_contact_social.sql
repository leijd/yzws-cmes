-- 制衣厂注册辅助联系方式：除联系电话外，记录微信 / QQ / 支付宝，便于平台审批与后续联系。
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS contact_wechat TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS contact_qq TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS contact_alipay TEXT;
COMMENT ON COLUMN tenant.contact_wechat IS '辅助信息：联系人微信号';
COMMENT ON COLUMN tenant.contact_qq IS '辅助信息：联系人 QQ 号';
COMMENT ON COLUMN tenant.contact_alipay IS '辅助信息：联系人支付宝账号';
