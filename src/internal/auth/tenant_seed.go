package auth

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

func (s *Service) SeedTenant(ctx context.Context, tx pgx.Tx, seed TenantSeed) (map[string]string, error) {
	if seed.Timezone == "" {
		seed.Timezone = "Asia/Shanghai"
	}
	if seed.AdminName == "" {
		seed.AdminName = "系统管理员"
	}
	now := platform.Now()
	tenantID := seed.ExistingTenantID
	if tenantID == "" {
		tenantID = platform.ID()
	}
	orgID, employeeID, userID, roleID := platform.ID(), platform.ID(), platform.ID(), platform.ID()
	shortCode := strings.ToUpper(strings.TrimSpace(seed.ShortCode))
	if shortCode == "" {
		shortCode = platform.TenantShortCodeBase(seed.Name, seed.Code)
	}
	usedShort := map[string]bool{}
	rows, err := tx.Query(ctx, "SELECT short_code FROM tenant")
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
	rowLogin, err := tx.Query(ctx, "SELECT lower(login_name) FROM user_account")
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
		if err := tx.QueryRow(ctx, "SELECT short_code FROM tenant WHERE id=$1", seed.ExistingTenantID).Scan(&currentShort); err != nil {
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
	shortCode = chosen
	if adminLogin == "" {
		adminLogin = shortCode + "admin"
	}
	if seed.ExistingTenantID != "" {
		if _, err := tx.Exec(ctx, "UPDATE tenant SET short_code=$1,timezone=$2,version=version+1 WHERE id=$3", shortCode, seed.Timezone, tenantID); err != nil {
			return nil, err
		}
	} else {
		if _, err := tx.Exec(ctx, "INSERT INTO tenant(id,code,name,short_code,timezone,status,created_at) VALUES($1,$2,$3,$4,$5,'active',$6)", tenantID, seed.Code, seed.Name, shortCode, seed.Timezone, now); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO org_unit(tenant_id,id,org_type,code,name,path,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,'department','MAIN','生产部',$3,$4,$5,$6,$4,$5,$6)`, tenantID, orgID, orgID+"/", now, userID, s.nodeID); err != nil {
		return nil, err
	}
	adminEmployeeNo := shortCode + "-000001"
	for seq := 1; seq <= 999999; seq++ {
		adminEmployeeNo = fmt.Sprintf("%s-%06d", shortCode, seq)
		var taken bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM employee WHERE upper(employee_no)=$1)", adminEmployeeNo).Scan(&taken); err != nil {
			return nil, err
		}
		if !taken {
			break
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO employee(tenant_id,id,employee_no,name,org_unit_id,position,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,'系统管理员',$6,$7,$8,$6,$7,$8)`, tenantID, employeeID, adminEmployeeNo, seed.AdminName, orgID, now, userID, s.nodeID); err != nil {
		return nil, err
	}
	verifier, err := hashPassword(seed.AdminPassword)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_account(tenant_id,id,employee_id,login_name,password_verifier,password_change_required,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,TRUE,$6,$2,$7,$6,$2,$7)`, tenantID, userID, employeeID, adminLogin, verifier, now, s.nodeID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO role(tenant_id,id,code,name,is_system,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,'tenant_admin','租户管理员',TRUE,$3,$4,$5,$3,$4,$5)`, tenantID, roleID, now, userID, s.nodeID); err != nil {
		return nil, err
	}
	permissions := []string{
		"identity:read", "identity:write",
	}
	for _, code := range permissions {
		permissionID := platform.ID()
		parts := strings.SplitN(code, ":", 2)
		if _, err := tx.Exec(ctx, "INSERT INTO permission(tenant_id,id,code,resource,action) VALUES($1,$2,$3,$4,$5)", tenantID, permissionID, code, parts[0], parts[1]); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO role_permission(tenant_id,id,role_id,permission_id) VALUES($1,$2,$3,$4)", tenantID, platform.ID(), roleID, permissionID); err != nil {
			return nil, err
		}
	}
	// 内置租户管理员只负责用户/角色/权限管理。
	if _, err := tx.Exec(ctx, "INSERT INTO user_role(tenant_id,id,user_id,role_id,valid_from) VALUES($1,$2,$3,$4,$5)", tenantID, platform.ID(), userID, roleID, now); err != nil {
		return nil, err
	}
	// 管理级别标准四级：管理级别决定数据范围，角色决定权限点（L1 厂级→L4 个人级）。
	levels := []struct {
		code, name, desc string
		sort             int
	}{
		{"L1_FACTORY", "厂级", "全厂数据范围", 1},
		{"L2_WORKSHOP", "车间级", "本车间数据范围", 2},
		{"L3_LINE", "产线级", "本产线数据范围", 3},
		{"L4_SELF", "个人级", "本人数据范围", 4},
	}
	for _, lv := range levels {
		if _, err := tx.Exec(ctx, `INSERT INTO mgmt_level(tenant_id,id,level_code,level_name,description,sort_no,status,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,'active',$7,$8,$9,$7,$8,$9)`,
			tenantID, platform.ID(), lv.code, lv.name, lv.desc, lv.sort, now, userID, s.nodeID); err != nil {
			return nil, err
		}
	}
	settings := []struct{ domain, key, value string }{
		{"report", "auto_approve", "false"}, {"report", "check_previous_process", "true"},
		{"report", "check_task_quantity", "true"}, {"task", "auto_start", "true"},
	}
	for _, setting := range settings {
		if _, err := tx.Exec(ctx, `INSERT INTO feature_setting(tenant_id,id,setting_domain,setting_key,setting_value,version,effective_at,updated_by,created_at) VALUES($1,$2,$3,$4,$5,1,$6,$7,$6)`, tenantID, platform.ID(), setting.domain, setting.key, setting.value, now, userID); err != nil {
			return nil, err
		}
	}
	return map[string]string{"tenant_id": tenantID, "tenant_admin_user_id": userID, "employee_id": employeeID}, nil
}
