package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/platform"
)

/* 注册联系电话（与注册/员工资料保持一致：11 位中国大陆手机号） */
var contactPhonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

func (a *API) RegisterAuthRoutes() {
	a.Public("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) error {
		var input auth.LoginInput
		if err := Decode(r, &input); err != nil {
			return err
		}
		result, err := a.Auth.Login(r.Context(), input)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return Error(http.StatusUnauthorized, "UNAUTHENTICATED", "账号或密码错误")
		}
		if err != nil {
			return err
		}
		return JSON(w, http.StatusOK, result)
	})
	a.Public("POST /api/v1/auth/unified-login", func(w http.ResponseWriter, r *http.Request) error {
		var input auth.PlatformLoginInput
		if err := Decode(r, &input); err != nil {
			return err
		}
		result, accountType, err := a.Auth.UnifiedLogin(r.Context(), input)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return Error(http.StatusUnauthorized, "UNAUTHENTICATED", "账号或密码错误")
		}
		if errors.Is(err, auth.ErrAmbiguousLogin) {
			return Error(http.StatusConflict, "AMBIGUOUS_LOGIN", "该账号在多个租户中存在，无法唯一确定登录租户")
		}
		if err != nil {
			return err
		}
		if accountType == "platform" {
			admin := result["admin"].(map[string]any)
			adminID := admin["id"].(string)
			_, _ = a.DB.Exec(r.Context(), `INSERT INTO platform_audit_log(id,request_id,actor_id,action,owner_type,owner_id,created_at) VALUES($1,$2,$3,'platform_login','platform_admin',$3,$4)`, "audit-"+adminID+"-"+RequestID(r.Context()), RequestID(r.Context()), adminID, platform.Now())
		}
		return JSON(w, http.StatusOK, result)
	})
	a.Public("POST /api/v1/auth/refresh", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			RefreshToken string `json:"refresh_token"`
			NodeID       string `json:"node_id"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		result, err := a.Auth.Refresh(r.Context(), input.RefreshToken, input.NodeID)
		if errors.Is(err, auth.ErrUnauthenticated) {
			return Error(http.StatusUnauthorized, "UNAUTHENTICATED", "会话已失效")
		}
		if err != nil {
			return err
		}
		return JSON(w, http.StatusOK, result)
	})
	a.Handle("POST /api/v1/auth/logout", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if err := a.Auth.Logout(r.Context(), p); err != nil {
			return err
		}
		return JSON(w, http.StatusOK, map[string]any{"revoked": true})
	})
	a.Handle("GET /api/v1/me", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var employeeName *string
		if p.EmployeeID != "" {
			_ = a.DB.QueryRow(r.Context(), "SELECT name FROM employee WHERE tenant_id=$1 AND id=$2", p.TenantID, p.EmployeeID).Scan(&employeeName)
		}
		return JSON(w, http.StatusOK, map[string]any{"user": map[string]any{"id": p.UserID, "tenant_id": p.TenantID, "employee_id": p.EmployeeID, "login_name": p.LoginName, "employee_name": employeeName}, "offline_policy": map[string]any{"read_only_after_expiry": true}})
	})
	a.Handle("POST /api/v1/auth/password/change", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var input struct {
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		revoked, err := a.Auth.ChangePassword(r.Context(), p, input.OldPassword, input.NewPassword)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return Error(http.StatusBadRequest, "INVALID_PASSWORD", "当前密码不正确")
		}
		if errors.Is(err, auth.ErrWeakPassword) {
			return Error(http.StatusBadRequest, "WEAK_PASSWORD", "新密码至少12位且不能与当前密码相同")
		}
		if err != nil {
			return err
		}
		return JSON(w, http.StatusOK, map[string]any{"password_change_required": false, "revoked_other_sessions": revoked})
	})
	a.Handle("GET /api/v1/me/permissions", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return JSON(w, http.StatusOK, map[string]any{"actions": p.Permissions, "policy_version": 1})
	})
	/* 当前登录手机号名下的制衣厂（员工手机号命中，或该厂注册联系电话命中）：门户顶层菜单据此切换制衣厂。 */
	a.Handle("GET /api/v1/auth/my-factories", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		items := []map[string]any{}
		phones, err := a.Auth.PrincipalPhones(r.Context(), p)
		if err != nil {
			return err
		}
		var currentCode string
		_ = a.DB.QueryRow(r.Context(), `SELECT code FROM tenant WHERE id=$1`, p.TenantID).Scan(&currentCode)
		if len(phones) > 0 {
			factories, err := a.Auth.PhoneTenants(r.Context(), phones)
			if err != nil {
				return err
			}
			for _, f := range factories {
				items = append(items, map[string]any{"code": f.Code, "name": f.Name, "short_code": f.ShortCode,
					"login_name": f.LoginName, "user_name": f.UserName, "current": f.Code == currentCode})
			}
		}
		return JSON(w, http.StatusOK, map[string]any{"items": items})
	})
	/* 切换到名下另一家制衣厂门户：手机号须登记在该厂（员工手机号或注册联系电话）。 */
	a.Handle("POST /api/v1/auth/switch-tenant", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var input struct {
			TenantCode string `json:"tenant_code"`
			NodeID     string `json:"node_id"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		tenantCode := strings.TrimSpace(input.TenantCode)
		if tenantCode == "" {
			return Error(http.StatusBadRequest, "INVALID_REQUEST", "缺少制衣厂编码")
		}
		result, err := a.Auth.SwitchTenant(r.Context(), p, tenantCode, input.NodeID)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return Error(http.StatusForbidden, "NO_ACCOUNT", "该手机号在此制衣厂没有绑定的账号")
		}
		if err != nil {
			return err
		}
		var tenantName string
		_ = a.DB.QueryRow(r.Context(), `SELECT name FROM tenant WHERE code=$1`, tenantCode).Scan(&tenantName)
		result["tenant_name"] = tenantName
		return JSON(w, http.StatusOK, result)
	})
	/* 旧数据租户补录注册联系电话：注册信息里没有手机号的厂管理员登录后先补录，再进入工作台。
	   仅登记号码，不发送、不校验短信验证码（身份已由账号密码登录会话证明）。 */
	a.Handle("POST /api/v1/auth/complete-phone", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var input struct {
			Phone string `json:"phone"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		phone := strings.TrimSpace(input.Phone)
		if !contactPhonePattern.MatchString(phone) {
			return Error(http.StatusBadRequest, "INVALID_PHONE", "联系电话须为 11 位手机号")
		}
		tx, err := a.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var admin bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_role ur JOIN role ro ON ro.tenant_id=ur.tenant_id AND ro.id=ur.role_id
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND ro.code='tenant_admin')`, p.TenantID, p.UserID).Scan(&admin); err != nil {
			return err
		}
		if !admin {
			return Error(http.StatusForbidden, "NOT_TENANT_ADMIN", "只有厂管理员可以补录本厂联系电话")
		}
		var before string
		if err := tx.QueryRow(r.Context(), `SELECT COALESCE(contact_phone,'') FROM tenant WHERE id=$1`, p.TenantID).Scan(&before); err != nil {
			return err
		}
		/* tenant 表只有 updated_at（无 updated_by/updated_node），改动留痕由 audit_log 负责 */
		if _, err := tx.Exec(r.Context(), `UPDATE tenant SET contact_phone=$1,version=version+1,updated_at=$2
			WHERE id=$3`, phone, platform.Now(), p.TenantID); err != nil {
			return err
		}
		if err := Audit(r.Context(), tx, p, a.Config.NodeID, "complete_tenant_contact_phone", "tenant", p.TenantID,
			map[string]any{"contact_phone": before}, map[string]any{"contact_phone": phone}, "旧数据租户补录注册联系电话"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return JSON(w, http.StatusOK, map[string]any{"saved": true, "contact_phone": phone, "need_contact_phone": false})
	})
}
