package platformadmin

import (
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerModules(api *httpapi.API) {
	api.PlatformHandle("GET /api/v1/platform/modules", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,parent_id,module_type,code,name,COALESCE(path,''),permission_code,sort_no,status,is_admin_only FROM platform_module ORDER BY sort_no,code`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, moduleType, code, name, path, permission, status string
			var parent *string
			var sortNo int
			var adminOnly bool
			if err := rows.Scan(&id, &parent, &moduleType, &code, &name, &path, &permission, &sortNo, &status, &adminOnly); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "parent_id": parent, "module_type": moduleType, "code": code, "name": name, "path": path, "permission_code": permission, "sort_no": sortNo, "status": status, "is_admin_only": adminOnly})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.PlatformHandle("POST /api/v1/platform/modules", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in moduleInput
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if err := in.validate(); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		id := platform.ID()
		if _, err := tx.Exec(r.Context(), `INSERT INTO platform_module(id,parent_id,module_type,code,name,path,permission_code,sort_no,is_admin_only,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, in.ParentID, in.ModuleType, in.Code, in.Name, in.Path, in.PermissionCode, in.SortNo, in.IsAdminOnly, platform.Now()); err != nil {
			return httpapi.Error(409, "DUPLICATE_MODULE", "模块编码或权限点已存在")
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "module_create", "platform_module", id, nil, in, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "status": "active"})
	})
	api.PlatformHandle("PATCH /api/v1/platform/modules/{id}", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in moduleInput
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if err := in.validate(); err != nil {
			return err
		}
		result, err := api.DB.Exec(r.Context(), `UPDATE platform_module SET parent_id=$1,module_type=$2,name=$3,path=$4,sort_no=$5,status=$6,is_admin_only=$7 WHERE id=$8`, in.ParentID, in.ModuleType, in.Name, in.Path, in.SortNo, in.Status, in.IsAdminOnly, r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(404, "NOT_FOUND", "功能模块不存在")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": in.Status, "is_admin_only": in.IsAdminOnly})
	})
}

type moduleInput struct {
	ParentID       *string `json:"parent_id"`
	ModuleType     string  `json:"module_type"`
	Code           string  `json:"code"`
	Name           string  `json:"name"`
	Path           *string `json:"path"`
	PermissionCode string  `json:"permission_code"`
	SortNo         int     `json:"sort_no"`
	Status         string  `json:"status"`
	IsAdminOnly    bool    `json:"is_admin_only"`
}

func (in *moduleInput) validate() error {
	if in.ModuleType != "menu" && in.ModuleType != "button" && in.ModuleType != "api" {
		return httpapi.Error(400, "VALIDATION_ERROR", "模块类型不合法")
	}
	if in.Code == "" || in.Name == "" || in.PermissionCode == "" {
		return httpapi.Error(400, "VALIDATION_ERROR", "模块编码、名称和权限点必填")
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Status != "active" && in.Status != "disabled" {
		return httpapi.Error(400, "VALIDATION_ERROR", "模块状态不合法")
	}
	return nil
}

var _ = pgx.ErrNoRows
