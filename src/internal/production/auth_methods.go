package production

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

/* 登录验证方式配置：sms（短信验证码）为恒开基础验证，不接受关闭；
 * password（密码验证）与 wechat（微信设备快捷登录）可由本人、或厂管理员按单个/勾选/全部员工开关。 */
type authMethodPatch struct {
	Password *bool `json:"password"`
	Wechat   *bool `json:"wechat"`
}

func (p authMethodPatch) empty() bool { return p.Password == nil && p.Wechat == nil }

func mergeAuthMethods(current auth.AuthMethods, patch authMethodPatch) auth.AuthMethods {
	if patch.Password != nil {
		current.Password = *patch.Password
	}
	if patch.Wechat != nil {
		current.Wechat = *patch.Wechat
	}
	return auth.NormalizeAuthMethods(current)
}

func registerAuthMethods(api *httpapi.API) {
	api.Handle("GET /api/v1/me/auth-methods", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"methods":  auth.LoadAuthMethods(r.Context(), api.DB, p.TenantID, p.UserID),
			"editable": []string{"password", "wechat"},
			"fixed":    []string{"sms"},
		})
	})

	api.Handle("PUT /api/v1/me/auth-methods", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var patch authMethodPatch
		if err := httpapi.Decode(r, &patch); err != nil {
			return err
		}
		if patch.empty() {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请选择要开通或关闭的验证方式")
		}
		updated, err := saveAuthMethods(r.Context(), api, p, []string{p.UserID}, patch, "本人调整登录验证方式")
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"updated": updated,
			"methods": auth.LoadAuthMethods(r.Context(), api.DB, p.TenantID, p.UserID)})
	})

	api.Handle("GET /api/v1/users/auth-methods", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT u.id,COALESCE(e.employee_no,''),COALESCE(e.name,''),COALESCE(u.auth_methods,'{}'::jsonb)
			FROM user_account u LEFT JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id
			WHERE u.tenant_id=$1 AND u.status='active'
			ORDER BY e.employee_no,u.login_name`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var userID, employeeNo, name string
			var raw []byte
			if err := rows.Scan(&userID, &employeeNo, &name, &raw); err != nil {
				return err
			}
			methods := auth.DefaultAuthMethods()
			_ = json.Unmarshal(raw, &methods)
			items = append(items, map[string]any{"user_id": userID, "employee_no": employeeNo,
				"name": name, "methods": auth.NormalizeAuthMethods(methods)})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("PUT /api/v1/users/auth-methods", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Scope    string   `json:"scope"`
			UserIDs  []string `json:"user_ids"`
			Password *bool    `json:"password"`
			Wechat   *bool    `json:"wechat"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		patch := authMethodPatch{Password: in.Password, Wechat: in.Wechat}
		if patch.empty() {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请选择要开通或关闭的验证方式")
		}
		scope := strings.TrimSpace(in.Scope)
		if scope == "" {
			scope = "selected"
		}
		if scope != "all" && scope != "selected" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "scope 只能为 selected 或 all")
		}
		ids := []string{}
		if scope == "all" {
			rows, err := api.DB.Query(r.Context(), `SELECT id FROM user_account WHERE tenant_id=$1 AND status='active'`, p.TenantID)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				ids = append(ids, id)
			}
			rows.Close()
		} else {
			seen := map[string]bool{}
			for _, id := range in.UserIDs {
				id = strings.TrimSpace(id)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				ids = append(ids, id)
			}
			if len(ids) == 0 {
				return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请先勾选员工")
			}
			rows, err := api.DB.Query(r.Context(), `SELECT id FROM user_account WHERE tenant_id=$1 AND status='active' AND id=ANY($2::text[])`, p.TenantID, ids)
			if err != nil {
				return err
			}
			valid := []string{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				valid = append(valid, id)
			}
			rows.Close()
			ids = valid
		}
		updated, err := saveAuthMethods(r.Context(), api, p, ids, patch, "厂管理员批量调整登录验证方式")
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"updated": updated, "scope": scope})
	})

	api.Handle("PUT /api/v1/users/{id}/auth-methods", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var patch authMethodPatch
		if err := httpapi.Decode(r, &patch); err != nil {
			return err
		}
		if patch.empty() {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请选择要开通或关闭的验证方式")
		}
		userID := strings.TrimSpace(r.PathValue("id"))
		var exists bool
		if err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_account WHERE tenant_id=$1 AND id=$2 AND status='active')`, p.TenantID, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return httpapi.Error(http.StatusNotFound, "ACCOUNT_NOT_FOUND", "员工账号不存在或已停用")
		}
		updated, err := saveAuthMethods(r.Context(), api, p, []string{userID}, patch, "厂管理员调整员工登录验证方式")
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"updated": updated,
			"methods": auth.LoadAuthMethods(r.Context(), api.DB, p.TenantID, userID)})
	})
}

type authMethodUpdate struct {
	userID  string
	methods auth.AuthMethods
}

/* saveAuthMethods 在事务外读取当前配置，双写事务内批量写回。
 * 关闭微信验证时同时解绑设备（wechat_unionid 置空），避免关闭后仍可扫码快捷登录。 */
func saveAuthMethods(ctx context.Context, api *httpapi.API, p auth.Principal, userIDs []string, patch authMethodPatch, reason string) (int, error) {
	updates := []authMethodUpdate{}
	seen := map[string]bool{}
	for _, userID := range userIDs {
		if seen[userID] {
			continue
		}
		seen[userID] = true
		current := auth.LoadAuthMethods(ctx, api.DB, p.TenantID, userID)
		updates = append(updates, authMethodUpdate{userID: userID, methods: mergeAuthMethods(current, patch)})
	}
	if len(updates) == 0 {
		return 0, nil
	}
	if api.Writer == nil {
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return 0, err
		}
		defer tx.Rollback(ctx)
		if err := execAuthMethodUpdates(ctx, tx, p.TenantID, updates); err != nil {
			return 0, err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "update_auth_methods", "tenant", p.TenantID, nil,
			map[string]any{"updated": len(updates), "patch": patch}, reason); err != nil {
			return 0, err
		}
		return len(updates), tx.Commit(ctx)
	}
	tx, err := api.Writer.Begin(ctx, p.TenantID)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	tx.MarkAggregate("tenant", p.TenantID)
	if err := func() error {
		if tx.Plain() != nil {
			if err := execAuthMethodUpdates(ctx, tx.Plain(), p.TenantID, updates); err != nil {
				return err
			}
		}
		if tx.Secure() != nil {
			if err := execAuthMethodUpdates(ctx, tx.Secure(), p.TenantID, updates); err != nil {
				return err
			}
		}
		auditTx := tx.Secure()
		if auditTx == nil {
			auditTx = tx.Plain()
		}
		if auditTx == nil {
			return nil
		}
		return httpapi.Audit(ctx, auditTx, p, api.Config.NodeID, "update_auth_methods", "tenant", p.TenantID, nil,
			map[string]any{"updated": len(updates), "patch": patch}, reason)
	}(); err != nil {
		return 0, err
	}
	return len(updates), tx.Commit(ctx)
}

func execAuthMethodUpdates(ctx context.Context, tx pgx.Tx, tenantID string, updates []authMethodUpdate) error {
	for _, item := range updates {
		raw, err := json.Marshal(item.methods)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE user_account SET auth_methods=$1::jsonb,
			wechat_unionid=CASE WHEN $4::boolean THEN '' ELSE wechat_unionid END
			WHERE tenant_id=$2 AND id=$3`, string(raw), tenantID, item.userID, !item.methods.Wechat); err != nil {
			return err
		}
	}
	return nil
}
