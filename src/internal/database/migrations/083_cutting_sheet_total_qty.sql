-- 裁床单主表新增「总件数」：可空，留空时由下级扎号明细件数合计回填
ALTER TABLE cutting_sheet ADD COLUMN IF NOT EXISTS total_qty BIGINT;
