package production

import (
	"fmt"
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerTasks(api *httpapi.API) {
	api.Handle("GET /api/v1/production-tasks", "task:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error { return listTasks(api, w, r, p) })
	api.Handle("GET /api/v1/production-tasks/{id}", "task:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error { return getTask(api, w, r, p) })
	api.Handle("POST /api/v1/production-tasks/{id}/assign", "task:assign", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error { return assignTask(api, w, r, p) })
	api.Handle("PATCH /api/v1/production-tasks/{id}", "task:assign", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version             int64   `json:"version"`
			PlannedQty          int64   `json:"planned_qty"`
			UnitPrice           string  `json:"unit_price"`
			StandardWorkSeconds int64   `json:"standard_work_seconds"`
			PlannedStartAt      *int64  `json:"planned_start_at"`
			PlannedEndAt        *int64  `json:"planned_end_at"`
			Remark              *string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		result, err := api.DB.Exec(r.Context(), `UPDATE production_task SET planned_qty=$1,unit_price=$2,standard_work_seconds=$3,planned_start_at=$4,planned_end_at=$5,remark=$6,version=version+1,updated_at=$7,updated_by=$8,updated_node=$9 WHERE tenant_id=$10 AND id=$11 AND version=$12 AND $1>=reported_qty`, in.PlannedQty, in.UnitPrice, in.StandardWorkSeconds, in.PlannedStartAt, in.PlannedEndAt, in.Remark, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), in.Version)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(409, "QUANTITY_OR_VERSION_CONFLICT", "计划量不能小于已报量，且版本必须一致")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "version": in.Version + 1})
	})
	for action, status := range map[string]string{"start": "in_progress", "pause": "paused", "complete": "completed"} {
		action, status := action, status
		api.Handle("POST /api/v1/production-tasks/{id}/"+action, "task:assign", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
			var in struct {
				Version int64  `json:"version"`
				Reason  string `json:"reason"`
			}
			if err := httpapi.Decode(r, &in); err != nil {
				return err
			}
			result, err := api.DB.Exec(r.Context(), fmt.Sprintf(`UPDATE production_task SET status=$1,actual_start_at=CASE WHEN $1='in_progress' THEN COALESCE(actual_start_at,$2) ELSE actual_start_at END,actual_end_at=CASE WHEN $1='completed' THEN $2 ELSE actual_end_at END,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6 AND version=$7`), status, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), in.Version)
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return httpapi.Error(409, "VERSION_CONFLICT", "任务已变更")
			}
			return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": status, "version": in.Version + 1})
		})
	}
	api.Handle("GET /api/v1/production-tasks/{id}/reports", "report:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,report_kind,good_qty,defect_qty,employee_id,unit_price::text,review_status,occurred_at FROM work_report WHERE tenant_id=$1 AND task_id=$2 ORDER BY occurred_at DESC,id`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, kind, employee, price, status string
			var good, defect, occurred int64
			if err := rows.Scan(&id, &kind, &good, &defect, &employee, &price, &status, &occurred); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "report_kind": kind, "good_qty": good, "defect_qty": defect, "employee_id": employee, "unit_price": price, "review_status": status, "occurred_at": occurred})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
}

func taskSelect() string {
	return `SELECT t.id,t.work_order_id,t.product_process_id,t.parent_task_id,t.department_id,t.employee_id,e.name,s.process_code,s.process_name,s.id,t.planned_qty,t.reported_qty,t.approved_good_qty,t.pending_qty,t.planned_qty-t.reported_qty,t.unit_price::text,t.report_ratio::text,t.standard_work_seconds,t.status,t.version,w.report_mode FROM production_task t JOIN work_order_process_snapshot s ON s.tenant_id=t.tenant_id AND s.work_order_id=t.work_order_id AND s.product_process_id=t.product_process_id JOIN work_order w ON w.tenant_id=t.tenant_id AND w.id=t.work_order_id LEFT JOIN employee e ON e.tenant_id=t.tenant_id AND e.id=t.employee_id`
}

func canViewAllTasks(p auth.Principal) bool {
	return p.Permissions["task:assign"] || p.Permissions["report:read"] || p.Permissions["review:approve"]
}

