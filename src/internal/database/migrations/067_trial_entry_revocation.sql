-- 体验进入令牌吊销记录：退出登录（平台注销）时按（厂, 手机号）记下吊销时刻，
-- 校验进入令牌时若签发时间早于吊销时刻即视为失效。
-- 背景：进入令牌是无状态 JWT（30 天有效），原先退出登录只清浏览器本地副本，
-- 旧令牌仍可直接换取该厂工作台会话（输地址即可“以原用户身份”再进去）。
CREATE TABLE IF NOT EXISTS trial_entry_revocation (
  tenant_id TEXT NOT NULL REFERENCES tenant(id) ON DELETE CASCADE,
  phone_hash TEXT NOT NULL,
  revoked_at BIGINT NOT NULL,
  PRIMARY KEY (tenant_id, phone_hash)
);
COMMENT ON TABLE trial_entry_revocation IS '体验进入令牌吊销记录（退出登录后旧令牌立即失效）';
COMMENT ON COLUMN trial_entry_revocation.tenant_id IS '制衣厂租户 ID';
COMMENT ON COLUMN trial_entry_revocation.phone_hash IS '手机号盲索引（sha256），不落明文';
COMMENT ON COLUMN trial_entry_revocation.revoked_at IS '吊销时刻（微秒）：签发时间早于该时刻的进入令牌一律失效';
