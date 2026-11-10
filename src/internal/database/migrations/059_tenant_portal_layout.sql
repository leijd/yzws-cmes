-- 厂长门户布局（设计 4.11/6.5）：风格与板块；员工门户固定模板不使用本表配置。
ALTER TABLE tenant_portal ADD COLUMN IF NOT EXISTS style_code TEXT NOT NULL DEFAULT 'classic';  -- classic/dark/fresh，仅厂长门户
ALTER TABLE tenant_portal ADD COLUMN IF NOT EXISTS sections JSONB;                              -- 板块勾选/顺序/文案，NULL=平台默认
COMMENT ON COLUMN tenant_portal.style_code IS '厂长门户风格：classic/dark/fresh；员工门户固定不使用';
COMMENT ON COLUMN tenant_portal.sections IS '厂长门户板块：welcome/overview/entries/progress 的选择、顺序与文案（JSONB 数组）';
