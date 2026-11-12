package dualwrite

/* Saga 小事务编排（设计原则：禁止长事务）。
 *
 * 多步写入拆为独立的短事务（每步仍走 Begin/Commit，两库 2PC 或单写降级，lock_timeout
 * 保护下撞锁快速失败）；第 N 步失败时对前 N-1 步按逆序执行 Compensate（每个补偿也是
 * 独立短事务）。步骤与补偿现场持久化到 dual_write_saga / dual_write_saga_step（两库
 * 各一份，自动提交的控制表写入，不占业务事务）：
 *   - 进程崩溃/请求中断：恢复循环对 state=running/compensating 且超过宽限期的 saga，
 *     按注册工厂重建补偿闭包继续补偿；未注册工厂的标记 failed 留人工核对；
 *   - 补偿本身失败：步骤记 compensation_failed、saga 记 failed，日志告警人工介入，
 *     绝不静默当作已回滚。
 */

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"clothp/internal/platform"
)

/* SagaStep 一步小事务。Compensate 为 nil 表示该步无需补偿（如纯查询或首步前的预检）。 */
type SagaStep struct {
	Name       string
	Do         func(ctx context.Context, tx *Tx) error
	Compensate func(ctx context.Context, tx *Tx) error
}

/* SagaFactory 由持久化 payload 重建步骤闭包，供崩溃恢复继续补偿。 */
type SagaFactory func(payload json.RawMessage) ([]SagaStep, error)

var sagaFactories = map[string]SagaFactory{}

/* RegisterSagaFactory 注册 saga 的重建工厂（模块 init 时调用）。未注册的 saga
 * 崩溃后只能标记 failed 人工核对。 */
func RegisterSagaFactory(name string, f SagaFactory) { sagaFactories[name] = f }

/* sagaRecoverGrace 运行中的 saga 超过该时长未更新才允许恢复介入（单步有 lock_timeout
 * 与请求超时兜底，正常执行远快于此，避免与活跃请求竞争）。 */
const sagaRecoverGrace = 90 * time.Second

func newSagaID() string { return "saga-" + platform.ID() }

/* sagaExec 控制表写入：两库各一份（自动提交），尽力而为，失败只记日志——
 * 日志丢失不阻塞业务，恢复侧按最坏情况（认为步骤未完成）处理。 */
func (w *Writer) sagaExec(ctx context.Context, sql string, args ...any) {
	for name, pool := range w.store.WritePools() {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			log.Printf("dualwrite saga: control write on %s failed: %v", name, err)
		}
	}
}

/* RunSaga 顺序执行步骤；任一步 Do 失败则逆序补偿已完成的步骤后返回原始错误。
 * Commit（2PC）阶段失败不补偿——事务可能处于 prepared 待恢复状态，补偿会与恢复提交
 * 叠加，此时 saga 记 failed 交恢复/人工处理。 */
func (w *Writer) RunSaga(ctx context.Context, tenantID, name string, payload any, steps []SagaStep) error {
	sagaID := newSagaID()
	now := platform.Now()
	payloadJSON, _ := json.Marshal(payload)
	w.sagaExec(ctx, `INSERT INTO dual_write_saga(saga_id,tenant_id,name,state,current_step,payload,created_at,updated_at)
		VALUES($1,$2,$3,'running',0,$4,$5,$5)`, sagaID, nullIfEmpty(tenantID), name, string(payloadJSON), now)

	runStep := func(no int, fn func(ctx context.Context, tx *Tx) error) error {
		tx, err := w.Begin(ctx, tenantID)
		if err != nil {
			return err
		}
		tx.MarkAggregate("saga:"+name, sagaID)
		if err := fn(ctx, tx); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		return tx.Commit(ctx)
	}

	for i, step := range steps {
		if step.Do == nil {
			continue
		}
		if err := runStep(i+1, step.Do); err != nil {
			now = platform.Now()
			w.sagaExec(ctx, `UPDATE dual_write_saga SET state=$2,current_step=$3,last_error=$4,updated_at=$5 WHERE saga_id=$1`,
				sagaID, "compensating", i, sanitizeError(err), now)
			if compErr := w.compensate(ctx, tenantID, sagaID, steps[:i]); compErr != nil {
				return fmt.Errorf("saga %s step %q failed: %w（补偿未完成：%v，已记录人工核对）", name, step.Name, err, compErr)
			}
			return fmt.Errorf("saga %s step %q failed: %w（已补偿回滚前 %d 步）", name, step.Name, err, i)
		}
		now = platform.Now()
		w.sagaExec(ctx, `INSERT INTO dual_write_saga_step(saga_id,step_no,step_name,state,updated_at)
			VALUES($1,$2,$3,'done',$4)
			ON CONFLICT(saga_id,step_no) DO UPDATE SET state='done',updated_at=EXCLUDED.updated_at`,
			sagaID, i+1, step.Name, now)
		w.sagaExec(ctx, `UPDATE dual_write_saga SET current_step=$2,updated_at=$3 WHERE saga_id=$1`, sagaID, i+1, now)
	}
	w.sagaExec(ctx, `UPDATE dual_write_saga SET state='done',updated_at=$2 WHERE saga_id=$1`, sagaID, platform.Now())
	return nil
}

/* compensate 逆序补偿；任一步补偿失败立即记 failed 并返回错误（剩余步骤不再继续，
 * 避免在未知现场上叠加操作）。 */
