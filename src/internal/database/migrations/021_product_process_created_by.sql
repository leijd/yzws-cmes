ALTER TABLE product_process ADD COLUMN created_by TEXT;
COMMENT ON COLUMN product_process.created_by IS '该工序路线版本的配置人（用户账号ID），保存产品工序时写入';
