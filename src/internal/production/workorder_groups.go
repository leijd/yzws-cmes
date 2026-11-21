package production

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* 生产工单下的生产组：车间主任/线长建组，指定组长、组员并分配裁床单；
 * 组长再按「裁床单+产品工序」把扎派工给组员（派工本身复用 production_task 既有接口）。 */
func registerWorkOrderGroups(api *httpapi.API) {
	api.Handle("GET /api/v1/work-orders/{id}/groups", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		workOrderID := r.PathValue("id")
		rows, err := api.DB.Query(r.Context(), `SELECT g.id,g.name,COALESCE(g.workshop_org_unit_id,''),COALESCE(g.line_org_unit_id,''),COALESCE(g.leader_employee_id,''),
			COALESCE(ws.name,''),COALESCE(l.name,''),COALESCE(le.name,'')
			FROM work_order_group g
			LEFT JOIN org_unit ws ON ws.tenant_id=g.tenant_id AND ws.id=g.workshop_org_unit_id
			LEFT JOIN org_unit l ON l.tenant_id=g.tenant_id AND l.id=g.line_org_unit_id
			LEFT JOIN employee le ON le.tenant_id=g.tenant_id AND le.id=g.leader_employee_id
			WHERE g.tenant_id=$1 AND g.work_order_id=$2 ORDER BY g.created_at,g.id`, p.TenantID, workOrderID)
		if err != nil {
			return err
		}
		defer rows.Close()
		groups := []map[string]any{}
		for rows.Next() {
			var id, name, workshopID, lineID, leaderID, workshopName, lineName, leaderName string
			if err := rows.Scan(&id, &name, &workshopID, &lineID, &leaderID, &workshopName, &lineName, &leaderName); err != nil {
				return err
			}
			groups = append(groups, map[string]any{"id": id, "name": name, "workshop_org_unit_id": workshopID, "line_org_unit_id": lineID,
				"leader_employee_id": leaderID, "workshop_name": workshopName, "line_name": lineName, "leader_name": leaderName,
				"members": []map[string]any{}, "sheets": []map[string]any{}})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		byID := map[string]map[string]any{}
		for _, g := range groups {
			byID[g["id"].(string)] = g
		}
		if len(groups) > 0 {
			memberRows, err := api.DB.Query(r.Context(), `SELECT m.group_id,e.id,e.employee_no,e.name FROM work_order_group_member m
				JOIN employee e ON e.tenant_id=m.tenant_id AND e.id=m.employee_id
				WHERE m.tenant_id=$1 AND m.group_id=ANY($2::text[]) ORDER BY e.employee_no`, p.TenantID, groupIDs(groups))
			if err != nil {
				return err
			}
			defer memberRows.Close()
			for memberRows.Next() {
				var gid, id, no, name string
				if err := memberRows.Scan(&gid, &id, &no, &name); err != nil {
					return err
				}
				g := byID[gid]
				g["members"] = append(g["members"].([]map[string]any), map[string]any{"employee_id": id, "employee_no": no, "name": name})
			}
			if err := memberRows.Err(); err != nil {
				return err
			}
			sheetRows, err := api.DB.Query(r.Context(), `SELECT s.group_id,s.cutting_sheet_id,cs.cutting_no,cs.status FROM work_order_group_sheet s
				LEFT JOIN cutting_sheet cs ON cs.tenant_id=s.tenant_id AND cs.id=s.cutting_sheet_id
				WHERE s.tenant_id=$1 AND s.group_id=ANY($2::text[]) ORDER BY cs.cutting_no`, p.TenantID, groupIDs(groups))
			if err != nil {
				return err
			}
			defer sheetRows.Close()
			for sheetRows.Next() {
				var gid, id string
				var no, status *string
				if err := sheetRows.Scan(&gid, &id, &no, &status); err != nil {
					return err
				}
				g := byID[gid]
				g["sheets"] = append(g["sheets"].([]map[string]any), map[string]any{"cutting_sheet_id": id, "cutting_no": no, "status": status})
			}
			if err := sheetRows.Err(); err != nil {
				return err
			}
		}
		return httpapi.JSON(w, 200, map[string]any{"items": groups})
	})

	api.Handle("POST /api/v1/work-orders/{id}/groups", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name               string   `json:"name"`
			WorkshopOrgUnitID  string   `json:"workshop_org_unit_id"`
			LineOrgUnitID      string   `json:"line_org_unit_id"`
			LeaderEmployeeID   string   `json:"leader_employee_id"`
			MemberIDs          []string `json:"member_ids"`
			CuttingSheetIDs    []string `json:"cutting_sheet_ids"`
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
		var projectID string
		var woStatus string
		if err = tx.QueryRow(r.Context(), "SELECT project_id,status FROM work_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, workOrderID).Scan(&projectID, &woStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "工单不存在")
			}
			return err
		}
		if woStatus == "completed" || woStatus == "cancelled" {
			return httpapi.Error(409, "INVALID_STATE", "工单已结束，不能再组建生产组")
		}
		/* 只选生产线时自动带出所属车间，避免前端必须两级联动 */
		if in.LineOrgUnitID != "" && in.WorkshopOrgUnitID == "" {
			if err = tx.QueryRow(r.Context(), "SELECT COALESCE(parent_id,'') FROM org_unit WHERE tenant_id=$1 AND id=$2 AND org_type='line'", p.TenantID, in.LineOrgUnitID).Scan(&in.WorkshopOrgUnitID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return httpapi.Error(400, "INVALID_LINE", "生产线不存在")
				}
				return err
			}
		}
		if in.LineOrgUnitID != "" || in.WorkshopOrgUnitID != "" {
			var lineID, workshopID *string
			if in.LineOrgUnitID != "" {
				lineID = &in.LineOrgUnitID
			}
			if in.WorkshopOrgUnitID != "" {
				workshopID = &in.WorkshopOrgUnitID
			}
			if err = validateWorkshopLine(r.Context(), tx, p.TenantID, workshopID, lineID); err != nil {
				return err
			}
		}
		for _, emp := range append(append([]string{}, in.MemberIDs...), in.LeaderEmployeeID) {
			if strings.TrimSpace(emp) == "" {
				continue
			}
			var ok bool
			if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND id=$2 AND status='active')", p.TenantID, emp).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return httpapi.Error(400, "INVALID_EMPLOYEE", "员工不存在或已离职")
			}
		}
		for _, sheetID := range in.CuttingSheetIDs {
			if strings.TrimSpace(sheetID) == "" {
				continue
			}
			var sheetProject string
			if err = tx.QueryRow(r.Context(), "SELECT COALESCE(project_id,'') FROM cutting_sheet WHERE tenant_id=$1 AND id=$2", p.TenantID, sheetID).Scan(&sheetProject); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return httpapi.Error(400, "INVALID_CUTTING_SHEET", "裁床单不存在")
				}
				return err
			}
			if sheetProject != "" && sheetProject != projectID {
				return httpapi.Error(400, "CUTTING_SHEET_PROJECT_MISMATCH", "裁床单不属于该工单的产品订单项目")
			}
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			var count int
			if err = tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM work_order_group WHERE tenant_id=$1 AND work_order_id=$2", p.TenantID, workOrderID).Scan(&count); err != nil {
				return err
			}
			name = fmt.Sprintf("第%d组", count+1)
		}
		id, now := platform.ID(), platform.Now()
		if _, err = tx.Exec(r.Context(), `INSERT INTO work_order_group(tenant_id,id,work_order_id,workshop_org_unit_id,line_org_unit_id,name,leader_employee_id,created_at,created_by,created_node,updated_at,updated_by,updated_node)
			VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''),$8,$9,$10,$8,$9,$10)`,
			p.TenantID, id, workOrderID, in.WorkshopOrgUnitID, in.LineOrgUnitID, name, in.LeaderEmployeeID, now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		for _, emp := range in.MemberIDs {
			if strings.TrimSpace(emp) == "" {
				continue
			}
			if _, err = tx.Exec(r.Context(), "INSERT INTO work_order_group_member(tenant_id,group_id,employee_id,created_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", p.TenantID, id, emp, now); err != nil {
				return err
			}
		}
		for _, sheetID := range in.CuttingSheetIDs {
			if strings.TrimSpace(sheetID) == "" {
				continue
			}
			if _, err = tx.Exec(r.Context(), "INSERT INTO work_order_group_sheet(tenant_id,group_id,cutting_sheet_id,created_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", p.TenantID, id, sheetID, now); err != nil {
				return err
			}
		}
		response := map[string]any{"id": id, "name": name, "work_order_id": workOrderID}
		if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create_work_order_group", "work_order_group", id, nil, response, ""); err != nil {
			return err
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, response)
	})

	api.Handle("DELETE /api/v1/work-orders/{id}/groups/{gid}", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		result, err := tx.Exec(r.Context(), "DELETE FROM work_order_group WHERE tenant_id=$1 AND id=$2 AND work_order_id=$3", p.TenantID, r.PathValue("gid"), r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(404, "NOT_FOUND", "生产组不存在")
		}
		if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete_work_order_group", "work_order_group", r.PathValue("gid"), nil, nil, "删除工单生产组"); err != nil {
			return err
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})
}

func groupIDs(groups []map[string]any) []string {
	ids := []string{}
	for _, g := range groups {
		ids = append(ids, g["id"].(string))
	}
	return ids
}
