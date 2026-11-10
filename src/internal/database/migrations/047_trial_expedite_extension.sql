-- 体验厂加急与延期：开始体验 → 注册审批（可加急）→ 体验中可申请延期（平台审批并指定新截止时间）
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS expedite_requested_at BIGINT;

CREATE TABLE IF NOT EXISTS trial_extension (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenant(id),
  requested_phone TEXT NOT NULL,
  reason TEXT,
  status TEXT NOT NULL DEFAULT 'pending',
  new_trial_ends_at BIGINT,
  created_at BIGINT NOT NULL,
  reviewed_at BIGINT,
  reviewed_by TEXT,
  review_reason TEXT
);
CREATE INDEX IF NOT EXISTS idx_trial_extension_tenant ON trial_extension(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_trial_extension_status ON trial_extension(status, created_at DESC);
