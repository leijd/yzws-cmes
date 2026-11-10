// Package orgapp 车间/产线专用 API 与数据范围计算（设计 5.2/5.3/5.7）。
package orgapp

import (
	"context"
	"fmt"

	"clothp/internal/auth"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

func nowMicro(ctx context.Context) int64 { _ = ctx; return platform.Now() }

/* Access 数据范围（设计 5.7）：All=全厂；否则按组织 ID 过滤；SelfOnly=仅本人。
 * 无有效归属时范围为空（不返回全厂）；tenant_admin 不参与业务数据。 */
type Access struct {
	All         bool
	WorkshopIDs []string
	LineIDs     []string
	TeamIDs     []string
	SelfOnly    bool
}

/* EmptyAccess 空范围：除身份管理外不可见任何业务数据。 */
func EmptyAccess() *Access { return &Access{} }

/* ComputeAccess 依据角色 default_data_scope 与当前主归属计算范围（设计 5.7 计算顺序 1-4 的简化实现）。
 * 取“角色默认范围、管理级别、当前归属”的交集口径：按最强业务角色归属范围执行。 */
func ComputeAccess(ctx context.Context, db *pgxpool.Pool, p auth.Principal) (*Access, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT r.code, COALESCE(r.default_data_scope,'')
		FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
		WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND ur.valid_from<=$3 AND (ur.valid_to IS NULL OR ur.valid_to>$3)
		  AND r.code<>'tenant_admin'`, p.TenantID, p.UserID, nowMicro(ctx))
	if err != nil {
		return nil, err
	}
	var scope string
	best := ""
	for rows.Next() {
		var code, s string
		if err := rows.Scan(&code, &s); err != nil {
			rows.Close()
			return nil, err
		}
		if s == "" {
			s = scopeForRole(code)
		}
		if rankOfScope(s) > rankOfScope(best) {
			best, scope = code, s
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if scope == "" {
		return EmptyAccess(), nil // tenant_admin 或无业务角色：无业务数据范围
	}
	/* 当前主归属 */
	var workshopID, lineID, teamID *string
	err = db.QueryRow(ctx, `SELECT workshop_org_id, line_org_id, team_org_id FROM employee
		WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.EmployeeID).Scan(&workshopID, &lineID, &teamID)
	if err != nil {
		/* 无员工档案（平台/辅助类账号）：无归属即空范围 */
		return EmptyAccess(), nil
	}
	switch scope {
	case "factory":
		return &Access{All: true}, nil
	case "workshop":
		if workshopID == nil {
			return EmptyAccess(), nil
		}
		return &Access{WorkshopIDs: []string{*workshopID}}, nil
	case "line":
		if lineID == nil {
			return EmptyAccess(), nil
		}
		return &Access{LineIDs: []string{*lineID}, TeamIDs: []string{}}, nil
	case "team":
		if teamID == nil {
			return EmptyAccess(), nil
		}
		return &Access{TeamIDs: []string{*teamID}, SelfOnly: true}, nil
	case "self":
		return &Access{SelfOnly: true}, nil
	default:
		return EmptyAccess(), nil
	}
}

/* scopeForRole 角色默认数据范围（设计 5.5 模板）。 */
func scopeForRole(code string) string {
	switch code {
	case "factory_head", "manager", "statistician", "process_engineer":
		return "factory"
	case "workshop_director", "final_inspector", "warehouse_keeper":
		return "workshop"
	case "group_leader", "line_inspector", "offline_process_leader", "finishing_leader":
		return "line"
	case "sewer", "worker":
		return "self"
	default:
		return ""
	}
}

func rankOfScope(scope string) int {
	switch scope {
	case "factory":
		return 6
	case "workshop":
		return 5
	case "line":
		return 4
	case "team":
		return 3
	case "self":
		return 2
	default:
		return 0
	}
}

/* AllowWorkshop / AllowLine 范围判定（详情/更新同样过滤，设计 5.7 第 5 条）。 */
func (a *Access) AllowWorkshop(workshopID string) bool {
	if a.All {
		return true
	}
	for _, id := range a.WorkshopIDs {
		if id == workshopID {
			return true
		}
	}
	return false
}

func (a *Access) AllowLine(lineID, workshopID string) bool {
	if a.All {
		return true
	}
	for _, id := range a.LineIDs {
		if id == lineID {
			return true
		}
	}
	return a.AllowWorkshop(workshopID)
}

/* ScopeSummary 用于响应与审计的范围摘要（不含敏感数据）。 */
func (a *Access) ScopeSummary() string {
	if a.All {
		return "factory"
	}
	if a.SelfOnly && len(a.TeamIDs) == 0 && len(a.LineIDs) == 0 && len(a.WorkshopIDs) == 0 {
		return "self"
	}
	return fmt.Sprintf("workshops=%d lines=%d teams=%d self=%v", len(a.WorkshopIDs), len(a.LineIDs), len(a.TeamIDs), a.SelfOnly)
}
