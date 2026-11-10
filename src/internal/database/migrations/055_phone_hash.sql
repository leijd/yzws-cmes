-- 手机号盲索引（设计 3.6）：employee.phone_hash 与 tenant.contact_phone_hash。
-- 两库保存相同 HMAC 值；明文库仍存手机号明文，密文库手机号列存 enc: 密文。
-- 身份证/银行卡等本版不提供等值查询，不建盲索引。org/people/login 其余扩展见后续迁移。

ALTER TABLE employee ADD COLUMN IF NOT EXISTS phone_hash TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS contact_phone_hash TEXT;

COMMENT ON COLUMN employee.phone_hash IS '手机号盲索引：base64url(HMAC-SHA256(本厂盲索引密钥,"employee.phone\0"+规范化手机号))，两库同值';
COMMENT ON COLUMN tenant.contact_phone_hash IS '注册联系电话盲索引：域 "tenant.contact_phone"，供管理员手机号登录/多厂识别';

/* 租户内唯一（部分索引，NULL 不参与）：同厂员工手机号不得重复；登录与多厂匹配只查 hash */
CREATE UNIQUE INDEX IF NOT EXISTS ux_employee_phone_hash ON employee(tenant_id,phone_hash) WHERE phone_hash IS NOT NULL;
/* 每厂注册电话唯一 */
CREATE UNIQUE INDEX IF NOT EXISTS ux_tenant_contact_phone_hash ON tenant(id,contact_phone_hash) WHERE contact_phone_hash IS NOT NULL;
