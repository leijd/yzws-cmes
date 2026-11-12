package datatool

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"clothp/internal/datastore"
	"clothp/internal/dualwrite"
	"clothp/internal/platform"
	"clothp/internal/seccrypto"
	"github.com/jackc/pgx/v5"
)

/* RepairPhoneHash 修复历史数据的手机号盲索引与密文形态（早期单库直写路径的产物）：
 *   - 密文库 employee/tenant 行：phone_hash 缺失 → 解密/读取明文后补算盲索引；
 *     phone 仍为明文 → 补加密为 enc:v1 信封；
 *   - 明文库同名行：补算 phone_hash；
 *   - 密文库有而明文库缺的 employee/user_account/user_role 行 → 敏感列解密后复制到明文库。
 * dry_run 只统计不落库。
 * 明文库是对账库，业务运行中禁止读取明文库做对比（设计 2.2 第 7 条）；本工具属
 * 管理员数据对照/修复的显式例外，只在 CLI 运维模式运行，不参与在线业务。
 */
func RepairPhoneHash(ctx context.Context, store *datastore.Store, crypto *seccrypto.Service, dryRun bool) (*Result, error) {
	if store.Secure == nil || store.Plain == nil {
		return nil, fmt.Errorf("plain and secure databases both required")
	}
	if crypto == nil {
		return nil, fmt.Errorf("crypto service required")
	}
	result := &Result{JobID: platform.ID(), Direction: "repair-phone-hash", DryRun: dryRun, Tables: map[string]int{}}

	secureTenants, err := tenantIDSet(ctx, store.Secure)
	if err != nil {
		return nil, err
	}
	plainTenants, err := tenantIDSet(ctx, store.Plain)
	if err != nil {
		return nil, err
	}

	/* 1) 员工手机号盲索引 + 明文补加密 */
	rows, err := store.Secure.Query(ctx, `SELECT tenant_id,id,phone FROM employee
		WHERE phone IS NOT NULL AND phone<>'' AND (phone_hash IS NULL OR phone_hash='')`)
	if err != nil {
		return nil, err
	}
	type empRow struct {
		tenantID, id, phone string
	}
	var emps []empRow
	for rows.Next() {
		var e empRow
		if err := rows.Scan(&e.tenantID, &e.id, &e.phone); err != nil {
			rows.Close()
			return nil, err
		}
		emps = append(emps, e)
	}
	rows.Close()
	for _, e := range emps {
		plain, err := decryptIfNeeded(ctx, crypto, e.tenantID, "employee", e.id, "phone", e.phone)
		if err != nil {
			log.Printf("repair-phone-hash: employee %s decrypt: %v (skipped)", e.id, err)
			result.Failed++
			continue
		}
		normalized := seccrypto.NormalizePhone(plain)
		if normalized == "" {
			continue
		}
		hash, err := crypto.PhoneHash(ctx, e.tenantID, "employee.phone", normalized)
		if err != nil {
			log.Printf("repair-phone-hash: employee %s phone_hash: %v (skipped)", e.id, err)
			result.Failed++
			continue
		}
		stored := e.phone
		if !seccrypto.IsEncrypted(stored) && !dryRun {
			enc, err := crypto.EncryptField(ctx, e.tenantID, "employee", e.id, "phone", []byte(plain))
			if err != nil {
				log.Printf("repair-phone-hash: employee %s encrypt: %v (skipped)", e.id, err)
				result.Failed++
				continue
			}
			stored = enc
		}
		if !dryRun {
			if _, err := store.Secure.Exec(ctx, `UPDATE employee SET phone=$1,phone_hash=$2 WHERE tenant_id=$3 AND id=$4`, stored, hash, e.tenantID, e.id); err != nil {
				return nil, err
			}
			if plainTenants[e.tenantID] {
				if _, err := store.Plain.Exec(ctx, `UPDATE employee SET phone_hash=$1 WHERE tenant_id=$2 AND id=$3`, hash, e.tenantID, e.id); err != nil {
					return nil, err
				}
			}
		}
		result.Tables["employee.phone_hash"]++
	}

	/* 2) 租户联系电话盲索引（tenant 表无主归属 tenant_id，逐行处理） */
	tRows, err := store.Secure.Query(ctx, `SELECT id,contact_phone FROM tenant
		WHERE contact_phone IS NOT NULL AND contact_phone<>'' AND (contact_phone_hash IS NULL OR contact_phone_hash='')`)
	if err != nil {
		return nil, err
	}
	var tenants []struct {
		id, phone string
	}
	for tRows.Next() {
		var t struct {
			id, phone string
		}
		if err := tRows.Scan(&t.id, &t.phone); err != nil {
			tRows.Close()
			return nil, err
		}
		tenants = append(tenants, t)
	}
	tRows.Close()
	for _, t := range tenants {
		plain, err := decryptIfNeeded(ctx, crypto, t.id, "tenant", t.id, "contact_phone", t.phone)
		if err != nil {
			log.Printf("repair-phone-hash: tenant %s decrypt: %v (skipped)", t.id, err)
			result.Failed++
			continue
		}
		normalized := seccrypto.NormalizePhone(plain)
		if normalized == "" {
			continue
		}
		hash, err := crypto.PhoneHash(ctx, t.id, "tenant.contact_phone", normalized)
		if err != nil {
			log.Printf("repair-phone-hash: tenant %s phone_hash: %v (skipped)", t.id, err)
			result.Failed++
			continue
		}
		if !dryRun {
			if _, err := store.Secure.Exec(ctx, `UPDATE tenant SET contact_phone_hash=$1 WHERE id=$2`, hash, t.id); err != nil {
				return nil, err
			}
			if _, err := store.Plain.Exec(ctx, `UPDATE tenant SET contact_phone_hash=$1 WHERE id=$2`, hash, t.id); err != nil {
				return nil, err
			}
		}
		result.Tables["tenant.contact_phone_hash"]++
	}

	/* 3) 密文库有、明文库缺的员工/账号/角色行：解密敏感列后复制到明文库 */
	rowCrypto := dualwrite.NewRowCrypto(crypto)
	for _, table := range []string{"employee", "user_account", "user_role"} {
		n, err := copySecureOrphans(ctx, store, rowCrypto, table, secureTenants, plainTenants, dryRun)
		if err != nil {
			log.Printf("repair-phone-hash: copy orphans %s: %v", table, err)
			result.Failed++
			continue
		}
		result.Tables[table+".orphan_copied"] = n
	}
	return result, nil
}

