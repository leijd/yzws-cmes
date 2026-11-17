package production

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platformadmin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

/* 角色库：平台角色模板是只读来源，租户管理员只能「补充缺少的角色」。
 * 已存在的角色（按 code 判定）保持原样，名称、权限都不被覆盖。 */

type roleCodeQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

/* nextCustomRoleCode 生成租户自定义角色编码 custom_001、custom_002…；
 * 平台模板角色使用模板编码（source='platform'），不占用该序列。 */
func nextCustomRoleCode(ctx context.Context, db roleCodeQuerier, tenantID string) (string, error) {
	var seq int
	if err := db.QueryRow(ctx, `SELECT COALESCE(MAX(substring(code from 8)::int),0) FROM role WHERE tenant_id=$1 AND code ~ '^custom_[0-9]+$'`, tenantID).Scan(&seq); err != nil {
		return "", err
	}
	return fmt.Sprintf("custom_%03d", seq+1), nil
}

func uniqueViolationConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}

func registerRoleLibrary(api *httpapi.API) {
	api.Handle("GET /api/v1/role-templates", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT t.id,t.code,t.name,COALESCE(t.description,''),
			(SELECT COUNT(*) FROM platform_role_template_permission tp WHERE tp.template_id=t.id),
			r.id
			FROM platform_role_template t
			LEFT JOIN role r ON r.tenant_id=$1 AND r.code=t.code
			WHERE t.status='active' AND t.code<>'tenant_admin'
			ORDER BY t.sort_no,t.code`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, description string
			var permissionCount int64
			var roleID *string
			if err := rows.Scan(&id, &code, &name, &description, &permissionCount, &roleID); err != nil {
				return err
			}
			item := map[string]any{"id": id, "code": code, "name": name, "description": description,
				"permission_count": permissionCount, "exists": roleID != nil}
			if roleID != nil {
				item["role_id"] = *roleID
			}
			items = append(items, item)
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/role-templates/apply", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			TemplateIDs []string `json:"template_ids"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ids := make([]string, 0, len(in.TemplateIDs))
		seen := map[string]bool{}
		for _, id := range in.TemplateIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "请选择需要补充的角色")
		}
		if len(ids) > 100 {
			return httpapi.Error(400, "VALIDATION_ERROR", "一次最多补充 100 个角色")
		}
		ctx := r.Context()
		/* 事务外只读计划：已存在的角色不进入计划（只增不改），ID 预生成保证两库一致 */
		plan, err := platformadmin.PlanMissingRoleTemplates(ctx, api.DB, p.TenantID, ids, p.UserID, api.Config.NodeID)
		if err != nil {
			return err
		}
		created := plan.Created()
		if created == nil {
			created = []string{}
		}
		if len(created) == 0 {
			return httpapi.JSON(w, 200, map[string]any{"created_roles": created, "created_count": 0,
				"skipped_existing": len(ids), "message": "所选角色均已存在，无需补充"})
		}
		auditAfter := map[string]any{"created_roles": created}
		if api.Writer == nil {
			/* 单库旧模式：无明文库可写时退化为一个本地事务 */
			tx, err := api.DB.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			if err := plan.Apply(ctx, tx); err != nil {
				return err
			}
			if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "apply_role_templates", "tenant", p.TenantID, nil, auditAfter, "从平台角色库补充缺少的角色"); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
		} else {
			tx, err := api.Writer.Begin(ctx, p.TenantID)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			tx.MarkAggregate("tenant", p.TenantID)
			if err := func() error {
				if tx.Plain() != nil {
					if err := plan.Apply(ctx, tx.Plain()); err != nil {
						return err
					}
				}
				if tx.Secure() != nil {
					if err := plan.Apply(ctx, tx.Secure()); err != nil {
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
				return httpapi.Audit(ctx, auditTx, p, api.Config.NodeID, "apply_role_templates", "tenant", p.TenantID, nil, auditAfter, "从平台角色库补充缺少的角色")
			}(); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
		}
		return httpapi.JSON(w, 200, map[string]any{"created_roles": created, "created_count": len(created),
			"skipped_existing": len(ids) - len(created)})
	})
}
