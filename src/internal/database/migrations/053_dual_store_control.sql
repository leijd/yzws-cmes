-- 双库控制：双写事务日志、互转/对账任务、数据存储模式配置（两库同结构）。
-- 约定：日志与任务表只保存主键/版本/状态等元数据，不保存业务明文或密文 payload，
--       补偿与对账通过主键与版本回读源库当前记录，避免控制表本身成为敏感数据副本。

-- ===== 双写事务日志（设计 4.12） =====
CREATE TABLE IF NOT EXISTS dual_write_journal (
  tx_id TEXT PRIMARY KEY,
  tenant_id TEXT,
  write_mode TEXT NOT NULL CHECK(write_mode IN ('both','plain_only','secure_only')),
  state TEXT NOT NULL CHECK(state IN ('pending','prepared','committed','aborted','recovering')),
  source_db TEXT CHECK(source_db IS NULL OR source_db IN ('plain','secure')),
  aggregate_type TEXT,
  aggregate_id TEXT,
  operation_count INTEGER NOT NULL DEFAULT 0,
  request_id TEXT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL,
  completed_at BIGINT,
  last_error TEXT
);
CREATE INDEX IF NOT EXISTS ix_dual_journal_state ON dual_write_journal(state,created_at);
CREATE INDEX IF NOT EXISTS ix_dual_journal_tenant ON dual_write_journal(tenant_id,created_at);
COMMENT ON TABLE dual_write_journal IS '双写事务日志：两库同结构；记录 2PC 状态与单写补偿起点，不含业务内容';

-- ===== 互转/对账任务（设计 4.13） =====
CREATE TABLE IF NOT EXISTS data_transfer_job (
  id TEXT PRIMARY KEY,
  job_type TEXT NOT NULL CHECK(job_type IN ('convert','verify','catch_up','reencrypt')),
  source_db TEXT NOT NULL CHECK(source_db IN ('plain','secure')),
  target_db TEXT NOT NULL CHECK(target_db IN ('plain','secure')),
  tenant_id TEXT,
  table_names JSONB NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','paused','succeeded','failed','cancelled')),
  dry_run BOOLEAN NOT NULL DEFAULT TRUE,
  total_rows BIGINT NOT NULL DEFAULT 0,
  processed_rows BIGINT NOT NULL DEFAULT 0,
  failed_rows BIGINT NOT NULL DEFAULT 0,
  cursor JSONB,
  requested_by TEXT NOT NULL,
  request_reason TEXT NOT NULL,
  started_at BIGINT,
  finished_at BIGINT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS ix_transfer_job_status ON data_transfer_job(status,created_at);
COMMENT ON TABLE data_transfer_job IS '数据互转/对账任务：convert 双向转换、verify 对账、catch_up 补数、reencrypt 密钥轮换重加密';

-- ===== 转换批次/错误明细（设计 4.14） =====
CREATE TABLE IF NOT EXISTS data_transfer_item (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES data_transfer_job(id),
  table_name TEXT NOT NULL,
  tenant_id TEXT,
  batch_start JSONB,
  batch_end JSONB,
  row_count BIGINT NOT NULL DEFAULT 0,
  success_count BIGINT NOT NULL DEFAULT 0,
  failed_count BIGINT NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'running' CHECK(status IN ('running','succeeded','failed','skipped')),
  source_version BIGINT,
  target_version BIGINT,
  error_code TEXT,
  error_detail TEXT,
  started_at BIGINT,
  finished_at BIGINT
);
-- 断点续跑去重：同一任务同一表同一批次只允许一条明细（JSONB 游标按文本比较）
CREATE UNIQUE INDEX IF NOT EXISTS ux_transfer_item_batch
  ON data_transfer_item(job_id,table_name,COALESCE(batch_start::text,''),COALESCE(batch_end::text,''));
CREATE INDEX IF NOT EXISTS ix_transfer_item_job ON data_transfer_item(job_id,status);
COMMENT ON TABLE data_transfer_item IS '转换批次/错误明细：与任务状态在同一目标库事务更新；双写模式下控制表同样双写';

-- ===== 数据存储模式配置（运行时切换留痕，配合平台审计，不记录密码/密钥） =====
CREATE TABLE IF NOT EXISTS data_store_setting (
  name TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  version BIGINT NOT NULL DEFAULT 1,
  updated_by TEXT,
  updated_at BIGINT NOT NULL,
  reason TEXT
);
COMMENT ON TABLE data_store_setting IS '数据存储模式：read_source(secure|plain)、write_mode(both|plain_only|secure_only) 等运行期配置及变更留痕';
INSERT INTO data_store_setting(name,value,version,updated_at)
VALUES ('read_source','secure',1,(extract(epoch from now())*1000000)::BIGINT),
       ('write_mode','both',1,(extract(epoch from now())*1000000)::BIGINT)
ON CONFLICT(name) DO NOTHING;
