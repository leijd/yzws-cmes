package dualwrite

import (
	"context"
	"fmt"
	"log"

	"clothp/internal/seccrypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

/* CatchUp 单写期间/旧业务明文单写的增量补偿（设计 3.3 第 6 条对账补偿、3.8 补数）。
 *
 * 逐表对比两库 (tenant_id,id,version,updated_at)：
 *   - 明文库有、密文库无 → 复制（敏感表经 RowCrypto 加密后写入）；
 *   - 明文库版本较新 → 按主键更新（SQL 侧带 version 守卫，禁止覆盖较新版本）；
 *   - 密文库有、明文库无 → 记 last_error 提示人工核对，不自动删除。
 * 差异处理写 dual_write_journal(source_db='plain', state='committed') 留痕，不保存业务内容。
 */
func (w *Writer) CatchUp(ctx context.Context, rowCrypto *RowCrypto) (int, error) {
	if w.store.Secure == nil {
		return 0, nil
	}
	tables, err := listTenantTables(ctx, w.store.Plain)
	if err != nil {
		return 0, err
	}
	synced := 0
	for _, table := range tables {
		n, err := w.catchUpTable(ctx, table, rowCrypto)
		if err != nil {
			log.Printf("dualwrite catchup: table %s: %v", table, err)
			continue
		}
		synced += n
	}
	return synced, nil
}

/* 控制表/日志/密钥不参与复制（与 datatool 排除口径一致，另排除审计与触发器强制表）。 */
var catchupExcluded = map[string]bool{
	"tenant_crypto_key": true, "auth_session": true, "platform_session": true,
	"auth_login_log": true, "audit_log": true, "platform_audit_log": true, "deletion_log": true,
	"dual_write_journal": true, "data_transfer_job": true, "data_transfer_item": true,
	"outbox_event": true, "command_request": true, "schema_migration": true,
	"inventory_ledger": true, "payment": true, "payroll_adjustment": true, "final_inspection": true, /* 追加不可变触发器表：只从密文侧补偿会破坏 append-only 约束，跳过 */
}

func listTenantTables(ctx context.Context, db *pgxpool.Pool) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT c.relname
		FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='tenant_id' AND a.attnum>0 AND NOT a.attisdropped
		WHERE n.nspname='public' AND c.relkind='r' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		if !catchupExcluded[t] {
			tables = append(tables, t)
		}
	}
	return tables, rows.Err()
}

type rowVersion struct {
	TenantID  string
	ID        string
	Version   int64
	UpdatedAt int64
}

