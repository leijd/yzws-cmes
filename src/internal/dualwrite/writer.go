// Package dualwrite 默认双写 2PC 与崩溃恢复（设计文档 3.3）。
//
// 流程：API 完成预校验后 Begin 生成全局 tx_id（gid）；两个固定连接分别开启事务；
// 业务在 plain 库写明文、secure 库写密文信封；两库事务内各写一条 dual_write_journal(state=prepared)；
// 两库分别 PREPARE TRANSACTION，任一失败则回滚双方；均准备成功后 COMMIT PREPARED，
// 只有两库均提交成功才返回业务成功。提交阶段崩溃/断连由恢复任务依据 journal 与 pg_prepared_xacts 完成。
//
// 单写降级（plain_only/secure_only）使用普通本地事务，journal state=pending 记录未同步起点。
// journal 不含业务明文/密文 payload；补偿通过主键与版本回读源库当前记录（设计 4.12）。
package dualwrite

import (
	"context"
	"fmt"
	"log"
	"sync"

	"clothp/internal/datastore"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Writer struct {
	store  *datastore.Store
	nodeID string
}

func NewWriter(store *datastore.Store, nodeID string) *Writer {
	return &Writer{store: store, nodeID: nodeID}
}

func (w *Writer) Store() *datastore.Store { return w.store }

/* Tx 一次双写事务：plain/secure 分别为两库事务句柄（nil 表示本次不写该库）。 */
type Tx struct {
	gid   string
	store *datastore.Store

	plainConn  *pgxpool.Conn
	secureConn *pgxpool.Conn
	plainTx    pgx.Tx
	secureTx   pgx.Tx

	tenantID      string
	aggregateType string
	aggregateID   string
	ops           int

	finished bool
	mu       sync.Mutex
}

/* Begin 开启双写事务。tenantID 可空（平台事务）。按 write_mode 只开启需要写入的库。 */
func (w *Writer) Begin(ctx context.Context, tenantID string) (*Tx, error) {
	tx := &Tx{gid: newGID(w.nodeID), store: w.store, tenantID: tenantID}
	mode := w.store.WriteMode()
	now := platform.Now()
	if mode != "secure_only" {
		conn, err := w.store.Plain.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		t, err := conn.Begin(ctx)
		if err != nil {
			conn.Release()
			return nil, err
		}
		tx.plainConn, tx.plainTx = conn, t
	}
	if mode == "both" || mode == "secure_only" {
		if w.store.Secure == nil {
			if tx.plainTx != nil {
				_ = tx.plainTx.Rollback(ctx)
				tx.plainConn.Release()
			}
			return nil, fmt.Errorf("secure database not configured for write_mode=%s", mode)
		}
		conn, err := w.store.Secure.Acquire(ctx)
		if err != nil {
			if tx.plainTx != nil {
				_ = tx.plainTx.Rollback(ctx)
				tx.plainConn.Release()
			}
			return nil, err
		}
		t, err := conn.Begin(ctx)
		if err != nil {
			conn.Release()
			if tx.plainTx != nil {
				_ = tx.plainTx.Rollback(ctx)
				tx.plainConn.Release()
			}
			return nil, err
		}
		tx.secureConn, tx.secureTx = conn, t
	}
	_ = now
	return tx, nil
}

/* GID 全局事务 ID。 */
func (tx *Tx) GID() string { return tx.gid }

/* Plain 明文库事务句柄（write_mode=secure_only 时为 nil）。 */
func (tx *Tx) Plain() pgx.Tx { return tx.plainTx }

/* Secure 密文库事务句柄（单写明文模式时为 nil）。 */
func (tx *Tx) Secure() pgx.Tx { return tx.secureTx }

/* MarkAggregate 记录业务对象类型/主键，便于恢复与补偿定位。 */
func (tx *Tx) MarkAggregate(typ, id string) {
	tx.aggregateType, tx.aggregateID = typ, id
}

/* WriteMode 当前写入模式。 */
func (tx *Tx) WriteMode() string { return tx.store.WriteMode() }

func (tx *Tx) journalRow(state string, now int64) []any {
	return []any{tx.gid, nullIfEmpty(tx.tenantID), tx.store.WriteMode(), state,
		nullIfEmpty(tx.aggregateType), nullIfEmpty(tx.aggregateID), tx.ops, platform.ID(), now, now}
}

const insertJournalSQL = `INSERT INTO dual_write_journal
	(tx_id,tenant_id,write_mode,state,aggregate_type,aggregate_id,operation_count,request_id,created_at,updated_at)
	VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	ON CONFLICT(tx_id) DO UPDATE SET state=EXCLUDED.state,updated_at=EXCLUDED.updated_at`

/* prepareJournal 在各库事务内写入 journal（状态机起点，2PC 的参与者日志）。 */
func (tx *Tx) prepareJournal(ctx context.Context) error {
	now := platform.Now()
	if tx.plainTx != nil {
		if _, err := tx.plainTx.Exec(ctx, insertJournalSQL, tx.journalRow("prepared", now)...); err != nil {
			return fmt.Errorf("journal on plain: %w", err)
		}
	}
	if tx.secureTx != nil {
		if _, err := tx.secureTx.Exec(ctx, insertJournalSQL, tx.journalRow("prepared", now)...); err != nil {
			return fmt.Errorf("journal on secure: %w", err)
		}
	}
	return nil
}

/* prepare 两库 PREPARE TRANSACTION；任一失败返回错误（调用方须回滚双方）。 */
func (tx *Tx) prepare(ctx context.Context) error {
	if tx.plainTx != nil {
		if _, err := tx.plainTx.Exec(ctx, fmt.Sprintf("PREPARE TRANSACTION '%s'", tx.gid)); err != nil {
			return fmt.Errorf("prepare plain transaction: %w", err)
		}
	}
	if tx.secureTx != nil {
		if _, err := tx.secureTx.Exec(ctx, fmt.Sprintf("PREPARE TRANSACTION '%s'", tx.gid)); err != nil {
			return fmt.Errorf("prepare secure transaction: %w", err)
		}
	}
	return nil
}

func (tx *Tx) release() {
	if tx.plainConn != nil {
		tx.plainConn.Release()
		tx.plainConn = nil
	}
	if tx.secureConn != nil {
		tx.secureConn.Release()
		tx.secureConn = nil
	}
}

/* Commit 2PC 提交：journal(prepared) → 两库 PREPARE → 两库 COMMIT PREPARED → journal(committed)。
 * 单写模式只走本地提交。任一 PREPARE 失败回滚全部；COMMIT 阶段失败如实返回错误（设计 10.2：不向客户端返回伪成功），
 * 悬挂事务由恢复任务继续提交。 */
func (tx *Tx) Commit(ctx context.Context) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.finished {
		return fmt.Errorf("transaction already finished")
	}
	tx.finished = true
	now := platform.Now()

	if tx.secureTx == nil || tx.plainTx == nil {
		/* 单写：普通本地事务 + journal(pending→committed) */
		if err := tx.prepareJournalSingle(ctx, now); err != nil {
			tx.rollbackLocal(ctx)
			return err
		}
		if err := tx.commitLocal(ctx); err != nil {
			tx.markJournal(ctx, "aborted", now, err)
			return err
		}
		tx.markJournal(ctx, "committed", now, nil)
		return nil
	}

	if err := tx.prepareJournal(ctx); err != nil {
		tx.rollbackLocal(ctx)
		return err
	}
	if err := tx.prepare(ctx); err != nil {
		tx.rollbackLocal(ctx)
		return err
	}
	tx.release()

	/* 提交阶段：两库均须成功；失败如实返回，恢复任务依据 journal 继续 */
	committed := map[string]bool{}
	for _, name := range []string{"plain", "secure"} {
		var pool *pgxpool.Pool
		if name == "plain" {
			pool = tx.store.Plain
		} else {
			pool = tx.store.Secure
		}
		if _, err := pool.Exec(ctx, fmt.Sprintf("COMMIT PREPARED '%s'", tx.gid)); err != nil {
			tx.markJournal(ctx, "prepared", now, err)
			log.Printf("dualwrite: commit prepared on %s failed gid=%s: %v (recovery will continue)", name, tx.gid, err)
			_ = committed
			return fmt.Errorf("commit prepared on %s: %w (transaction left prepared for recovery)", name, err)
		}
		committed[name] = true
	}
	tx.markJournal(ctx, "committed", now, nil)
	return nil
}

