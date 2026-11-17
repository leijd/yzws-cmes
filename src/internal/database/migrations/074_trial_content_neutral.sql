-- 074_trial_content_neutral.sql
-- 门户体验内容去平台化：
-- ① 存量「解决方案标题」统一为「我们的解决方案」（前端与后端默认值已改，线上内容来自 platform_setting 已保存版本）；
-- ② 清理已保存文案（欢迎标题/欢迎词/解决方案文案）中的平台产品名与服务商信息。
-- 只追加版本记录的 platform_setting 直接更新对应版本的 JSON，读取方取最新版本。

-- ① 解决方案标题
UPDATE platform_setting
SET setting_value = jsonb_set(setting_value, '{solution_title}', '"我们的解决方案"'::jsonb, true)
WHERE setting_key = 'trial_page_content'
  AND COALESCE(setting_value ->> 'solution_title', '') <> '我们的解决方案';

-- ② 欢迎标题：去平台产品名
UPDATE platform_setting
SET setting_value = jsonb_set(setting_value, '{welcome_title}',
  to_jsonb(replace(replace(replace(COALESCE(setting_value ->> 'welcome_title', ''),
    '一智算制衣 MES', '生产管理系统'), '一智算', ''), '一智万商', '')), true)
WHERE setting_key = 'trial_page_content'
  AND (setting_value ->> 'welcome_title' LIKE '%一智算%' OR setting_value ->> 'welcome_title' LIKE '%一智万商%');

-- ② 欢迎词：去平台产品名/服务商名
UPDATE platform_setting
SET setting_value = jsonb_set(setting_value, '{welcome_text}',
  to_jsonb(replace(replace(COALESCE(setting_value ->> 'welcome_text', ''), '一智算', ''), '一智万商', '')), true)
WHERE setting_key = 'trial_page_content'
  AND (setting_value ->> 'welcome_text' LIKE '%一智算%' OR setting_value ->> 'welcome_text' LIKE '%一智万商%');

-- ② 解决方案文案：去平台产品名/服务商名
UPDATE platform_setting
SET setting_value = jsonb_set(setting_value, '{solution_text}',
  to_jsonb(replace(replace(COALESCE(setting_value ->> 'solution_text', ''), '一智算', ''), '一智万商', '')), true)
WHERE setting_key = 'trial_page_content'
  AND (setting_value ->> 'solution_text' LIKE '%一智算%' OR setting_value ->> 'solution_text' LIKE '%一智万商%');
