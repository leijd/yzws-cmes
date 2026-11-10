package production

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerAssignments(api *httpapi.API) {
	api.Handle("GET /api/v1/production-tasks/{id}/bundle-assignments", "task:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT a.id,a.bundle_id,b.bundle_no,b.color,b.size,a.employee_id,e.name,a.planned_qty,a.status,a.version FROM task_bundle_assignment a JOIN bundle b ON b.tenant_id=a.tenant_id AND b.id=a.bundle_id JOIN employee e ON e.tenant_id=a.tenant_id AND e.id=a.employee_id WHERE a.tenant_id=$1 AND a.task_id=$2 ORDER BY b.bundle_no::integer,e.name,a.id`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, bundleID, bundleNo, color, size, employeeID, employeeName, status string
			var planned, version int64
			if err := rows.Scan(&id, &bundleID, &bundleNo, &color, &size, &employeeID, &employeeName, &planned, &status, &version); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "bundle_id": bundleID, "bundle_no": bundleNo, "color": color, "size": size, "employee_id": employeeID, "employee_name": employeeName, "planned_qty": planned, "status": status, "version": version})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/production-tasks/{id}/bundle-assignments", "task:assign", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version int64 `json:"version"`
			Items   []struct {
				BundleID   string `json:"bundle_id"`
				EmployeeID string `json:"employee_id"`
				PlannedQty int64  `json:"planned_qty"`
			} `json:"items"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if len(in.Items) == 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "派工明细不能为空")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		taskID := r.PathValue("id")
		var workOrderID, taskStatus, reportMode, workOrderStatus string
		var version int64
		err = tx.QueryRow(r.Context(), `SELECT t.work_order_id,t.status,t.version,w.report_mode,w.status FROM production_task t JOIN work_order w ON w.tenant_id=t.tenant_id AND w.id=t.work_order_id WHERE t.tenant_id=$1 AND t.id=$2 FOR UPDATE OF t`, p.TenantID, taskID).Scan(&workOrderID, &taskStatus, &version, &reportMode, &workOrderStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "任务不存在")
		}
		if err != nil {
			return err
		}
		if in.Version != version {
			return httpapi.Error(409, "VERSION_CONFLICT", "任务已变更，请刷新后重试")
		}
		if reportMode != "A" {
			return httpapi.Error(409, "INVALID_STATE", "工单为模式 B，不支持扎级派工")
		}
		if workOrderStatus != "in_production" {
			return httpapi.Error(409, "INVALID_STATE", "工单未下发或已结束，不能派工")
		}
		if taskStatus == "completed" || taskStatus == "cancelled" {
			return httpapi.Error(409, "INVALID_STATE", "任务已结束，不能派工")
		}
		now := platform.Now()
		allocated := map[string]int64{}
		created := []map[string]any{}
		for _, item := range in.Items {
			item.BundleID = strings.TrimSpace(item.BundleID)
			item.EmployeeID = strings.TrimSpace(item.EmployeeID)
			if item.BundleID == "" || item.EmployeeID == "" || item.PlannedQty <= 0 {
				return httpapi.Error(400, "VALIDATION_ERROR", "扎、人员和计划量必填，计划量须大于零")
			}
			var bundleWorkOrder string
			var bundleQty int64
			err = tx.QueryRow(r.Context(), "SELECT work_order_id,current_qty FROM bundle WHERE tenant_id=$1 AND id=$2", p.TenantID, item.BundleID).Scan(&bundleWorkOrder, &bundleQty)
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(400, "INVALID_BUNDLE", "扎不存在")
			}
			if err != nil {
				return err
			}
			if bundleWorkOrder != workOrderID {
				return httpapi.Error(400, "BUNDLE_NOT_IN_TASK", "扎不属于该任务所在工单")
			}
			var employeeActive bool
			if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND id=$2 AND status='active')", p.TenantID, item.EmployeeID).Scan(&employeeActive); err != nil {
				return err
			}
			if !employeeActive {
				return httpapi.Error(400, "INVALID_EMPLOYEE", "员工不存在或已离职")
			}
			var duplicate bool
			if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM task_bundle_assignment WHERE tenant_id=$1 AND task_id=$2 AND bundle_id=$3 AND employee_id=$4 AND status='active')", p.TenantID, taskID, item.BundleID, item.EmployeeID).Scan(&duplicate); err != nil {
				return err
			}
			if duplicate {
				return httpapi.Error(409, "DUPLICATE_BUNDLE_ASSIGNMENT", "该员工已领取此扎的本工序")
			}
			var assignedQty int64
			if err := tx.QueryRow(r.Context(), "SELECT COALESCE(SUM(planned_qty),0) FROM task_bundle_assignment WHERE tenant_id=$1 AND task_id=$2 AND bundle_id=$3 AND status='active'", p.TenantID, taskID, item.BundleID).Scan(&assignedQty); err != nil {
				return err
			}
			total := assignedQty + allocated[item.BundleID] + item.PlannedQty
			if total > bundleQty {
				appErr := httpapi.Error(400, "QUANTITY_EXCEEDED", "派工数量超过扎件数")
				appErr.Details = map[string]any{"bundle_id": item.BundleID, "bundle_qty": bundleQty, "assigned": assignedQty + allocated[item.BundleID], "requested": item.PlannedQty}
				return appErr
			}
			allocated[item.BundleID] += item.PlannedQty
			assignmentID := platform.ID()
			if _, err := tx.Exec(r.Context(), `INSERT INTO task_bundle_assignment(tenant_id,id,task_id,bundle_id,employee_id,planned_qty,assigned_by,assigned_at,status,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'active',$8,$7,$9,$8,$7,$9)`, p.TenantID, assignmentID, taskID, item.BundleID, item.EmployeeID, item.PlannedQty, p.UserID, now, api.Config.NodeID); err != nil {
				return err
			}
			created = append(created, map[string]any{"id": assignmentID, "bundle_id": item.BundleID, "employee_id": item.EmployeeID, "planned_qty": item.PlannedQty})
		}
		if _, err := tx.Exec(r.Context(), "UPDATE production_task SET version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5", now, p.UserID, api.Config.NodeID, p.TenantID, taskID); err != nil {
			return err
		}
		response := map[string]any{"task_id": taskID, "version": version + 1, "assignments": created}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "assign_bundles", "production_task", taskID, nil, response, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, response)
	})

	api.Handle("DELETE /api/v1/bundle-assignments/{id}", "task:assign", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rawVersion := strings.TrimSpace(r.URL.Query().Get("version"))
		if rawVersion == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "版本号必填")
		}
		version, err := strconv.ParseInt(rawVersion, 10, 64)
		if err != nil {
			return httpapi.Error(400, "VALIDATION_ERROR", "版本号格式不正确")
		}
		reason := strings.TrimSpace(r.URL.Query().Get("reason"))
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		id := r.PathValue("id")
		var taskID, bundleID, employeeID, status string
		var currentVersion int64
		err = tx.QueryRow(r.Context(), "SELECT task_id,bundle_id,employee_id,status,version FROM task_bundle_assignment WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, id).Scan(&taskID, &bundleID, &employeeID, &status, &currentVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "派工记录不存在")
		}
		if err != nil {
			return err
		}
		if version != currentVersion {
			return httpapi.Error(409, "VERSION_CONFLICT", "派工记录已变更，请刷新后重试")
		}
		if status != "active" {
			return httpapi.Error(409, "INVALID_STATE", "只有有效的派工可以撤回")
		}
		var hasReports bool
		if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM work_report WHERE tenant_id=$1 AND task_id=$2 AND bundle_id=$3 AND employee_id=$4 AND report_kind<>'reversal')", p.TenantID, taskID, bundleID, employeeID).Scan(&hasReports); err != nil {
			return err
		}
		if hasReports {
			appErr := httpapi.Error(409, "BUNDLE_ASSIGNMENT_IN_USE", "该员工已对此扎报工，不能撤回派工")
			appErr.Details = map[string]any{"task_id": taskID, "bundle_id": bundleID, "employee_id": employeeID}
			return appErr
		}
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), "UPDATE task_bundle_assignment SET status='revoked',revoked_by=$1,revoked_at=$2,revoked_reason=$3,version=version+1,updated_at=$2,updated_by=$1,updated_node=$4 WHERE tenant_id=$5 AND id=$6", p.UserID, now, reason, api.Config.NodeID, p.TenantID, id); err != nil {
			return err
		}
		response := map[string]any{"id": id, "status": "revoked", "version": currentVersion + 1}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "revoke_bundle_assignment", "task_bundle_assignment", id, map[string]any{"status": status}, response, reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
	})

	api.Handle("GET /api/v1/work-orders/{id}/assignable-bundles", "task:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID, r.PathValue("id")}
		where := "b.tenant_id=$1 AND b.work_order_id=$2 AND t.status<>'cancelled' AND t.parent_task_id IS NULL"
		if code := strings.TrimSpace(r.URL.Query().Get("process_code")); code != "" {
			args = append(args, code)
			where += fmt.Sprintf(" AND s.process_code=$%d", len(args))
		}
		rows, err := api.DB.Query(r.Context(), fmt.Sprintf(`SELECT b.id,b.bundle_no,b.color,b.size,b.current_qty,t.id,s.sequence_no,s.process_code,s.process_name,
			COALESCE((SELECT SUM(a.planned_qty) FROM task_bundle_assignment a WHERE a.tenant_id=b.tenant_id AND a.bundle_id=b.id AND a.task_id=t.id AND a.status='active'),0),
			COALESCE((SELECT SUM(wr.good_qty+wr.defect_qty) FROM work_report wr WHERE wr.tenant_id=b.tenant_id AND wr.bundle_id=b.id AND wr.task_id=t.id AND wr.report_kind<>'reversal' AND wr.review_status IN ('pending','approved') AND NOT EXISTS (SELECT 1 FROM work_report rv WHERE rv.tenant_id=wr.tenant_id AND rv.original_report_id=wr.id AND rv.report_kind='reversal')),0)
			FROM bundle b JOIN production_task t ON t.tenant_id=b.tenant_id AND t.work_order_id=b.work_order_id JOIN work_order_process_snapshot s ON s.tenant_id=t.tenant_id AND s.work_order_id=t.work_order_id AND s.product_process_id=t.product_process_id
			WHERE %s ORDER BY b.bundle_no::integer,s.sequence_no`, where), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var bundleID, bundleNo, color, size, taskID, processCode, processName string
			var bundleQty, assigned, reported int64
			var sequence int
			if err := rows.Scan(&bundleID, &bundleNo, &color, &size, &bundleQty, &taskID, &sequence, &processCode, &processName, &assigned, &reported); err != nil {
				return err
			}
			items = append(items, map[string]any{"bundle_id": bundleID, "bundle_no": bundleNo, "color": color, "size": size, "bundle_qty": bundleQty, "task_id": taskID, "sequence_no": sequence, "process_code": processCode, "process_name": processName, "assigned_qty": assigned, "reported_qty": reported, "assignable_qty": bundleQty - assigned})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
}
