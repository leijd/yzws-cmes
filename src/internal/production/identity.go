package production

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func registerIdentity(api *httpapi.API) {
	api.Handle("GET /api/v1/permissions", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT code,module,name,kind,COALESCE(description,''),admin_only,sort_no FROM platform_permission ORDER BY sort_no,code`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var code, module, name, kind, description string
			var adminOnly bool
			var sortNo int
			if err := rows.Scan(&code, &module, &name, &kind, &description, &adminOnly, &sortNo); err != nil {
				return err
			}
			items = append(items, map[string]any{"code": code, "module": module, "name": name, "kind": kind, "description": description, "admin_only": adminOnly, "sort_no": sortNo})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("GET /api/v1/roles", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT r.id,r.code,r.name,COALESCE(r.description,''),r.is_system,r.version,r.source,r.platform_template_id,COUNT(DISTINCT ur.user_id)
			FROM role r LEFT JOIN user_role ur ON ur.tenant_id=r.tenant_id AND ur.role_id=r.id
			WHERE r.tenant_id=$1
			GROUP BY r.id,r.code,r.name,r.description,r.is_system,r.version,r.source,r.platform_template_id
			ORDER BY r.source DESC,r.code`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, description, source string
			var templateID *string
			var system bool
			var version, users int64
			if err := rows.Scan(&id, &code, &name, &description, &system, &version, &source, &templateID, &users); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "description": description, "is_system": system, "version": version, "source": source, "platform_template_id": templateID, "user_count": users})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("POST /api/v1/roles", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Code        string `json:"code"`
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Code == "" || in.Name == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "角色编码和名称必填")
		}
		id, now := platform.ID(), platform.Now()
		_, err := api.DB.Exec(r.Context(), `INSERT INTO role(tenant_id,id,code,name,description,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$6,$7,$8)`, p.TenantID, id, in.Code, in.Name, in.Description, now, p.UserID, api.Config.NodeID)
		if isUniqueViolation(err) {
			return httpapi.Error(409, "DUPLICATE_ROLE", "角色编码已存在")
		}
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "version": 1})
	})
	api.Handle("GET /api/v1/roles/{id}/permissions", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT p.code FROM role_permission rp JOIN permission p ON p.tenant_id=rp.tenant_id AND p.id=rp.permission_id WHERE rp.tenant_id=$1 AND rp.role_id=$2 ORDER BY p.code`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		codes := []string{}
		for rows.Next() {
			var code string
			if err := rows.Scan(&code); err != nil {
				return err
			}
			codes = append(codes, code)
		}
		return httpapi.JSON(w, 200, map[string]any{"permission_codes": codes})
	})
	api.Handle("PUT /api/v1/roles/{id}/permissions", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			PermissionCodes []string `json:"permission_codes"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var system bool
		if err := tx.QueryRow(r.Context(), "SELECT is_system FROM role WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&system); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "角色不存在")
			}
			return err
		}
		if system {
			return httpapi.Error(409, "SYSTEM_ROLE_IMMUTABLE", "内置管理员角色不可修改，请创建自定义角色")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM role_permission WHERE tenant_id=$1 AND role_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		for _, code := range in.PermissionCodes {
			var resource, action string
			if err := tx.QueryRow(r.Context(), "SELECT split_part(permission_code,':',1),split_part(permission_code,':',2) FROM platform_module WHERE permission_code=$1 AND status='active' AND is_admin_only=FALSE", code).Scan(&resource, &action); err != nil {
				return httpapi.Error(400, "INVALID_PERMISSION", "权限点不存在、已停用或仅限固定租户管理员使用："+code)
			}
			permissionID := platform.ID()
			if _, err := tx.Exec(r.Context(), `INSERT INTO permission(tenant_id,id,code,resource,action) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,code) DO NOTHING`, p.TenantID, permissionID, code, resource, action); err != nil {
				return err
			}
			if err := tx.QueryRow(r.Context(), "SELECT id FROM permission WHERE tenant_id=$1 AND code=$2", p.TenantID, code).Scan(&permissionID); err != nil {
				return err
			}
			if _, err := tx.Exec(r.Context(), "INSERT INTO role_permission(tenant_id,id,role_id,permission_id) VALUES($1,$2,$3,$4)", p.TenantID, platform.ID(), r.PathValue("id"), permissionID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), "UPDATE role SET version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5", platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "configure_permissions", "role", r.PathValue("id"), nil, in, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"role_id": r.PathValue("id"), "permission_codes": in.PermissionCodes})
	})
	api.Handle("DELETE /api/v1/roles/{id}", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var system bool
		var source string
		if err := tx.QueryRow(r.Context(), "SELECT is_system,source FROM role WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&system, &source); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "角色不存在")
		}
		if system {
			return httpapi.Error(409, "SYSTEM_ROLE_IMMUTABLE", "固定租户管理员角色不能删除")
		}
		var userCount int
		if err := tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM user_role WHERE tenant_id=$1 AND role_id=$2", p.TenantID, r.PathValue("id")).Scan(&userCount); err != nil {
			return err
		}
		if userCount > 0 {
			return httpapi.Error(409, "ROLE_IN_USE", "角色已分配给用户，不能删除，可先停用或调整用户角色")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM role_permission WHERE tenant_id=$1 AND role_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM user_role WHERE tenant_id=$1 AND role_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM role WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete_role", "role", r.PathValue("id"), map[string]any{"source": source}, nil, "无用户使用，删除角色"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true, "source": source})
	})
	api.Handle("GET /api/v1/users", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT u.id,u.login_name,e.name,u.status,
			COALESCE(json_agg(r.id) FILTER(WHERE r.id IS NOT NULL),'[]'),
			COALESCE(json_agg(r.code) FILTER(WHERE r.code IS NOT NULL),'[]')
			FROM user_account u JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id
			LEFT JOIN user_role ur ON ur.tenant_id=u.tenant_id AND ur.user_id=u.id
			LEFT JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
			WHERE u.tenant_id=$1
			GROUP BY u.id,u.login_name,e.name,u.status
			ORDER BY u.login_name`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, login, name, status string
			var roleIDBody, roleCodeBody []byte
			if err := rows.Scan(&id, &login, &name, &status, &roleIDBody, &roleCodeBody); err != nil {
				return err
			}
			roleIDs, roleCodes := []string{}, []string{}
			_ = json.Unmarshal(roleIDBody, &roleIDs)
			_ = json.Unmarshal(roleCodeBody, &roleCodes)
			items = append(items, map[string]any{"id": id, "login_name": login, "name": name, "status": status, "role_ids": roleIDs, "roles": roleCodes})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("POST /api/v1/users", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			EmployeeNo string   `json:"employee_no"`
			Name       string   `json:"name"`
			Gender     *string  `json:"gender"`
			BirthDate  *string  `json:"birth_date"`
			Phone      *string  `json:"phone"`
			OrgUnitID  *string  `json:"org_unit_id"`
			Position   *string  `json:"position"`
			LoginName  string   `json:"login_name"`
			Password   string   `json:"password"`
			RoleIDs    []string `json:"role_ids"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		in.LoginName = in.EmployeeNo
		if in.EmployeeNo == "" || in.Name == "" || len(in.Password) < 12 {
			return httpapi.Error(400, "VALIDATION_ERROR", "工号、姓名和至少12位密码必填")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var employeeExists bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND employee_no=$2)", p.TenantID, in.EmployeeNo).Scan(&employeeExists); err != nil {
			return err
		}
		if employeeExists {
			return httpapi.Error(409, "DUPLICATE_EMPLOYEE", "员工工号已存在")
		}
		if err := validateRoleIDs(r.Context(), tx, p.TenantID, in.RoleIDs); err != nil {
			return err
		}
		if err := checkUserQuota(r.Context(), tx, p.TenantID); err != nil {
			return err
		}
		id, employeeID, err := api.Auth.CreateTenantUser(r.Context(), tx, p.TenantID, in.LoginName, in.Password, in.RoleIDs, auth.TenantUserEmployee{EmployeeNo: in.EmployeeNo, Name: in.Name, Gender: in.Gender, BirthDate: in.BirthDate, Phone: in.Phone, OrgUnitID: in.OrgUnitID, Position: in.Position})
		if isUniqueViolation(err) {
			return httpapi.Error(409, "DUPLICATE_USER", "登录名已存在")
		}
		if err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create_user", "user_account", id, nil, map[string]any{"employee_id": employeeID, "login_name": in.LoginName}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "employee_id": employeeID, "status": "active"})
	})
	api.Handle("PUT /api/v1/users/{id}/roles", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			RoleIDs []string `json:"role_ids"`
			Status  string   `json:"status"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Status != "active" && in.Status != "disabled" {
			return httpapi.Error(400, "VALIDATION_ERROR", "用户状态不合法")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var exists bool
		var currentStatus string
		if err := tx.QueryRow(r.Context(), "SELECT status FROM user_account WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&currentStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "用户不存在")
			}
			return err
		}
		exists = currentStatus != ""
		if !exists {
			return httpapi.Error(404, "NOT_FOUND", "用户不存在")
		}
		if err := validateRoleIDs(r.Context(), tx, p.TenantID, in.RoleIDs); err != nil {
			return err
		}
		if in.Status == "active" && currentStatus != "active" {
			if err := checkUserQuota(r.Context(), tx, p.TenantID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM user_role WHERE tenant_id=$1 AND user_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		for _, roleID := range in.RoleIDs {
			if _, err := tx.Exec(r.Context(), `INSERT INTO user_role(tenant_id,id,user_id,role_id,valid_from) SELECT $1,$2,$3,id,$4 FROM role WHERE tenant_id=$1 AND id=$5`, p.TenantID, platform.ID(), r.PathValue("id"), platform.Now(), roleID); err != nil {
				return err
			}
		}
		result, err := tx.Exec(r.Context(), "UPDATE user_account SET status=$1,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6", in.Status, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(404, "NOT_FOUND", "用户不存在")
		}
		if in.Status == "disabled" {
			if _, err := tx.Exec(r.Context(), "UPDATE auth_session SET revoked_at=$1,reason='user_disabled' WHERE tenant_id=$2 AND user_id=$3 AND revoked_at IS NULL", platform.Now(), p.TenantID, r.PathValue("id")); err != nil {
				return err
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": in.Status, "role_ids": in.RoleIDs})
	})
	api.Handle("DELETE /api/v1/users/{id}", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if r.PathValue("id") == p.UserID {
			return httpapi.Error(400, "CANNOT_DELETE_SELF", "不能删除当前登录管理员")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var employeeID string
		if err := tx.QueryRow(r.Context(), "SELECT employee_id FROM user_account WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&employeeID); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "用户不存在")
		}
		used, err := employeeBusinessUsage(r.Context(), tx, p.TenantID, employeeID)
		if err != nil {
			return err
		}
		if used {
			return httpapi.Error(409, "USER_IN_USE", "用户已参与业务，不能删除，可改为停用")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM user_role WHERE tenant_id=$1 AND user_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM auth_session WHERE tenant_id=$1 AND user_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM user_account WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM employee_pay_profile WHERE tenant_id=$1 AND employee_id=$2", p.TenantID, employeeID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM employee_skill WHERE tenant_id=$1 AND employee_id=$2", p.TenantID, employeeID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM employee WHERE tenant_id=$1 AND id=$2", p.TenantID, employeeID); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete_user", "user_account", r.PathValue("id"), nil, map[string]any{"employee_id": employeeID}, "无业务参与，删除用户及入职资料"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true, "employee_id": employeeID})
	})
	api.Handle("POST /api/v1/employees/{id}/account", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			LoginName string   `json:"login_name"`
			Password  string   `json:"password"`
			RoleIDs   []string `json:"role_ids"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var employeeNo string
		if err := tx.QueryRow(r.Context(), "SELECT employee_no FROM employee WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&employeeNo); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "员工档案不存在")
			}
			return err
		}
		var existing string
		err = tx.QueryRow(r.Context(), "SELECT id FROM user_account WHERE tenant_id=$1 AND employee_id=$2", p.TenantID, r.PathValue("id")).Scan(&existing)
		if err == nil {
			return httpapi.Error(409, "ACCOUNT_EXISTS", "该员工已开通账号")
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		loginName := strings.TrimSpace(in.LoginName)
		if loginName == "" {
			loginName = employeeNo
		}
		password, generated := in.Password, false
		if password == "" {
			if password, err = auth.GenerateTemporaryPassword(12); err != nil {
				return err
			}
			generated = true
		}
		if len(password) < 12 {
			return httpapi.Error(400, "WEAK_PASSWORD", "初始密码至少 12 位")
		}
		if err := validateRoleIDs(r.Context(), tx, p.TenantID, in.RoleIDs); err != nil {
			return err
		}
		if err := checkUserQuota(r.Context(), tx, p.TenantID); err != nil {
			return err
		}
		id, err := api.Auth.CreateEmployeeAccount(r.Context(), tx, p.TenantID, r.PathValue("id"), loginName, password, p.UserID, in.RoleIDs)
		if isUniqueViolation(err) {
			return httpapi.Error(409, "DUPLICATE_LOGIN", "登录名已被占用")
		}
		if err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create_account", "user_account", id, nil, map[string]any{"employee_id": r.PathValue("id"), "login_name": loginName, "auto_password": generated}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		result := map[string]any{"account_id": id, "employee_id": r.PathValue("id"), "login_name": loginName, "generated": generated, "status": "active"}
		if generated {
			result["initial_password"] = password
		}
		return httpapi.JSON(w, 201, result)
	})
	api.Handle("POST /api/v1/employees/{id}/account/reset-password", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Password string `json:"password"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var accountID, loginName string
		if err := tx.QueryRow(r.Context(), "SELECT id,login_name FROM user_account WHERE tenant_id=$1 AND employee_id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&accountID, &loginName); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "ACCOUNT_NOT_FOUND", "该员工尚未开通账号")
			}
			return err
		}
		password, generated := in.Password, false
		if password == "" {
			if password, err = auth.GenerateTemporaryPassword(12); err != nil {
				return err
			}
			generated = true
		}
		if len(password) < 12 {
			return httpapi.Error(400, "WEAK_PASSWORD", "密码至少 12 位")
		}
		if err := api.Auth.ResetAccountPassword(r.Context(), tx, p.TenantID, accountID, password, p.UserID); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "reset_password", "user_account", accountID, nil, map[string]any{"employee_id": r.PathValue("id"), "auto_password": generated}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		result := map[string]any{"account_id": accountID, "employee_id": r.PathValue("id"), "login_name": loginName, "generated": generated}
		if generated {
			result["initial_password"] = password
		}
		return httpapi.JSON(w, 200, result)
	})
}

// checkUserQuota 校验订阅规模：tenant_subscription.user_limit 为本厂启用账号数量上限，
// 未配置订阅状态（如既有租户）或上限为空时不限制。
func checkUserQuota(ctx context.Context, tx pgx.Tx, tenantID string) error {
	var limit *int
	if err := tx.QueryRow(ctx, "SELECT user_limit FROM tenant_subscription WHERE tenant_id=$1", tenantID).Scan(&limit); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if limit == nil || *limit <= 0 {
		return nil
	}
	var active int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM user_account WHERE tenant_id=$1 AND status='active'", tenantID).Scan(&active); err != nil {
		return err
	}
	if active >= *limit {
		return httpapi.Error(http.StatusConflict, "USER_LIMIT_EXCEEDED",
			fmt.Sprintf("订阅规模为 %d 个启用账号，已达上限；请先停用闲置账号或联系平台管理员调整订阅规模", *limit))
	}
	return nil
}

func employeeBusinessUsage(ctx context.Context, tx pgx.Tx, tenantID, employeeID string) (bool, error) {
	var count int
	err := tx.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM production_task WHERE tenant_id=$1 AND employee_id=$2)+
		(SELECT COUNT(*) FROM work_report WHERE tenant_id=$1 AND employee_id=$2)+
		(SELECT COUNT(*) FROM piecework_entry WHERE tenant_id=$1 AND employee_id=$2)+
		(SELECT COUNT(*) FROM task_transfer WHERE tenant_id=$1 AND (from_employee_id=$2 OR to_employee_id=$2))+
		(SELECT COUNT(*) FROM final_inspection WHERE tenant_id=$1 AND inspector_employee_id=$2)+
		(SELECT COUNT(*) FROM finished_receipt WHERE tenant_id=$1 AND created_employee_id=$2)+
		(SELECT COUNT(*) FROM shipment WHERE tenant_id=$1 AND operator_employee_id=$2)+
		(SELECT COUNT(*) FROM payment WHERE tenant_id=$1 AND operator_employee_id=$2)+
		(SELECT COUNT(*) FROM material_receipt WHERE tenant_id=$1 AND receiver_employee_id=$2)+
		(SELECT COUNT(*) FROM rework_order WHERE tenant_id=$1 AND employee_id=$2)`, tenantID, employeeID).Scan(&count)
	return count > 0, err
}

func validateRoleIDs(ctx context.Context, tx pgx.Tx, tenantID string, roleIDs []string) error {
	if len(roleIDs) == 0 {
		return nil
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT COUNT(DISTINCT id) FROM role WHERE tenant_id=$1 AND id=ANY($2)", tenantID, roleIDs).Scan(&count); err != nil {
		return err
	}
	distinct := map[string]bool{}
	for _, roleID := range roleIDs {
		if roleID != "" {
			distinct[roleID] = true
		}
	}
	if count != len(distinct) {
		return httpapi.Error(400, "INVALID_ROLE", "角色不存在或不属于当前租户")
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

var _ = strings.TrimSpace
var _ = pgx.ErrNoRows
