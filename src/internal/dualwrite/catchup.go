package dualwrite

import (
	"context"
	"fmt"
	"log"
	"strings"

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
	/* 根表先行：tenant 无 tenant_id 列不在探测清单内，但所有租户表 FK 依赖它——
	 * 必须最先同步，否则子表补偿恒 FK 23503（曾致两库种子数据分叉）。 */
	tables = append([]string{"tenant"}, tables...)
	/* 目标库（secure）已有的租户根行集合：子表只补偿根行已存在的租户；
	 * 缺根行的租户（未审批待 DEK 签发等）跳过并降格为提示，不再刷 FK 错误。 */
	secureTenants, err := tenantIDSet(ctx, w.store.Secure)
	if err != nil {
		return 0, err
	}
	plainTenants, err := tenantIDSet(ctx, w.store.Plain)
	if err != nil {
		return 0, err
	}
	skipped := map[string]bool{}
	synced := 0
	for _, table := range tables {
		n, err := w.catchUpTable(ctx, table, rowCrypto, secureTenants, plainTenants, skipped)
		if err != nil {
			log.Printf("dualwrite catchup: table %s: %v", table, err)
			continue
		}
		synced += n
	}
	if len(skipped) > 0 {
		log.Printf("dualwrite catchup: %d tenant(s) skipped (root row not in secure yet, e.g. pending approval)", len(skipped))
	}
	return synced, nil
}

