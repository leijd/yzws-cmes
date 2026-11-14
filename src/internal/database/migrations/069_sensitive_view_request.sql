-- 敏感信息查看授权申请（审批链路）：非管理员在解锁弹窗发起申请 → 租户管理员提醒列表收到
-- 一条「有人申请敏感信息查看授权码」消息 → 查看详情后允许/拒绝 → 允许时签发授权码并短信下发，
-- 未接真实短信服务商（演示模式）时授权码回显在申请人页面。
-- 授权码明文本身只存 sha256 哈希到 sensitive_view_grant；demo_code 仅演示模式保存，供申请人页面展示。
-- 两库同结构（密文库/明文库各建一份）。

CREATE TABLE IF NOT EXISTS sensitive_view_request (
  id                 TEXT PRIMARY KEY,
  tenant_id          TEXT NOT NULL,
  requester_user_id  TEXT NOT NULL,              -- 申请人账号
  requester_name     TEXT NOT NULL DEFAULT '',
  requester_phone    TEXT NOT NULL DEFAULT '',   -- 接收授权码短信的手机号
  employee_id        TEXT NOT NULL,              -- 申请查看的员工档案
  employee_name      TEXT NOT NULL DEFAULT '',
  field_key          TEXT NOT NULL DEFAULT '',
  field_label        TEXT NOT NULL DEFAULT '',   -- 触发申请的字段（如 联系电话）
  reason             TEXT NOT NULL DEFAULT '',
  status             TEXT NOT NULL DEFAULT 'pending', -- pending / approved / rejected
  grant_id           TEXT NOT NULL DEFAULT '',
  demo_code          TEXT NOT NULL DEFAULT '',   -- 演示模式授权码（未接短信时回显给申请人）
  code_expires_at    BIGINT NOT NULL DEFAULT 0,
  sms_sent           BOOLEAN NOT NULL DEFAULT FALSE,
  reviewed_by        TEXT NOT NULL DEFAULT '',
  reviewed_at        BIGINT NOT NULL DEFAULT 0,
  created_at         BIGINT NOT NULL,
  updated_at         BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS ix_svr_requester ON sensitive_view_request(tenant_id,requester_user_id,created_at DESC);
CREATE INDEX IF NOT EXISTS ix_svr_status ON sensitive_view_request(tenant_id,status,created_at DESC);
