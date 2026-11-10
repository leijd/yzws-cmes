-- tenant_crypto_key 支持平台级密钥主体（设计 3.4）：tenant_id 为 'platform' 表示平台级敏感数据
-- （visitor_lead 等无租户归属的运营数据）的独立密钥主体，不借用任何制衣厂密钥。
-- 原 FK 仅允许 tenant(id)，移除后由应用层保证主体合法性（迁移只增不改，原约束见 043）。
ALTER TABLE tenant_crypto_key DROP CONSTRAINT IF EXISTS tenant_crypto_key_tenant_id_fkey;
COMMENT ON COLUMN tenant_crypto_key.tenant_id IS '密钥主体：制衣厂 id 或 platform（平台级，设计 3.4）；应用层保证主体合法性';
