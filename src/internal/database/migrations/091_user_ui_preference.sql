/* 用户级 UI 偏好：按人员保存物料入库明细表格的列设置（顺序、显示、固定列） */
CREATE TABLE IF NOT EXISTS user_ui_preference (
  tenant_id TEXT NOT NULL REFERENCES tenant(id),
  user_id TEXT NOT NULL,
  pref_key TEXT NOT NULL,
  pref_value JSONB NOT NULL DEFAULT '{}',
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, user_id, pref_key)
);
