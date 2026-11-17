// Package orgapp 车间/产线专用 API 与数据范围计算（设计 5.2/5.3/5.7）。
package orgapp

import (
	"context"
	"fmt"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
)

func nowMicro(ctx context.Context) int64 { _ = ctx; return platform.Now() }

/* Access 数据范围（设计 5.7）：All=全厂；否则按组织 ID 过滤；SelfOnly=仅本人。
 * 无有效归属时范围为空（不返回全厂）；tenant_admin 不参与业务数据。 */
type Access struct {
	All          bool
	WorkshopIDs  []string
	LineIDs      []string
	TeamIDs      []string
	WarehouseIDs []string
	SelfOnly     bool
	/* SelfEmployeeID 本人员工档案 ID：SelfOnly 但未挂靠车间/产线/班组时，
	 * 组织范围谓词退化为按本人过滤（e.id = 该值），而非 AND FALSE。 */
	SelfEmployeeID string
	/* Deny 禁止范围（user_scope effect='deny'，优先级高于允许范围，设计 5.7 第 3 条） */
	Deny *DenyScope
}

/* DenyScope 显式禁止的组织/仓库范围。 */
type DenyScope struct {
	WorkshopIDs  []string
	LineIDs      []string
	TeamIDs      []string
	WarehouseIDs []string
}

/* EmptyAccess 空范围：除身份管理外不可见任何业务数据。 */
func EmptyAccess() *Access { return &Access{} }

/* ComputeAccess 依据角色 default_data_scope 与当前主归属计算范围（设计 5.7 计算顺序 1-4 的简化实现）。
 * 取“角色默认范围、管理级别、当前归属”的交集口径：按最强业务角色归属范围执行。 */
func ComputeAccess(ctx context.Context, db *pgxpool.Pool, p auth.Principal) (*Access, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT r.code, COALESCE(r.default_data_scope,'')
		FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
		WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND ur.valid_from<=$3 AND (ur.valid_to IS NULL OR ur.valid_to>$3)`,
		p.TenantID, p.UserID, nowMicro(ctx))
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
	/* 管理级别决定可见上限（设计 4.17.1/5.7）：与角色默认范围取交集，取更窄者 */
	if levelScope := mgmtLevelScope(ctx, db, p); levelScope != "" && rankOfScope(levelScope) < rankOfScope(scope) {
		scope = levelScope
	}
	/* 当前主归属 */
	var workshopID, lineID, teamID *string
	err = db.QueryRow(ctx, `SELECT workshop_org_id, line_org_id, team_org_id FROM employee
		WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.EmployeeID).Scan(&workshopID, &lineID, &teamID)
	if err != nil {
		/* 无员工档案（平台/辅助类账号）：厂级范围不依赖个人归属，其余范围无归属即空 */
		if scope == "factory" {
			return applyUserScope(ctx, db, p, &Access{All: true})
		}
		return EmptyAccess(), nil
	}
	var result *Access
	switch scope {
	case "factory":
		result = &Access{All: true}
	case "workshop":
		if workshopID != nil {
			result = &Access{WorkshopIDs: []string{*workshopID}}
		}
	case "line":
		if lineID != nil {
			result = &Access{LineIDs: []string{*lineID}}
		}
	case "team":
		if teamID != nil {
			result = &Access{TeamIDs: []string{*teamID}, SelfOnly: true, SelfEmployeeID: p.EmployeeID}
		}
	case "self":
		result = &Access{SelfOnly: true, SelfEmployeeID: p.EmployeeID}
	}
	if result == nil {
		return EmptyAccess(), nil
	}
	return applyUserScope(ctx, db, p, result)
}

/* mgmtLevelScope 账号管理级别映射的数据范围（设计 4.17.1：L1_FACTORY/L2_WORKSHOP/L3_LINE/L4_SELF）。
 * 未配置管理级别时返回空串，表示不额外收窄。 */
