package platformadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

var tenantCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,32}$`)
var tenantShortCodePattern = regexp.MustCompile(`^[A-Za-z0-9]{5}$`)
var settingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,100}$`)

func Register(api *httpapi.API) {
	api.Public("POST /api/v1/platform/auth/login", func(w http.ResponseWriter, r *http.Request) error {
		var input auth.PlatformLoginInput
		if err := httpapi.Decode(r, &input); err != nil {
			return err
		}
		result, err := api.Auth.PlatformLogin(r.Context(), input)
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return httpapi.Error(http.StatusUnauthorized, "PLATFORM_UNAUTHENTICATED", "平台账号或密码错误")
		}
		if err != nil {
			return err
		}
		admin := result["admin"].(map[string]any)
		adminID := admin["id"].(string)
		if _, err := api.DB.Exec(r.Context(), `INSERT INTO platform_audit_log(id,request_id,actor_id,action,owner_type,owner_id,created_at) VALUES($1,$2,$3,'platform_login','platform_admin',$3,$4)`, platform.ID(), httpapi.RequestID(r.Context()), adminID, platform.Now()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, result)
	})
	api.PlatformHandle("POST /api/v1/platform/auth/logout", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		if err := api.Auth.PlatformLogout(r.Context(), p); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"revoked": true})
	})
	api.PlatformHandle("GET /api/v1/platform/auth/me", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var name string
		if err := api.DB.QueryRow(r.Context(), "SELECT COALESCE(name,'') FROM platform_admin WHERE id=$1", p.AdminID).Scan(&name); err != nil {
			return err
		}
		codes := []string{}
		for code := range p.Permissions {
			codes = append(codes, code)
		}
		return httpapi.JSON(w, 200, map[string]any{"id": p.AdminID, "login_name": p.LoginName, "role": p.Role, "name": name, "permissions": codes})
	})
	api.PlatformHandle("GET /api/v1/platform/statistics", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		metrics := map[string]any{}
		queries := map[string]string{
			"tenant_count":          "SELECT COUNT(*) FROM tenant",
			"active_tenant_count":   "SELECT COUNT(*) FROM tenant WHERE status='active'",
			"disabled_tenant_count": "SELECT COUNT(*) FROM tenant WHERE status='disabled'",
			"user_count":            "SELECT COUNT(*) FROM user_account",
			"work_order_count":      "SELECT COUNT(*) FROM work_order",
			"report_count":          "SELECT COUNT(*) FROM work_report",
		}
		for key, query := range queries {
			var count int64
			if err := api.DB.QueryRow(r.Context(), query).Scan(&count); err != nil {
				return err
			}
			metrics[key] = count
		}
		return httpapi.JSON(w, 200, map[string]any{"metrics": metrics, "data_version": platform.Now()})
	})
	api.PlatformHandle("GET /api/v1/platform/tenants", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		return listTenants(api, w, r, "")
	})
	api.PlatformHandle("GET /api/v1/platform/tenants/{id}", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		return listTenants(api, w, r, r.PathValue("id"))
	})
	api.PlatformHandle("GET /api/v1/platform/tenants/preview", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		name := strings.TrimSpace(r.URL.Query().Get("name"))
		code := strings.TrimSpace(r.URL.Query().Get("code"))
		if name == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请先填写租户名称")
		}
		if code == "" {
			code = platform.TenantCodeBase(name, "")
		}
		usedCode := map[string]bool{}
		rows, err := api.DB.Query(r.Context(), "SELECT code FROM tenant")
		if err != nil {
			return err
		}
		for rows.Next() {
			var existing string
			if err := rows.Scan(&existing); err != nil {
				rows.Close()
				return err
			}
			usedCode[existing] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := 0; usedCode[code] && i < 10; i++ {
			code = platform.TenantCodeBase(name, "") + fmt.Sprint(i+1)
		}
		usedShort := map[string]bool{}
		rowsShort, err := api.DB.Query(r.Context(), "SELECT short_code FROM tenant")
		if err != nil {
			return err
		}
		for rowsShort.Next() {
			var existing string
			if err := rowsShort.Scan(&existing); err != nil {
				rowsShort.Close()
				return err
			}
			usedShort[existing] = true
		}
		rowsShort.Close()
		if err := rowsShort.Err(); err != nil {
			return err
		}
		usedLogin := map[string]bool{}
		rowsLogin, err := api.DB.Query(r.Context(), "SELECT lower(login_name) FROM user_account")
		if err != nil {
			return err
		}
		for rowsLogin.Next() {
			var existing string
			if err := rowsLogin.Scan(&existing); err != nil {
				rowsLogin.Close()
				return err
			}
			usedLogin[existing] = true
		}
		rowsLogin.Close()
		if err := rowsLogin.Err(); err != nil {
			return err
		}
		chosen := ""
		for _, candidate := range platform.ShortCodeCandidates(platform.TenantShortCodeBase(name, code)) {
			if usedShort[candidate] || usedLogin[strings.ToLower(candidate+"admin")] {
				continue
			}
			chosen = candidate
			break
		}
		if chosen == "" {
			return httpapi.Error(http.StatusConflict, "SHORT_CODE_EXHAUSTED", "无法生成可用的租户标识")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"code": code, "short_code": chosen, "admin_login_name": chosen + "admin", "admin_password": "123456123456"})
	})
	api.PlatformHandle("PATCH /api/v1/platform/tenants/{id}", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			Name      string `json:"name"`
			ShortCode string `json:"short_code"`
			Timezone  string `json:"timezone"`
			Version   int64  `json:"version"`
			Reason    string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		name := strings.TrimSpace(in.Name)
		shortCode := strings.ToUpper(strings.TrimSpace(in.ShortCode))
		timezone := strings.TrimSpace(in.Timezone)
		if name == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "租户名称必填")
		}
		if !tenantShortCodePattern.MatchString(shortCode) {
			return httpapi.Error(http.StatusBadRequest, "INVALID_SHORT_CODE", "租户标识须为 5 位英文字母或数字")
		}
		if timezone == "" {
			timezone = "Asia/Shanghai"
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var before map[string]any
		if err := tx.QueryRow(r.Context(), `SELECT json_build_object('name',name,'short_code',short_code,'timezone',timezone) FROM tenant WHERE id=$1`, r.PathValue("id")).Scan(&before); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "租户不存在")
			}
			return err
		}
		var taken bool
		if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenant WHERE short_code=$1 AND id<>$2)`, shortCode, r.PathValue("id")).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return httpapi.Error(http.StatusConflict, "DUPLICATE_SHORT_CODE", "租户标识已被占用")
		}
		result, err := tx.Exec(r.Context(), `UPDATE tenant SET name=$1,short_code=$2,timezone=$3,version=version+1,updated_at=$4 WHERE id=$5 AND version=$6`, name, shortCode, timezone, platform.Now(), r.PathValue("id"), in.Version)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "租户信息已变更，请刷新后重试")
		}
		after := map[string]any{"name": name, "short_code": shortCode, "timezone": timezone}
		if err := platformAudit(r.Context(), tx, p.AdminID, "update_tenant", "tenant", r.PathValue("id"), before, after, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "name": name, "short_code": shortCode, "timezone": timezone, "version": in.Version + 1})
	})
	api.PlatformHandle("GET /api/v1/platform/tenants/{id}/admins", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT DISTINCT u.id,u.login_name,e.name,u.status,u.password_change_required FROM user_account u JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id JOIN user_role ur ON ur.tenant_id=u.tenant_id AND ur.user_id=u.id JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id AND r.code='tenant_admin' WHERE u.tenant_id=$1 ORDER BY u.login_name`, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var userID, login, name, status string
			var required bool
			if err := rows.Scan(&userID, &login, &name, &status, &required); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": userID, "login_name": login, "name": name, "status": status, "password_change_required": required})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.PlatformHandle("POST /api/v1/platform/tenants/{id}/admins/{userId}/reset-password", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Reason == "" {
			return httpapi.Error(400, "REASON_REQUIRED", "重置密码必须填写原因")
		}
		temporaryPassword, err := auth.GenerateTemporaryPassword(12)
		if err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if err := api.Auth.ResetTenantAdminPassword(r.Context(), tx, r.PathValue("id"), r.PathValue("userId"), temporaryPassword, p.AdminID); err == pgx.ErrNoRows {
			return httpapi.Error(404, "NOT_FOUND", "租户管理员不存在")
		} else if err != nil {
			return err
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "tenant_admin_password_reset", "user_account", r.PathValue("userId"), map[string]any{"password_change_required": false}, map[string]any{"password_change_required": true}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"user_id": r.PathValue("userId"), "temporary_password": temporaryPassword, "password_change_required": true, "display_once": true})
	})
	api.PlatformHandle("POST /api/v1/platform/tenants", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			Code             string   `json:"code"`
			Name             string   `json:"name"`
			ShortCode        string   `json:"short_code"`
			Timezone         string   `json:"timezone"`
			AdminLoginName   string   `json:"admin_login_name"`
			AdminName        string   `json:"admin_name"`
			AdminPassword    string   `json:"admin_password"`
			RoleTemplateIDs  []string `json:"role_template_ids"`
			RoleTemplateMode string   `json:"role_template_mode"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if !tenantCodePattern.MatchString(in.Code) || in.Name == "" || in.AdminPassword == "" || len(in.AdminPassword) < 12 {
			return httpapi.Error(400, "VALIDATION_ERROR", "租户编码、名称必填，初始管理员密码至少12位")
		}
		in.ShortCode = strings.ToUpper(strings.TrimSpace(in.ShortCode))
		if in.ShortCode != "" && !tenantShortCodePattern.MatchString(in.ShortCode) {
			return httpapi.Error(400, "INVALID_SHORT_CODE", "租户标识须为 5 位英文字母或数字")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		ids, err := api.Auth.SeedTenant(r.Context(), tx, auth.TenantSeed{Code: in.Code, Name: in.Name, ShortCode: in.ShortCode, Timezone: in.Timezone, AdminName: in.AdminName, AdminPassword: in.AdminPassword})
		if err != nil {
			return httpapi.Error(409, "TENANT_CODE_EXISTS", "租户编码已存在或初始化数据无效")
		}
		templateMode := in.RoleTemplateMode
		if templateMode == "" {
			if in.RoleTemplateIDs != nil {
				templateMode = "custom"
			} else {
				templateMode = "all"
			}
		}
		if templateMode != "all" && templateMode != "none" && templateMode != "custom" {
			return httpapi.Error(400, "VALIDATION_ERROR", "role_template_mode 只能为 all、none 或 custom")
		}
		if templateMode == "custom" && len(in.RoleTemplateIDs) == 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "部分复制时必须选择至少一个角色模板")
		}
		templates := map[string]any{"applied_templates": 0, "created_roles": []string{}, "updated_roles": []string{}, "skipped": templateMode == "none"}
		if templateMode != "none" {
			templateIDs := in.RoleTemplateIDs
			if templateMode == "all" {
				templateIDs = nil
			}
			templates, err = applyRoleTemplates(r.Context(), tx, ids["tenant_id"], templateIDs, p.AdminID, api.Config.NodeID)
			if err != nil {
				return err
			}
		}
		templates["mode"] = templateMode
		response := map[string]any{"id": ids["tenant_id"], "code": in.Code, "name": in.Name, "status": "active", "admin_user_id": ids["tenant_admin_user_id"], "role_templates": templates}
		if err := platformAudit(r.Context(), tx, p.AdminID, "tenant_register", "tenant", ids["tenant_id"], nil, map[string]any{"code": in.Code, "name": in.Name, "timezone": in.Timezone}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, response)
	})
	for action, status := range map[string]string{"disable": "disabled", "enable": "active"} {
		action, status := action, status
		api.PlatformHandle("POST /api/v1/platform/tenants/{id}/"+action, func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
			var in struct {
				Version int64  `json:"version"`
				Reason  string `json:"reason"`
			}
			if err := httpapi.Decode(r, &in); err != nil {
				return err
			}
			if in.Reason == "" {
				return httpapi.Error(400, "REASON_REQUIRED", "租户启停必须填写原因")
			}
			tx, err := api.DB.Begin(r.Context())
			if err != nil {
				return err
			}
			defer tx.Rollback(r.Context())
			var previous string
			if err := tx.QueryRow(r.Context(), "SELECT status FROM tenant WHERE id=$1 FOR UPDATE", r.PathValue("id")).Scan(&previous); err != nil {
				return httpapi.Error(404, "NOT_FOUND", "租户不存在")
			}
			result, err := tx.Exec(r.Context(), "UPDATE tenant SET status=$1,version=version+1 WHERE id=$2 AND version=$3", status, r.PathValue("id"), in.Version)
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return httpapi.Error(409, "VERSION_CONFLICT", "租户状态或版本已变化")
			}
			if status == "disabled" {
				if _, err := tx.Exec(r.Context(), "UPDATE auth_session SET revoked_at=$1,reason='tenant_disabled' WHERE tenant_id=$2 AND revoked_at IS NULL", platform.Now(), r.PathValue("id")); err != nil {
					return err
				}
			}
			if err := platformAudit(r.Context(), tx, p.AdminID, "tenant_"+action, "tenant", r.PathValue("id"), map[string]any{"status": previous}, map[string]any{"status": status}, in.Reason); err != nil {
				return err
			}
			if err := tx.Commit(r.Context()); err != nil {
				return err
			}
			return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": status, "version": in.Version + 1})
		})
	}
	api.PlatformHandle("GET /api/v1/platform/settings", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT DISTINCT ON(setting_key) setting_key,setting_value,version,updated_by,change_reason,created_at FROM platform_setting ORDER BY setting_key,version DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var key, updatedBy, reason string
			var value json.RawMessage
			var version, created int64
			if err := rows.Scan(&key, &value, &version, &updatedBy, &reason, &created); err != nil {
				return err
			}
			items = append(items, map[string]any{"setting_key": key, "setting_value": value, "version": version, "updated_by": updatedBy, "change_reason": reason, "created_at": created})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.PlatformHandle("PUT /api/v1/platform/settings/{key}", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			Value   any    `json:"value"`
			Version int64  `json:"version"`
			Reason  string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if !settingKeyPattern.MatchString(r.PathValue("key")) || in.Reason == "" || in.Version < 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "参数键、版本和变更原因不合法")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if _, err := tx.Exec(r.Context(), "SELECT pg_advisory_xact_lock(hashtext($1))", "platform_setting:"+r.PathValue("key")); err != nil {
			return err
		}
		var current int64
		if err := tx.QueryRow(r.Context(), "SELECT COALESCE(MAX(version),0) FROM platform_setting WHERE setting_key=$1", r.PathValue("key")).Scan(&current); err != nil {
			return err
		}
		if current != in.Version {
			return httpapi.Error(409, "VERSION_CONFLICT", "平台参数版本已变化")
		}
		body, _ := json.Marshal(in.Value)
		id := platform.ID()
		if _, err := tx.Exec(r.Context(), `INSERT INTO platform_setting(id,setting_key,setting_value,version,updated_by,change_reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, r.PathValue("key"), body, current+1, p.AdminID, in.Reason, platform.Now()); err != nil {
			return err
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "setting_update", "platform_setting", r.PathValue("key"), map[string]any{"version": current}, map[string]any{"version": current + 1, "value": in.Value}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": id, "setting_key": r.PathValue("key"), "version": current + 1, "value": in.Value})
	})
	api.PlatformHandle("GET /api/v1/platform/audit-logs", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,request_id,actor_id,action,owner_type,owner_id,reason,created_at FROM platform_audit_log ORDER BY created_at DESC LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, requestID, action, ownerType, ownerID string
			var actor, reason *string
			var created int64
			if err := rows.Scan(&id, &requestID, &actor, &action, &ownerType, &ownerID, &reason, &created); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "request_id": requestID, "actor_id": actor, "action": action, "owner_type": ownerType, "owner_id": ownerID, "reason": reason, "created_at": created})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	registerModules(api)
	registerRoleTemplates(api)
	registerAssistants(api)
}

