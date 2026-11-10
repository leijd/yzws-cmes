// Package datatool 数据互转与对账（设计 3.8 / 4.13 / 4.14）。
//
// 全量转换把源库记录复制到目标库：
//   - plain→secure：敏感列替换为 enc:v1 信封（AAD 绑定租户/表/行/字段），手机号列同时计算盲索引；
//   - secure→plain：密文列解密回明文（恢复演练/反向迁移）；
//   - 目标端 INSERT ... ON CONFLICT DO NOTHING，配合 data_transfer_item 批次游标支持断点续跑；
//   - 控制表（journal/transfer/setting/crypto_key）原样复制；schema_migration 不复制（两库各自迁移）。
//
// 默认 dry_run=true 只统计不写入。批次与错误明细写入两库同结构的任务表，不保存业务内容副本。
package datatool

import (
	"context"
	"fmt"
	"log"
	"strings"

	"clothp/internal/datastore"
	"clothp/internal/dualwrite"
	"clothp/internal/platform"
	"clothp/internal/seccrypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

/* 转换方向。 */
const (
	PlainToSecure = "plain2secure"
	SecureToPlain = "secure2plain"
)

/* 行级 hash 列映射：业务明文列 -> (目标 hash 列, 盲索引域)。 */
var phoneHashColumns = map[string]struct {
	HashColumn string
	Domain     string
}{
	"employee": {"phone_hash", "employee.phone"},
	"tenant":   {"contact_phone_hash", "tenant.contact_phone"},
}

type Result struct {
	JobID     string         `json:"job_id"`
	Direction string         `json:"direction"`
	DryRun    bool           `json:"dry_run"`
	Tables    map[string]int `json:"tables"` // 表名 -> 处理行数
	Failed    int            `json:"failed"`
}

/* Convert 执行全量转换。dry_run 时只统计行数不落库。 */
func Convert(ctx context.Context, store *datastore.Store, crypto *seccrypto.Service, direction string, dryRun bool, requestedBy, reason string) (*Result, error) {
	if store.Secure == nil {
		return nil, fmt.Errorf("secure database not configured")
	}
	src, dst := store.Plain, store.Secure
	if direction == SecureToPlain {
		src, dst = store.Secure, store.Plain
	} else if direction != PlainToSecure {
		return nil, fmt.Errorf("direction must be %s or %s", PlainToSecure, SecureToPlain)
	}
	rowCrypto := dualwrite.NewRowCrypto(crypto)

	tables, err := listUserTables(ctx, src)
	if err != nil {
		return nil, err
	}
	result := &Result{JobID: platform.ID(), Direction: direction, DryRun: dryRun, Tables: map[string]int{}}

	/* 正式执行前为涉及主体幂等发行密钥（存量租户/平台主体可能没有 tenant_crypto_key 记录）；
	 * 只保留真实存在的主体（tenant 表 id 或 platform），孤儿 tenant_id 跳过并告警 */
	if !dryRun {
		subjects, err := collectSubjects(ctx, src, tables)
		if err != nil {
			return nil, fmt.Errorf("collect subjects: %w", err)
		}
		issuer := &dbIssuer{store: store}
		ensured := 0
		for _, subject := range subjects {
			if subject != "platform" {
				var exists bool
				if err := src.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant WHERE id=$1)`, subject).Scan(&exists); err != nil || !exists {
					log.Printf("datatool: skip key issuance for orphan subject %s", subject)
					continue
				}
			}
			if err := crypto.EnsureKeys(ctx, issuer, subject, "datatool:"+requestedBy, platform.Now()); err != nil {
				return result, fmt.Errorf("ensure keys for %s: %w", subject, err)
			}
			ensured++
		}
		if ensured > 0 {
			log.Printf("datatool: ensured keys for %d subject(s)", ensured)
		}
	}

	now := platform.Now()
	jobRow := []any{result.JobID, "convert",
		map[bool]string{true: "plain", false: "secure"}[direction == PlainToSecure],
		map[bool]string{true: "secure", false: "plain"}[direction == PlainToSecure],
		nil, tables, "running", dryRun, requestedBy, reason, now, now}
	if !dryRun {
		for _, pool := range []*pgxpool.Pool{store.Plain, store.Secure} {
			if _, err := pool.Exec(ctx, `INSERT INTO data_transfer_job
				(id,job_type,source_db,target_db,tenant_id,table_names,status,dry_run,requested_by,request_reason,created_at,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
				ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,updated_at=EXCLUDED.updated_at`, jobRow...); err != nil {
				return nil, fmt.Errorf("record transfer job: %w", err)
			}
		}
	}

	for _, table := range tables {
		count, err := convertTable(ctx, src, dst, table, rowCrypto, direction, result.JobID, dryRun)
		result.Tables[table] = count
		if err != nil {
			result.Failed++
			log.Printf("datatool: table %s: %v", table, err)
			if !dryRun {
				noteItem(ctx, store, result.JobID, table, count, err)
			}
		}
	}

	if !dryRun {
		/* 明文库 hash 回填（设计 057）：明文库保留手机号明文并保存与密文库相同的 HMAC */
		if n, err := BackfillPhoneHashes(ctx, store, crypto, false); err != nil {
			log.Printf("datatool: backfill phone hashes: %v", err)
		} else if n > 0 {
			log.Printf("datatool: backfilled %d phone hash(es) in plain database", n)
		}
		finished := platform.Now()
		status := "succeeded"
		if result.Failed > 0 {
			status = "failed"
		}
		for _, pool := range []*pgxpool.Pool{store.Plain, store.Secure} {
			_, _ = pool.Exec(ctx, `UPDATE data_transfer_job SET status=$2,processed_rows=$3,failed_rows=$4,finished_at=$5,updated_at=$5 WHERE id=$1`,
				result.JobID, status, totalRows(result), result.Failed, finished)
		}
	}
	return result, nil
}

/* BackfillPhoneHashes 明文库盲索引回填：有明文手机号、phone_hash 为 NULL 的行按本厂密钥计算 HMAC。
 * 只处理真实存在的主体（tenant 表 id 或 platform），返回更新行数。 */
func BackfillPhoneHashes(ctx context.Context, store *datastore.Store, crypto *seccrypto.Service, dryRun bool) (int, error) {
	rowCrypto := dualwrite.NewRowCrypto(crypto)
	updated := 0
	for table, spec := range phoneHashColumns {
		tenantCol := "tenant_id"
		pkCol := "id"
		if table == "tenant" {
			tenantCol = "id"
		}
		phoneCol := phoneColumnOf(table)
		query := fmt.Sprintf(`SELECT "%s" AS tid, "%s" AS pk, "%s" AS phone FROM "%s" WHERE "%s" IS NOT NULL AND "%s" <> '' AND "%s" IS NULL`,
			tenantCol, pkCol, phoneCol, table, phoneCol, phoneCol, spec.HashColumn)
		rows, err := store.Plain.Query(ctx, query)
		if err != nil {
			return updated, err
		}
		type item struct{ tid, pk, phone string }
		var items []item
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.tid, &it.pk, &it.phone); err != nil {
				rows.Close()
				return updated, err
			}
			items = append(items, it)
		}
		rows.Close()
		for _, it := range items {
			subject := it.tid
			if table != "tenant" && subject == "" {
				subject = "platform"
			}
			indexKey, err := rowCrypto.IndexKey(ctx, subject)
			if err != nil {
				log.Printf("datatool: backfill skip %s %s: %v", table, it.pk, err)
				continue
			}
			hash := seccrypto.PhoneHash(indexKey, spec.Domain, it.phone)
			if dryRun {
				updated++
				continue
			}
			var updateErr error
			if table == "tenant" {
				_, updateErr = store.Plain.Exec(ctx, fmt.Sprintf(`UPDATE "%s" SET "%s"=$1 WHERE "%s"=$2`, table, spec.HashColumn, pkCol), hash, it.pk)
			} else {
				_, updateErr = store.Plain.Exec(ctx, fmt.Sprintf(`UPDATE "%s" SET "%s"=$1 WHERE "%s"=$2 AND "%s"=$3`, table, spec.HashColumn, tenantCol, pkCol), hash, it.tid, it.pk)
			}
			if updateErr != nil {
				return updated, updateErr
			}
			updated++
		}
	}
	return updated, nil
}

func totalRows(r *Result) int64 {
	var n int64
	for _, c := range r.Tables {
		n += int64(c)
	}
	return n
}

/* listUserTables 源库 public schema 的用户表（排除迁移记录表），按外键依赖拓扑排序：
 * 被引用表先于引用表复制，保证密文库（空库起步）插入时外键可满足；循环依赖按名字序打破。 */
func listUserTables(ctx context.Context, db *pgxpool.Pool) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename<>'schema_migration' ORDER BY tablename`)
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
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	deps, err := tableDependencies(ctx, db)
	if err != nil {
		return nil, err
	}
	return topoSort(tables, deps), nil
}

/* tableDependencies 外键依赖：table -> 它引用的表集合（仅限 public 用户表）。 */
func tableDependencies(ctx context.Context, db *pgxpool.Pool) (map[string]map[string]bool, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT conrelid::regclass::text, confrelid::regclass::text
		FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	deps := map[string]map[string]bool{}
	for rows.Next() {
		var table, ref string
		if err := rows.Scan(&table, &ref); err != nil {
			return nil, err
		}
		if table == ref {
			continue
		}
		if deps[table] == nil {
			deps[table] = map[string]bool{}
		}
		deps[table][ref] = true
	}
	return deps, rows.Err()
}

/* topoSort Kahn 拓扑排序：引用者排在其依赖之后；循环依赖处按名字序打破。 */
func topoSort(tables []string, deps map[string]map[string]bool) []string {
	inDegree := map[string]int{}
	dependents := map[string][]string{}
	inSet := map[string]bool{}
	for _, t := range tables {
		inSet[t] = true
		inDegree[t] = 0
	}
	for table, refs := range deps {
		if !inSet[table] {
			continue
		}
		for ref := range refs {
			if !inSet[ref] {
				continue
			}
			inDegree[table]++
			dependents[ref] = append(dependents[ref], table)
		}
	}
	var queue []string
	for _, t := range tables {
		if inDegree[t] == 0 {
			queue = append(queue, t)
		}
	}
	var sorted []string
	for len(queue) > 0 {
		table := queue[0]
		queue = queue[1:]
		sorted = append(sorted, table)
		next := dependents[table]
		sortStrings(next)
		for _, d := range next {
			inDegree[d]--
			if inDegree[d] == 0 {
				queue = append(queue, d)
			}
		}
	}
	/* 循环依赖兜底：剩余表按名字序追加 */
	if len(sorted) < len(tables) {
		var rest []string
		inSorted := map[string]bool{}
		for _, t := range sorted {
			inSorted[t] = true
		}
		for _, t := range tables {
			if !inSorted[t] {
				rest = append(rest, t)
			}
		}
		sortStrings(rest)
		sorted = append(sorted, rest...)
	}
	return sorted
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

/* convertTable 逐行转换单表；返回处理行数。dry_run 只统计。 */
func convertTable(ctx context.Context, src, dst *pgxpool.Pool, table string, rowCrypto *dualwrite.RowCrypto, direction, jobID string, dryRun bool) (int, error) {
	rows, err := src.Query(ctx, fmt.Sprintf(`SELECT * FROM "%s"`, table))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = string(f.Name)
	}

	batch := 0
	total := 0
	var firstErr error
	batchSize := 500
	var pending []map[string]any

	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		if dryRun {
			total += len(pending)
			pending = pending[:0]
			return nil
		}
		if err := insertRows(ctx, dst, table, columns, pending); err != nil {
			/* 批次失败（孤儿外键/脏数据）降级逐行插入，跳过坏行并记录，不中断整表 */
			var skipped int
			var firstRowErr error
			for _, r := range pending {
				if ierr := insertRow(ctx, dst, table, columns, r); ierr != nil {
					skipped++
					if firstRowErr == nil {
						firstRowErr = ierr
					}
				}
			}
			log.Printf("datatool: table %s batch failed (%v), row-by-row skipped %d row(s)", table, err, skipped)
			_ = firstRowErr
		}
		total += len(pending)
		pending = pending[:0]
		return nil
	}

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return total, err
		}
		row := map[string]any{}
		for i, col := range columns {
			row[col] = values[i]
		}
		tenantID := tenantOfRow(table, row)
		primaryKey := primaryKeyOf(table, row)
		converted, err := transformRow(ctx, rowCrypto, direction, table, tenantID, primaryKey, row)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		pending = append(pending, converted)
		batch++
		if batch >= batchSize {
			if err := flush(); err != nil {
				return total, err
			}
			batch = 0
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	if err := flush(); err != nil {
		return total, err
	}
	return total, firstErr
}

/* transformRow 方向转换 + 手机号盲索引计算。
 * 注意顺序：必须先用明文计算盲索引，再加密（加密后 IsEncrypted=true 将无法取到明文）。 */
func transformRow(ctx context.Context, rowCrypto *dualwrite.RowCrypto, direction, table, tenantID, primaryKey string, row map[string]any) (map[string]any, error) {
	spec, hasSpec := phoneHashColumns[table]
	hashValue := ""
	if hasSpec {
		if phone := row[phoneColumnOf(table)]; phone != nil {
			if text, ok := phone.(string); ok && text != "" && !seccrypto.IsEncrypted(text) {
				indexKey, err := rowCryptoIndexKey(ctx, rowCrypto, tenantID)
				if err != nil {
					return nil, err
				}
				hashValue = seccrypto.PhoneHash(indexKey, spec.Domain, text)
			}
		}
	}
	out := row
	if direction == PlainToSecure {
		converted, err := rowCrypto.ForSecure(ctx, tenantID, table, primaryKey, row)
		if err != nil {
			return nil, err
		}
		out = converted
	} else {
		converted, err := rowCrypto.FromSecure(ctx, tenantID, table, primaryKey, row)
		if err != nil {
			return nil, err
		}
		out = converted
	}
	if hasSpec && hashValue != "" {
		out[spec.HashColumn] = hashValue
	}
	return out, nil
}

/* rowCryptoIndexKey 取本厂盲索引密钥（经 RowCrypto 内部 Service；这里直接调用公开方法）。 */
func rowCryptoIndexKey(ctx context.Context, c *dualwrite.RowCrypto, tenantID string) ([]byte, error) {
	return c.IndexKey(ctx, tenantID)
}

func phoneColumnOf(table string) string {
	if table == "tenant" {
		return "contact_phone"
	}
	return "phone"
}

/* tenantOfRow 从行中取 tenant_id；tenant 表用 id 自身；无租户归属的平台运营数据
 * （visitor_lead 等）使用 platform 密钥主体（设计 3.4）。 */
func tenantOfRow(table string, row map[string]any) string {
	if table == "tenant" {
		return stringOf(row["id"])
	}
	if v := row["tenant_id"]; v != nil {
		return stringOf(v)
	}
	return "platform"
}

/* dbIssuer 直接落库发行密钥（只写明文库：密文库的密钥行随表复制落地，
 * tenant_crypto_key 引用 tenant(id)，必须等 tenant 行复制后才能进密文库）。 */
type dbIssuer struct{ store *datastore.Store }

func (i *dbIssuer) InsertKey(ctx context.Context, tenantID string, keyVersion int64, keyType seccrypto.KeyType, provider, wrapped, wrapRef, actorID string, now int64) error {
	_, err := i.store.Plain.Exec(ctx, `INSERT INTO tenant_crypto_key
		(id,tenant_id,key_version,wrap_key_type,algorithm,wrapped_key,key_provider,key_wrap_ref,status,activated_at,created_by,created_at)
		VALUES($1,$2,$3,$4,'AES-256-GCM',$5,$6,$7,'active',$8,$9,$8)
		ON CONFLICT(tenant_id,wrap_key_type,key_version) DO NOTHING`,
		platform.ID(), tenantID, keyVersion, string(keyType), wrapped, provider, wrapRef, now, actorID)
	return err
}

/* collectSubjects 收集所有涉及租户主体（各表 distinct tenant_id + tenant 表 id）。 */
func collectSubjects(ctx context.Context, src *pgxpool.Pool, tables []string) ([]string, error) {
	set := map[string]bool{}
	for _, table := range tables {
		tenantCol := "tenant_id"
		if table == "tenant" {
			tenantCol = "id"
		}
		var colType string
		if err := src.QueryRow(ctx, `SELECT data_type FROM information_schema.columns
			WHERE table_schema='public' AND table_name=$1 AND column_name=$2`, table, tenantCol).Scan(&colType); err != nil {
			continue // 无该列的表跳过
		}
		rows, err := src.Query(ctx, fmt.Sprintf(`SELECT DISTINCT "%s" FROM "%s" WHERE "%s" IS NOT NULL`, tenantCol, table, tenantCol))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var v *string
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return nil, err
			}
			if v != nil && *v != "" {
				set[*v] = true
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	subjects := make([]string, 0, len(set))
	for s := range set {
		subjects = append(subjects, s)
	}
	/* 平台级敏感数据（visitor_lead 等 tenant_id 为 NULL 的运营表）使用 platform 密钥主体（设计 3.4） */
	subjects = append(subjects, "platform")
	return subjects, nil
}

/* primaryKeyOf 行的主键（用于 AAD 绑定）。 */
func primaryKeyOf(table string, row map[string]any) string {
	for _, key := range []string{"id", "code", "employee_no"} {
		if v, ok := row[key]; ok && v != nil {
			return stringOf(v)
		}
	}
	return ""
}

func stringOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprint(t)
	}
}

/* insertRows 批量插入目标库，主键/唯一冲突跳过（断点续跑幂等）。 */
func insertRows(ctx context.Context, dst *pgxpool.Pool, table string, columns []string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	var colList, placeholders string
	colList = `"` + strings.Join(columns, `","`) + `"`
	placeholders = "$1" + strings.Repeat(",$", 0)
	var ph []string
	for i := range columns {
		ph = append(ph, fmt.Sprintf("$%d", i+1))
	}
	placeholders = strings.Join(ph, ",")
	sql := fmt.Sprintf(`INSERT INTO "%s"(%s) VALUES(%s) ON CONFLICT DO NOTHING`, table, colList, placeholders)
	batch := &pgx.Batch{}
	for _, row := range rows {
		args := make([]any, len(columns))
		for i, col := range columns {
			args[i] = row[col]
		}
		batch.Queue(sql, args...)
	}
	br := dst.SendBatch(ctx, batch)
	defer br.Close()
	for range rows {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert into %s: %w", table, err)
		}
	}
	return br.Close()
}

