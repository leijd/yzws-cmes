package auth

/* 租户种子数据：计划与执行分离（设计原则：禁止长事务；双写两库 ID 必须一致）。
 *
 * PrepareTenantSeed 在事务外完成全部只读解析（短码/登录名冲突探测、工号分配、
 * 密码散列）与 ID 预生成；Exec* 阶段函数为纯写入，可在 plain/secure 两个事务中
 * 各执行一遍而结果完全一致，供 Saga 小事务逐步提交。 */

import (
	"context"
	"fmt"
	"strings"

	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

type TenantSeed struct {
	// ExistingTenantID 非空时表示在已存在的（待审批）租户上落种：不新建 tenant 行，
	// 仅更新其标识/时区，并创建组织、管理员员工与账号、角色权限等。
	ExistingTenantID string
	Code             string
	Name             string
	ShortCode        string
	Timezone         string
	AdminLoginName   string
	AdminName        string
	AdminPassword    string
}

/* TenantSeedPlan 种子写入计划：全部取值在 Prepare 阶段确定（含预生成 ID 与密码散列）。 */
type TenantSeedPlan struct {
	Seed       TenantSeed
	TenantID   string
	ShortCode  string
	AdminLogin string
	EmployeeNo string
	Verifier   string
	Now        int64

	OrgID, EmployeeID, UserID, RoleID string
	PermissionIDs                     []string // 与内置权限点一一对应
	RolePermissionIDs                 []string
	UserRoleID                        string
	MgmtLevelIDs                      []string // 4 级管理级别
	FeatureSettingIDs                 []string // 内置功能设置
	NodeID                            string
}

var seedPermissions = []string{"identity:read", "identity:write"}

var seedMgmtLevels = []struct {
	Code, Name, Desc string
	Sort             int
}{
	{"L1_FACTORY", "厂级", "全厂数据范围", 1},
	{"L2_WORKSHOP", "车间级", "本车间数据范围", 2},
	{"L3_LINE", "产线级", "本产线数据范围", 3},
	{"L4_SELF", "个人级", "本人数据范围", 4},
}

var seedFeatureSettings = []struct {
	Domain, Key, Value string
}{
	{"report", "auto_approve", "false"}, {"report", "check_previous_process", "true"},
	{"report", "check_task_quantity", "true"}, {"task", "auto_start", "true"},
}

type seedQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

/* PrepareTenantSeed 只读解析 + 预生成 ID（事务外执行，不占任何锁）。 */
func (s *Service) PrepareTenantSeed(ctx context.Context, db seedQuerier, seed TenantSeed) (*TenantSeedPlan, error) {
	if seed.Timezone == "" {
		seed.Timezone = "Asia/Shanghai"
	}
	if seed.AdminName == "" {
		seed.AdminName = "系统管理员"
	}
	plan := &TenantSeedPlan{Seed: seed, Now: platform.Now(), NodeID: s.nodeID}
	plan.TenantID = seed.ExistingTenantID
	if plan.TenantID == "" {
		plan.TenantID = platform.ID()
	}
	plan.OrgID, plan.EmployeeID, plan.UserID, plan.RoleID = platform.ID(), platform.ID(), platform.ID(), platform.ID()
	plan.UserRoleID = platform.ID()
	for range seedPermissions {
		plan.PermissionIDs = append(plan.PermissionIDs, platform.ID())
		plan.RolePermissionIDs = append(plan.RolePermissionIDs, platform.ID())
	}
	for range seedMgmtLevels {
		plan.MgmtLevelIDs = append(plan.MgmtLevelIDs, platform.ID())
	}
	for range seedFeatureSettings {
		plan.FeatureSettingIDs = append(plan.FeatureSettingIDs, platform.ID())
	}

	shortCode := strings.ToUpper(strings.TrimSpace(seed.ShortCode))
	if shortCode == "" {
		shortCode = platform.TenantShortCodeBase(seed.Name, seed.Code)
	}
	usedShort := map[string]bool{}
	rows, err := db.Query(ctx, "SELECT short_code FROM tenant")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var existing string
		if err := rows.Scan(&existing); err != nil {
			rows.Close()
			return nil, err
		}
		usedShort[existing] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	usedLogin := map[string]bool{}
	rowLogin, err := db.Query(ctx, "SELECT lower(login_name) FROM user_account")
	if err != nil {
		return nil, err
	}
	for rowLogin.Next() {
		var existing string
		if err := rowLogin.Scan(&existing); err != nil {
			rowLogin.Close()
			return nil, err
		}
		usedLogin[existing] = true
	}
	rowLogin.Close()
	if err := rowLogin.Err(); err != nil {
		return nil, err
	}
	// 已存在租户（注册审批场景）：自身当前标识不参与占用判定。
	if seed.ExistingTenantID != "" {
		var currentShort *string
		if err := db.QueryRow(ctx, "SELECT short_code FROM tenant WHERE id=$1", seed.ExistingTenantID).Scan(&currentShort); err != nil {
			return nil, err
		}
		if currentShort != nil && *currentShort != "" {
			delete(usedShort, *currentShort)
		}
	}
	// 租户管理员账号默认 {租户标识}admin，冲突时调整租户标识。
	adminLogin := seed.AdminLoginName
	chosen := ""
	for _, candidate := range platform.ShortCodeCandidates(shortCode) {
		if usedShort[candidate] {
			continue
		}
		login := adminLogin
		if login == "" {
			login = candidate + "admin"
		}
		if usedLogin[strings.ToLower(login)] {
			continue
		}
		chosen = candidate
		break
	}
	if chosen == "" {
		return nil, fmt.Errorf("no available tenant short code for %s", seed.Name)
	}
	plan.ShortCode = chosen
	plan.AdminLogin = adminLogin
	if plan.AdminLogin == "" {
		plan.AdminLogin = plan.ShortCode + "admin"
	}
	// 管理员工号 {标识}-{6 位序号} 取首个空闲。
	plan.EmployeeNo = plan.ShortCode + "-000001"
	for seq := 1; seq <= 999999; seq++ {
		plan.EmployeeNo = fmt.Sprintf("%s-%06d", plan.ShortCode, seq)
		var taken bool
		if err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM employee WHERE upper(employee_no)=$1)", plan.EmployeeNo).Scan(&taken); err != nil {
			return nil, err
		}
		if !taken {
			break
		}
	}
	verifier, err := hashPassword(seed.AdminPassword)
	if err != nil {
		return nil, err
	}
	plan.Verifier = verifier
	return plan, nil
}

