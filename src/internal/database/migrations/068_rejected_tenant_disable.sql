-- 注册驳回即禁用：早期版本驳回只改 registration_status='rejected'，tenant.status 仍为 active，
-- 导致该厂既不出现在注册申请列表，又在租户运营中显示为启用（且账号登录链路未按审批状态拦截）。
-- 本迁移把历史驳回记录统一置为停用；注册申请列表只保留 pending/approved，驳回记录在租户运营的停用列表查看。
UPDATE tenant SET status='disabled'
WHERE registration_status='rejected' AND status='active';

COMMENT ON COLUMN tenant.registration_status IS '注册审批状态：pending 待平台确认 / approving 审批中 / approved 已启用 / rejected 已驳回（驳回即停用，租户运营列表按停用展示）';