func decryptIfNeeded(ctx context.Context, crypto *seccrypto.Service, tenantID, table, pk, column, value string) (string, error) {
	if !seccrypto.IsEncrypted(value) {
		return value, nil
	}
	return crypto.DecryptField(ctx, tenantID, table, pk, column, value)
}

/* copySecureOrphans 把只存在于密文库的行复制到明文库（employee 敏感列解密回明文）。 */
func copySecureOrphans(ctx context.Context, store *datastore.Store, rowCrypto *dualwrite.RowCrypto, table string, secureTenants, plainTenants map[string]bool, dryRun bool) (int, error) {
	/* 明文库已有键集合（两库是各自连接，必须在 Go 侧比对） */
	plainKeys := map[string]bool{}
	pRows, err := store.Plain.Query(ctx, fmt.Sprintf(`SELECT tenant_id,id FROM "%s"`, table))
	if err != nil {
		return 0, err
	}
	for pRows.Next() {
		var tenantID, id string
		if err := pRows.Scan(&tenantID, &id); err != nil {
			pRows.Close()
			return 0, err
		}
		plainKeys[tenantID+"/"+id] = true
	}
	pRows.Close()
	rows, err := store.Secure.Query(ctx, fmt.Sprintf(`SELECT * FROM "%s"`, table))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = string(f.Name)
	}
	copied := 0
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return copied, err
		}
		row := map[string]any{}
		for i, col := range columns {
			row[col] = vals[i]
		}
		tenantID, _ := row["tenant_id"].(string)
		id, _ := row["id"].(string)
		if plainKeys[tenantID+"/"+id] || !secureTenants[tenantID] || !plainTenants[tenantID] {
			continue
		}
		if table == "employee" {
			if id, ok := row["id"].(string); ok {
				dec, err := rowCrypto.FromSecure(ctx, tenantID, "employee", id, row)
				if err != nil {
					return copied, fmt.Errorf("decrypt employee %s: %w", id, err)
				}
				row = dec
			}
		}
		if dryRun {
			copied++
			continue
		}
		sortedCols := make([]string, 0, len(row))
		for col := range row {
			sortedCols = append(sortedCols, col)
		}
		sort.Strings(sortedCols)
		placeholders := make([]string, len(sortedCols))
		args := make([]any, len(sortedCols))
		for i, col := range sortedCols {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = row[col]
		}
		sqlText := fmt.Sprintf(`INSERT INTO "%s"(%s) VALUES(%s) ON CONFLICT DO NOTHING`, table, strings.Join(sortedCols, ","), strings.Join(placeholders, ","))
		if _, err := store.Plain.Exec(ctx, sqlText, args...); err != nil {
			return copied, err
		}
		copied++
	}
	return copied, rows.Err()
}

func tenantIDSet(ctx context.Context, db pgxQuerier) (map[string]bool, error) {
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

type pgxQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
