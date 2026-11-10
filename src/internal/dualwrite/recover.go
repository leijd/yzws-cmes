package dualwrite

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"clothp/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

/* Recover 依据 dual_write_journal 与 pg_prepared_xacts 恢复悬挂事务（设计 3.3 步骤 5）。
 *
 * - state='prepared'/'recovering'：逐库探测 pg_prepared_xacts；已准备未提交者继续提交（不允许只写成功一库被当作正常）。
 * - 两库均无 prepared 记录：说明都已提交或都已回滚，按 journal 记录目标状态收尾（committed/aborted）。
 * - 只处理 updated_at 早于 now-grace 的记录，避免与正在提交的事务竞争。
 */
func (w *Writer) Recover(ctx context.Context) error {
	grace := time.Now().Add(-15 * time.Second).UnixMicro()
	dbs := w.store.WritePools()
	type journalRow struct {
		GID   string
		State string
	}
	for name, pool := range dbs {
		rows, err := pool.Query(ctx, `SELECT tx_id,state FROM dual_write_journal
			WHERE state IN ('prepared','recovering') AND updated_at<$1 ORDER BY created_at LIMIT 200`, grace)
		if err != nil {
			return fmt.Errorf("scan journal on %s: %w", name, err)
		}
		var pending []journalRow
		for rows.Next() {
			var j journalRow
			if err := rows.Scan(&j.GID, &j.State); err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, j)
		}
		rows.Close()
		for _, j := range pending {
			if err := w.recoverOne(ctx, dbs, j.GID, j.State); err != nil {
				log.Printf("dualwrite: recover gid=%s: %v", j.GID, err)
			}
		}
	}
	return nil
}

func (w *Writer) recoverOne(ctx context.Context, dbs map[string]*pgxpool.Pool, gid, state string) error {
	/* 先标记 recovering，避免多节点重复恢复（应用层幂等，Postgres 2PC 自身也保证单结论文） */
	now := platform.Now()
	for _, pool := range dbs {
		_, _ = pool.Exec(ctx, `UPDATE dual_write_journal SET state='recovering',updated_at=$2 WHERE tx_id=$1 AND state=$3`, gid, now, state)
	}
	prepared := map[string]bool{}
	for name, pool := range dbs {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_prepared_xacts WHERE transaction=$1)`, gid).Scan(&exists); err != nil {
			return fmt.Errorf("check prepared on %s: %w", name, err)
		}
		prepared[name] = exists
	}
	allGone := true
	for _, p := range prepared {
		if p {
			allGone = false
		}
	}
	switch {
	case allGone:
		/* 两库都已提交或都已回滚且无悬挂：保守按 aborted 收尾并留错误提示人工核对 */
		return w.finishJournal(ctx, dbs, gid, "aborted", "no prepared transaction found on either database")
	default:
		/* 已准备未提交者继续提交；不存在的说明已提交/已回滚，跳过 */
		for name, pool := range dbs {
			if !prepared[name] {
				continue
			}
			if _, err := pool.Exec(ctx, fmt.Sprintf("COMMIT PREPARED '%s'", gid)); err != nil {
				return fmt.Errorf("commit prepared on %s: %w", name, err)
			}
		}
		return w.finishJournal(ctx, dbs, gid, "committed", "")
	}
}

func (w *Writer) finishJournal(ctx context.Context, dbs map[string]*pgxpool.Pool, gid, state, lastErr string) error {
	now := platform.Now()
	for _, pool := range dbs {
		if _, err := pool.Exec(ctx, `UPDATE dual_write_journal SET state=$2,updated_at=$3,completed_at=$3,last_error=NULLIF($4,'') WHERE tx_id=$1`,
			gid, state, now, lastErr); err != nil {
			return err
		}
	}
	return nil
}

/* StartRecoverLoop 后台恢复循环：按 reconcile_interval 周期执行 Recover（悬挂 2PC）与
 * CatchUp（旧业务明文单写的增量补偿，设计 3.3 第 6 条）。返回停止函数。 */
func (w *Writer) StartRecoverLoop(ctx context.Context, interval time.Duration, rowCrypto *RowCrypto) func() {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				recCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				if err := w.Recover(recCtx); err != nil {
					log.Printf("dualwrite: recovery pass: %v", err)
				}
				if n, err := w.CatchUp(recCtx, rowCrypto); err != nil {
					log.Printf("dualwrite: catch-up pass: %v", err)
				} else if n > 0 {
					log.Printf("dualwrite: catch-up synced %d row(s) plain->secure", n)
				}
				cancel()
			}
		}
	}()
	return func() {
		close(stop)
		wg.Wait()
	}
}
