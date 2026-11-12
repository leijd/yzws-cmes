package platformadmin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerRoleTemplates(api *httpapi.API) {
	api.PlatformHandle("GET /api/v1/platform/permissions", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT code,module,name,kind,COALESCE(description,''),sort_no FROM platform_permission WHERE admin_only=FALSE ORDER BY sort_no,code`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var code, module, name, kind, description string
			var sortNo int
			if err := rows.Scan(&code, &module, &name, &kind, &description, &sortNo); err != nil {
				return err
			}
			items = append(items, map[string]any{"code": code, "module": module, "name": name, "kind": kind, "description": description, "sort_no": sortNo})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.PlatformHandle("GET /api/v1/platform/role-templates", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT t.id,t.code,t.name,COALESCE(t.description,''),t.sort_no,t.status,COALESCE(json_agg(p.permission_code) FILTER(WHERE p.permission_code IS NOT NULL),'[]'),(SELECT COUNT(*) FROM role r WHERE r.platform_template_id=t.id) FROM platform_role_template t LEFT JOIN platform_role_template_permission p ON p.template_id=t.id GROUP BY t.id ORDER BY t.sort_no,t.code`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, description, status string
			var sortNo, tenantUseCount int
			var body []byte
			if err := rows.Scan(&id, &code, &name, &description, &sortNo, &status, &body, &tenantUseCount); err != nil {
				return err
			}
			var codes []string
			_ = json.Unmarshal(body, &codes)
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "description": description, "sort_no": sortNo, "status": status, "permission_codes": codes, "tenant_use_count": tenantUseCount})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.PlatformHandle("POST /api/v1/platform/role-templates", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in templateInput
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Code == "" || in.Name == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "模板编码和名称必填")
		}
		if in.Status == "" {
			in.Status = "active"
		}
		id := platform.ID()
		_, err := api.DB.Exec(r.Context(), `INSERT INTO platform_role_template(id,code,name,description,sort_no,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, in.Code, in.Name, in.Description, in.SortNo, in.Status, platform.Now())
		if err != nil {
			return httpapi.Error(409, "DUPLICATE_TEMPLATE", "角色模板编码已存在")
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "status": in.Status})
	})
	api.PlatformHandle("PATCH /api/v1/platform/role-templates/{id}", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in templateInput
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Name == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "模板名称必填")
		}
		if in.Status == "" {
			in.Status = "active"
		}
		result, err := api.DB.Exec(r.Context(), `UPDATE platform_role_template SET name=$1,description=$2,sort_no=$3,status=$4 WHERE id=$5`, in.Name, in.Description, in.SortNo, in.Status, r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(404, "NOT_FOUND", "角色模板不存在")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": in.Status})
	})
	api.PlatformHandle("DELETE /api/v1/platform/role-templates/{id}", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var useCount int
		if err := tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM role WHERE platform_template_id=$1", r.PathValue("id")).Scan(&useCount); err != nil {
			return err
		}
		if useCount > 0 {
			return httpapi.Error(409, "TEMPLATE_IN_USE", "该平台角色已被租户使用，不能删除，可停用")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM platform_role_template_permission WHERE template_id=$1", r.PathValue("id")); err != nil {
			return err
		}
		result, err := tx.Exec(r.Context(), "DELETE FROM platform_role_template WHERE id=$1", r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(404, "NOT_FOUND", "角色模板不存在")
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "template_delete", "platform_role_template", r.PathValue("id"), nil, nil, "无租户使用，删除平台角色模板"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})
	api.PlatformHandle("PUT /api/v1/platform/role-templates/{id}/permissions", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
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
		if _, err := tx.Exec(r.Context(), "DELETE FROM platform_role_template_permission WHERE template_id=$1", r.PathValue("id")); err != nil {
			return err
		}
		for _, code := range in.PermissionCodes {
			if err := insertTemplatePermission(r.Context(), tx, r.PathValue("id"), code); err != nil {
				return err
			}
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "template_permission_update", "platform_role_template", r.PathValue("id"), nil, in, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"template_id": r.PathValue("id"), "permission_codes": in.PermissionCodes})
	})
	api.PlatformHandle("POST /api/v1/platform/tenants/{id}/apply-role-templates", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			TemplateIDs []string `json:"template_ids"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		tenantID := r.PathValue("id")
		/* 计划事务外完成（读读主库 api.DB，即 secure；设计 2.2 第 7 条：业务不得读明文库）；
		 * 单步双写事务落两库（设计原则：禁止长事务） */
		plan, err := planRoleTemplates(ctx, api.DB, tenantID, in.TemplateIDs, p.AdminID, api.Config.NodeID)
		if err != nil {
			return err
		}
		tx, err := api.Writer.Begin(ctx, tenantID)
		if err != nil {
			return err
		}
		tx.MarkAggregate("tenant", tenantID)
		if err := func() error {
			if tx.Plain() != nil {
				if err := plan.apply(ctx, tx.Plain()); err != nil {
					return err
				}
			}
			if tx.Secure() != nil {
				if err := plan.apply(ctx, tx.Secure()); err != nil {
					return err
				}
			}
			if tx.Plain() != nil {
				return platformAudit(ctx, tx.Plain(), p.AdminID, "tenant_role_template_apply", "tenant", tenantID, nil, plan.Summary(), "")
			}
			return nil
		}(); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, plan.Summary())
	})
}

