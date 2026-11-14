package tenantreg

/* 注册审批 Saga 步骤定义（设计原则：禁止长事务）。
 * 步骤：claim（CAS 认领，替代原 SELECT ... FOR UPDATE 长锁）→ 种子四阶段 →
 * 审批落库（订阅/体验扩展）→ 加密密钥 → 线索与审计。补偿按逆序幂等恢复；
 * payload 持久化供崩溃恢复重建补偿闭包（见 init 工厂注册）。 */

import (
	"context"
	"encoding/json"

	"clothp/internal/dualwrite"
	"clothp/internal/platform"

	"github.com/jackc/pgx/v5"
)

func init() {
	dualwrite.RegisterSagaFactory("tenant.approve", func(payload json.RawMessage) ([]dualwrite.SagaStep, error) {
		var p tenantApprovePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return tenantApproveSteps(p, nil), nil
	})
}

type tenantApprovePayload struct {
	TenantID        string `json:"tenant_id"`
	AdminUserID     string `json:"admin_user_id"`
	AdminEmployeeID string `json:"admin_employee_id"`
	IsTrial         bool   `json:"is_trial"`
	OldShortCode    string `json:"old_short_code"`
	OldTimezone     string `json:"old_timezone"`
	OldCustomerType string `json:"old_customer_type"`
	OldExpediteAt   *int64 `json:"old_expedite_at,omitempty"`
	HadSubscription bool   `json:"had_subscription"`
	OldSubscription string `json:"old_subscription,omitempty"` // row_to_json 快照
}

/* existingSubscriptionID 取该租户已有订阅行 id（优先密文库=运行期读源，回落明文库），
 * 两侧都没有时才新生成。两库共用同一 id 是 UK(tenant_id) 不分叉的前提：
 * 若一侧缺行时各自 INSERT 新 id，两库会各有一行不同 id 的订阅，增量补偿永久 23505。 */
func existingSubscriptionID(ctx context.Context, tx *dualwrite.Tx, tenantID string) string {
	for _, q := range []pgx.Tx{tx.Secure(), tx.Plain()} {
		if q == nil {
			continue
		}
		var id string
		if err := q.QueryRow(ctx, `SELECT id FROM tenant_subscription WHERE tenant_id=$1`, tenantID).Scan(&id); err == nil && id != "" {
			return id
		}
	}
	return platform.ID()
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

/* tenantApproveSteps 审批步骤与补偿；do 传 nil（崩溃恢复）时只保留补偿闭包。 */
func tenantApproveSteps(p tenantApprovePayload, do map[string]func(context.Context, *dualwrite.Tx) error) []dualwrite.SagaStep {
	doFn := func(name string) func(context.Context, *dualwrite.Tx) error {
		if do == nil {
			return nil
		}
		return do[name]
	}
	return []dualwrite.SagaStep{
		/* 认领：approving 中间态；并发/重复审批只有第一个 CAS 成功 */
		{Name: "claim", Do: doFn("claim"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return execBothTx(ctx, tx, `UPDATE tenant SET registration_status='pending',version=version+1 WHERE id=$1 AND registration_status='approving'`, p.TenantID)
		}},
		/* 密钥紧随 claim：ensure_root 加密补根行、approve_core 体验手机号盲索引
		 * 都依赖 tenant_crypto_key 已落库（待审批租户此前无 DEK，catchup 跳过）。
		 * 密钥幂等（ON CONFLICT DO NOTHING），补偿无害保留 */
		{Name: "crypto_key", Do: doFn("crypto_key"), Compensate: nil},
		/* 根行补偿：待审批租户可能从未同步到密文库（无 DEK 时 catchup 跳过），
		 * 上一步密钥已就绪，此处幂等补行；保留不补偿（claim 回退后仍为 pending 原状） */
		{Name: "ensure_root", Do: doFn("ensure_root"), Compensate: nil},
		{Name: "seed_root", Do: doFn("seed_root"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			if err := execBothTx(ctx, tx, `DELETE FROM org_unit WHERE tenant_id=$1`, p.TenantID); err != nil {
				return err
			}
			return execBothTx(ctx, tx, `UPDATE tenant SET short_code=NULLIF($2,''),timezone=$3,version=version+1 WHERE id=$1`,
				p.TenantID, p.OldShortCode, p.OldTimezone)
		}},
		{Name: "seed_admin", Do: doFn("seed_admin"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			if err := execBothTx(ctx, tx, `DELETE FROM user_account WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.AdminUserID); err != nil {
				return err
			}
			return execBothTx(ctx, tx, `DELETE FROM employee WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.AdminEmployeeID)
		}},
		{Name: "seed_role", Do: doFn("seed_role"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			for _, sql := range []string{
				`DELETE FROM user_role WHERE tenant_id=$1`,
				`DELETE FROM role_permission WHERE tenant_id=$1`,
				`DELETE FROM permission WHERE tenant_id=$1`,
				`DELETE FROM role WHERE tenant_id=$1`,
			} {
				if err := execBothTx(ctx, tx, sql, p.TenantID); err != nil {
					return err
				}
			}
			return nil
		}},
		{Name: "seed_settings", Do: doFn("seed_settings"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			if err := execBothTx(ctx, tx, `DELETE FROM mgmt_level WHERE tenant_id=$1`, p.TenantID); err != nil {
				return err
			}
			return execBothTx(ctx, tx, `DELETE FROM feature_setting WHERE tenant_id=$1`, p.TenantID)
		}},
		/* 审批落库的补偿：回退审批字段 + 恢复订阅快照（无旧订阅则删除新行）。
		 * 体验扩展（管理员手机号/改密标记）随 seed_admin 补偿整体删除，无需单独恢复。 */
		{Name: "approve_core", Do: doFn("approve_core"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			if err := execBothTx(ctx, tx, `UPDATE tenant SET registration_status='pending',customer_type=$2,
				approved_at=NULL,approved_by=NULL,admin_login_name=NULL,admin_password_verifier=NULL,admin_employee_id=NULL,
				expedite_requested_at=$3,version=version+1 WHERE id=$1`,
				p.TenantID, p.OldCustomerType, p.OldExpediteAt); err != nil {
				return err
			}
			if err := execBothTx(ctx, tx, `DELETE FROM tenant_subscription WHERE tenant_id=$1`, p.TenantID); err != nil {
				return err
			}
			if p.HadSubscription {
				return execBothTx(ctx, tx, `INSERT INTO tenant_subscription SELECT * FROM json_populate_record(null::tenant_subscription,$2::jsonb)`,
					p.TenantID, p.OldSubscription)
			}
			return nil
		}},
		/* 线索/审计为追加式记录，不回滚 */
		{Name: "lead_audit", Do: doFn("lead_audit"), Compensate: nil},
	}
}