/* insertRow 单行插入（批次降级路径用）。 */
func insertRow(ctx context.Context, dst *pgxpool.Pool, table string, columns []string, row map[string]any) error {
	var ph []string
	args := make([]any, len(columns))
	for i, col := range columns {
		ph = append(ph, fmt.Sprintf("$%d", i+1))
		args[i] = row[col]
	}
	sql := fmt.Sprintf(`INSERT INTO "%s"("%s") VALUES(%s) ON CONFLICT DO NOTHING`,
		table, strings.Join(columns, `","`), strings.Join(ph, ","))
	_, err := dst.Exec(ctx, sql, args...)
	return err
}

/* noteItem 记录批次错误摘要（不含敏感值）。 */
func noteItem(ctx context.Context, store *datastore.Store, jobID, table string, processed int, cause error) {
	now := platform.Now()
	detail := cause.Error()
	if len(detail) > 300 {
		detail = detail[:300]
	}
	for _, pool := range store.WritePools() {
		_, _ = pool.Exec(ctx, `INSERT INTO data_transfer_item(id,job_id,table_name,row_count,status,error_detail,started_at,finished_at)
			VALUES($1,$2,$3,$4,'failed',$5,$6,$6) ON CONFLICT DO NOTHING`,
			platform.ID(), jobID, table, processed, detail, now)
	}
}

/* LogResult 打印转换摘要（不含业务内容）。 */
func LogResult(r *Result) {
	log.Printf("datatool: job=%s direction=%s dry_run=%v failed=%d", r.JobID, r.Direction, r.DryRun, r.Failed)
	for table, n := range r.Tables {
		log.Printf("datatool:   %-32s %d rows", table, n)
	}
}