func tenantIDSet(ctx context.Context, db *pgxpool.Pool) (map[string]bool, error) {
	rows, err := db.Query(ctx, `SELECT id FROM tenant`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

/* 控制表/日志/密钥不参与复制（与 datatool 排除口径一致，另排除审计与触发器强制表）。 */
var catchupExcluded = map[string]bool{
	"tenant_crypto_key": true, "auth_session": true, "platform_session": true,
	"auth_login_log": true, "audit_log": true, "platform_audit_log": true, "deletion_log": true,
	"dual_write_journal": true, "dual_write_saga": true, "dual_write_saga_step": true,
	"data_transfer_job": true, "data_transfer_item": true,
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

/* tableShape 按表实际结构探测：冲突键（主键/唯一索引列）、是否有 version/updated_at。
 * 各表结构不一（inbox_event 主键 (tenant_id,event_id) 无 id 列、tenant_subscription
 * 唯一约束为单列、部分表无 version/updated_at），不能硬编码 (tenant_id,id,version)。 */
type tableShape struct {
	PK           []string
	HasVersion   bool
	HasUpdatedAt bool
}

func detectShape(ctx context.Context, db *pgxpool.Pool, table string) (*tableShape, error) {
	colRows, err := db.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1`, table)
	if err != nil {
		return nil, err
	}
	cols := map[string]bool{}
	for colRows.Next() {
		var c string
		if err := colRows.Scan(&c); err != nil {
			colRows.Close()
			return nil, err
		}
		cols[c] = true
	}
	colRows.Close()
	if err := colRows.Err(); err != nil {
		return nil, err
	}
	shape := &tableShape{HasVersion: cols["version"], HasUpdatedAt: cols["updated_at"]}
	var pk any
	err = db.QueryRow(ctx, `SELECT (SELECT array_agg(a.attname ORDER BY k.ord)
			FROM unnest(i.indkey) WITH ORDINALITY AS k(attnum,ord)
			JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum)
		FROM pg_index i
		WHERE i.indrelid=$1::regclass AND i.indisvalid
			AND (i.indisprimary OR (i.indisunique AND i.indpred IS NULL))
		ORDER BY i.indisprimary DESC, i.indexrelid LIMIT 1`, "public."+table).Scan(&pk)
	if err != nil {
		return nil, err
	}
	switch v := pk.(type) {
	case []string:
		shape.PK = v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				shape.PK = append(shape.PK, s)
			}
		}
	}
	if len(shape.PK) == 0 {
		return nil, fmt.Errorf("no primary key or unique index found")
	}
	return shape, nil
}

type rowVersion struct {
	Key       string
	Version   int64
	UpdatedAt int64
}

func loadVersions(ctx context.Context, db *pgxpool.Pool, table string, shape *tableShape) (map[string]rowVersion, error) {
	pkSel := make([]string, len(shape.PK))
	for i, c := range shape.PK {
		pkSel[i] = fmt.Sprintf(`"%s"::text`, c)
	}
	verSel, updSel := "0", "0"
	if shape.HasVersion {
		verSel = "COALESCE(version,0)"
	}
	if shape.HasUpdatedAt {
		updSel = "COALESCE(updated_at,0)"
	}
	rows, err := db.Query(ctx, fmt.Sprintf(`SELECT %s,%s,%s FROM "%s"`, stringsJoin(pkSel, ","), verSel, updSel, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]rowVersion{}
	for rows.Next() {
		var rv rowVersion
		pkVals := make([]any, len(shape.PK))
		dest := make([]any, len(shape.PK))
		for i := range pkVals {
			dest[i] = &pkVals[i]
		}
		if err := rows.Scan(append(dest, &rv.Version, &rv.UpdatedAt)...); err != nil {
			return nil, err
		}
		parts := make([]string, len(pkVals))
		for i, v := range pkVals {
			parts[i] = fmt.Sprint(v)
		}
		rv.Key = stringsJoin(parts, "/")
		out[rv.Key] = rv
	}
	return out, rows.Err()
}

func (w *Writer) catchUpTable(ctx context.Context, table string, rowCrypto *RowCrypto, secureTenants, plainTenants map[string]bool, skipped map[string]bool) (int, error) {
	shape, err := detectShape(ctx, w.store.Plain, table)
	if err != nil {
		return 0, err
	}
	plainVersions, err := loadVersions(ctx, w.store.Plain, table, shape)
	if err != nil {
		return 0, err
	}
	secureVersions, err := loadVersions(ctx, w.store.Secure, table, shape)
	if err != nil {
		return 0, err
	}
	/* 需要复制/更新的冲突键；比较口径：优先 version，无 version 用 updated_at，都没有则只补缺失行 */
	newer := func(pv, sv rowVersion) bool {
		if shape.HasVersion {
			return pv.Version > sv.Version
		}
		return shape.HasUpdatedAt && pv.UpdatedAt > sv.UpdatedAt
	}
	var insertKeys, updateKeys []string
	for key, pv := range plainVersions {
		sv, ok := secureVersions[key]
		if !ok {
			insertKeys = append(insertKeys, key)
		} else if newer(pv, sv) {
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
			n, err := w.copyRows(ctx, table, keys[start:end], update, rowCrypto, shape, secureTenants, skipped)
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

/* copyRows 按冲突键从明文库读行写入密文库；敏感表先加密；version/updated_at 守卫禁止旧值覆盖新值。
 * 目标库缺租户根行的行跳过（待根行就绪后下一轮自然同步）；tenant 根行缺 DEK 时同样跳过。 */
func (w *Writer) copyRows(ctx context.Context, table string, keys []string, update bool, rowCrypto *RowCrypto, shape *tableShape, secureTenants map[string]bool, skipped map[string]bool) (int, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	synced := 0
	for _, key := range keys {
		pkVals := splitKeyN(key, len(shape.PK))
		where := make([]string, len(shape.PK))
		args := make([]any, len(shape.PK))
		for i, c := range shape.PK {
			where[i] = fmt.Sprintf(`"%s"=$%d`, c, i+1)
			args[i] = pkVals[i]
		}
		rows, err := w.store.Plain.Query(ctx, fmt.Sprintf(`SELECT * FROM "%s" WHERE %s`, table, stringsJoin(where, " AND ")), args...)
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
		tenantID, _ := row["tenant_id"].(string)
		if tenantID == "" {
			/* 根表（tenant）：行 id 即租户自身 */
			tenantID, _ = row["id"].(string)
		}
		if table != "tenant" && !secureTenants[tenantID] {
			skipped[tenantID] = true
			continue
		}
		if rowCrypto != nil && rowCrypto.Enabled() && len(sensitiveSet(table)) > 0 {
			encrypted, err := rowCrypto.ForSecure(ctx, tenantID, table, fmt.Sprint(pkVals[len(pkVals)-1]), row)
			if err != nil {
				if table == "tenant" {
					/* 未签发 DEK 的租户（如待审批注册）：跳过待密钥就绪，不阻断整表 */
					skipped[tenantID] = true
					continue
				}
				return synced, fmt.Errorf("encrypt %s %s: %w", table, key, err)
			}
			row = encrypted
		}
		if err := upsertSecureRow(ctx, w.store.Secure, table, columns, row, update, shape); err != nil {
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

func upsertSecureRow(ctx context.Context, db *pgxpool.Pool, table string, columns []string, row map[string]any, update bool, shape *tableShape) error {
	var ph []string
	args := make([]any, len(columns))
	for i, col := range columns {
		ph = append(ph, fmt.Sprintf("$%d", i+1))
		args[i] = row[col]
	}
	colList := `"` + joinColumns(columns) + `"`
	pkList := `"` + joinColumns(shape.PK) + `"`
	if update {
		/* version/updated_at 守卫：只在新版本更大时更新（禁止静默覆盖较新版本） */
		sets := make([]string, 0, len(columns))
		for i, col := range columns {
			sets = append(sets, fmt.Sprintf(`"%s"=$%d`, col, i+1))
		}
		where := make([]string, 0, len(shape.PK))
		for i, col := range shape.PK {
			where = append(where, fmt.Sprintf(`"%s"=$%d`, col, len(columns)+1+i))
			args = append(args, row[col])
		}
		if shape.HasVersion {
			where = append(where, fmt.Sprintf(`version<$%d`, len(args)+1))
			args = append(args, row["version"])
		} else if shape.HasUpdatedAt {
			where = append(where, fmt.Sprintf(`updated_at<$%d`, len(args)+1))
			args = append(args, row["updated_at"])
		}
		sql := fmt.Sprintf(`UPDATE "%s" SET %s WHERE %s`, table, joinSets(sets), stringsJoin(where, " AND "))
		_, err := db.Exec(ctx, sql, args...)
		return err
	}
	sql := fmt.Sprintf(`INSERT INTO "%s"(%s) VALUES(%s) ON CONFLICT (%s) DO NOTHING`, table, colList, joinPh(ph), pkList)
	_, err := db.Exec(ctx, sql, args...)
	return err
}

/* splitKeyN 把冲突键字符串按列数拆回各主键列值（列值内不出现 '/'）。 */
func splitKeyN(key string, n int) []string {
	parts := strings.SplitN(key, "/", n)
	for len(parts) < n {
		parts = append(parts, "")
	}
	return parts
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
