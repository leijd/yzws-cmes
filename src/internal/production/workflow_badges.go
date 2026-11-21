package production

import (
	"encoding/json"
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
)

type workflowBadgeFlow struct {
	ID     string `json:"id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	Status string `json:"status"`
	SortNo int    `json:"sort_no"`
}

type workflowBadgeStep struct {
	ID                 string   `json:"id"`
	FlowID             string   `json:"flow_id"`
	StepCode           string   `json:"step_code"`
	Name               string   `json:"name"`
	MenuPath           string   `json:"menu_path"`
	PermissionCode     string   `json:"permission_code"`
	RecipientRoleCodes []string `json:"recipient_role_codes"`
	CountKind          string   `json:"count_kind"`
	TriggerEvent       string   `json:"trigger_event"`
	Enabled            bool     `json:"enabled"`
	SortNo             int      `json:"sort_no"`
	NodeX              int      `json:"node_x"`
	NodeY              int      `json:"node_y"`
}

type workflowBadgeEdge struct {
	ID         string `json:"id"`
	FlowID     string `json:"flow_id"`
	FromStepID string `json:"from_step_id"`
	ToStepID   string `json:"to_step_id"`
}

func registerWorkflowBadges(api *httpapi.API) {
	api.Handle("GET /api/v1/workflow-badges", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listWorkflowBadges(api, w, r, p)
	})
	api.Handle("GET /api/v1/workflow-badge-config", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return getWorkflowBadgeConfig(api, w, r, p)
	})
	api.Handle("PUT /api/v1/workflow-badge-config", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return putWorkflowBadgeConfig(api, w, r, p)
	})
}

func listWorkflowBadges(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	now := platform.Now()
	roleRows, err := api.DB.Query(r.Context(), `SELECT DISTINCT r.code FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
		WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND ur.valid_from<=$3 AND (ur.valid_to IS NULL OR ur.valid_to>$3)`, p.TenantID, p.UserID, now)
	if err != nil {
		return err
	}
	roleSet := map[string]bool{}
	for roleRows.Next() {
		var code string
		if err := roleRows.Scan(&code); err != nil {
			roleRows.Close()
			return err
		}
		roleSet[code] = true
	}
	rowErr := roleRows.Err()
	roleRows.Close()
	if rowErr != nil {
		return rowErr
	}

	rows, err := api.DB.Query(r.Context(), `SELECT s.menu_path,s.permission_code,s.recipient_role_codes,s.count_kind
		FROM workflow_badge_step s JOIN workflow_badge_flow f ON f.tenant_id=s.tenant_id AND f.id=s.flow_id
		WHERE s.tenant_id=$1 AND f.status='active' AND s.enabled=TRUE AND s.menu_path<>''`, p.TenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	badges := map[string]int64{}
	for rows.Next() {
		var menuPath, permissionCode, countKind string
		var rolesRaw []byte
		if err := rows.Scan(&menuPath, &permissionCode, &rolesRaw, &countKind); err != nil {
			return err
		}
		var roles []string
		_ = json.Unmarshal(rolesRaw, &roles)
		relevant := permissionCode != "" && p.Permissions[permissionCode]
		if !relevant {
			for _, code := range roles {
				if roleSet[code] {
					relevant = true
					break
				}
			}
		}
		if !relevant {
			continue
		}
		count, err := workflowBadgeCount(r, api, p, countKind)
		if err != nil {
			return err
		}
		badges[menuPath] += count
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"badges": badges})
}

func workflowBadgeCount(r *http.Request, api *httpapi.API, p auth.Principal, kind string) (int64, error) {
	var count int64
	var err error
	switch kind {
	case "material_issue_pending_confirm":
		if p.Permissions["inventory:issue"] {
			err = api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM material_issue WHERE tenant_id=$1 AND status='pending_confirm'", p.TenantID).Scan(&count)
		} else {
			err = api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM material_issue WHERE tenant_id=$1 AND status='pending_confirm' AND receiver_employee_id=$2", p.TenantID, p.EmployeeID).Scan(&count)
		}
	case "project_confirmed_pending_receipt":
		err = api.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM project pj WHERE pj.tenant_id=$1 AND pj.status='confirmed'
			AND NOT EXISTS (SELECT 1 FROM material_receipt mr WHERE mr.tenant_id=pj.tenant_id AND mr.project_id=pj.id)`, p.TenantID).Scan(&count)
	case "material_receipt_draft":
		err = api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM material_receipt WHERE tenant_id=$1 AND status='draft'", p.TenantID).Scan(&count)
	case "none":
		count = 0
	default:
		count = 0
	}
	return count, err
}

