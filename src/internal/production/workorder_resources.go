package production

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* 生产工单不再建组：工单直接挂裁床单，所选生产线的在职员工全部作为工单参与者。 */
func registerWorkOrderResources(api *httpapi.API) {
	api.Handle("GET /api/v1/work-orders/line-participants", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		lineID := strings.TrimSpace(r.URL.Query().Get("line_id"))
		if lineID == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "请选择生产线")
		}
		items, err := loadLineParticipants(r.Context(), api.DB, p.TenantID, lineID)
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("GET /api/v1/work-orders/{id}/participants", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var lineID *string
		if err := api.DB.QueryRow(r.Context(), "SELECT line_org_unit_id FROM work_order WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&lineID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "工单不存在")
			}
			return err
		}
		if lineID == nil || *lineID == "" {
			return httpapi.JSON(w, 200, map[string]any{"items": []map[string]any{}})
		}
		items, err := loadLineParticipants(r.Context(), api.DB, p.TenantID, *lineID)
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("GET /api/v1/work-orders/{id}/cutting-sheets", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT cs.id,cs.cutting_no,COALESCE(cs.project_id,''),cs.status,
			COALESCE((SELECT b.style_no FROM cutting_bed b WHERE b.tenant_id=cs.tenant_id AND b.sheet_id=cs.id ORDER BY b.sort_no,b.id LIMIT 1),''),
			COALESCE((SELECT b.color FROM cutting_bed b WHERE b.tenant_id=cs.tenant_id AND b.sheet_id=cs.id ORDER BY b.sort_no,b.id LIMIT 1),''),
			COALESCE(cs.total_qty,(SELECT COALESCE(SUM(d.piece_qty),0) FROM cutting_bed b JOIN cutting_bundle_detail d ON d.tenant_id=b.tenant_id AND d.bed_id=b.id WHERE b.tenant_id=cs.tenant_id AND b.sheet_id=cs.id))
			FROM work_order_cutting_sheet wocs JOIN cutting_sheet cs ON cs.tenant_id=wocs.tenant_id AND cs.id=wocs.cutting_sheet_id
			WHERE wocs.tenant_id=$1 AND wocs.work_order_id=$2 ORDER BY cs.cutting_no,cs.id`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, projectID, status, styleNo, color string
			var qty int64
			if err := rows.Scan(&id, &no, &projectID, &status, &styleNo, &color, &qty); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "cutting_no": no, "project_id": projectID, "status": status,
				"style_no": styleNo, "color": color, "piece_qty": qty})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("PUT /api/v1/work-orders/{id}/cutting-sheets", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			CuttingSheetIDs []string `json:"cutting_sheet_ids"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		workOrderID := r.PathValue("id")
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var projectID, status string
		if err := tx.QueryRow(r.Context(), "SELECT project_id,status FROM work_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, workOrderID).Scan(&projectID, &status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "工单不存在")
			}
			return err
		}
		if status == "completed" || status == "cancelled" {
			return httpapi.Error(409, "INVALID_STATE", "工单已结束，不能再调整裁床单")
		}
		ids, err := validateWorkOrderSheets(r.Context(), tx, p.TenantID, projectID, in.CuttingSheetIDs)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM work_order_cutting_sheet WHERE tenant_id=$1 AND work_order_id=$2", p.TenantID, workOrderID); err != nil {
			return err
		}
		now := platform.Now()
		for _, sheetID := range ids {
			if _, err := tx.Exec(r.Context(), `INSERT INTO work_order_cutting_sheet(tenant_id,work_order_id,cutting_sheet_id,created_at,created_by)
				VALUES($1,$2,$3,$4,$5)`, p.TenantID, workOrderID, sheetID, now, p.UserID); err != nil {
				return err
			}
		}
		response := map[string]any{"work_order_id": workOrderID, "cutting_sheet_ids": ids, "count": len(ids)}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "update_work_order_cutting_sheets", "work_order", workOrderID, nil, response, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
	})
}

func loadLineParticipants(ctx context.Context, q dbQuerier, tenantID, lineID string) ([]map[string]any, error) {
	rows, err := q.Query(ctx, `SELECT e.id,e.employee_no,e.name,COALESCE(e.position,''),
		EXISTS(SELECT 1 FROM org_unit o WHERE o.tenant_id=$1 AND o.id=$2 AND o.manager_employee_id=e.id)
		FROM employee e WHERE e.tenant_id=$1 AND e.status='active' AND (
			EXISTS(SELECT 1 FROM employee_org_binding b WHERE b.tenant_id=$1 AND b.employee_id=e.id AND b.line_org_id=$2 AND b.is_primary=TRUE AND b.valid_to IS NULL)
			OR EXISTS(SELECT 1 FROM org_unit o WHERE o.tenant_id=$1 AND o.id=$2 AND o.manager_employee_id=e.id)
		) ORDER BY 5 DESC,e.employee_no`, tenantID, lineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, no, name, position string
		var manager bool
		if err := rows.Scan(&id, &no, &name, &position, &manager); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"employee_id": id, "employee_no": no, "name": name, "position": position, "is_manager": manager})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func validateWorkOrderSheets(ctx context.Context, q dbQuerier, tenantID, projectID string, ids []string) ([]string, error) {
	cleaned := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		var sheetProject string
		if err := q.QueryRow(ctx, "SELECT COALESCE(project_id,'') FROM cutting_sheet WHERE tenant_id=$1 AND id=$2", tenantID, id).Scan(&sheetProject); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, httpapi.Error(400, "INVALID_CUTTING_SHEET", "裁床单不存在")
			}
			return nil, err
		}
		if sheetProject != "" && sheetProject != projectID {
			return nil, httpapi.Error(400, "CUTTING_SHEET_PROJECT_MISMATCH", "裁床单不属于该工单的产品订单项目")
		}
		seen[id] = true
		cleaned = append(cleaned, id)
	}
	return cleaned, nil
}
