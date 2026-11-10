-- tenant_crypto_key 扩展（设计 4.10 / 9.2 迁移 054）：
-- 非对称信封包装（RSA 公钥包装 DEK/盲索引密钥，私钥仅存于 HSM token / 云 KMS）、
-- 盲索引密钥独立行、key_provider 区分提供方。两库保存同一密钥版本及包装后密文，均不得保存明文密钥。

ALTER TABLE tenant_crypto_key ADD COLUMN IF NOT EXISTS wrap_key_type TEXT NOT NULL DEFAULT 'dek';
ALTER TABLE tenant_crypto_key ADD COLUMN IF NOT EXISTS key_provider TEXT NOT NULL DEFAULT 'local';
ALTER TABLE tenant_crypto_key ADD COLUMN IF NOT EXISTS public_key TEXT;

COMMENT ON COLUMN tenant_crypto_key.wrap_key_type IS '密钥材料类型：dek=数据加密密钥（AES-256-GCM）；index=手机号盲索引 HMAC 密钥';
COMMENT ON COLUMN tenant_crypto_key.key_provider IS '包装提供方：local（仅迁移）| softhsm2 | cloud_kms';
COMMENT ON COLUMN tenant_crypto_key.public_key IS 'RSA 公钥（base64 DER），用于本地包装路径；私钥永不落库，仅存在于 HSM token / 云 KMS';

/* 一厂一类型一版本唯一；盲索引密钥随 DEK 同版本发行 */
CREATE UNIQUE INDEX IF NOT EXISTS ux_tenant_key_type_version
  ON tenant_crypto_key(tenant_id,wrap_key_type,key_version);
CREATE INDEX IF NOT EXISTS ix_tenant_key_provider ON tenant_crypto_key(tenant_id,wrap_key_type,status);
