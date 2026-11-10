-- v0.0.4.0：厂商（制衣厂）域数据库设计落地
-- 依据：req/20261107-1-数据库设计.docx 第 4/5 章 + req/20261107-讨论.txt
-- 范围：制衣厂主表扩展、订阅协议/订阅费/订阅状态、系统版本发布、
--       访客/潜在客户（游客→体验→付费转化）、制衣厂数据加密密钥表。
-- 约定：主键 TEXT(UUIDv7)；时间 BIGINT(UTC 微秒)；金额 NUMERIC(20,6)；布尔 BOOLEAN。
--       平台运营表不含 tenant_id 隔离键（订阅类表含 tenant_id 仅为引用键）。

-- ===== 5.1 tenant 制衣厂主表（扩展）=====
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS customer_type TEXT NOT NULL DEFAULT 'trial';
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS qr_code_content TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS admin_login_name TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS admin_password_verifier TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS admin_employee_id TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS registered_at BIGINT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS contact_name TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS contact_phone TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS address TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS region TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS scale TEXT;
ALTER TABLE tenant ADD COLUMN IF NOT EXISTS remark TEXT;

-- 回填：注册时间取创建时间；管理账户取本厂 tenant_admin 角色账号
DO $$
DECLARE rec RECORD;
BEGIN
  UPDATE tenant SET registered_at = created_at WHERE registered_at IS NULL;
  FOR rec IN
    SELECT t.id AS tenant_id, u.login_name, u.password_verifier
    FROM tenant t
    JOIN user_account u ON u.tenant_id = t.id
    JOIN user_role ur ON ur.tenant_id = u.tenant_id AND ur.user_id = u.id
    JOIN role r ON r.tenant_id = ur.tenant_id AND r.id = ur.role_id AND r.code = 'tenant_admin'
    WHERE u.status = 'active'
    ORDER BY u.created_at
  LOOP
    UPDATE tenant
    SET admin_login_name = rec.login_name,
        admin_password_verifier = rec.password_verifier
    WHERE tenant.id = rec.tenant_id AND tenant.admin_login_name IS NULL;
  END LOOP;
END $$;

ALTER TABLE tenant ADD CONSTRAINT ck_tenant_customer_type
  CHECK(customer_type IN ('trial','paid'));
ALTER TABLE tenant ADD CONSTRAINT ck_tenant_scale
  CHECK(scale IS NULL OR scale IN ('micro','small','medium','large'));

CREATE UNIQUE INDEX IF NOT EXISTS ux_tenant_qr ON tenant(qr_code_content) WHERE qr_code_content IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS ux_tenant_admin_login ON tenant(admin_login_name) WHERE admin_login_name IS NOT NULL;

COMMENT ON COLUMN tenant.customer_type IS '客户类型：trial 体验 / paid 付费；以 tenant_subscription 为准同步';
COMMENT ON COLUMN tenant.qr_code_content IS '唯一二维码内容：短码+签名，一厂一码，扫码注册/绑定入口';
COMMENT ON COLUMN tenant.admin_login_name IS '厂管理账户登录名（厂长/经理级管理员，注册时建档）';
COMMENT ON COLUMN tenant.admin_password_verifier IS '厂管理账户密码 Argon2id 验证器，绝不明文';
COMMENT ON COLUMN tenant.admin_employee_id IS '厂管理账户关联员工档案（可空）';
COMMENT ON COLUMN tenant.registered_at IS '注册时间（UTC 微秒）';
COMMENT ON COLUMN tenant.contact_name IS '辅助信息：联系人姓名';
COMMENT ON COLUMN tenant.contact_phone IS '辅助信息：联系电话（加密存储，脱敏展示）';
COMMENT ON COLUMN tenant.address IS '辅助信息：厂地址';
COMMENT ON COLUMN tenant.region IS '辅助信息：所在国/省市区，用于区域统计与收费地区匹配';
COMMENT ON COLUMN tenant.scale IS '厂规模：micro 微型 / small 小型 / medium 中型 / large 大型';

