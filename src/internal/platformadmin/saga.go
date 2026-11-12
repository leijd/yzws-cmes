package platformadmin

/* 平台建厂/启停 Saga 步骤定义（设计原则：禁止长事务）。
 * 种子四阶段 + 角色模板 + 审计；补偿按 tenant_id 逆序幂等删除（新租户无历史数据，
 * 整租户清除安全）。payload 持久化供崩溃恢复重建补偿闭包。 */

import (
	"context"
	"encoding/json"

	"clothp/internal/dualwrite"
)

func init() {
	dualwrite.RegisterSagaFactory("tenant.create", func(payload json.RawMessage) ([]dualwrite.SagaStep, error) {
		var p tenantCreatePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return tenantCreateSteps(p, nil), nil
	})
	dualwrite.RegisterSagaFactory("tenant.set_status", func(payload json.RawMessage) ([]dualwrite.SagaStep, error) {
		var p tenantSetStatusPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return tenantSetStatusSteps(p, nil), nil
	})
}

type tenantCreatePayload struct {
	TenantID string `json:"tenant_id"`
}

func execBothTx(ctx context.Context, tx *dualwrite.Tx, sql string, args ...any) error {
	if tx.Plain() != nil {
		if _, err := tx.Plain().Exec(ctx, sql, args...); err != nil {
			return err
		}
	}
	if tx.Secure() != nil {
		if _, err := tx.Secure().Exec(ctx, sql, args...); err != nil {
			return err
		}
	}
	return nil
}

/* deleteTenantTables 按依赖逆序清除租户业务数据（建厂补偿共用）。 */
func deleteTenantTables(ctx context.Context, tx *dualwrite.Tx, tenantID string, tables ...string) error {
	for _, table := range tables {
		if err := execBothTx(ctx, tx, `DELETE FROM "`+table+`" WHERE tenant_id=$1`, tenantID); err != nil {
			return err
		}
	}
	return nil
}

func tenantCreateSteps(p tenantCreatePayload, do map[string]func(context.Context, *dualwrite.Tx) error) []dualwrite.SagaStep {
	doFn := func(name string) func(context.Context, *dualwrite.Tx) error {
		if do == nil {
			return nil
		}
		return do[name]
	}
	return []dualwrite.SagaStep{
		{Name: "seed_root", Do: doFn("seed_root"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			if err := deleteTenantTables(ctx, tx, p.TenantID, "org_unit"); err != nil {
				return err
			}
			if tx.Plain() != nil {
				if _, err := tx.Plain().Exec(ctx, `DELETE FROM tenant WHERE id=$1`, p.TenantID); err != nil {
					return err
				}
			}
			if tx.Secure() != nil {
				if _, err := tx.Secure().Exec(ctx, `DELETE FROM tenant WHERE id=$1`, p.TenantID); err != nil {
					return err
				}
			}
			return nil
		}},
		{Name: "seed_admin", Do: doFn("seed_admin"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return deleteTenantTables(ctx, tx, p.TenantID, "user_account", "employee")
		}},
		{Name: "seed_role", Do: doFn("seed_role"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return deleteTenantTables(ctx, tx, p.TenantID, "user_role", "role_permission", "permission", "role")
		}},
		{Name: "seed_settings", Do: doFn("seed_settings"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return deleteTenantTables(ctx, tx, p.TenantID, "mgmt_level", "feature_setting")
		}},
		/* 模板写入的角色/权限行由 seed_role 补偿的整租户清除覆盖 */
		{Name: "role_templates", Do: doFn("role_templates"), Compensate: nil},
		/* 审计为追加式记录，不回滚 */
		{Name: "audit", Do: doFn("audit"), Compensate: nil},
	}
}

type tenantSetStatusPayload struct {
	TenantID string `json:"tenant_id"`
	Previous string `json:"previous"`
}

func tenantSetStatusSteps(p tenantSetStatusPayload, do map[string]func(context.Context, *dualwrite.Tx) error) []dualwrite.SagaStep {
	doFn := func(name string) func(context.Context, *dualwrite.Tx) error {
		if do == nil {
			return nil
		}
		return do[name]
	}
	return []dualwrite.SagaStep{
		{Name: "set_status", Do: doFn("set_status"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return execBothTx(ctx, tx, `UPDATE tenant SET status=$2,version=version+1 WHERE id=$1`, p.TenantID, p.Previous)
		}},
		{Name: "audit", Do: doFn("audit"), Compensate: nil},
	}
}
