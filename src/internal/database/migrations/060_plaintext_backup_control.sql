-- 明文离线备份导出控制（设计 3.9 / 9.2 迁移 058）：任务元数据扩展 + 导出权限点。
-- 备份包为受控例外：显式授权、完整校验、一次性下载，必须由用户按高敏感文件保管。

ALTER TABLE backup_job ADD COLUMN IF NOT EXISTS source_db TEXT NOT NULL DEFAULT 'plain';        -- plain/secure；明文导出固定 secure
ALTER TABLE backup_job ADD COLUMN IF NOT EXISTS format_version TEXT NOT NULL DEFAULT 'v1';     -- 备份包格式版本
ALTER TABLE backup_job ADD COLUMN IF NOT EXISTS snapshot_at BIGINT;                            -- 快照时间（导出开始时）
ALTER TABLE backup_job ADD COLUMN IF NOT EXISTS requested_by TEXT;                             -- 操作者（本厂账号 id）
ALTER TABLE backup_job ADD COLUMN IF NOT EXISTS total_rows BIGINT NOT NULL DEFAULT 0;          -- 导出行数合计
ALTER TABLE backup_job ADD COLUMN IF NOT EXISTS table_count INTEGER NOT NULL DEFAULT 0;        -- 导出的表数
ALTER TABLE backup_job ADD COLUMN IF NOT EXISTS download_token_hash TEXT;                      -- 一次性下载凭证（只存哈希）
ALTER TABLE backup_job ADD COLUMN IF NOT EXISTS download_expires_at BIGINT;                    -- 下载凭证有效期
ALTER TABLE backup_job ADD COLUMN IF NOT EXISTS downloaded_at BIGINT;                          -- 已下载时间（一次性下载后作废）

COMMENT ON COLUMN backup_job.backup_type IS '备份类型：既有的库级备份 / plaintext_export=密文库解密明文导出（设计 3.9 受控例外）';
COMMENT ON COLUMN backup_job.download_token_hash IS '明文导出一次性下载凭证的 SHA-256；下载后凭证作废，不可重复取包';

/* 明文导出权限点（backup:export，按租户补齐） */
INSERT INTO permission(tenant_id,id,code,resource,action,sensitivity,description)
SELECT t.id, 'perm-'||t.id||'-backup-export', 'backup:export', 'backup', 'export', 'sensitive', '明文离线备份导出'
FROM tenant t
ON CONFLICT DO NOTHING;