-- ===== 4.1 system_release 系统版本发布表 =====
CREATE TABLE system_release (
  id TEXT PRIMARY KEY,
  system_name TEXT NOT NULL,
  version_no TEXT NOT NULL,
  channel TEXT NOT NULL CHECK(channel IN ('stable','beta')),
  release_notes TEXT,
  download_link TEXT,
  apk_link TEXT,
  doc_link TEXT,
  publish_link TEXT,
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','published','deprecated')),
  published_at BIGINT,
  remark TEXT,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX uk_release_version ON system_release(version_no);
COMMENT ON TABLE system_release IS '系统版本发布表：系统名、版本、订阅客户说明、链接同表，订阅版本确认的单一事实来源';
COMMENT ON COLUMN system_release.version_no IS '版本号，语义化（如 v0.0.4.2），全局唯一';
COMMENT ON COLUMN system_release.publish_link IS '对外发布页链接（主页吸引潜在客户入口引用的落地页）';

-- ===== 4.2 visitor_lead 访客/潜在客户表 =====
CREATE TABLE visitor_lead (
  id TEXT PRIMARY KEY,
  lead_no TEXT NOT NULL,
  factory_name TEXT NOT NULL,
  contact_name TEXT,
  contact_phone TEXT,
  address TEXT,
  source TEXT NOT NULL DEFAULT 'online' CHECK(source IN ('visit','phone','referral','fission','online')),
  referrer_lead_id TEXT REFERENCES visitor_lead(id),
  referrer_tenant_id TEXT REFERENCES tenant(id),
  wechat_unionid TEXT,
  intent_status TEXT NOT NULL DEFAULT 'curious' CHECK(intent_status IN ('curious','interested','engaged','promotion','won','lost')),
  lost_reason TEXT,
  trial_tenant_id TEXT REFERENCES tenant(id),
  trial_started_at BIGINT,
  trial_expires_at BIGINT,
  assigned_admin_id TEXT,
  last_follow_at BIGINT,
  follow_remark TEXT,
  converted_tenant_id TEXT REFERENCES tenant(id),
  remark TEXT,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  CHECK(intent_status <> 'lost' OR lost_reason IS NOT NULL)
);
CREATE UNIQUE INDEX uk_lead_no ON visitor_lead(lead_no);
CREATE INDEX ix_lead_status ON visitor_lead(intent_status,last_follow_at);
CREATE INDEX ix_lead_source ON visitor_lead(source,created_at);
CREATE INDEX ix_lead_referrer ON visitor_lead(referrer_tenant_id);
COMMENT ON TABLE visitor_lead IS '访客/潜在客户表：走访、电话、制衣车推荐、裂变等渠道线索；支撑五状态转化漏斗与免费体验开通';
COMMENT ON COLUMN visitor_lead.lead_no IS '线索编号：XK+年月日+三位流水，如 XK20261107001';
COMMENT ON COLUMN visitor_lead.intent_status IS '转化状态：curious 好奇→interested 感兴趣→engaged 参与→promotion 促销→won 成功 / lost 失败；只前进不回退';
COMMENT ON COLUMN visitor_lead.trial_tenant_id IS '免费体验开通的体验制衣厂 ID；未开通为空';
COMMENT ON COLUMN visitor_lead.converted_tenant_id IS '转化成功后创建的正式制衣厂 ID（won 时回填）';

-- ===== 4.3 subscription_agreement 订阅协议表 =====
CREATE TABLE subscription_agreement (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenant(id),
  agreement_no TEXT NOT NULL,
  version_no TEXT NOT NULL,
  channel TEXT NOT NULL DEFAULT 'formal' CHECK(channel IN ('trial','formal')),
  signed_at BIGINT,
  start_date TEXT NOT NULL,
  end_date TEXT NOT NULL,
  terms JSONB,
  attachment_id TEXT,
  status TEXT NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','signed','expired','terminated')),
  terminated_reason TEXT,
  remark TEXT,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1
);
CREATE UNIQUE INDEX uk_agreement_no ON subscription_agreement(agreement_no);
CREATE INDEX ix_agreement_tenant ON subscription_agreement(tenant_id,status,start_date,end_date);
COMMENT ON TABLE subscription_agreement IS '订阅协议表：使用条件①签订订阅协议；一份协议对应一个版本与订阅周期，可续签';
COMMENT ON COLUMN subscription_agreement.tenant_id IS '制衣厂ID（引用键，标识哪个制衣厂的协议）';
COMMENT ON COLUMN subscription_agreement.version_no IS '订阅的系统版本（引用 system_release.version_no）';
COMMENT ON COLUMN subscription_agreement.terms IS '协议条款快照：版本功能清单、账号数、价格、保密与数据条款等';

-- ===== 4.4 subscription_payment 订阅费交付表 =====
CREATE TABLE subscription_payment (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenant(id),
  agreement_id TEXT NOT NULL REFERENCES subscription_agreement(id),
  amount NUMERIC(20,6) NOT NULL CHECK(amount >= 0),
  currency TEXT NOT NULL DEFAULT 'CNY',
  period_start TEXT NOT NULL,
  period_end TEXT NOT NULL,
  pay_method TEXT,
  paid_at BIGINT,
  voucher_attachment_id TEXT,
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','confirmed','overdue','refunded')),
  confirmed_by TEXT,
  confirmed_at BIGINT,
  remark TEXT,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1
);
CREATE INDEX ix_payment_tenant ON subscription_payment(tenant_id,status,paid_at);
CREATE INDEX ix_payment_agreement ON subscription_payment(agreement_id);
COMMENT ON TABLE subscription_payment IS '订阅费交付表：使用条件②交付订阅费；confirmed 后禁止修改金额（应用事务保证）';
COMMENT ON COLUMN subscription_payment.tenant_id IS '制衣厂ID（引用键）';

-- ===== 4.5 tenant_subscription 制衣厂订阅状态表 =====
CREATE TABLE tenant_subscription (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  agreement_id TEXT NOT NULL REFERENCES subscription_agreement(id),
  edition TEXT NOT NULL,
  features JSONB NOT NULL DEFAULT '[]',
  customer_type TEXT NOT NULL DEFAULT 'trial' CHECK(customer_type IN ('trial','paid')),
  status TEXT NOT NULL DEFAULT 'trial' CHECK(status IN ('trial','active','expired','suspended')),
  health_status TEXT CHECK(health_status IS NULL OR health_status IN ('activated','stable','active_user')),
  trial_ends_at BIGINT,
  activated_at BIGINT,
  expires_at BIGINT,
  features_confirmed_by TEXT,
  features_confirmed_at BIGINT,
  last_evaluated_at BIGINT,
  remark TEXT,
  created_at BIGINT NOT NULL,
  created_by TEXT NOT NULL,
  updated_at BIGINT NOT NULL,
  updated_by TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  CONSTRAINT uk_subscription_tenant UNIQUE(tenant_id),
  FOREIGN KEY(tenant_id) REFERENCES tenant(id)
);
CREATE INDEX ix_subscription_status ON tenant_subscription(customer_type,status,expires_at);
COMMENT ON TABLE tenant_subscription IS '制衣厂订阅状态表：一厂一行；三个使用条件校验结果汇总；付费用户使用状态（激活→稳定→积极）载体';
COMMENT ON COLUMN tenant_subscription.edition IS '确认订阅的版本档位：basic 基础版 / standard 标准版 / pro 专业版';
COMMENT ON COLUMN tenant_subscription.features IS '确认订阅的功能清单（模块编码数组）；体验厂按用户体验进度点亮（体验到=1，未到=0）';
COMMENT ON COLUMN tenant_subscription.health_status IS '付费用户使用状态：activated 激活→stable 稳定→active_user 积极；仅 paid 有值，本期预留';

-- ===== 5.2.1 tenant_crypto_key 制衣厂数据加密密钥表 =====
CREATE TABLE tenant_crypto_key (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL REFERENCES tenant(id),
  key_version BIGINT NOT NULL,
  algorithm TEXT NOT NULL DEFAULT 'AES-256-GCM',
  wrapped_key TEXT NOT NULL,
  key_wrap_ref TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','rotated','revoked')),
  activated_at BIGINT NOT NULL,
  rotated_at BIGINT,
  created_by TEXT,
  created_at BIGINT NOT NULL,
  UNIQUE(tenant_id,key_version)
);
CREATE INDEX ix_tenant_key_active ON tenant_crypto_key(tenant_id,status);
COMMENT ON TABLE tenant_crypto_key IS '制衣厂数据加密密钥表：每厂一个 DEK，被平台 KMS 主密钥包装后落库；密钥只用于本厂数据加解密';
COMMENT ON COLUMN tenant_crypto_key.wrapped_key IS '被 KMS 包装后的 DEK（Base64），禁止明文入库';
COMMENT ON COLUMN tenant_crypto_key.key_wrap_ref IS '包装所用 KMS 主密钥引用（密钥 ID/版本）';
