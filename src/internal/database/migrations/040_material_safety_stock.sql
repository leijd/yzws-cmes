-- 物料安全库存：用于物料库存预警
ALTER TABLE material ADD COLUMN safety_stock NUMERIC(20,6) NOT NULL DEFAULT 0 CHECK(safety_stock >= 0);
COMMENT ON COLUMN material.safety_stock IS '安全库存；库存低于或等于该值时触发低库存预警';