func listTenants(api *httpapi.API, w http.ResponseWriter, r *http.Request, id string) error {
	where, args := "", []any{}
	if id != "" {
		where = "WHERE t.id=$1"
		args = append(args, id)
	} else if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		/* 关键字查询：厂编码/名称/标识/联系人/联系电话 */
		where = `WHERE (t.code ILIKE $1 OR t.name ILIKE $1 OR COALESCE(t.short_code,'') ILIKE $1
			OR COALESCE(t.contact_name,'') ILIKE $1 OR COALESCE(t.contact_phone,'') ILIKE $1
			OR COALESCE(t.contact_wechat,'') ILIKE $1 OR COALESCE(t.contact_qq,'') ILIKE $1
			OR COALESCE(t.contact_alipay,'') ILIKE $1)`
		args = append(args, "%"+q+"%")
	}
	rows, err := api.DB.Query(r.Context(), `SELECT t.id,t.code,t.name,t.short_code,t.timezone,t.status,t.registration_status,t.customer_type,
		COALESCE(t.contact_name,''),COALESCE(t.contact_phone,''),COALESCE(t.contact_wechat,''),COALESCE(t.contact_qq,''),
		COALESCE(t.contact_alipay,''),COALESCE(t.address,''),COALESCE(t.region,''),COALESCE(t.scale,''),COALESCE(t.remark,''),
		COALESCE(t.admin_login_name,''),t.registered_at,t.approved_at,t.version,t.created_at,
		(SELECT COUNT(*) FROM user_account u WHERE u.tenant_id=t.id),
		(SELECT COUNT(*) FROM work_order w WHERE w.tenant_id=t.id),
		(SELECT COUNT(*) FROM work_report wr WHERE wr.tenant_id=t.id)
		FROM tenant t `+where+" ORDER BY t.created_at DESC", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var tenantID, code, name, shortCode, timezone, status, regStatus, customerType string
		var contactName, contactPhone, contactWechat, contactQQ, contactAlipay, address, region, scale, remark, adminLogin string
		var registeredAt, approvedAt *int64
		var version, created, users, orders, reports int64
		if err := rows.Scan(&tenantID, &code, &name, &shortCode, &timezone, &status, &regStatus, &customerType,
			&contactName, &contactPhone, &contactWechat, &contactQQ, &contactAlipay, &address, &region, &scale, &remark,
			&adminLogin, &registeredAt, &approvedAt, &version, &created, &users, &orders, &reports); err != nil {
			return err
		}
		items = append(items, map[string]any{"id": tenantID, "code": code, "name": name, "short_code": shortCode, "timezone": timezone, "status": status, "registration_status": regStatus, "customer_type": customerType,
			"contact_name": contactName, "contact_phone": contactPhone, "contact_wechat": contactWechat, "contact_qq": contactQQ, "contact_alipay": contactAlipay,
			"address": address, "region": region, "scale": scale, "remark": remark, "admin_login_name": adminLogin,
			"registered_at": registeredAt, "approved_at": approvedAt, "version": version, "created_at": created,
			"user_count": users, "work_order_count": orders, "report_count": reports})
	}
	if id != "" {
		if len(items) == 0 {
			return httpapi.Error(404, "NOT_FOUND", "租户不存在")
		}
		return httpapi.JSON(w, 200, items[0])
	}
	return httpapi.JSON(w, 200, map[string]any{"items": items})
}

func platformAudit(ctx context.Context, tx pgx.Tx, adminID, action, ownerType, ownerID string, before, after any, reason string) error {
	var beforeBody, afterBody []byte
	if before != nil {
		beforeBody, _ = json.Marshal(before)
	}
	if after != nil {
		afterBody, _ = json.Marshal(after)
	}
	_, err := tx.Exec(ctx, `INSERT INTO platform_audit_log(id,request_id,actor_id,action,owner_type,owner_id,before_values,after_values,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, platform.ID(), httpapi.RequestID(ctx), adminID, action, ownerType, ownerID, beforeBody, afterBody, reason, platform.Now())
	return err
}
