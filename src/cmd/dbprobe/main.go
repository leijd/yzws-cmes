// dbprobe 双库对账诊断：报告两库租户差异、关键表行数差异、悬挂事务与锁定等待。
// 用法：go run ./cmd/dbprobe [-config config.yaml]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"

	"clothp/internal/config"
	"clothp/internal/datastore"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

func main() {
	cfgPath := flag.String("config", "config.yaml", "配置文件路径")
	fixPrepared := flag.String("fix-prepared", "", "处置悬挂事务：格式 db:commit|rollback:gid（如 plain:rollback:clothp-xxx）")
	terminate := flag.String("terminate-pids", "", "终止后端进程：逗号分隔的 pid 列表（两库实例共享，一次即可）")
	purgeOrphans := flag.String("purge-secure-orphans", "", "删除密文库中 tenant 表不存在的租户残留（逗号分隔租户ID，按依赖序删除）")
	backfill := flag.String("backfill-plain", "", "把密文库有而明文库缺的非敏感表行补齐到明文库（逗号分隔租户ID）")
	query := flag.String("query", "", "在明文库执行任意 SELECT 并打印行（调试用）")
	flag.Parse()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	ctx := context.Background()
	store, err := datastore.Open(ctx, cfg)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()
	if store.Secure == nil {
		log.Fatalf("secure database not configured")
	}

	if *terminate != "" {
		for _, pidStr := range splitComma(*terminate) {
			var ok bool
			if err := store.Plain.QueryRow(ctx, `SELECT pg_terminate_backend($1)`, atoi(pidStr)).Scan(&ok); err != nil {
				fmt.Printf("terminate pid=%s 失败: %v\n", pidStr, err)
			} else {
				fmt.Printf("terminate pid=%s -> %v\n", pidStr, ok)
			}
		}
		return
	}
	if *query != "" {
		rows, err := store.Plain.Query(ctx, *query)
		if err != nil {
			log.Fatalf("query: %v", err)
		}
		defer rows.Close()
		fields := rows.FieldDescriptions()
		var header []string
		for _, f := range fields {
			header = append(header, string(f.Name))
		}
		fmt.Println(joinComma(header))
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				log.Fatalf("row: %v", err)
			}
			var cells []string
			for _, v := range values {
				cells = append(cells, fmt.Sprint(v))
			}
			fmt.Println(joinComma(cells))
		}
		return
	}
	if *backfill != "" {
		/* 仅无敏感列的结构表；敏感表请走服务端 catchup（含加密）。 */
		tables := []string{"org_unit", "role", "permission", "role_permission", "mgmt_level", "feature_setting"}
		for _, tenantID := range splitComma(*backfill) {
			for _, table := range tables {
				n, err := backfillTable(ctx, store, table, tenantID)
				if err != nil {
					log.Fatalf("backfill %s/%s: %v", table, tenantID, err)
				}
				if n > 0 {
					fmt.Printf("  plain.%s 补入 %d 行（tenant=%s）\n", table, n, tenantID)
				}
			}
		}
		return
	}
	if *purgeOrphans != "" {
		/* 依赖逆序：子表先删。仅处理本次诊断确认有残留的表。 */
		tables := []string{"role_permission", "permission", "role", "org_unit"}
		for _, tenantID := range splitComma(*purgeOrphans) {
			var exists bool
			if err := store.Plain.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant WHERE id=$1)`, tenantID).Scan(&exists); err != nil {
				log.Fatalf("check plain tenant: %v", err)
			}
			var existsSecure bool
			if err := store.Secure.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant WHERE id=$1)`, tenantID).Scan(&existsSecure); err != nil {
				log.Fatalf("check secure tenant: %v", err)
			}
			if exists || existsSecure {
				log.Fatalf("租户 %s 在 tenant 表中存在（plain=%v secure=%v），拒绝按孤儿清除", tenantID, exists, existsSecure)
			}
			for _, table := range tables {
				result, err := store.Secure.Exec(ctx, fmt.Sprintf(`DELETE FROM "%s" WHERE tenant_id=$1`, table), tenantID)
				if err != nil {
					log.Fatalf("purge %s/%s: %v", table, tenantID, err)
				}
				fmt.Printf("  secure.%s 删除 %d 行（tenant=%s）\n", table, result.RowsAffected(), tenantID)
			}
		}
		return
	}
	if *fixPrepared != "" {
		parts := splitN3(*fixPrepared)
		db, action, gid := parts[0], parts[1], parts[2]
		pool := store.Plain
		if db == "secure" {
			pool = store.Secure
		}
		sql := "COMMIT PREPARED '" + gid + "'"
		if action == "rollback" {
			sql = "ROLLBACK PREPARED '" + gid + "'"
		} else if action != "commit" {
			log.Fatalf("action 只能为 commit 或 rollback")
		}
		if _, err := pool.Exec(ctx, sql); err != nil {
			log.Fatalf("%s 失败: %v", sql, err)
		}
		fmt.Printf("%s 成功\n", sql)
		return
	}

	fmt.Println("== 租户差异（plain vs secure）==")
	plainTenants := tenantSet(ctx, store, "plain")
	secureTenants := tenantSet(ctx, store, "secure")
	for id, desc := range plainTenants {
		if _, ok := secureTenants[id]; !ok {
			fmt.Printf("  仅明文库: %s\n", desc)
		}
	}
	for id, desc := range secureTenants {
		if _, ok := plainTenants[id]; !ok {
			fmt.Printf("  仅密文库: %s\n", desc)
		}
	}
	if len(plainTenants) == len(secureTenants) {
		same := true
		for id := range plainTenants {
			if _, ok := secureTenants[id]; !ok {
				same = false
			}
		}
		if same {
			fmt.Println("  （无差异）")
		}
	}

	fmt.Println("== 关键表行数（plain / secure / 孤儿明细）==")
	for _, table := range []string{"org_unit", "employee", "user_account", "role", "permission", "role_permission", "user_role", "tenant_subscription", "mgmt_level", "feature_setting", "tenant_crypto_key"} {
		reportTable(ctx, store, table)
	}

	for _, name := range []string{"plain", "secure"} {
		pool := store.Plain
		if name == "secure" {
			pool = store.Secure
		}
		fmt.Printf("== %s 悬挂 prepared 事务 ==\n", name)
		var pxCount int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM pg_prepared_xacts`).Scan(&pxCount); err != nil {
			fmt.Printf("  COUNT 查询失败: %v\n", err)
		} else {
			fmt.Printf("  pg_prepared_xacts 行数: %d\n", pxCount)
		}
		rows, err := pool.Query(ctx, `SELECT gid, database, owner, prepared::text FROM pg_prepared_xacts`)
		if err != nil {
			fmt.Printf("  查询失败: %v\n", err)
		} else {
			n := 0
			for rows.Next() {
				var gid, db, owner, prepared string
				if err := rows.Scan(&gid, &db, &owner, &prepared); err == nil {
					fmt.Printf("  gid=%s db=%s owner=%s prepared=%s\n", gid, db, owner, prepared)
					n++
				}
			}
			rows.Close()
			if n == 0 {
				fmt.Println("  （无）")
			}
		}
		fmt.Printf("== %s 阻塞链 ==\n", name)
		rows, err = pool.Query(ctx, `SELECT a.pid, a.datname, pg_blocking_pids(a.pid)::text, a.state, (now()-a.xact_start)::text, left(a.query,80)
			FROM pg_stat_activity a WHERE cardinality(pg_blocking_pids(a.pid))>0 OR a.pid IN
			(SELECT unnest(pg_blocking_pids(pid)) FROM pg_stat_activity WHERE cardinality(pg_blocking_pids(pid))>0)`)
		if err != nil {
			fmt.Printf("  查询失败: %v\n", err)
		} else {
			for rows.Next() {
				var pid int
				var db, blockers, state, age, q string
				if err := rows.Scan(&pid, &db, &blockers, &state, &age, &q); err != nil {
					fmt.Printf("  扫描失败: %v\n", err)
					continue
				}
				fmt.Printf("  pid=%d db=%s blocked_by=%s state=%s age=%s q=%s\n", pid, db, blockers, state, age, q)
			}
			rows.Close()
		}
		fmt.Printf("== %s transactionid 锁持有者与等待者 ==\n", name)
		rows2, err := pool.Query(ctx, `SELECT w.pid, w.txid, h.pid, h.virtualtransaction, h.granted
			FROM pg_locks w JOIN pg_locks h ON h.locktype='transactionid' AND h.transactionid=w.txid AND h.granted
			WHERE w.locktype='transactionid' AND NOT w.granted`)
		if err != nil {
			fmt.Printf("  查询失败: %v\n", err)
		} else {
			n := 0
			for rows2.Next() {
				var wpid int
				var txid int64
				var hpid *int
				var vxid *string
				var granted bool
				if err := rows2.Scan(&wpid, &txid, &hpid, &vxid, &granted); err != nil {
					fmt.Printf("  扫描失败: %v\n", err)
					continue
				}
				fmt.Printf("  waiter pid=%d 等待 xid=%d；持有者 pid=%v vxid=%v\n", wpid, txid, intv(hpid), strv(vxid))
				n++
			}
			rows2.Close()
			if n == 0 {
				fmt.Println("  （无 transactionid 锁等待）")
			}
		}
		fmt.Printf("== %s 无 pid 持锁（prepared xact 特征）==\n", name)
		rows3, err := pool.Query(ctx, `SELECT locktype, mode, COALESCE(relation::regclass::text,''), COALESCE(transactionid::text,'') FROM pg_locks WHERE pid IS NULL AND granted`)
		if err != nil {
			fmt.Printf("  查询失败: %v\n", err)
		} else {
			n := 0
			for rows3.Next() {
				var lt, mode, rel, txid string
				if err := rows3.Scan(&lt, &mode, &rel, &txid); err != nil {
					continue
				}
				fmt.Printf("  locktype=%s mode=%s relation=%s xid=%s\n", lt, mode, rel, txid)
				n++
			}
			rows3.Close()
			if n == 0 {
				fmt.Println("  （无）")
			}
		}
		fmt.Printf("== %s 未收尾双写流水 ==\n", name)
		rows, err = pool.Query(ctx, `SELECT tx_id,state,COALESCE(last_error,'') FROM dual_write_journal WHERE state NOT IN ('committed','aborted')`)
		if err != nil {
			fmt.Printf("  查询失败: %v\n", err)
		} else {
			n := 0
			for rows.Next() {
				var txID, state, lastErr string
				if err := rows.Scan(&txID, &state, &lastErr); err == nil {
					fmt.Printf("  tx=%s state=%s err=%s\n", txID, state, lastErr)
					n++
				}
			}
			rows.Close()
			if n == 0 {
				fmt.Println("  （无）")
			}
		}
		fmt.Printf("== %s 锁等待 ==\n", name)
		rows, err = pool.Query(ctx, `SELECT pid, wait_event_type, wait_event, state, left(query,60) FROM pg_stat_activity WHERE wait_event_type='Lock'`)
		if err != nil {
			fmt.Printf("  查询失败: %v\n", err)
		} else {
			n := 0
			for rows.Next() {
				var pid int
				var t, e, s, q *string
				if err := rows.Scan(&pid, &t, &e, &s, &q); err == nil {
					fmt.Printf("  pid=%d %v/%v state=%v q=%v\n", pid, strv(t), strv(e), strv(s), strv(q))
					n++
				}
			}
			rows.Close()
			if n == 0 {
				fmt.Println("  （无）")
			}
		}
	}
}

func strv(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intv(i *int) int {
	if i == nil {
		return -1
	}
	return *i
}

func tenantSet(ctx context.Context, store *datastore.Store, which string) map[string]string {
	pool := store.Plain
	if which == "secure" {
		pool = store.Secure
	}
	out := map[string]string{}
	rows, err := pool.Query(ctx, `SELECT id,code,name,COALESCE(customer_type,''),COALESCE(registration_status,'') FROM tenant ORDER BY created_at`)
	if err != nil {
		fmt.Printf("  %s tenant 查询失败: %v\n", which, err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, code, name, ct, rs string
		if err := rows.Scan(&id, &code, &name, &ct, &rs); err == nil {
			out[id] = fmt.Sprintf("%s %s %s type=%s reg=%s", id, code, name, ct, rs)
		}
	}
	return out
}

func reportTable(ctx context.Context, store *datastore.Store, table string) {
	var pc, sc int64
	if err := store.Plain.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, table)).Scan(&pc); err != nil {
		fmt.Printf("  %s: plain 查询失败 %v\n", table, err)
		return
	}
	if err := store.Secure.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, table)).Scan(&sc); err != nil {
		fmt.Printf("  %s: secure 查询失败 %v\n", table, err)
		return
	}
	mark := ""
	if pc != sc {
		mark = "  <== 不一致"
	}
	fmt.Printf("  %-22s %d / %d%s\n", table, pc, sc, mark)
	if pc == sc {
		return
	}
	/* 跨库 key 对比（应用层）：报告两边独有行按租户聚合 */
	plainKeys := rowKeys(ctx, store.Plain, table)
	secureKeys := rowKeys(ctx, store.Secure, table)
	onlyPlain := diffByTenant(plainKeys, secureKeys)
	onlySecure := diffByTenant(secureKeys, plainKeys)
	if len(onlyPlain) > 0 {
		fmt.Printf("    仅明文库: %s\n", joinComma(onlyPlain))
	}
	if len(onlySecure) > 0 {
		fmt.Printf("    仅密文库: %s\n", joinComma(onlySecure))
	}
}

/* backfillTable 把 secure 有而 plain 无（按 tenant_id+id）的行原样复制到 plain（非敏感表）。 */
func backfillTable(ctx context.Context, store *datastore.Store, table, tenantID string) (int, error) {
	plainKeys := rowKeys(ctx, store.Plain, table)
	rows, err := store.Secure.Query(ctx, fmt.Sprintf(`SELECT * FROM "%s" WHERE tenant_id=$1`, table), tenantID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = string(f.Name)
	}
	synced := 0
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return synced, err
		}
		rowMap := map[string]any{}
		for i, col := range columns {
			rowMap[col] = values[i]
		}
		key := fmt.Sprint(rowMap["tenant_id"]) + "/" + fmt.Sprint(rowMap["id"])
		if _, ok := plainKeys[key]; ok {
			continue
		}
		var colList, ph []string
		args := make([]any, len(columns))
		for i, col := range columns {
			colList = append(colList, `"`+col+`"`)
			ph = append(ph, fmt.Sprintf("$%d", i+1))
			args[i] = rowMap[col]
		}
		if _, err := store.Plain.Exec(ctx, fmt.Sprintf(`INSERT INTO "%s"(%s) VALUES(%s)`, table, joinComma(colList), joinComma(ph)), args...); err != nil {
			return synced, fmt.Errorf("insert %s key=%s: %w", table, key, err)
		}
		synced++
	}
	return synced, rows.Err()
}

func rowKeys(ctx context.Context, pool interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}, table string) map[string]string {
	out := map[string]string{}
	rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT tenant_id, id FROM "%s"`, table))
	if err != nil {
		fmt.Printf("    rowKeys %s 查询失败: %v\n", table, err)
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var tid, id string
		if err := rows.Scan(&tid, &id); err == nil {
			out[tid+"/"+id] = tid
		}
	}
	return out
}

func diffByTenant(a, b map[string]string) []string {
	counts := map[string]int{}
	for key, tid := range a {
		if _, ok := b[key]; !ok {
			counts[tid]++
		}
	}
	var out []string
	for tid, n := range counts {
		out = append(out, fmt.Sprintf("%s:%d", tid, n))
	}
	sort.Strings(out)
	if len(out) > 8 {
		out = out[:8]
	}
	return out
}

func joinComma(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out
}

func splitComma(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitN3(s string) [3]string {
	parts := strings.SplitN(s, ":", 3)
	var out [3]string
	copy(out[:], parts)
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func init() { os.Setenv("PGCONNECT_TIMEOUT", "10") }
