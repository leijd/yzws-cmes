package datatool

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"clothp/internal/datastore"
	"clothp/internal/platform"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

/* RepairPermissionDivergence 修复两库权限点/角色授权的主键分叉（datatool 显式运维例外）。
 *
 * 现象：同租户同 code 的 permission 行两库 id 不同（各自被独立种子/回补生成）。
 * 增量补偿以主键比对，明文侧这些行被判为"密文库缺失"→ INSERT 撞 (tenant_id,code)
 * 唯一约束；密文库 role_permission 仍指向旧 permission_id，补偿再撞外键 23503，
 * 形成每轮固定失败 + 刷屏，且永不收敛。
 *
 * 修复口径：以明文库为准（运行期鉴权读明文库 ReadPool），把密文库中旧 id 的
 * role_permission 授权与 permission 行替换为明文库的行；随后补偿即可收敛。
 * 只处理"两侧同 code 但 id 不同"的项；仅一侧存在的项只报告，由补偿/人工处理。
 * dry_run 只统计不落库。
 */
func RepairPermissionDivergence(ctx context.Context, store *datastore.Store, dryRun bool) (*Result, error) {
	if store.Plain == nil || store.Secure == nil {
		return nil, fmt.Errorf("plain and secure databases both required")
	}
	result := &Result{JobID: platform.ID(), Direction: "repair-permission-divergence", DryRun: dryRun, Tables: map[string]int{}}

	plainTenants, err := tenantIDSet(ctx, store.Plain)
	if err != nil {
		return nil, err
	}
	secureTenants, err := tenantIDSet(ctx, store.Secure)
	if err != nil {
		return nil, err
	}
	var tenants []string
	for id := range plainTenants {
		if secureTenants[id] {
			tenants = append(tenants, id)
		}
	}
	sort.Strings(tenants)

	permCols, err := tableColumns(ctx, store.Secure, "permission")
	if err != nil {
		return nil, err
	}
	grantCols, err := tableColumns(ctx, store.Secure, "role_permission")
	if err != nil {
		return nil, err
	}

	for _, tenantID := range tenants {
		plainPerms, err := loadRowsByCode(ctx, store.Plain, tenantID)
		if err != nil {
			return result, fmt.Errorf("load plain permission %s: %w", tenantID, err)
		}
		securePerms, err := loadRowsByCode(ctx, store.Secure, tenantID)
		if err != nil {
			return result, fmt.Errorf("load secure permission %s: %w", tenantID, err)
		}
		var oldIDs, newIDs []string
		var newRows []map[string]any
		for code, p := range plainPerms {
			s, ok := securePerms[code]
			if !ok {
				continue /* 密文库缺行：交给增量补偿正常插入，无唯一约束冲突 */
			}
			if fmt.Sprint(s["id"]) == fmt.Sprint(p["id"]) {
				continue
			}
			oldIDs = append(oldIDs, fmt.Sprint(s["id"]))
			newIDs = append(newIDs, fmt.Sprint(p["id"]))
			newRows = append(newRows, p)
		}
		for code := range securePerms {
			if _, ok := plainPerms[code]; !ok {
				result.Tables["permission.secure_only"]++
				if result.Tables["permission.secure_only"] <= 3 {
					log.Printf("repair-permission-divergence: tenant %s code %s only in secure database (manual check)", tenantID, code)
				}
			}
		}
		if len(oldIDs) == 0 {
			continue
		}
		sort.Slice(newRows, func(i, j int) bool { return fmt.Sprint(newRows[i]["id"]) < fmt.Sprint(newRows[j]["id"]) })
		result.Tables["permission.replaced"] += len(oldIDs)
		if dryRun {
			continue
		}
		/* 明文库这些权限点的角色授权行，替换后原样回填密文库 */
		grantRows, err := queryRows(ctx, store.Plain,
			`SELECT * FROM role_permission WHERE tenant_id=$1 AND permission_id = ANY($2)`, tenantID, newIDs)
		if err != nil {
			return result, fmt.Errorf("load plain role_permission %s: %w", tenantID, err)
		}
		tx, err := store.Secure.Begin(ctx)
		if err != nil {
			return result, err
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback(ctx)
			}
		}()
		tag, err := tx.Exec(ctx, `DELETE FROM role_permission WHERE tenant_id=$1 AND permission_id = ANY($2)`, tenantID, oldIDs)
		if err != nil {
			return result, fmt.Errorf("delete secure role_permission %s: %w", tenantID, err)
		}
		result.Tables["role_permission.replaced"] += int(tag.RowsAffected())
		if _, err := tx.Exec(ctx, `DELETE FROM permission WHERE tenant_id=$1 AND id = ANY($2)`, tenantID, oldIDs); err != nil {
			return result, fmt.Errorf("delete secure permission %s: %w", tenantID, err)
		}
		repaired := 0
		for _, row := range newRows {
			if err := insertRowTx(ctx, tx, "permission", permCols, row); err != nil {
				return result, fmt.Errorf("insert secure permission %s: %w", tenantID, err)
			}
			repaired++
		}
		for _, row := range grantRows {
			if err := insertRowTx(ctx, tx, "role_permission", grantCols, row); err != nil {
				return result, fmt.Errorf("insert secure role_permission %s: %w", tenantID, err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return result, fmt.Errorf("commit %s: %w", tenantID, err)
		}
		committed = true
		result.Tables["permission.tenants"]++
		log.Printf("repair-permission-divergence: tenant %s replaced %d permission id(s)", tenantID, repaired)
	}
	return result, nil
}

/* loadRowsByCode 读某租户全部权限点，按 code 索引整行（用于跨库比对与整行替换）。 */
func loadRowsByCode(ctx context.Context, db pgxpoolQuerier, tenantID string) (map[string]map[string]any, error) {
	rows, err := db.Query(ctx, `SELECT * FROM permission WHERE tenant_id=$1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = string(f.Name)
	}
	out := map[string]map[string]any{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		code, _ := row["code"].(string)
		out[code] = row
	}
	return out, rows.Err()
}

func queryRows(ctx context.Context, db pgxpoolQuerier, sql string, args ...any) ([]map[string]any, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	cols := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = string(f.Name)
	}
	var out []map[string]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

/* tableColumns 目标表列集合（按目标库实际结构裁剪，避免两库列漂移导致插入失败）。 */
func tableColumns(ctx context.Context, db pgxpoolQuerier, table string) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1 ORDER BY ordinal_position`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

/* insertRowTx 按给定列集合插入整行（列缺失则跳过该列），冲突忽略。 */
func insertRowTx(ctx context.Context, q execQuerier, table string, cols []string, row map[string]any) error {
	use := make([]string, 0, len(cols))
	ph := make([]string, 0, len(cols))
	args := make([]any, 0, len(cols))
	for _, c := range cols {
		v, ok := row[c]
		if !ok {
			continue
		}
		use = append(use, fmt.Sprintf(`"%s"`, c))
		args = append(args, v)
		ph = append(ph, fmt.Sprintf("$%d", len(args)))
	}
	if len(use) == 0 {
		return fmt.Errorf("no matching column")
	}
	sql := fmt.Sprintf(`INSERT INTO "%s"(%s) VALUES(%s) ON CONFLICT DO NOTHING`,
		table, strings.Join(use, ","), strings.Join(ph, ","))
	_, err := q.Exec(ctx, sql, args...)
	return err
}

type pgxpoolQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type execQuerier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}
