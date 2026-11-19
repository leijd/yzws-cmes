package production

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"clothp/internal/seccrypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

/* firstHandle 取双写事务的第一个可用库句柄（secure 优先）做只读校验。 */
func firstHandle(tx *dualwrite.Tx) pgx.Tx {
	if tx.Secure() != nil {
		return tx.Secure()
	}
	return tx.Plain()
}

/* withIdentityWrites 身份配置类写入统一入口：同一份写在两个库各执行一次。
 * 禁止直接用 api.DB.Begin/Exec 写业务表——api.DB 是读库，单写会造成 secure/plain 漂移。
 * writes 内不得再生成随机 ID（两库会不一致）；需要新 ID 时在闭包外预生成。
 * audit 仅在 secure（无 secure 则 plain）执行一次，避免审计 ID 两库分叉。 */
func withIdentityWrites(api *httpapi.API, ctx context.Context, tenantID, aggregateType, aggregateID string,
	writes func(tx pgx.Tx) error, audit func(tx pgx.Tx) error) error {
	if api.Writer == nil {
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if err := writes(tx); err != nil {
			return err
		}
		if audit != nil {
			if err := audit(tx); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	tx, err := api.Writer.Begin(ctx, tenantID)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tx.MarkAggregate(aggregateType, aggregateID)
	if err := func() error {
		if t := tx.Plain(); t != nil {
			if err := writes(t); err != nil {
				return err
			}
		}
		if t := tx.Secure(); t != nil {
			if err := writes(t); err != nil {
				return err
			}
		}
		if audit != nil {
			if t := tx.Secure(); t != nil {
				return audit(t)
			}
			if t := tx.Plain(); t != nil {
				return audit(t)
			}
		}
		return nil
	}(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

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
		rows, err := api.DB.Query(r.Context(), `SELECT r.id,r.code,r.name,COALESCE(r.description,''),r.is_system,r.version,r.source,r.platform_template_id,COALESCE(r.default_data_scope,''),COUNT(DISTINCT ur.user_id)
			FROM role r LEFT JOIN user_role ur ON ur.tenant_id=r.tenant_id AND ur.role_id=r.id
			WHERE r.tenant_id=$1
			GROUP BY r.id,r.code,r.name,r.description,r.is_system,r.version,r.source,r.platform_template_id,r.default_data_scope
			ORDER BY r.source DESC,r.code`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, description, source, dataScope string
			var templateID *string
			var system bool
			var version, users int64
			if err := rows.Scan(&id, &code, &name, &description, &system, &version, &source, &templateID, &dataScope, &users); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "description": description, "is_system": system, "version": version, "source": source, "platform_template_id": templateID, "default_data_scope": dataScope, "user_count": users})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	/* 角色默认数据范围（设计 4.8）：factory/workshop/line/team/self/warehouse/custom，仅自定义角色可改。 */
	api.Handle("PATCH /api/v1/roles/{id}", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name             string `json:"name"`
			Description      string `json:"description"`
			DefaultDataScope string `json:"default_data_scope"`
			Version          int64  `json:"version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		var system bool
		var currentVersion int64
		var currentScope string
		if err := api.DB.QueryRow(r.Context(), `SELECT is_system,version,COALESCE(default_data_scope,'') FROM role WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id")).Scan(&system, &currentVersion, &currentScope); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "角色不存在")
		}
		scope := strings.TrimSpace(in.DefaultDataScope)
		if scope == "" {
			/* 未传数据范围时保持原值：原先把空值默认成 self，会把“厂长”等角色静默降级为仅本人可见 */
			scope = currentScope
		}
		if scope != "" {
			switch scope {
			case "factory", "workshop", "line", "team", "self", "warehouse", "custom":
			default:
				return httpapi.Error(400, "VALIDATION_ERROR", "数据范围必须是 factory/workshop/line/team/self/warehouse/custom")
			}
		}
		if system {
			return httpapi.Error(409, "SYSTEM_ROLE_IMMUTABLE", "内置管理员角色不可修改，请创建自定义角色")
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "角色名称必填")
		}
		var duplicated bool
		if err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM role WHERE tenant_id=$1 AND id<>$2 AND lower(btrim(name))=lower($3))`, p.TenantID, r.PathValue("id"), name).Scan(&duplicated); err != nil {
			return err
		}
		if duplicated {
			return httpapi.Error(409, "DUPLICATE_ROLE_NAME", "角色名称已存在，请换一个名称")
		}
		now := platform.Now()
		err := withIdentityWrites(api, r.Context(), p.TenantID, "role", r.PathValue("id"), func(tx pgx.Tx) error {
			result, err := tx.Exec(r.Context(), `UPDATE role SET name=$3,description=$4,default_data_scope=$5,
				version=version+1,updated_at=$6,updated_by=$7,updated_node=$8
				WHERE tenant_id=$1 AND id=$2 AND version=$9`,
				p.TenantID, r.PathValue("id"), name, in.Description, scope, now, p.UserID, api.Config.NodeID, in.Version)
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return httpapi.Error(409, "VERSION_CONFLICT", "角色信息已变更，请刷新后重试")
			}
			return nil
		}, nil)
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"saved": true, "version": currentVersion + 1})
	})
	/* 管理级别字典（设计 4.17.1）：L1～L4，供人员建档选择。 */
	api.Handle("GET /api/v1/mgmt-levels", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,level_code,level_name,COALESCE(description,''),sort_no,status
			FROM mgmt_level WHERE tenant_id=$1 AND status='active' ORDER BY sort_no`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, description, status string
			var sortNo int64
			if err := rows.Scan(&id, &code, &name, &description, &sortNo, &status); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "level_code": code, "level_name": name, "description": description, "sort_no": sortNo, "status": status})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	/* 用户数据范围补充（设计 4.8 user_scope）：整体替换，effect=deny 优先。 */
	api.Handle("PUT /api/v1/users/{id}/scopes", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Scopes []struct {
				ScopeType  string `json:"scope_type"`
				ScopeValue string `json:"scope_value"`
				Effect     string `json:"effect"`
			} `json:"scopes"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		userID := r.PathValue("id")
		var exists bool
		if err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM user_account WHERE tenant_id=$1 AND id=$2)`, p.TenantID, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return httpapi.Error(404, "NOT_FOUND", "账号不存在")
		}
		type scopeRow struct{ id, scopeType, scopeValue, effect string }
		rows := make([]scopeRow, 0, len(in.Scopes))
		for _, scope := range in.Scopes {
			scopeType := strings.TrimSpace(scope.ScopeType)
			scopeValue := strings.TrimSpace(scope.ScopeValue)
			if scopeValue == "" {
				return httpapi.Error(400, "VALIDATION_ERROR", "数据范围取值必填")
			}
			switch scopeType {
			case "workshop", "line", "team", "warehouse", "self", "field":
			default:
				return httpapi.Error(400, "VALIDATION_ERROR", "数据范围类型必须是 workshop/line/team/warehouse/self/field")
			}
			effect := strings.TrimSpace(scope.Effect)
			if effect == "" {
				effect = "allow"
			}
			if effect != "allow" && effect != "deny" {
				return httpapi.Error(400, "VALIDATION_ERROR", "effect 必须是 allow 或 deny")
			}
			rows = append(rows, scopeRow{id: platform.ID(), scopeType: scopeType, scopeValue: scopeValue, effect: effect})
		}
		err := withIdentityWrites(api, r.Context(), p.TenantID, "user_account", userID, func(tx pgx.Tx) error {
			if _, err := tx.Exec(r.Context(), `DELETE FROM user_scope WHERE tenant_id=$1 AND user_id=$2`, p.TenantID, userID); err != nil {
				return err
			}
			for _, scope := range rows {
				if _, err := tx.Exec(r.Context(), `INSERT INTO user_scope(tenant_id,id,user_id,scope_type,scope_value,effect)
					VALUES($1,$2,$3,$4,$5,$6)`, p.TenantID, scope.id, userID, scope.scopeType, scope.scopeValue, scope.effect); err != nil {
					return err
				}
			}
			return nil
		}, func(tx pgx.Tx) error {
			return httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "configure_user_scope", "user_account", userID, nil, in, "")
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"saved": true, "count": len(in.Scopes)})
	})
	/* 读取用户数据范围补充，供人员管理页回显。 */
	api.Handle("GET /api/v1/users/{id}/scopes", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT scope_type,scope_value,effect FROM user_scope
			WHERE tenant_id=$1 AND user_id=$2 ORDER BY scope_type,scope_value`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var scopeType, scopeValue, effect string
			if err := rows.Scan(&scopeType, &scopeValue, &effect); err != nil {
				return err
			}
			items = append(items, map[string]any{"scope_type": scopeType, "scope_value": scopeValue, "effect": effect})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("POST /api/v1/roles", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "角色名称必填")
		}
		/* 同厂不允许同名（忽略大小写与首尾空格）；编码由服务端生成，不接受前端传入 */
		var duplicated bool
		if err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM role WHERE tenant_id=$1 AND lower(btrim(name))=lower($2))`, p.TenantID, name).Scan(&duplicated); err != nil {
			return err
		}
		if duplicated {
			return httpapi.Error(409, "DUPLICATE_ROLE_NAME", "角色名称已存在，请换一个名称")
		}
		id, now := platform.ID(), platform.Now()
		var code string
		var err error
		/* 并发创建可能取到同一个自动编码：由唯一约束兜底后重新取号；写入走双库同一 ID。 */
		for attempt := 0; attempt < 5; attempt++ {
			if code, err = nextCustomRoleCode(r.Context(), api.DB, p.TenantID); err != nil {
				return err
			}
			err = withIdentityWrites(api, r.Context(), p.TenantID, "role", id, func(tx pgx.Tx) error {
				_, err := tx.Exec(r.Context(), `INSERT INTO role(tenant_id,id,code,name,description,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$6,$7,$8)`, p.TenantID, id, code, name, in.Description, now, p.UserID, api.Config.NodeID)
				return err
			}, nil)
			if err == nil {
				break
			}
			if !isUniqueViolation(err) {
				return err
			}
			if uniqueViolationConstraint(err) == "ux_role_tenant_name" {
				return httpapi.Error(409, "DUPLICATE_ROLE_NAME", "角色名称已存在，请换一个名称")
			}
		}
		if err != nil {
			return httpapi.Error(409, "DUPLICATE_ROLE_CODE", "角色编码生成冲突，请重试")
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "code": code, "version": 1})
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
		var system bool
		if err := api.DB.QueryRow(r.Context(), "SELECT is_system FROM role WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&system); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "角色不存在")
			}
			return err
		}
		if system {
			return httpapi.Error(409, "SYSTEM_ROLE_IMMUTABLE", "内置管理员角色不可修改，请创建自定义角色")
		}
		/* 计划阶段只读读库并预生成 ID：两库写入必须用同一批 ID，
		 * 但 permission_id 按各库实际行解析，兼容历史主键分叉。 */
		type permPlan struct{ code, resource, action, permissionID, grantID string }
		seen := map[string]bool{}
		planned := []permPlan{}
		now := platform.Now()
		for _, code := range in.PermissionCodes {
			if seen[code] {
				continue
			}
			seen[code] = true
			var resource, action string
			if err := api.DB.QueryRow(r.Context(), "SELECT split_part(permission_code,':',1),split_part(permission_code,':',2) FROM platform_module WHERE permission_code=$1 AND status='active' AND is_admin_only=FALSE", code).Scan(&resource, &action); err != nil {
				return httpapi.Error(400, "INVALID_PERMISSION", "权限点不存在、已停用或仅限固定租户管理员使用："+code)
			}
			planned = append(planned, permPlan{code: code, resource: resource, action: action, permissionID: platform.ID(), grantID: platform.ID()})
		}
		err := withIdentityWrites(api, r.Context(), p.TenantID, "role", r.PathValue("id"), func(tx pgx.Tx) error {
			if _, err := tx.Exec(r.Context(), "DELETE FROM role_permission WHERE tenant_id=$1 AND role_id=$2", p.TenantID, r.PathValue("id")); err != nil {
				return err
			}
			for _, item := range planned {
				if _, err := tx.Exec(r.Context(), `INSERT INTO permission(tenant_id,id,code,resource,action) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,code) DO NOTHING`, p.TenantID, item.permissionID, item.code, item.resource, item.action); err != nil {
					return err
				}
				var permissionID string
				if err := tx.QueryRow(r.Context(), "SELECT id FROM permission WHERE tenant_id=$1 AND code=$2", p.TenantID, item.code).Scan(&permissionID); err != nil {
					return err
				}
				if _, err := tx.Exec(r.Context(), "INSERT INTO role_permission(tenant_id,id,role_id,permission_id) VALUES($1,$2,$3,$4)", p.TenantID, item.grantID, r.PathValue("id"), permissionID); err != nil {
					return err
				}
			}
			_, err := tx.Exec(r.Context(), "UPDATE role SET version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5", now, p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"))
			return err
		}, func(tx pgx.Tx) error {
			return httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "configure_permissions", "role", r.PathValue("id"), nil, in, "")
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"role_id": r.PathValue("id"), "permission_codes": in.PermissionCodes})
	})
	api.Handle("DELETE /api/v1/roles/{id}", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var system bool
		var source string
		if err := api.DB.QueryRow(r.Context(), "SELECT is_system,source FROM role WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&system, &source); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "角色不存在")
		}
		if system {
			return httpapi.Error(409, "SYSTEM_ROLE_IMMUTABLE", "固定租户管理员角色不能删除")
		}
		var userCount int
		if err := api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM user_role WHERE tenant_id=$1 AND role_id=$2", p.TenantID, r.PathValue("id")).Scan(&userCount); err != nil {
			return err
		}
		if userCount > 0 {
			return httpapi.Error(409, "ROLE_IN_USE", "角色已分配给用户，不能删除，可先停用或调整用户角色")
		}
		err := withIdentityWrites(api, r.Context(), p.TenantID, "role", r.PathValue("id"), func(tx pgx.Tx) error {
			if _, err := tx.Exec(r.Context(), "DELETE FROM role_permission WHERE tenant_id=$1 AND role_id=$2", p.TenantID, r.PathValue("id")); err != nil {
				return err
			}
			if _, err := tx.Exec(r.Context(), "DELETE FROM user_role WHERE tenant_id=$1 AND role_id=$2", p.TenantID, r.PathValue("id")); err != nil {
				return err
			}
			_, err := tx.Exec(r.Context(), "DELETE FROM role WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id"))
			return err
		}, func(tx pgx.Tx) error {
			return httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete_role", "role", r.PathValue("id"), map[string]any{"source": source}, nil, "无用户使用，删除角色")
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true, "source": source})
	})
	api.Handle("GET /api/v1/users", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT u.id,u.login_name,u.employee_id,e.employee_no,e.name,e.position,u.status,
			(SELECT w.name FROM org_unit w WHERE w.tenant_id=e.tenant_id AND w.id=e.workshop_org_id) AS workshop_name,
			(SELECT l.name FROM org_unit l WHERE l.tenant_id=e.tenant_id AND l.id=e.line_org_id) AS line_name,
			COALESCE(json_agg(r.id) FILTER(WHERE r.id IS NOT NULL),'[]'),
			COALESCE(json_agg(r.code) FILTER(WHERE r.code IS NOT NULL),'[]')
			FROM user_account u JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id
			LEFT JOIN user_role ur ON ur.tenant_id=u.tenant_id AND ur.user_id=u.id
			LEFT JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
			WHERE u.tenant_id=$1
			GROUP BY u.id,u.login_name,u.employee_id,e.tenant_id,e.employee_no,e.name,e.position,e.workshop_org_id,e.line_org_id,u.status
			ORDER BY u.login_name`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, login, employeeID, employeeNo, name, status string
			var position, workshopName, lineName *string
			var roleIDBody, roleCodeBody []byte
			if err := rows.Scan(&id, &login, &employeeID, &employeeNo, &name, &position, &status, &workshopName, &lineName, &roleIDBody, &roleCodeBody); err != nil {
				return err
			}
			roleIDs, roleCodes := []string{}, []string{}
			_ = json.Unmarshal(roleIDBody, &roleIDs)
			_ = json.Unmarshal(roleCodeBody, &roleCodes)
			items = append(items, map[string]any{"id": id, "login_name": login, "employee_id": employeeID,
				"employee_no": employeeNo, "name": name, "position": position, "status": status,
				"workshop_name": workshopName, "line_name": lineName, "role_ids": roleIDs, "roles": roleCodes})
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
		if api.Writer == nil || api.Store == nil {
			return httpapi.Error(500, "DUALWRITE_REQUIRED", "双写存储未配置，无法创建员工账号")
		}
		/* 敏感列加密与盲索引前置到事务外（HSM/密钥读取不占事务持锁时间） */
		employeeID := platform.ID()
		var phone, securePhone *string
		phoneHash := ""
		if in.Phone != nil && strings.TrimSpace(*in.Phone) != "" {
			normalized := seccrypto.NormalizePhone(*in.Phone)
			phone = &normalized
			if api.Crypto != nil && api.Store.Secure != nil {
				h, err := api.Crypto.PhoneHash(r.Context(), p.TenantID, "employee.phone", normalized)
				if err != nil {
					return err
				}
				phoneHash = h
				enc, err := api.Crypto.EncryptField(r.Context(), p.TenantID, "employee", employeeID, "phone", []byte(normalized))
				if err != nil {
					return err
				}
				securePhone = &enc
			}
		}
		tx, err := api.Writer.Begin(r.Context(), p.TenantID)
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var employeeExists bool
		if err := api.DB.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND employee_no=$2)", p.TenantID, in.EmployeeNo).Scan(&employeeExists); err != nil {
			return err
		}
		if employeeExists {
			return httpapi.Error(409, "DUPLICATE_EMPLOYEE", "员工工号已存在")
		}
		if err := validateRoleIDs(r.Context(), firstHandle(tx), p.TenantID, in.RoleIDs); err != nil {
			return err
		}
		if err := checkUserQuota(r.Context(), firstHandle(tx), p.TenantID); err != nil {
			return err
		}
		id, employeeID, err := api.Auth.CreateTenantUser(r.Context(), tx, p.TenantID, in.LoginName, in.Password, in.RoleIDs, auth.TenantUserEmployee{EmployeeID: employeeID, EmployeeNo: in.EmployeeNo, Name: in.Name, Gender: in.Gender, BirthDate: in.BirthDate, Phone: phone, SecurePhone: securePhone, PhoneHash: phoneHash, OrgUnitID: in.OrgUnitID, Position: in.Position})
		if isUniqueViolation(err) {
			return httpapi.Error(409, "DUPLICATE_USER", "登录名已存在")
		}
		if err != nil {
			return err
		}
		auditTx := tx.Secure()
		if auditTx == nil {
			auditTx = tx.Plain()
		}
		if err := httpapi.Audit(r.Context(), auditTx, p, api.Config.NodeID, "create_user", "user_account", id, nil, map[string]any{"employee_id": employeeID, "login_name": in.LoginName}, ""); err != nil {
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
		var currentStatus string
		if err := api.DB.QueryRow(r.Context(), "SELECT status FROM user_account WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&currentStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "用户不存在")
			}
			return err
		}
		grantIDs := make([]string, 0, len(in.RoleIDs))
		for range in.RoleIDs {
			grantIDs = append(grantIDs, platform.ID())
		}
		now := platform.Now()
		err := withIdentityWrites(api, r.Context(), p.TenantID, "user_account", r.PathValue("id"), func(tx pgx.Tx) error {
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
			for i, roleID := range in.RoleIDs {
				if _, err := tx.Exec(r.Context(), `INSERT INTO user_role(tenant_id,id,user_id,role_id,valid_from) SELECT $1,$2,$3,id,$4 FROM role WHERE tenant_id=$1 AND id=$5`, p.TenantID, grantIDs[i], r.PathValue("id"), now, roleID); err != nil {
					return err
				}
			}
			result, err := tx.Exec(r.Context(), "UPDATE user_account SET status=$1,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6", in.Status, now, p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"))
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return httpapi.Error(404, "NOT_FOUND", "用户不存在")
			}
			if in.Status == "disabled" {
				if _, err := tx.Exec(r.Context(), "UPDATE auth_session SET revoked_at=$1,reason='user_disabled' WHERE tenant_id=$2 AND user_id=$3 AND revoked_at IS NULL", now, p.TenantID, r.PathValue("id")); err != nil {
					return err
				}
			}
			return nil
		}, nil)
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": in.Status, "role_ids": in.RoleIDs})
	})
	api.Handle("DELETE /api/v1/users/{id}", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if r.PathValue("id") == p.UserID {
			return httpapi.Error(400, "CANNOT_DELETE_SELF", "不能删除当前登录管理员")
		}
		var employeeID string
		if err := api.DB.QueryRow(r.Context(), "SELECT employee_id FROM user_account WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&employeeID); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "用户不存在")
		}
		err := withIdentityWrites(api, r.Context(), p.TenantID, "user_account", r.PathValue("id"), func(tx pgx.Tx) error {
			used, err := employeeBusinessUsage(r.Context(), tx, p.TenantID, employeeID)
			if err != nil {
				return err
			}
			if used {
				return httpapi.Error(409, "USER_IN_USE", "用户已参与业务，不能删除，可改为停用")
			}
			for _, stmt := range []struct {
				sql  string
				args []any
			}{
				{"DELETE FROM user_role WHERE tenant_id=$1 AND user_id=$2", []any{p.TenantID, r.PathValue("id")}},
				{"DELETE FROM auth_session WHERE tenant_id=$1 AND user_id=$2", []any{p.TenantID, r.PathValue("id")}},
				{"DELETE FROM user_account WHERE tenant_id=$1 AND id=$2", []any{p.TenantID, r.PathValue("id")}},
				{"DELETE FROM employee_pay_profile WHERE tenant_id=$1 AND employee_id=$2", []any{p.TenantID, employeeID}},
				{"DELETE FROM employee_skill WHERE tenant_id=$1 AND employee_id=$2", []any{p.TenantID, employeeID}},
				{"DELETE FROM employee WHERE tenant_id=$1 AND id=$2", []any{p.TenantID, employeeID}},
			} {
				if _, err := tx.Exec(r.Context(), stmt.sql, stmt.args...); err != nil {
					return err
				}
			}
			return nil
		}, func(tx pgx.Tx) error {
			return httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete_user", "user_account", r.PathValue("id"), nil, map[string]any{"employee_id": employeeID}, "无业务参与，删除用户及入职资料")
		})
		if err != nil {
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
		if api.Writer == nil || api.Store == nil {
			return httpapi.Error(500, "DUALWRITE_REQUIRED", "双写存储未配置，无法开通账号")
		}
		tx, err := api.Writer.Begin(r.Context(), p.TenantID)
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		readTx := firstHandle(tx)
		var employeeNo string
		if err := readTx.QueryRow(r.Context(), "SELECT employee_no FROM employee WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&employeeNo); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "员工档案不存在")
			}
			return err
		}
		var existing string
		err = readTx.QueryRow(r.Context(), "SELECT id FROM user_account WHERE tenant_id=$1 AND employee_id=$2", p.TenantID, r.PathValue("id")).Scan(&existing)
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
		if api.Writer == nil || api.Store == nil {
			return httpapi.Error(500, "DUALWRITE_REQUIRED", "双写存储未配置，无法开通账号")
		}
		if err := validateRoleIDs(r.Context(), firstHandle(tx), p.TenantID, in.RoleIDs); err != nil {
			return err
		}
		if err := checkUserQuota(r.Context(), firstHandle(tx), p.TenantID); err != nil {
			return err
		}
		id, err := api.Auth.CreateEmployeeAccount(r.Context(), tx, p.TenantID, r.PathValue("id"), loginName, password, p.UserID, in.RoleIDs)
		if isUniqueViolation(err) {
			return httpapi.Error(409, "DUPLICATE_LOGIN", "登录名已被占用")
		}
		if err != nil {
			return err
		}
		auditTx := tx.Secure()
		if auditTx == nil {
			auditTx = tx.Plain()
		}
		if err := httpapi.Audit(r.Context(), auditTx, p, api.Config.NodeID, "create_account", "user_account", id, nil, map[string]any{"employee_id": r.PathValue("id"), "login_name": loginName, "auto_password": generated}, ""); err != nil {
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
		if api.Writer == nil {
			return httpapi.Error(500, "DUALWRITE_REQUIRED", "双写存储未配置，无法重置密码")
		}
		var accountID, loginName string
		if err := api.DB.QueryRow(r.Context(), "SELECT id,login_name FROM user_account WHERE tenant_id=$1 AND employee_id=$2", p.TenantID, r.PathValue("id")).Scan(&accountID, &loginName); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "ACCOUNT_NOT_FOUND", "该员工尚未开通账号")
			}
			return err
		}
		password, generated := in.Password, false
		if password == "" {
			generatedPassword, err := auth.GenerateTemporaryPassword(12)
			if err != nil {
				return err
			}
			password, generated = generatedPassword, true
		}
		if len(password) < 12 {
			return httpapi.Error(400, "WEAK_PASSWORD", "密码至少 12 位")
		}
		tx, err := api.Writer.Begin(r.Context(), p.TenantID)
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		tx.MarkAggregate("user_account", accountID)
		if err := api.Auth.ResetAccountPassword(r.Context(), tx, p.TenantID, accountID, password, p.UserID); err != nil {
			return err
		}
		auditTx := tx.Secure()
		if auditTx == nil {
			auditTx = tx.Plain()
		}
		if err := httpapi.Audit(r.Context(), auditTx, p, api.Config.NodeID, "reset_password", "user_account", accountID, nil, map[string]any{"employee_id": r.PathValue("id"), "auto_password": generated}, ""); err != nil {
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
	registerRoleLibrary(api)
	registerAuthMethods(api)
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