func scanTask(row interface{ Scan(...any) error }) (map[string]any, error) {
	var id, wo, pp, processCode, processName, processSnapshotID, price, ratio, status, reportMode string
	var parent, department, employee, employeeName *string
	var planned, reported, approved, pending, remaining, seconds, version int64
	if err := row.Scan(&id, &wo, &pp, &parent, &department, &employee, &employeeName, &processCode, &processName, &processSnapshotID, &planned, &reported, &approved, &pending, &remaining, &price, &ratio, &seconds, &status, &version, &reportMode); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "work_order_id": wo, "product_process_id": pp, "process_snapshot_id": processSnapshotID, "parent_task_id": parent, "department_id": department, "employee_id": employee, "employee_name": employeeName, "process_code": processCode, "process_name": processName, "planned_qty": planned, "reported_qty": reported, "approved_good_qty": approved, "pending_qty": pending, "remaining_qty": remaining, "unit_price": price, "report_ratio": ratio, "standard_work_seconds": seconds, "status": status, "version": version, "report_mode": reportMode}, nil
}
func listTasks(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	args := []any{p.TenantID}
	where := "t.tenant_id=$1"
	if workOrder := r.URL.Query().Get("work_order_id"); workOrder != "" {
		args = append(args, workOrder)
		where += fmt.Sprintf(" AND t.work_order_id=$%d", len(args))
	}
	if status := r.URL.Query().Get("status"); status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND t.status=$%d", len(args))
	}
	if employee := r.URL.Query().Get("employee_id"); employee != "" {
		args = append(args, employee)
		where += fmt.Sprintf(" AND t.employee_id=$%d", len(args))
	}
	if !canViewAllTasks(p) {
		if p.EmployeeID == "" {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "当前账号未绑定员工，不能查看生产任务")
		}
		args = append(args, p.EmployeeID)
		where += fmt.Sprintf(" AND t.employee_id=$%d", len(args))
	}
	rows, err := api.DB.Query(r.Context(), taskSelect()+" WHERE "+where+" ORDER BY t.id DESC LIMIT 200", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		item, err := scanTask(rows)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	// 工序父任务本身不直接报工，按子任务汇总已报、未审批和有效良品数量。
	type childSum struct{ reported, approved, pending int64 }
	sums := map[string]*childSum{}
	for _, item := range items {
		parentID, _ := item["parent_task_id"].(*string)
		if parentID == nil {
			continue
		}
		entry := sums[*parentID]
		if entry == nil {
			entry = &childSum{}
			sums[*parentID] = entry
		}
		entry.reported += item["reported_qty"].(int64)
		entry.approved += item["approved_good_qty"].(int64)
		entry.pending += item["pending_qty"].(int64)
	}
	for _, item := range items {
		parentID, _ := item["parent_task_id"].(*string)
		if parentID != nil {
			continue
		}
		entry := sums[item["id"].(string)]
		if entry == nil {
			continue
		}
		planned := item["planned_qty"].(int64)
		item["reported_qty"] = entry.reported
		item["approved_good_qty"] = entry.approved
		item["pending_qty"] = entry.pending
		item["remaining_qty"] = planned - entry.reported
		if entry.approved >= planned {
			item["status"] = "completed"
		} else if entry.reported > 0 {
			item["status"] = "in_progress"
		}
	}
	for _, item := range items {
		planned := item["planned_qty"].(int64)
		approved := item["approved_good_qty"].(int64)
		if approved >= planned {
			item["status"] = "completed"
		} else if item["reported_qty"].(int64) > 0 {
			item["status"] = "in_progress"
		}
	}
	return httpapi.JSON(w, 200, map[string]any{"items": items, "has_more": false})
}
func getTask(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	item, err := scanTask(api.DB.QueryRow(r.Context(), taskSelect()+" WHERE t.tenant_id=$1 AND t.id=$2", p.TenantID, r.PathValue("id")))
	if err == pgx.ErrNoRows {
		return httpapi.Error(404, "NOT_FOUND", "任务不存在")
	}
	if err != nil {
		return err
	}
	if !canViewAllTasks(p) {
		employeeID, _ := item["employee_id"].(*string)
		if employeeID == nil || *employeeID != p.EmployeeID {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "无权查看该任务")
		}
	}
	return httpapi.JSON(w, 200, item)
}