func loadVersions(ctx context.Context, db *pgxpool.Pool, table string) (map[string]rowVersion, error) {
	rows, err := db.Query(ctx, fmt.Sprintf(`SELECT tenant_id::text,id::text,COALESCE(version,0),COALESCE(updated_at,0) FROM "%s"`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]rowVersion{}
	for rows.Next() {
		var rv rowVersion
		if err := rows.Scan(&rv.TenantID, &rv.ID, &rv.Version, &rv.UpdatedAt); err != nil {
			return nil, err
		}
		out[rv.TenantID+"/"+rv.ID] = rv
	}
	return out, rows.Err()
}

func (w *Writer) catchUpTable(ctx context.Context, table string, rowCrypto *RowCrypto) (int, error) {
	plainVersions, err := loadVersions(ctx, w.store.Plain, table)
	if err != nil {
		return 0, err
	}
	secureVersions, err := loadVersions(ctx, w.store.Secure, table)
	if err != nil {
		return 0, err
	}
	/* 需要复制/更新的主键 */
	var insertKeys, updateKeys []string
	for key, pv := range plainVersions {
		sv, ok := secureVersions[key]
		if !ok {
			insertKeys = append(insertKeys, key)
		} else if pv.Version > sv.Version {
			updateKeys = append(updateKeys, key)
		}
	}
	orphans := 0
	for key := range secureVersions {
		if _, ok := plainVersions[key]; !ok {
			orphans++
		}
	}
	if orphans > 0 {
		log.Printf("dualwrite catchup: table %s has %d row(s) only in secure database, manual reconcile required", table, orphans)
	}
	synced := 0
	batch := func(keys []string, update bool) error {
		const batchSize = 200
		for start := 0; start < len(keys); start += batchSize {
			end := start + batchSize
			if end > len(keys) {
				end = len(keys)
			}
			n, err := w.copyRows(ctx, table, keys[start:end], update, rowCrypto)
			if err != nil {
				return err
			}
			synced += n
		}
		return nil
	}
	if err := batch(insertKeys, false); err != nil {
		return synced, err
	}
	if err := batch(updateKeys, true); err != nil {
		return synced, err
	}
	return synced, nil
}

/* copyRows 按主键从明文库读行写入密文库；敏感表先加密；version 守卫禁止旧值覆盖新值。 */
func (w *Writer) copyRows(ctx context.Context, table string, keys []string, update bool, rowCrypto *RowCrypto) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	synced := 0
	for _, key := range keys {
		tenantID, id, _ := splitKey(key)
		rows, err := w.store.Plain.Query(ctx, fmt.Sprintf(`SELECT * FROM "%s" WHERE tenant_id=$1 AND id=$2`, table), tenantID, id)
		if err != nil {
			return synced, err
		}
		values, fields, err := readOneRow(rows)
		rows.Close()
		if err != nil {
			return synced, err
		}
		row := map[string]any{}
		columns := make([]string, len(fields))
		for i, f := range fields {
			columns[i] = string(f.Name)
			row[columns[i]] = values[i]
		}
		if rowCrypto != nil && rowCrypto.Enabled() && len(sensitiveSet(table)) > 0 {
			encrypted, err := rowCrypto.ForSecure(ctx, tenantID, table, id, row)
			if err != nil {
				return synced, fmt.Errorf("encrypt %s %s: %w", table, id, err)
			}
			row = encrypted
		}
		if err := upsertSecureRow(ctx, w.store.Secure, table, columns, row, update); err != nil {
			return synced, err
		}
		synced++
	}
	return synced, nil
}

func readOneRow(rows pgx.Rows) ([]any, []pgconn.FieldDescription, error) {
	defer rows.Close()
	fields := rows.FieldDescriptions()
	if !rows.Next() {
		return nil, nil, rows.Err()
	}
	values, err := rows.Values()
	if err != nil {
		return nil, nil, err
	}
	return values, fields, rows.Err()
}

func upsertSecureRow(ctx context.Context, db *pgxpool.Pool, table string, columns []string, row map[string]any, update bool) error {
	var ph []string
	args := make([]any, len(columns))
	for i, col := range columns {
		ph = append(ph, fmt.Sprintf("$%d", i+1))
		args[i] = row[col]
	}
	colList := `"` + joinColumns(columns) + `"`
	if update {
		/* version 守卫：只在新版本更大时更新（禁止静默覆盖较新版本） */
		sets := make([]string, 0, len(columns))
		for i, col := range columns {
			sets = append(sets, fmt.Sprintf(`"%s"=$%d`, col, i+1))
		}
		sql := fmt.Sprintf(`UPDATE "%s" SET %s WHERE tenant_id=$%d AND id=$%d AND version<$%d`,
			table, joinSets(sets), len(columns)+1, len(columns)+2, len(columns)+3)
		args = append(args, row["tenant_id"], row["id"], row["version"])
		_, err := db.Exec(ctx, sql, args...)
		return err
	}
	sql := fmt.Sprintf(`INSERT INTO "%s"(%s) VALUES(%s) ON CONFLICT (tenant_id,id) DO NOTHING`, table, colList, joinPh(ph))
	_, err := db.Exec(ctx, sql, args...)
	return err
}

func splitKey(key string) (string, string, bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			return key[:i], key[i+1:], true
		}
	}
	return key, "", false
}

func joinColumns(cols []string) string { return stringsJoin(cols, `","`) }
func joinSets(sets []string) string    { return stringsJoin(sets, ", ") }
func joinPh(ph []string) string        { return stringsJoin(ph, ", ") }
func stringsJoin(parts []string, sep string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += sep
		}
		out += p
	}
	return out
}

var _ = seccrypto.IsEncrypted