/* ExecRoot 阶段 1：tenant 行（新建或标识更新）+ 主组织。 */
func (p *TenantSeedPlan) ExecRoot(ctx context.Context, tx pgx.Tx) error {
	if p.Seed.ExistingTenantID != "" {
		if _, err := tx.Exec(ctx, "UPDATE tenant SET short_code=$1,timezone=$2,version=version+1 WHERE id=$3", p.ShortCode, p.Seed.Timezone, p.TenantID); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, "INSERT INTO tenant(id,code,name,short_code,timezone,status,created_at) VALUES($1,$2,$3,$4,$5,'active',$6)", p.TenantID, p.Seed.Code, p.Seed.Name, p.ShortCode, p.Seed.Timezone, p.Now); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `INSERT INTO org_unit(tenant_id,id,org_type,code,name,path,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,'department','MAIN','生产部',$3,$4,$5,$6,$4,$5,$6)`, p.TenantID, p.OrgID, p.OrgID+"/", p.Now, p.UserID, p.NodeID)
	return err
}

/* ExecAdmin 阶段 2：管理员员工档案 + 登录账号。 */
func (p *TenantSeedPlan) ExecAdmin(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `INSERT INTO employee(tenant_id,id,employee_no,name,org_unit_id,position,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,'系统管理员',$6,$7,$8,$6,$7,$8)`, p.TenantID, p.EmployeeID, p.EmployeeNo, p.Seed.AdminName, p.OrgID, p.Now, p.UserID, p.NodeID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO user_account(tenant_id,id,employee_id,login_name,password_verifier,password_change_required,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,TRUE,$6,$2,$7,$6,$2,$7)`, p.TenantID, p.UserID, p.EmployeeID, p.AdminLogin, p.Verifier, p.Now, p.NodeID)
	return err
}

/* ExecRole 阶段 3：内置租户管理员角色 + 权限点 + 授权。 */
func (p *TenantSeedPlan) ExecRole(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `INSERT INTO role(tenant_id,id,code,name,is_system,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,'tenant_admin','租户管理员',TRUE,$3,$4,$5,$3,$4,$5)`, p.TenantID, p.RoleID, p.Now, p.UserID, p.NodeID); err != nil {
		return err
	}
	for i, code := range seedPermissions {
		parts := strings.SplitN(code, ":", 2)
		if _, err := tx.Exec(ctx, "INSERT INTO permission(tenant_id,id,code,resource,action) VALUES($1,$2,$3,$4,$5)", p.TenantID, p.PermissionIDs[i], code, parts[0], parts[1]); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO role_permission(tenant_id,id,role_id,permission_id) VALUES($1,$2,$3,$4)", p.TenantID, p.RolePermissionIDs[i], p.RoleID, p.PermissionIDs[i]); err != nil {
			return err
		}
	}
	// 内置租户管理员只负责用户/角色/权限管理。
	_, err := tx.Exec(ctx, "INSERT INTO user_role(tenant_id,id,user_id,role_id,valid_from) VALUES($1,$2,$3,$4,$5)", p.TenantID, p.UserRoleID, p.UserID, p.RoleID, p.Now)
	return err
}

/* 下拉选项字典种子（与迁移 066 一致）：员工表单下拉从数据库实际数据获取。 */
var seedDictionaryItems = []struct {
	Type, Code, Name string
	Sort             int
}{
	{"nation", "汉", "汉", 1}, {"nation", "壮", "壮", 2}, {"nation", "回", "回", 3}, {"nation", "苗", "苗", 4}, {"nation", "瑶", "瑶", 5}, {"nation", "其他", "其他", 9},
	{"position", "车工", "车工", 1}, {"position", "裁床师傅", "裁床师傅", 2}, {"position", "组长", "组长", 3}, {"position", "质检", "质检", 4},
	{"position", "仓管", "仓管", 5}, {"position", "整烫工", "整烫工", 6}, {"position", "生产工艺员", "生产工艺员", 7},
	{"skill_grade", "A", "A 熟练", 1}, {"skill_grade", "B", "B 合格", 2}, {"skill_grade", "C", "C 学徒", 3},
	{"rate_level", "high", "高档（系数 1.25）", 1}, {"rate_level", "mid", "中档（系数 1.0）", 2}, {"rate_level", "low", "低档（系数 0.85）", 3},
}

/* ExecSettings 阶段 4：管理级别四级 + 内置功能设置 + 下拉选项字典。 */
func (p *TenantSeedPlan) ExecSettings(ctx context.Context, tx pgx.Tx) error {
	for i, lv := range seedMgmtLevels {
		if _, err := tx.Exec(ctx, `INSERT INTO mgmt_level(tenant_id,id,level_code,level_name,description,sort_no,status,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,'active',$7,$8,$9,$7,$8,$9)`,
			p.TenantID, p.MgmtLevelIDs[i], lv.Code, lv.Name, lv.Desc, lv.Sort, p.Now, p.UserID, p.NodeID); err != nil {
			return err
		}
	}
	for i, setting := range seedFeatureSettings {
		if _, err := tx.Exec(ctx, `INSERT INTO feature_setting(tenant_id,id,setting_domain,setting_key,setting_value,version,effective_at,updated_by,created_at) VALUES($1,$2,$3,$4,$5,1,$6,$7,$6)`, p.TenantID, p.FeatureSettingIDs[i], setting.Domain, setting.Key, setting.Value, p.Now, p.UserID); err != nil {
			return err
		}
	}
	for _, item := range seedDictionaryItems {
		if _, err := tx.Exec(ctx, `INSERT INTO dictionary_item(tenant_id,id,dictionary_type,code,name,sort_no,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$7,$8,$9) ON CONFLICT DO NOTHING`,
			p.TenantID, fmt.Sprintf("dic-%s-%s-%s", p.TenantID, item.Type, item.Code), item.Type, item.Code, item.Name, item.Sort, p.Now, p.UserID, p.NodeID); err != nil {
			return err
		}
	}
	return nil
}

/* SeedTenant 单事务顺序执行全部阶段（仅启动引导等单库场景使用；
 * 运行期审批/建厂走 Saga 分步双写，勿再新增调用）。 */
func (s *Service) SeedTenant(ctx context.Context, tx pgx.Tx, seed TenantSeed) (map[string]string, error) {
	plan, err := s.PrepareTenantSeed(ctx, tx, seed)
	if err != nil {
		return nil, err
	}
	for _, stage := range []func(context.Context, pgx.Tx) error{plan.ExecRoot, plan.ExecAdmin, plan.ExecRole, plan.ExecSettings} {
		if err := stage(ctx, tx); err != nil {
			return nil, err
		}
	}
	return map[string]string{"tenant_id": plan.TenantID, "tenant_admin_user_id": plan.UserID, "employee_id": plan.EmployeeID}, nil
}
