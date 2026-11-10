-- v0.0.4.1：制衣厂注册审批与订阅规模
-- 流程：制衣厂自助注册（registration_status=pending，尚无登录账号）
--       → 平台管理员确认（确认订阅版本 edition 与订阅规模 user_limit，
--         分配管理员账户与初始密码）→ 启用（approved，可登录业务主页）。
-- 说明：不改 tenant.status 的取值集合（避免 schema 自修复重复添加更严格的 CHECK），
--       注册审批状态由 registration_status 表达；未审批厂没有账号，无法登录。

ALTER TABLE tenant ADD COLUMN IF NOT EXISTS registration_status TEXT NOT NULL DEFAULT 'approved';
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS approved_at BIGINT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS approved_by TEXT;

ALTER TABLE tenant ADD CONSTRAINT ck_tenant_registration_status
  CHECK(registration_status IN ('pending','approved','rejected'));

-- 订阅状态表：协议可后补（确认订阅时可能尚未签订书面协议），增加订阅规模（启用用户数上限）
ALTER TABLE tenant_subscription ALTER COLUMN agreement_id DROP NOT NULL;
ALTER TABLE tenant_subscription ADD COLUMN IF NOT EXISTS user_limit INTEGER;

ALTER TABLE tenant_subscription ADD CONSTRAINT ck_tenant_subscription_user_limit
  CHECK(user_limit IS NULL OR user_limit > 0);
ALTER TABLE tenant_subscription ADD CONSTRAINT ck_tenant_subscription_edition
  CHECK(edition IN ('basic','standard','pro'));

COMMENT ON COLUMN tenant.registration_status IS '注册审批状态：pending 待平台确认 / approved 已启用 / rejected 已驳回';
COMMENT ON COLUMN tenant.approved_at IS '平台管理员确认启用时间（UTC 微秒）';
COMMENT ON COLUMN tenant.approved_by IS '确认启用的平台管理员';
COMMENT ON COLUMN tenant_subscription.user_limit IS '订阅规模：本厂启用用户数量上限（NULL 表示不限）';
