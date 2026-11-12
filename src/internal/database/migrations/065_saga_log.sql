/* Saga 小事务步骤日志（设计原则：禁止长事务；多步写入拆分为独立短事务，
 * 失败按逆序补偿；补偿现场持久化，进程崩溃后由恢复循环继续补偿或标记人工核对）。
 * 控制表，不参与 catchup 业务对账（与 dual_write_journal 同口径）。 */
CREATE TABLE IF NOT EXISTS dual_write_saga (
  saga_id TEXT PRIMARY KEY,
  tenant_id TEXT,
  name TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('running','compensating','done','aborted','failed')),
  current_step INT NOT NULL DEFAULT 0,
  payload JSONB,
  last_error TEXT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_dual_write_saga_state ON dual_write_saga(state, updated_at);

CREATE TABLE IF NOT EXISTS dual_write_saga_step (
  saga_id TEXT NOT NULL REFERENCES dual_write_saga(saga_id),
  step_no INT NOT NULL,
  step_name TEXT NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('done','compensated','compensation_failed')),
  updated_at BIGINT NOT NULL,
  PRIMARY KEY (saga_id, step_no)
);

/* 审批 Saga 的"认领"中间态：替代原长事务 SELECT ... FOR UPDATE 防重闸门
 *（小事务第一步 CAS 认领，并发审批只有一个能进入种子阶段）。 */
ALTER TABLE tenant DROP CONSTRAINT IF EXISTS ck_tenant_registration_status;
ALTER TABLE tenant ADD CONSTRAINT ck_tenant_registration_status
  CHECK(registration_status IN ('pending','approving','approved','rejected'));
