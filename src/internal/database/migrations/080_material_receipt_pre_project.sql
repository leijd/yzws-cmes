-- 物料入库前置流程调整：来料入库时可能尚未建立项目，改由客户 + 产品名称/款号/款式标识。
-- 仓库、供应商支持在入库单窗口内快速新增，其主数据接口本身不变。
ALTER TABLE material_receipt ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE material_receipt ADD COLUMN IF NOT EXISTS customer_id TEXT;
ALTER TABLE material_receipt ADD COLUMN IF NOT EXISTS product_name TEXT;
ALTER TABLE material_receipt ADD COLUMN IF NOT EXISTS style_no TEXT;
ALTER TABLE material_receipt ADD COLUMN IF NOT EXISTS style_name TEXT;

CREATE INDEX IF NOT EXISTS ix_material_receipt_customer ON material_receipt(tenant_id, customer_id);
