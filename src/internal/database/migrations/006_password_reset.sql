ALTER TABLE user_account ADD COLUMN password_change_required BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE user_account ADD COLUMN password_reset_at BIGINT;
ALTER TABLE user_account ADD COLUMN password_reset_by TEXT;
COMMENT ON COLUMN user_account.password_change_required IS '平台重置密码后强制用户下次登录改密';
COMMENT ON COLUMN user_account.password_reset_at IS '最近密码重置时间，UTC微秒';
COMMENT ON COLUMN user_account.password_reset_by IS '密码重置操作者ID，平台管理员或用户本人';
