package person

/* 建档/换线/停用的 Saga 步骤定义（设计原则：禁止长事务）。
 * 步骤拆分为独立短事务，补偿闭包与持久化 payload 供在线失败回滚与崩溃恢复共用；
 * 补偿一律按主键/聚合 ID 幂等删除或恢复旧值，可安全重放。 */

import (
	"context"
	"encoding/json"

	"clothp/internal/dualwrite"
	"clothp/internal/platform"
)

func init() {
	dualwrite.RegisterSagaFactory("person.create", func(payload json.RawMessage) ([]dualwrite.SagaStep, error) {
		var p personCreatePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return personCreateSteps(p, nil), nil
	})
	dualwrite.RegisterSagaFactory("person.org_binding", func(payload json.RawMessage) ([]dualwrite.SagaStep, error) {
		var p orgBindingPayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return orgBindingSteps(p, nil), nil
	})
	dualwrite.RegisterSagaFactory("person.disable", func(payload json.RawMessage) ([]dualwrite.SagaStep, error) {
		var p disablePayload
		if err := json.Unmarshal(payload, &p); err != nil {
			return nil, err
		}
		return disableSteps(p, nil), nil
	})
}

type personCreatePayload struct {
	TenantID   string `json:"tenant_id"`
	EmployeeID string `json:"employee_id"`
	AccountID  string `json:"account_id"`
	HasBinding bool   `json:"has_binding"`
	HasRoles   bool   `json:"has_roles"`
	HasSkills  bool   `json:"has_skills"`
	LineOrgID  string `json:"line_org_id,omitempty"`
	OldLineMgr string `json:"old_line_manager,omitempty"`
}

/* personCreateSteps 建档步骤：employee(+归属) → 账号 → 角色 → 专长 → 线长。
 * do 传 nil（崩溃恢复）时步骤只含补偿闭包。 */
func personCreateSteps(p personCreatePayload, do map[string]func(context.Context, *dualwrite.Tx) error) []dualwrite.SagaStep {
	doFn := func(name string) func(context.Context, *dualwrite.Tx) error {
		if do == nil {
			return nil
		}
		return do[name]
	}
	steps := []dualwrite.SagaStep{
		{Name: "employee", Do: doFn("employee"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			if err := execTwo(ctx, tx, `DELETE FROM employee_org_binding WHERE tenant_id=$1 AND employee_id=$2`, p.TenantID, p.EmployeeID); err != nil {
				return err
			}
			return execTwo(ctx, tx, `DELETE FROM employee WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.EmployeeID)
		}},
		{Name: "user_account", Do: doFn("user_account"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return execTwo(ctx, tx, `DELETE FROM user_account WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.AccountID)
		}},
	}
	if p.HasRoles {
		steps = append(steps, dualwrite.SagaStep{Name: "user_roles", Do: doFn("user_roles"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return execTwo(ctx, tx, `DELETE FROM user_role WHERE tenant_id=$1 AND user_id=$2`, p.TenantID, p.AccountID)
		}})
	}
	if p.HasSkills {
		steps = append(steps, dualwrite.SagaStep{Name: "employee_skills", Do: doFn("employee_skills"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return execTwo(ctx, tx, `DELETE FROM employee_skill WHERE tenant_id=$1 AND employee_id=$2`, p.TenantID, p.EmployeeID)
		}})
	}
	if p.LineOrgID != "" {
		steps = append(steps, dualwrite.SagaStep{Name: "line_manager", Do: doFn("line_manager"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return execTwo(ctx, tx, `UPDATE org_unit SET manager_employee_id=NULLIF($3,''),updated_at=$4 WHERE tenant_id=$1 AND id=$2 AND org_type='line'`,
				p.TenantID, p.LineOrgID, p.OldLineMgr, platform.Now())
		}})
	}
	return steps
}

type orgBindingPayload struct {
	TenantID     string `json:"tenant_id"`
	EmployeeID   string `json:"employee_id"`
	NewBindingID string `json:"new_binding_id"`
	Effective    int64  `json:"effective"`
	OldWorkshop  string `json:"old_workshop"`
	OldLine      string `json:"old_line"`
	OldTeam      string `json:"old_team"`
	OldOrgUnit   string `json:"old_org_unit"`
}

/* orgBindingSteps 换线步骤：归属历史切换（结束旧+插新，保证历史不重不漏）→ employee 当前归属。 */
func orgBindingSteps(p orgBindingPayload, do map[string]func(context.Context, *dualwrite.Tx) error) []dualwrite.SagaStep {
	doFn := func(name string) func(context.Context, *dualwrite.Tx) error {
		if do == nil {
			return nil
		}
		return do[name]
	}
	return []dualwrite.SagaStep{
		{Name: "switch_binding", Do: doFn("switch_binding"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			if err := execTwo(ctx, tx, `DELETE FROM employee_org_binding WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.NewBindingID); err != nil {
				return err
			}
			return execTwo(ctx, tx, `UPDATE employee_org_binding SET valid_to=NULL WHERE tenant_id=$1 AND employee_id=$2 AND is_primary=TRUE AND valid_to=$3`,
				p.TenantID, p.EmployeeID, p.Effective)
		}},
		{Name: "update_employee", Do: doFn("update_employee"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			return execTwo(ctx, tx, `UPDATE employee SET workshop_org_id=NULLIF($3,''),line_org_id=NULLIF($4,''),team_org_id=NULLIF($5,''),org_unit_id=NULLIF($6,''),updated_at=$7
				WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.EmployeeID, p.OldWorkshop, p.OldLine, p.OldTeam, p.OldOrgUnit, platform.Now())
		}},
	}
}

type disablePayload struct {
	TenantID          string `json:"tenant_id"`
	EmployeeID        string `json:"employee_id"`
	OldAccountStatus  string `json:"old_account_status"`
	OldEmployeeStatus string `json:"old_employee_status"`
	Now               int64  `json:"now"`
}

/* disableSteps 停用步骤：账号停用+会话撤销 → 员工离职+归属结束。
 * 会话撤销不可补偿（重新登录即可），其余恢复旧值。 */
func disableSteps(p disablePayload, do map[string]func(context.Context, *dualwrite.Tx) error) []dualwrite.SagaStep {
	doFn := func(name string) func(context.Context, *dualwrite.Tx) error {
		if do == nil {
			return nil
		}
		return do[name]
	}
	return []dualwrite.SagaStep{
		{Name: "disable_account", Do: doFn("disable_account"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			/* $3=''（停用前无账号，Do 为 0 行空操作）时补偿同样空操作 */
			return execTwo(ctx, tx, `UPDATE user_account SET status=$3,updated_at=$4 WHERE tenant_id=$1 AND employee_id=$2 AND $3<>''`,
				p.TenantID, p.EmployeeID, p.OldAccountStatus, platform.Now())
		}},
		{Name: "terminate_employee", Do: doFn("terminate_employee"), Compensate: func(ctx context.Context, tx *dualwrite.Tx) error {
			if err := execTwo(ctx, tx, `UPDATE employee SET status=$3,updated_at=$4 WHERE tenant_id=$1 AND id=$2 AND $3<>''`,
				p.TenantID, p.EmployeeID, p.OldEmployeeStatus, platform.Now()); err != nil {
				return err
			}
			return execTwo(ctx, tx, `UPDATE employee_org_binding SET valid_to=NULL WHERE tenant_id=$1 AND employee_id=$2 AND is_primary=TRUE AND valid_to=$3`,
				p.TenantID, p.EmployeeID, p.Now)
		}},
	}
}