func mgmtLevelScope(ctx context.Context, db *pgxpool.Pool, p auth.Principal) string {
	if p.UserID == "" {
		return ""
	}
	var levelCode string
	err := db.QueryRow(ctx, `SELECT COALESCE(m.level_code,'') FROM user_account u
		LEFT JOIN mgmt_level m ON m.tenant_id=u.tenant_id AND m.id=u.mgmt_level_id
		WHERE u.tenant_id=$1 AND u.id=$2`, p.TenantID, p.UserID).Scan(&levelCode)
	if err != nil {
		return ""
	}
	switch levelCode {
	case "L1_FACTORY":
		return "factory"
	case "L2_WORKSHOP":
		return "workshop"
	case "L3_LINE":
		return "line"
	case "L4_SELF":
		return "self"
	default:
		return ""
	}
}

/* applyUserScope 叠加 user_scope 显式允许/禁止范围（设计 5.7 第 3 条：用户范围优先，禁止优先）。
 * scope_type: workshop / line / team / warehouse / self；effect: allow（默认）/ deny。 */
func applyUserScope(ctx context.Context, db *pgxpool.Pool, p auth.Principal, base *Access) (*Access, error) {
	rows, err := db.Query(ctx, `SELECT scope_type,scope_value,COALESCE(effect,'allow') FROM user_scope
		WHERE tenant_id=$1 AND user_id=$2`, p.TenantID, p.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	allowOrg, allowSelf := false, false
	var allowW, allowL, allowT, allowWH []string
	deny := &DenyScope{}
	for rows.Next() {
		var scopeType, scopeValue, effect string
		if err := rows.Scan(&scopeType, &scopeValue, &effect); err != nil {
			return nil, err
		}
		if effect == "deny" {
			switch scopeType {
			case "workshop":
				deny.WorkshopIDs = append(deny.WorkshopIDs, scopeValue)
			case "line":
				deny.LineIDs = append(deny.LineIDs, scopeValue)
			case "team":
				deny.TeamIDs = append(deny.TeamIDs, scopeValue)
			case "warehouse":
				deny.WarehouseIDs = append(deny.WarehouseIDs, scopeValue)
			}
			continue
		}
		switch scopeType {
		case "workshop":
			allowOrg = true
			allowW = append(allowW, scopeValue)
		case "line":
			allowOrg = true
			allowL = append(allowL, scopeValue)
		case "team":
			allowOrg = true
			allowT = append(allowT, scopeValue)
		case "warehouse":
			allowWH = append(allowWH, scopeValue)
		case "self":
			allowSelf = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	/* 显式允许的组织范围覆盖角色默认范围（用于多车间/多产线兼管）；
	 * 仓库范围总是叠加，避免角色级配置被误清空。 */
	if allowOrg {
		base.All = false
		base.WorkshopIDs, base.LineIDs, base.TeamIDs = allowW, allowL, allowT
	}
	if len(allowWH) > 0 {
		base.WarehouseIDs = allowWH
	}
	if allowSelf {
		base.SelfOnly = true
		if base.SelfEmployeeID == "" {
			base.SelfEmployeeID = p.EmployeeID
		}
	}
	if len(deny.WorkshopIDs)+len(deny.LineIDs)+len(deny.TeamIDs)+len(deny.WarehouseIDs) > 0 {
		base.Deny = deny
		base.WorkshopIDs = removeIDs(base.WorkshopIDs, deny.WorkshopIDs)
		base.LineIDs = removeIDs(base.LineIDs, deny.LineIDs)
		base.TeamIDs = removeIDs(base.TeamIDs, deny.TeamIDs)
		base.WarehouseIDs = removeIDs(base.WarehouseIDs, deny.WarehouseIDs)
	}
	return base, nil
}

func removeIDs(source, remove []string) []string {
	if len(source) == 0 || len(remove) == 0 {
		return source
	}
	drop := map[string]bool{}
	for _, id := range remove {
		drop[id] = true
	}
	out := source[:0]
	for _, id := range source {
		if !drop[id] {
			out = append(out, id)
		}
	}
	return out
}

/* scopeForRole 角色默认数据范围（设计 5.5 模板）。
 * tenant_admin（租户管理员）= 本厂最高管理员，拥有厂级全量数据范围：
 * 体验厂管理员/厂主都以该角色登录，创建车间等管理操作后必须能看到全厂数据。
 * 角色编码同时兼容旧模板（factory_head/FChief/FM 等）与平台新模板
 * （factory_manager/production_supervisor 等），两套命名映射到同一语义。 */
func scopeForRole(code string) string {
	switch code {
	case "tenant_admin", "factory_head", "manager", "statistician", "process_engineer",
		"factory_manager", "production_supervisor", "process_technician", "payroll_finance",
		"FChief", "FM":
		return "factory"
	case "workshop_director", "final_inspector", "warehouse_keeper",
		"warehouse", "cutting_lead":
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

/* AllowWorkshop / AllowLine / AllowTeam / AllowWarehouse 范围判定（详情/更新同样过滤，设计 5.7 第 5 条），
 * 显式禁止项优先。 */
func (a *Access) AllowWorkshop(workshopID string) bool {
	if a.denied(workshopID) {
		return false
	}
	if a.All {
		return true
	}
	return containsID(a.WorkshopIDs, workshopID)
}

func (a *Access) AllowLine(lineID, workshopID string) bool {
	if a.denied(lineID) || a.denied(workshopID) {
		return false
	}
	if a.All {
		return true
	}
	return containsID(a.LineIDs, lineID) || a.AllowWorkshop(workshopID)
}

func (a *Access) AllowTeam(teamID string) bool {
	if a.denied(teamID) {
		return false
	}
	return a.All || containsID(a.TeamIDs, teamID)
}

func (a *Access) AllowWarehouse(warehouseID string) bool {
	if a.denied(warehouseID) {
		return false
	}
	return a.All || containsID(a.WarehouseIDs, warehouseID)
}

func (a *Access) denied(id string) bool {
	if a.Deny == nil || id == "" {
		return false
	}
	return containsID(a.Deny.WorkshopIDs, id) || containsID(a.Deny.LineIDs, id) ||
		containsID(a.Deny.TeamIDs, id) || containsID(a.Deny.WarehouseIDs, id)
}

/* AllowEmployeeOrg 按员工当前归属判定是否能看该员工的数据（派工/报工/工资明细）。
 * employeeID 为被访问数据所属员工的 ID；纯 self 范围下仅允许访问本人数据。 */
func (a *Access) AllowEmployeeOrg(workshopID, lineID, teamID *string, employeeID ...string) bool {
	if a.All && a.Deny == nil {
		return true
	}
	/* 纯 self 范围且无组织归属：只允许本人数据 */
	if a.SelfOnly && a.SelfEmployeeID != "" && len(a.WorkshopIDs) == 0 && len(a.LineIDs) == 0 && len(a.TeamIDs) == 0 {
		if len(employeeID) > 0 && employeeID[0] != "" {
			return employeeID[0] == a.SelfEmployeeID
		}
		return false
	}
	workshop := ""
	if workshopID != nil {
		workshop = *workshopID
	}
	if workshop != "" && a.AllowWorkshop(workshop) {
		return true
	}
	if lineID != nil && *lineID != "" && a.AllowLine(*lineID, workshop) {
		return true
	}
	if teamID != nil && *teamID != "" && a.AllowTeam(*teamID) {
		return true
	}
	return false
}

func containsID(ids []string, id string) bool {
	for _, item := range ids {
		if item == id {
			return true
		}
	}
	return false
}

/* OrgScopeSQL 人员归属范围谓词（用于生产域按「车间/产线/班组」过滤，设计 5.7）。
 * employeeAlias 为 employee 表别名；返回谓词片段与参数，调用方拼接到 WHERE 后。 */
func (a *Access) OrgScopeSQL(employeeAlias string, firstArg int) (string, []any) {
	if a.All && a.Deny == nil {
		return "", nil
	}
	var conds []string
	args := []any{}
	appendIn := func(column string, ids []string) {
		if len(ids) == 0 {
			return
		}
		var ph []string
		for _, id := range ids {
			ph = append(ph, fmt.Sprintf("$%d", firstArg+len(args)))
			args = append(args, id)
		}
		conds = append(conds, fmt.Sprintf("%s.%s IN (%s)", employeeAlias, column, strings.Join(ph, ",")))
	}
	if !a.All {
		appendIn("workshop_org_id", a.WorkshopIDs)
		appendIn("line_org_id", a.LineIDs)
		appendIn("team_org_id", a.TeamIDs)
		if len(conds) == 0 {
			/* 纯 self 范围（车工等）：无车间/产线/班组归属时按本人员工档案过滤，
			 * 而不是 AND FALSE 导致本人数据恒空（设计 5.7「仅本人」语义）。 */
			if a.SelfOnly && a.SelfEmployeeID != "" {
				conds = append(conds, fmt.Sprintf("%s.id=$%d", employeeAlias, firstArg+len(args)))
				args = append(args, a.SelfEmployeeID)
			} else {
				return " AND FALSE", nil
			}
		}
	}
	if a.Deny != nil {
		appendIn("workshop_org_id", a.Deny.WorkshopIDs)
		appendIn("line_org_id", a.Deny.LineIDs)
		appendIn("team_org_id", a.Deny.TeamIDs)
	}
	return " AND (" + strings.Join(conds, " OR ") + ")", args
}

/* OrgColumnsScopeSQL 直接按组织列过滤（无 employee 关联的场景，如进度投影/报表）。
 * workshopColumn/lineColumn 为空表示该表没有对应列。 */
func (a *Access) OrgColumnsScopeSQL(workshopColumn, lineColumn string, firstArg int) (string, []any) {
	if a.All && a.Deny == nil {
		return "", nil
	}
	var conds []string
	args := []any{}
	appendIn := func(column string, ids []string) {
		if column == "" || len(ids) == 0 {
			return
		}
		var ph []string
		for _, id := range ids {
			ph = append(ph, fmt.Sprintf("$%d", firstArg+len(args)))
			args = append(args, id)
		}
		conds = append(conds, fmt.Sprintf("%s IN (%s)", column, strings.Join(ph, ",")))
	}
	if !a.All {
		appendIn(workshopColumn, a.WorkshopIDs)
		appendIn(lineColumn, a.LineIDs)
		if len(conds) == 0 {
			return " AND FALSE", nil
		}
	}
	if a.Deny != nil {
		appendIn(workshopColumn, a.Deny.WorkshopIDs)
		appendIn(lineColumn, a.Deny.LineIDs)
	}
	return " AND (" + strings.Join(conds, " OR ") + ")", args
}

/* WarehouseScopeSQL 仓库范围谓词（物料领退/库存查询）。 */
func (a *Access) WarehouseScopeSQL(warehouseColumn string, firstArg int) (string, []any) {
	if a.All && a.Deny == nil {
		return "", nil
	}
	var conds []string
	args := []any{}
	appendIn := func(ids []string) {
		if len(ids) == 0 {
			return
		}
		var ph []string
		for _, id := range ids {
			ph = append(ph, fmt.Sprintf("$%d", firstArg+len(args)))
			args = append(args, id)
		}
		conds = append(conds, fmt.Sprintf("%s IN (%s)", warehouseColumn, strings.Join(ph, ",")))
	}
	if !a.All {
		appendIn(a.WarehouseIDs)
		if len(conds) == 0 {
			return " AND FALSE", nil
		}
	}
	if a.Deny != nil {
		appendIn(a.Deny.WarehouseIDs)
	}
	return " AND (" + strings.Join(conds, " OR ") + ")", args
}

/* DenyOrgSQL 禁止组织范围谓词（全部为 NOT IN，设计 5.7 禁止优先）。
 * idColumn 为组织自身列，parentColumn 为上级组织列（产线/班组需同时排除被禁车间）。 */
func (a *Access) DenyOrgSQL(idColumn, parentColumn string, firstArg int) (string, []any) {
	if a.Deny == nil {
		return "", nil
	}
	ids := append(append(append([]string{}, a.Deny.WorkshopIDs...), a.Deny.LineIDs...), a.Deny.TeamIDs...)
	if len(ids) == 0 {
		return "", nil
	}
	var ph []string
	args := []any{}
	for _, id := range ids {
		ph = append(ph, fmt.Sprintf("$%d", firstArg+len(args)))
		args = append(args, id)
	}
	in := strings.Join(ph, ",")
	if parentColumn == "" {
		return fmt.Sprintf(" AND %s NOT IN (%s)", idColumn, in), args
	}
	return fmt.Sprintf(" AND %s NOT IN (%s) AND (%s IS NULL OR %s NOT IN (%s))", idColumn, in, parentColumn, parentColumn, in), args
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