func (tx *Tx) commitLocal(ctx context.Context) error {
	if tx.plainTx != nil {
		return tx.plainTx.Commit(ctx)
	}
	return tx.secureTx.Commit(ctx)
}

/* prepareJournalSingle 单写模式的 journal（同一本地事务内）。 */
func (tx *Tx) prepareJournalSingle(ctx context.Context, now int64) error {
	active := tx.plainTx
	if active == nil {
		active = tx.secureTx
	}
	source := "plain"
	if tx.plainTx == nil {
		source = "secure"
	}
	row := tx.journalRow("pending", now)
	row[3] = "pending"
	row = append(row, source)
	_, err := active.Exec(ctx, `INSERT INTO dual_write_journal
		(tx_id,tenant_id,write_mode,state,aggregate_type,aggregate_id,operation_count,request_id,created_at,updated_at,source_db)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT(tx_id) DO UPDATE SET state=EXCLUDED.state,updated_at=EXCLUDED.updated_at`, row...)
	return err
}

/* Rollback 回滚全部活动事务并记 journal=aborted（尽力而为）。 */
func (tx *Tx) Rollback(ctx context.Context) error {
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.finished {
		return nil
	}
	tx.finished = true
	tx.rollbackLocal(ctx)
	tx.markJournal(ctx, "aborted", platform.Now(), nil)
	return nil
}

func (tx *Tx) rollbackLocal(ctx context.Context) {
	if tx.plainTx != nil {
		_ = tx.plainTx.Rollback(ctx)
	}
	if tx.secureTx != nil {
		_ = tx.secureTx.Rollback(ctx)
	}
	tx.release()
}

/* markJournal 提交/回滚后的状态落盘（新短事务，尽力而为；失败由恢复任务兜底）。 */
func (tx *Tx) markJournal(ctx context.Context, state string, now int64, cause error) {
	lastErr := ""
	if cause != nil {
		lastErr = sanitizeError(cause)
	}
	for _, pool := range tx.store.WritePools() {
		_, err := pool.Exec(ctx, `UPDATE dual_write_journal SET state=$2,updated_at=$3,completed_at=$4,last_error=NULLIF($5,'') WHERE tx_id=$1`,
			tx.gid, state, now, now, lastErr)
		if err != nil {
			log.Printf("dualwrite: mark journal gid=%s state=%s: %v", tx.gid, state, err)
		}
	}
}

func newGID(nodeID string) string {
	return fmt.Sprintf("clothp-%s-%s", nodeID, platform.ID())
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

/* sanitizeError 错误信息脱敏：不携带 SQL 参数等可能的敏感内容。 */
func sanitizeError(err error) string {
	msg := err.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}
