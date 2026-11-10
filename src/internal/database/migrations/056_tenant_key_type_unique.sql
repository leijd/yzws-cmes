-- 旧唯一约束 UNIQUE(tenant_id,key_version) 与每版本两类密钥材料（dek + 盲索引密钥）冲突，
-- 由 054 的 ux_tenant_key_type_version(tenant_id,wrap_key_type,key_version) 取代（迁移只增不改）。
ALTER TABLE tenant_crypto_key DROP CONSTRAINT IF EXISTS tenant_crypto_key_tenant_id_key_version_key;
