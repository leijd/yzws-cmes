/* 客户名称按租户唯一（忽略大小写与首尾空格） */
CREATE UNIQUE INDEX IF NOT EXISTS ux_customer_tenant_name ON customer(tenant_id, lower(trim(name)));
COMMENT ON INDEX ux_customer_tenant_name IS '租户内客户名称唯一（忽略大小写和首尾空格）';
