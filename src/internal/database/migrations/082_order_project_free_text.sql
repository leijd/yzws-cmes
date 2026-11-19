-- 订单项目解耦：客户/产品为自由文本，产品字典仅辅助输入；编号留空时后端自动生成。
ALTER TABLE project ADD COLUMN IF NOT EXISTS customer_name TEXT;
ALTER TABLE project ADD COLUMN IF NOT EXISTS product_name TEXT;
ALTER TABLE project ALTER COLUMN customer_id DROP NOT NULL;
ALTER TABLE project ALTER COLUMN product_id DROP NOT NULL;