func getWorkflowBadgeConfig(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	flows := []workflowBadgeFlow{}
	flowRows, err := api.DB.Query(r.Context(), "SELECT id,code,name,status,sort_no FROM workflow_badge_flow WHERE tenant_id=$1 ORDER BY sort_no,code", p.TenantID)
	if err != nil {
		return err
	}
	for flowRows.Next() {
		var f workflowBadgeFlow
		if err := flowRows.Scan(&f.ID, &f.Code, &f.Name, &f.Status, &f.SortNo); err != nil {
			flowRows.Close()
			return err
		}
		flows = append(flows, f)
	}
	flowErr := flowRows.Err()
	flowRows.Close()
	if flowErr != nil {
		return flowErr
	}

	steps := []workflowBadgeStep{}
	stepRows, err := api.DB.Query(r.Context(), `SELECT id,flow_id,step_code,name,menu_path,permission_code,recipient_role_codes::text,count_kind,trigger_event,enabled,sort_no,node_x,node_y
		FROM workflow_badge_step WHERE tenant_id=$1 ORDER BY sort_no,step_code`, p.TenantID)
	if err != nil {
		return err
	}
	for stepRows.Next() {
		var s workflowBadgeStep
		var rolesRaw string
		if err := stepRows.Scan(&s.ID, &s.FlowID, &s.StepCode, &s.Name, &s.MenuPath, &s.PermissionCode, &rolesRaw, &s.CountKind, &s.TriggerEvent, &s.Enabled, &s.SortNo, &s.NodeX, &s.NodeY); err != nil {
			stepRows.Close()
			return err
		}
		_ = json.Unmarshal([]byte(rolesRaw), &s.RecipientRoleCodes)
		steps = append(steps, s)
	}
	stepErr := stepRows.Err()
	stepRows.Close()
	if stepErr != nil {
		return stepErr
	}

	edges := []workflowBadgeEdge{}
	edgeRows, err := api.DB.Query(r.Context(), "SELECT id,flow_id,from_step_id,to_step_id FROM workflow_badge_edge WHERE tenant_id=$1 ORDER BY id", p.TenantID)
	if err != nil {
		return err
	}
	for edgeRows.Next() {
		var e workflowBadgeEdge
		if err := edgeRows.Scan(&e.ID, &e.FlowID, &e.FromStepID, &e.ToStepID); err != nil {
			edgeRows.Close()
			return err
		}
		edges = append(edges, e)
	}
	edgeErr := edgeRows.Err()
	edgeRows.Close()
	if edgeErr != nil {
		return edgeErr
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"flows": flows, "steps": steps, "edges": edges})
}

func putWorkflowBadgeConfig(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		Flows []workflowBadgeFlow  `json:"flows"`
		Steps []workflowBadgeStep  `json:"steps"`
		Edges []workflowBadgeEdge  `json:"edges"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), "DELETE FROM workflow_badge_edge WHERE tenant_id=$1", p.TenantID); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), "DELETE FROM workflow_badge_step WHERE tenant_id=$1", p.TenantID); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), "DELETE FROM workflow_badge_flow WHERE tenant_id=$1", p.TenantID); err != nil {
		return err
	}
	now := platform.Now()
	flowIDs := map[string]bool{}
	for i := range in.Flows {
		f := &in.Flows[i]
		if f.ID == "" {
			f.ID = platform.ID()
		}
		if f.Code == "" || f.Name == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "流程编码与名称必填")
		}
		if f.Status == "" {
			f.Status = "active"
		}
		flowIDs[f.ID] = true
		if _, err := tx.Exec(r.Context(), `INSERT INTO workflow_badge_flow(tenant_id,id,code,name,status,sort_no,created_at,created_by,updated_at,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$7,$8)`, p.TenantID, f.ID, f.Code, f.Name, f.Status, f.SortNo, now, p.UserID); err != nil {
			return err
		}
	}
	stepIDs := map[string]bool{}
	for i := range in.Steps {
		s := &in.Steps[i]
		if s.ID == "" {
			s.ID = platform.ID()
		}
		if !flowIDs[s.FlowID] {
			return httpapi.Error(400, "VALIDATION_ERROR", "步骤引用了不存在的流程")
		}
		if s.StepCode == "" || s.Name == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "步骤编码与名称必填")
		}
		stepIDs[s.ID] = true
		roles, _ := json.Marshal(s.RecipientRoleCodes)
		if _, err := tx.Exec(r.Context(), `INSERT INTO workflow_badge_step(tenant_id,id,flow_id,step_code,name,menu_path,permission_code,recipient_role_codes,count_kind,trigger_event,enabled,sort_no,node_x,node_y)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, p.TenantID, s.ID, s.FlowID, s.StepCode, s.Name, s.MenuPath, s.PermissionCode, roles, s.CountKind, s.TriggerEvent, s.Enabled, s.SortNo, s.NodeX, s.NodeY); err != nil {
			return err
		}
	}
	/* 校验连线：只检查引用与同流程；流程拓扑允许循环（如返工回路） */
	stepFlow := map[string]string{}
	for i := range in.Steps {
		stepFlow[in.Steps[i].ID] = in.Steps[i].FlowID
	}
	for _, e := range in.Edges {
		if !flowIDs[e.FlowID] || !stepIDs[e.FromStepID] || !stepIDs[e.ToStepID] {
			return httpapi.Error(400, "VALIDATION_ERROR", "连线引用了不存在的流程或步骤")
		}
		if stepFlow[e.FromStepID] != e.FlowID || stepFlow[e.ToStepID] != e.FlowID {
			return httpapi.Error(400, "VALIDATION_ERROR", "连线两端必须属于同一流程")
		}
	}
	for _, e := range in.Edges {
		if e.ID == "" {
			e.ID = platform.ID()
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO workflow_badge_edge(tenant_id,id,flow_id,from_step_id,to_step_id) VALUES($1,$2,$3,$4,$5)`, p.TenantID, e.ID, e.FlowID, e.FromStepID, e.ToStepID); err != nil {
			return err
		}
	}
	response := map[string]any{"flows": len(in.Flows), "steps": len(in.Steps), "edges": len(in.Edges)}
	if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "update", "workflow_badge_config", "current", nil, response, ""); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, response)
}