type templateInput struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	SortNo      int    `json:"sort_no"`
	Status      string `json:"status"`
}

func insertTemplatePermission(ctx context.Context, tx pgx.Tx, templateID, permissionCode string) error {
	var resource, action string
	if err := tx.QueryRow(ctx, "SELECT split_part(permission_code,':',1),split_part(permission_code,':',2) FROM platform_module WHERE permission_code=$1 AND status='active' AND is_admin_only=FALSE", permissionCode).Scan(&resource, &action); err != nil {
		return httpapi.Error(400, "INVALID_PERMISSION", "权限点不存在、已停用或仅限固定租户管理员使用："+permissionCode)
	}
	_, err := tx.Exec(ctx, "INSERT INTO platform_role_template_permission(id,template_id,permission_code,created_at) VALUES($1,$2,$3,$4)", platform.ID(), templateID, permissionCode, platform.Now())
	return err
}

/* 角色模板应用：计划与执行分离（设计原则：禁止长事务；双写两库 ID 必须一致）。
 * planRoleTemplates 事务外只读解析模板/权限并预生成全部 ID；apply 为纯写入，
 * 可在 plain/secure 两个事务各执行一遍而结果一致。 */

type plannedPermission struct {
	Code, Resource, Action string
	PermissionID           string
	InsertPermission       bool // 该权限点本厂尚无，需要插入
	RolePermissionID       string
}

type plannedRole struct {
	RoleID, Code, Name, Description, TemplateID string
	IsNew                                      bool
	Permissions                                []plannedPermission
}

type roleApplyPlan struct {
	TenantID string
	ActorID  string
	NodeID   string
	Now      int64
	Roles    []plannedRole
	Created  []string
	Updated  []string
}

type templateQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func planRoleTemplates(ctx context.Context, db templateQuerier, tenantID string, templateIDs []string, actorID, nodeID string) (*roleApplyPlan, error) {
	query := `SELECT id,code,name,description FROM platform_role_template WHERE status='active' ORDER BY sort_no,code`
	args := []any{}
	if len(templateIDs) > 0 {
		query = `SELECT id,code,name,description FROM platform_role_template WHERE status='active' AND id=ANY($1) ORDER BY sort_no,code`
		args = append(args, templateIDs)
	}
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	type template struct{ id, code, name, description string }
	templates := []template{}
	for rows.Next() {
		var v template
		if err := rows.Scan(&v.id, &v.code, &v.name, &v.description); err != nil {
			rows.Close()
			return nil, err
		}
		templates = append(templates, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(templateIDs) > 0 && len(templates) != len(templateIDs) {
		return nil, httpapi.Error(400, "INVALID_TEMPLATE", "角色模板不存在或已停用")
	}
	plan := &roleApplyPlan{TenantID: tenantID, ActorID: actorID, NodeID: nodeID, Now: platform.Now()}
	for _, item := range templates {
		if item.code == "tenant_admin" {
			return nil, httpapi.Error(400, "INVALID_TEMPLATE", "角色模板不能覆盖固定租户管理员")
		}
		role := plannedRole{Code: item.code, Name: item.name, Description: item.description, TemplateID: item.id}
		err = db.QueryRow(ctx, "SELECT id FROM role WHERE tenant_id=$1 AND code=$2", tenantID, item.code).Scan(&role.RoleID)
		if errors.Is(err, pgx.ErrNoRows) {
			role.IsNew = true
			role.RoleID = platform.ID()
			plan.Created = append(plan.Created, item.code)
		} else if err != nil {
			return nil, err
		} else {
			plan.Updated = append(plan.Updated, item.code)
		}
		permRows, err := db.Query(ctx, "SELECT permission_code FROM platform_role_template_permission WHERE template_id=$1 ORDER BY permission_code", item.id)
		if err != nil {
			return nil, err
		}
		codes := []string{}
		for permRows.Next() {
			var code string
			if err := permRows.Scan(&code); err != nil {
				permRows.Close()
				return nil, err
			}
			codes = append(codes, code)
		}
		permRows.Close()
		for _, code := range codes {
			perm := plannedPermission{Code: code, RolePermissionID: platform.ID()}
			if err = db.QueryRow(ctx, "SELECT split_part(permission_code,':',1),split_part(permission_code,':',2) FROM platform_module WHERE permission_code=$1 AND status='active' AND is_admin_only=FALSE", code).Scan(&perm.Resource, &perm.Action); err != nil {
				return nil, httpapi.Error(400, "INVALID_PERMISSION", "模板包含无效权限点："+code)
			}
			err = db.QueryRow(ctx, "SELECT id FROM permission WHERE tenant_id=$1 AND code=$2", tenantID, code).Scan(&perm.PermissionID)
			if errors.Is(err, pgx.ErrNoRows) {
				perm.InsertPermission = true
				perm.PermissionID = platform.ID()
			} else if err != nil {
				return nil, err
			}
			role.Permissions = append(role.Permissions, perm)
		}
		plan.Roles = append(plan.Roles, role)
	}
	return plan, nil
}

/* Summary 与原 applyRoleTemplates 返回口径一致。 */
func (p *roleApplyPlan) Summary() map[string]any {
	return map[string]any{"created_roles": p.Created, "updated_roles": p.Updated, "applied_templates": len(p.Roles)}
}

/* apply 纯写入：角色 upsert + 授权重建 + 权限点补齐（同一计划在两库各执行一遍）。 */
func (p *roleApplyPlan) apply(ctx context.Context, tx pgx.Tx) error {
	for _, role := range p.Roles {
		if role.IsNew {
			if _, err := tx.Exec(ctx, "INSERT INTO role(tenant_id,id,code,name,description,is_system,source,platform_template_id,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,FALSE,'platform',$6,$7,$8,$9,$7,$8,$9)", p.TenantID, role.RoleID, role.Code, role.Name, role.Description, role.TemplateID, p.Now, p.ActorID, p.NodeID); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, "UPDATE role SET name=$1,description=$2,source='platform',platform_template_id=$3,version=version+1,updated_at=$4,updated_by=$5,updated_node=$6 WHERE tenant_id=$7 AND id=$8", role.Name, role.Description, role.TemplateID, p.Now, p.ActorID, p.NodeID, p.TenantID, role.RoleID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, "DELETE FROM role_permission WHERE tenant_id=$1 AND role_id=$2", p.TenantID, role.RoleID); err != nil {
			return err
		}
		for _, perm := range role.Permissions {
			if perm.InsertPermission {
				if _, err := tx.Exec(ctx, "INSERT INTO permission(tenant_id,id,code,resource,action) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,code) DO NOTHING", p.TenantID, perm.PermissionID, perm.Code, perm.Resource, perm.Action); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, "INSERT INTO role_permission(tenant_id,id,role_id,permission_id) VALUES($1,$2,$3,$4)", p.TenantID, perm.RolePermissionID, role.RoleID, perm.PermissionID); err != nil {
				return err
			}
		}
	}
	return nil
}

/* applyRoleTemplates 单事务兼容入口（计划+执行同事务）。新代码请用 planRoleTemplates
 * 事务外计划、双写事务分别 apply。 */
func applyRoleTemplates(ctx context.Context, tx pgx.Tx, tenantID string, templateIDs []string, actorID, nodeID string) (map[string]any, error) {
	plan, err := planRoleTemplates(ctx, tx, tenantID, templateIDs, actorID, nodeID)
	if err != nil {
		return nil, err
	}
	if err := plan.apply(ctx, tx); err != nil {
		return nil, err
	}
	return plan.Summary(), nil
}
