package platformadmin

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"

	"github.com/jackc/pgx/v5"
)

var assistantLoginPattern = regexp.MustCompile(`^[a-z0-9_]{3,32}$`)

/**
 * 平台辅助人员管理（仅平台管理员可用，辅助人员访问 assistants.manage 被网关 403）：
 * 创建辅助人员账号、分配权限点、启停、重置密码。
 * 权限点见 AssistantPermissions；辅助人员不能创建/管理平台管理员与辅助人员。
 */

type assistantPermission struct {
	Code string
	Name string
}

var AssistantPermissions = []assistantPermission{
	{"tenants.read", "查看租户列表/详情/统计"},
	{"tenants.write", "注册租户、编辑租户、启停租户、重置租户管理员密码"},
	{"registrations.read", "查看注册申请"},
	{"registrations.write", "审批启用/驳回注册申请"},
	{"settings.read", "查看平台参数与体验页内容"},
	{"settings.write", "编辑平台参数与体验页内容"},
	{"audit.read", "查看平台审计日志"},
	{"modules.read", "查看功能模块清单"},
	{"modules.write", "维护功能模块清单"},
	{"role_templates.read", "查看角色模板与权限点"},
	{"role_templates.write", "维护角色模板"},
}

var assistantPermissionSet = func() map[string]bool {
	set := map[string]bool{}
	for _, item := range AssistantPermissions {
		set[item.Code] = true
	}
	return set
}()

func registerAssistants(api *httpapi.API) {
	api.PlatformHandle("GET /api/v1/platform/assistants", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT a.id,a.login_name,COALESCE(a.name,''),a.status,a.created_at,a.last_login_at,
			COALESCE((SELECT json_agg(permission_code) FROM platform_admin_permission ap WHERE ap.admin_id=a.id),'[]'::json)
			FROM platform_admin a WHERE a.role='assistant' ORDER BY a.created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, login, name, status string
			var createdAt int64
			var lastLogin *int64
			var permissions json.RawMessage
			if err := rows.Scan(&id, &login, &name, &status, &createdAt, &lastLogin, &permissions); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "login_name": login, "name": name, "status": status,
				"created_at": createdAt, "last_login_at": lastLogin, "permission_codes": permissions})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "catalog": AssistantPermissions})
	})

	api.PlatformHandle("POST /api/v1/platform/assistants", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			LoginName       string   `json:"login_name"`
			Name            string   `json:"name"`
			Password        string   `json:"password"`
			PermissionCodes []string `json:"permission_codes"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		login := strings.ToLower(strings.TrimSpace(in.LoginName))
		if !assistantLoginPattern.MatchString(login) {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "登录名须为 3-32 位小写字母、数字或 _")
		}
		if len(in.Password) < 12 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "初始密码至少 12 位")
		}
		codes, err := normalizeAssistantPermissions(in.PermissionCodes)
		if err != nil {
			return err
		}
		verifier, err := auth.HashPlatformPassword(in.Password)
		if err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var taken bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM platform_admin WHERE login_name=$1)", login).Scan(&taken); err != nil {
			return err
		}
		if taken {
			return httpapi.Error(http.StatusConflict, "LOGIN_EXISTS", "登录名已存在")
		}
		id := platform.ID()
		if _, err := tx.Exec(r.Context(), `INSERT INTO platform_admin(id,login_name,password_verifier,name,role,status,created_at)
			VALUES($1,$2,$3,$4,'assistant','active',$5)`, id, login, verifier, strings.TrimSpace(in.Name), platform.Now()); err != nil {
			return err
		}
		if err := replaceAssistantPermissions(r.Context(), tx, id, codes, p.AdminID, "assistant_create"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "login_name": login, "permission_codes": codes})
	})

	api.PlatformHandle("PATCH /api/v1/platform/assistants/{id}", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			Status string `json:"status"`
			Name   string `json:"name"`
			Reason string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Status != "active" && in.Status != "disabled" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "status 只能为 active 或 disabled")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		tag, err := tx.Exec(r.Context(), `UPDATE platform_admin SET name=COALESCE(NULLIF($1,''),name),status=$2 WHERE id=$3 AND role='assistant'`,
			strings.TrimSpace(in.Name), in.Status, r.PathValue("id"))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "辅助人员不存在")
		}
		if in.Status == "disabled" {
			if _, err := tx.Exec(r.Context(), "UPDATE platform_session SET revoked_at=$1 WHERE admin_id=$2 AND revoked_at IS NULL", platform.Now(), r.PathValue("id")); err != nil {
				return err
			}
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "assistant_"+map[string]string{"active": "enable", "disabled": "disable"}[in.Status],
			"platform_admin", r.PathValue("id"), nil, map[string]any{"status": in.Status, "name": in.Name}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "status": in.Status})
	})

	api.PlatformHandle("PUT /api/v1/platform/assistants/{id}/permissions", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			PermissionCodes []string `json:"permission_codes"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		codes, err := normalizeAssistantPermissions(in.PermissionCodes)
		if err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var exists bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM platform_admin WHERE id=$1 AND role='assistant')", r.PathValue("id")).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "辅助人员不存在")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM platform_admin_permission WHERE admin_id=$1", r.PathValue("id")); err != nil {
			return err
		}
		if err := replaceAssistantPermissions(r.Context(), tx, r.PathValue("id"), codes, p.AdminID, "assistant_permission_update"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "permission_codes": codes})
	})

	api.PlatformHandle("POST /api/v1/platform/assistants/{id}/reset-password", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.Reason) == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请填写重置原因")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var login string
		err = tx.QueryRow(r.Context(), "SELECT login_name FROM platform_admin WHERE id=$1 AND role='assistant'", r.PathValue("id")).Scan(&login)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "辅助人员不存在")
		}
		if err != nil {
			return err
		}
		temporary, err := auth.GenerateTemporaryPassword(12)
		if err != nil {
			return err
		}
		verifier, err := auth.HashPlatformPassword(temporary)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE platform_admin SET password_verifier=$1 WHERE id=$2 AND role='assistant'", verifier, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE platform_session SET revoked_at=$1 WHERE admin_id=$2 AND revoked_at IS NULL", platform.Now(), r.PathValue("id")); err != nil {
			return err
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "assistant_password_reset", "platform_admin", r.PathValue("id"), nil, nil, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"temporary_password": temporary, "display_once": true})
	})
}

func normalizeAssistantPermissions(codes []string) ([]string, error) {
	seen := map[string]bool{}
	out := []string{}
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" || seen[code] {
			continue
		}
		if !assistantPermissionSet[code] {
			return nil, httpapi.Error(http.StatusBadRequest, "INVALID_PERMISSION", "未知平台辅助权限点："+code)
		}
		seen[code] = true
		out = append(out, code)
	}
	return out, nil
}

func replaceAssistantPermissions(ctx context.Context, tx pgx.Tx, adminID string, codes []string, operatorID, action string) error {
	for _, code := range codes {
		if _, err := tx.Exec(ctx, "INSERT INTO platform_admin_permission(admin_id,permission_code) VALUES($1,$2) ON CONFLICT DO NOTHING", adminID, code); err != nil {
			return err
		}
	}
	var after []string
	rows, err := tx.Query(ctx, "SELECT permission_code FROM platform_admin_permission WHERE admin_id=$1 ORDER BY permission_code", adminID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return err
		}
		after = append(after, code)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return platformAudit(ctx, tx, operatorID, action, "platform_admin", adminID, nil, map[string]any{"permission_codes": after}, "")
}
