package datatool

import (
	"context"
	"fmt"
	"log"
	"sort"

	"clothp/internal/datastore"
	"clothp/internal/platform"
)

/* AlignSubscriptionKeys 修复 tenant_subscription 的业务键分叉（datatool 显式运维例外）。
 *
 * 该表 UNIQUE(tenant_id)：一厂一行。若两库对同一租户各自生成了不同 id 的订阅行，
 * 增量补偿以主键比对 → 明文侧行被判"密文库缺失"→ INSERT 撞 uk_subscription_tenant(23505)，
 * 每轮固定失败一行，永不收敛。
 *
 * 修复口径：运行期读源是密文库（databases.read_source 默认 secure），因此密文库为准，
 * 只把明文库该租户行的主键改成密文库的 id（不触碰读源，catchup 随即收敛）。
 * 仅在同租户两侧除 id/created_at/updated_at 外的业务列完全一致时自动执行，
 * 其余情况只报告、不动数据（订阅档位/状态差异需业务判断）。
 * dry_run 只统计不落库。
 */
func AlignSubscriptionKeys(ctx context.Context, store *datastore.Store, dryRun bool) (*Result, error) {
	if store.Plain == nil || store.Secure == nil {
		return nil, fmt.Errorf("plain and secure databases both required")
	}
	result := &Result{JobID: platform.ID(), Direction: "align-subscription-keys", DryRun: dryRun, Tables: map[string]int{}}

	/* 主键改写前确认没有任何子表外键引用 tenant_subscription(id) */
	refs, err := referencingTables(ctx, store.Secure, "tenant_subscription")
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		return nil, fmt.Errorf("tenant_subscription(id) is referenced by %v, manual reconcile required", refs)
	}

	plainRows, err := loadSubscriptionRows(ctx, store.Plain)
	if err != nil {
		return nil, fmt.Errorf("load plain subscription: %w", err)
	}
	secureRows, err := loadSubscriptionRows(ctx, store.Secure)
	if err != nil {
		return nil, fmt.Errorf("load secure subscription: %w", err)
	}

	tenants := make([]string, 0, len(secureRows))
	for t := range secureRows {
		tenants = append(tenants, t)
	}
	sort.Strings(tenants)
	for _, tenantID := range tenants {
		sr := secureRows[tenantID]
		pr, ok := plainRows[tenantID]
		if !ok {
			result.Tables["tenant_subscription.secure_only"]++
			continue
		}
		if fmt.Sprint(pr["id"]) == fmt.Sprint(sr["id"]) {
			continue
		}
		if differing := differingColumns(pr, sr, subscriptionIgnoredColumns...); len(differing) > 0 {
			result.Tables["tenant_subscription.conflict_skipped"]++
			log.Printf("align-subscription-keys: tenant %s plain id=%s secure id=%s columns differ (%v), manual reconcile required",
				tenantID, pr["id"], sr["id"], differing)
			continue
		}
		result.Tables["tenant_subscription.aligned"]++
		if dryRun {
			continue
		}
		/* 明文库改写主键：表上无子外键，单行 UPDATE 即可 */
		if _, err := store.Plain.Exec(ctx,
			`UPDATE tenant_subscription SET id=$1 WHERE tenant_id=$2 AND id=$3`,
			sr["id"], tenantID, pr["id"]); err != nil {
			return result, fmt.Errorf("align plain subscription %s: %w", tenantID, err)
		}
		log.Printf("align-subscription-keys: tenant %s plain id %s -> %s", tenantID, pr["id"], sr["id"])
	}
	for tenantID := range plainRows {
		if _, ok := secureRows[tenantID]; !ok {
			result.Tables["tenant_subscription.plain_only"]++
		}
	}
	return result, nil
}

/* subscriptionIgnoredColumns 只比业务状态（档位/状态/有效期/配额/功能/确认信息）；
 * id、创建/更新时间以及 created_by/updated_by/version 属于两侧各自的操作痕迹与并发守卫，
 * 分叉时必然不同，不影响"是否同一份订阅"的判断。 */
var subscriptionIgnoredColumns = []string{"id", "created_at", "updated_at", "created_by", "updated_by", "version"}

func loadSubscriptionRows(ctx context.Context, db pgxpoolQuerier) (map[string]map[string]any, error) {
	rows, err := db.Query(ctx, `SELECT * FROM tenant_subscription`)
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
		tenantID, _ := row["tenant_id"].(string)
		out[tenantID] = row
	}
	return out, rows.Err()
}

/* differingColumns 逐列比较业务字段（忽略给定列），返回不一致的列名；值统一按字面量比较。 */
func differingColumns(a, b map[string]any, skip ...string) []string {
	ignored := map[string]bool{}
	for _, s := range skip {
		ignored[s] = true
	}
	cols := map[string]bool{}
	for c := range a {
		cols[c] = true
	}
	for c := range b {
		cols[c] = true
	}
	var out []string
	for c := range cols {
		if ignored[c] {
			continue
		}
		av, aok := a[c]
		bv, bok := b[c]
		if aok != bok || fmt.Sprint(av) != fmt.Sprint(bv) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

/* referencingTables 列出以 target 主键为外键的子表（改写主键前的安全检查）。 */
func referencingTables(ctx context.Context, db pgxpoolQuerier, target string) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT tc.table_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name=tc.constraint_name
		WHERE tc.constraint_type='FOREIGN KEY' AND ccu.table_name=$1 AND tc.table_name<>$1`, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
