-- 订单项目作为流程起点：项目表补充款号、款式；裁床单主表关联订单项目号。
-- 款号/款式可在订单项目中直接录入，便于下游裁床、物料入库引用。
ALTER TABLE project ADD COLUMN IF NOT EXISTS style_no TEXT;
ALTER TABLE project ADD COLUMN IF NOT EXISTS style_name TEXT;
ALTER TABLE cutting_sheet ADD COLUMN IF NOT EXISTS project_id TEXT;