func (w *Writer) compensate(ctx context.Context, tenantID, sagaID string, done []SagaStep) error {
	for j := len(done) - 1; j >= 0; j-- {
		step := done[j]
		if step.Compensate == nil {
			continue
		}
		err := func() error {
			tx, err := w.Begin(ctx, tenantID)
			if err != nil {
				return err
			}
			tx.MarkAggregate("saga-compensate", sagaID)
			if err := step.Compensate(ctx, tx); err != nil {
				_ = tx.Rollback(ctx)
				return err
			}
			return tx.Commit(ctx)
		}()
		now := platform.Now()
		if err != nil {
			log.Printf("dualwrite saga: compensate saga=%s step=%s failed: %v", sagaID, step.Name, err)
			w.sagaExec(ctx, `INSERT INTO dual_write_saga_step(saga_id,step_no,step_name,state,updated_at)
				VALUES($1,$2,$3,'compensation_failed',$4)
				ON CONFLICT(saga_id,step_no) DO UPDATE SET state='compensation_failed',updated_at=EXCLUDED.updated_at`,
				sagaID, j+1, step.Name, now)
			w.sagaExec(ctx, `UPDATE dual_write_saga SET state='failed',last_error=$2,updated_at=$3 WHERE saga_id=$1`,
				sagaID, "compensation failed at step "+step.Name+": "+sanitizeError(err), now)
			return err
		}
		w.sagaExec(ctx, `INSERT INTO dual_write_saga_step(saga_id,step_no,step_name,state,updated_at)
			VALUES($1,$2,$3,'compensated',$4)
			ON CONFLICT(saga_id,step_no) DO UPDATE SET state='compensated',updated_at=EXCLUDED.updated_at`,
			sagaID, j+1, step.Name, now)
	}
	w.sagaExec(ctx, `UPDATE dual_write_saga SET state='aborted',updated_at=$2 WHERE saga_id=$1`, sagaID, platform.Now())
	return nil
}

/* recoverSagas 恢复循环入口：接管中断的 saga（崩溃/请求超时留下 running/compensating）。
 * 已注册工厂：重建闭包，补偿全部已记 done 的步骤；未注册：标记 failed 人工核对。 */
func (w *Writer) recoverSagas(ctx context.Context) error {
	grace := time.Now().Add(-sagaRecoverGrace).UnixMicro()
	type sagaRow struct {
		id, name, state string
		tenantID        *string
		payload         []byte
	}
	seen := map[string]bool{}
	var stale []sagaRow
	for _, pool := range w.store.WritePools() {
		rows, err := pool.Query(ctx, `SELECT saga_id,tenant_id,name,state,COALESCE(payload::text,'') FROM dual_write_saga
			WHERE state IN ('running','compensating') AND updated_at<$1 ORDER BY updated_at LIMIT 100`, grace)
		if err != nil {
			return fmt.Errorf("scan sagas: %w", err)
		}
		for rows.Next() {
			var s sagaRow
			var payload string
			if err := rows.Scan(&s.id, &s.tenantID, &s.name, &s.state, &payload); err != nil {
				rows.Close()
				return err
			}
			s.payload = []byte(payload)
			if !seen[s.id] {
				seen[s.id] = true
				stale = append(stale, s)
			}
		}
		rows.Close()
	}
	for _, s := range stale {
		if err := w.recoverOneSaga(ctx, s.id, s.tenantID, s.name, s.payload); err != nil {
			log.Printf("dualwrite saga: recover saga=%s name=%s: %v", s.id, s.name, err)
		}
	}
	return nil
}

func (w *Writer) recoverOneSaga(ctx context.Context, sagaID string, tenantID *string, name string, payload []byte) error {
	now := platform.Now()
	factory := sagaFactories[name]
	if factory == nil {
		w.sagaExec(ctx, `UPDATE dual_write_saga SET state='failed',last_error=$2,updated_at=$3 WHERE saga_id=$1 AND state IN ('running','compensating')`,
			sagaID, "interrupted mid-saga and no factory registered; manual reconcile required", now)
		log.Printf("dualwrite saga: saga=%s name=%s interrupted, manual reconcile required (no factory)", sagaID, name)
		return nil
	}
	steps, err := factory(payload)
	if err != nil {
		w.sagaExec(ctx, `UPDATE dual_write_saga SET state='failed',last_error=$2,updated_at=$3 WHERE saga_id=$1`,
			sagaID, "rebuild steps failed: "+sanitizeError(err), now)
		return err
	}
	/* 已完成的步骤（任一库记录 done 即视为已完成，需补偿） */
	doneNos := map[int]bool{}
	for _, pool := range w.store.WritePools() {
		rows, err := pool.Query(ctx, `SELECT step_no FROM dual_write_saga_step WHERE saga_id=$1 AND state='done'`, sagaID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var no int
			if err := rows.Scan(&no); err != nil {
				rows.Close()
				return err
			}
			doneNos[no] = true
		}
		rows.Close()
	}
	var done []SagaStep
	for i, step := range steps {
		if doneNos[i+1] {
			done = append(done, step)
		}
	}
	tid := ""
	if tenantID != nil {
		tid = *tenantID
	}
	if err := w.compensate(ctx, tid, sagaID, done); err != nil {
		return err
	}
	log.Printf("dualwrite saga: recovered saga=%s name=%s, compensated %d step(s)", sagaID, name, len(done))
	return nil
}