type assignmentInput struct {
	EmployeeID     string  `json:"employee_id"`
	PlannedQty     int64   `json:"planned_qty"`
	PlannedStartAt *int64  `json:"planned_start_at"`
	PlannedEndAt   *int64  `json:"planned_end_at"`
	Remark         *string `json:"remark"`
}

func assignTask(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		Version      int64             `json:"version"`
		DepartmentID *string           `json:"department_id"`
		Assignments  []assignmentInput `json:"assignments"`
		Remark       string            `json:"remark"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if len(in.Assignments) == 0 {
		return httpapi.Error(400, "VALIDATION_ERROR", "至少选择一名员工")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var planned, version int64
	var parentStatus string
	if err = tx.QueryRow(r.Context(), "SELECT planned_qty,version,status FROM production_task WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&planned, &version, &parentStatus); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "任务不存在")
	}
	if version != in.Version {
		return httpapi.Error(409, "VERSION_CONFLICT", "任务已变更")
	}
	if parentStatus == "completed" || parentStatus == "cancelled" {
		return httpapi.Error(409, "INVALID_STATE", "已完成或取消任务不能派工")
	}
	var used int64
	if err = tx.QueryRow(r.Context(), "SELECT COALESCE(SUM(planned_qty),0) FROM production_task WHERE tenant_id=$1 AND parent_task_id=$2", p.TenantID, r.PathValue("id")).Scan(&used); err != nil {
		return err
	}
	remaining := planned - used
	var requested int64
	for _, a := range in.Assignments {
		requested += a.PlannedQty
		if a.EmployeeID == "" || a.PlannedQty <= 0 {
			return httpapi.Error(400, "INVALID_ASSIGNMENT", "人员和计划量不合法")
		}
	}
	if requested > remaining {
		err := httpapi.Error(400, "QUANTITY_EXCEEDED", "派工数量超过父任务余额")
		err.Details = map[string]any{"remaining": remaining, "requested": requested}
		return err
	}
	taskIDs := []string{}
	for _, a := range in.Assignments {
		var employeeName string
		if err = tx.QueryRow(r.Context(), "SELECT name FROM employee WHERE tenant_id=$1 AND id=$2 AND status='active'", p.TenantID, a.EmployeeID).Scan(&employeeName); err != nil {
			return httpapi.Error(400, "INVALID_EMPLOYEE", "员工不存在或已离职")
		}
		id, now := platform.ID(), platform.Now()
		_, err = tx.Exec(r.Context(), `INSERT INTO production_task(tenant_id,id,work_order_id,product_process_id,parent_task_id,department_id,employee_id,planned_qty,unit_price,report_ratio,standard_work_seconds,status,planned_start_at,planned_end_at,remark,created_at,created_by,created_node,updated_at,updated_by,updated_node) SELECT tenant_id,$1,work_order_id,product_process_id,$2,$3,$4,$5,unit_price,report_ratio,standard_work_seconds,'assigned',$6,$7,$8,$9,$10,$11,$9,$10,$11 FROM production_task WHERE tenant_id=$12 AND id=$2`, id, r.PathValue("id"), in.DepartmentID, a.EmployeeID, a.PlannedQty, a.PlannedStartAt, a.PlannedEndAt, a.Remark, now, p.UserID, api.Config.NodeID, p.TenantID)
		if err != nil {
			return err
		}
		taskIDs = append(taskIDs, id)
		if _, err = tx.Exec(r.Context(), `INSERT INTO task_transfer(tenant_id,id,source_task_id,target_task_id,to_employee_id,transfer_qty,reason,operator_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, p.TenantID, platform.ID(), r.PathValue("id"), id, a.EmployeeID, a.PlannedQty, "派工", p.UserID, now); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(r.Context(), "UPDATE production_task SET status='assigned',version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5", platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
		return err
	}
	response := map[string]any{"parent_task_id": r.PathValue("id"), "parent_remaining_qty": remaining - requested, "task_ids": taskIDs, "version": version + 1}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "assign", "production_task", r.PathValue("id"), nil, response, in.Remark); err != nil {
		return err
	}
	if err = httpapi.Outbox(r.Context(), tx, p.TenantID, api.Config.NodeID, "production_task", r.PathValue("id"), "task.assigned", response); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 200, response)
}
