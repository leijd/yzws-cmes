package production

import (
	"errors"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerRework(api *httpapi.API) {
	api.Handle("GET /api/v1/rework-orders", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT rw.id,rw.rework_no,COALESCE(rw.inspection_id,rw.process_inspection_id),COALESCE(fi.inspection_no,pi.inspection_no),COALESCE(rw.work_order_id,fi.work_order_id),wo.work_order_code,pj.project_code,pr.product_code,pr.name,b.bundle_no,b.color,b.size,
			rw.qty,rw.reported_qty,COALESCE(rw.process_code,''),COALESCE(rw.process_name,''),COALESCE(rw.employee_id,''),COALESCE(e.name,''),rw.status,COALESCE(rw.due_at,0),rw.reason,rw.version,rw.created_at,rw.source_type
			FROM rework_order rw
			LEFT JOIN final_inspection fi ON fi.tenant_id=rw.tenant_id AND fi.id=rw.inspection_id
			LEFT JOIN process_inspection pi ON pi.tenant_id=rw.tenant_id AND pi.id=rw.process_inspection_id
			JOIN work_order wo ON wo.tenant_id=rw.tenant_id AND wo.id=COALESCE(rw.work_order_id,fi.work_order_id)
			JOIN project pj ON pj.tenant_id=wo.tenant_id AND pj.id=wo.project_id
			JOIN product pr ON pr.tenant_id=pj.tenant_id AND pr.id=pj.product_id
			JOIN bundle b ON b.tenant_id=rw.tenant_id AND b.id=COALESCE(rw.bundle_id,fi.bundle_id)
			LEFT JOIN employee e ON e.tenant_id=rw.tenant_id AND e.id=rw.employee_id
			WHERE rw.tenant_id=$1 ORDER BY rw.created_at DESC`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, inspectionID, inspectionNo, workOrderID, workOrderCode, projectCode, productCode, productName, bundleNo, color, size, processCode, processName, employeeID, employeeName, status, reason, sourceType string
			var qty, reportedQty, dueAt, version, createdAt int64
			if err := rows.Scan(&id, &no, &inspectionID, &inspectionNo, &workOrderID, &workOrderCode, &projectCode, &productCode, &productName, &bundleNo, &color, &size, &qty, &reportedQty, &processCode, &processName, &employeeID, &employeeName, &status, &dueAt, &reason, &version, &createdAt, &sourceType); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "rework_no": no, "inspection_id": inspectionID, "inspection_no": inspectionNo, "work_order_id": workOrderID, "work_order_code": workOrderCode, "project_code": projectCode, "product_code": productCode, "product_name": productName, "bundle_no": bundleNo, "color": color, "size": size, "qty": qty, "reported_qty": reportedQty, "process_code": processCode, "process_name": processName, "employee_id": employeeID, "employee_name": employeeName, "status": status, "due_at": dueAt, "reason": reason, "version": version, "created_at": createdAt, "source_type": sourceType})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/rework-orders", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ReworkNo                string  `json:"rework_no"`
			InspectionID            string  `json:"inspection_id"`
			DefectRecordID          *string `json:"defect_record_id"`
			TaskID                  *string `json:"task_id"`
			ResponsibilityProcessID *string `json:"responsibility_process_id"`
			EmployeeID              *string `json:"employee_id"`
			Qty                     int64   `json:"qty"`
			DueAt                   *int64  `json:"due_at"`
			Reason                  string  `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.InspectionID == "" || in.Qty <= 0 || strings.TrimSpace(in.Reason) == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "总检单、返工数量和返工原因必填")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var reworkQty int64
		var status string
		if err := tx.QueryRow(r.Context(), "SELECT rework_qty,status FROM final_inspection WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, in.InspectionID).Scan(&reworkQty, &status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "总检记录不存在")
			}
			return err
		}
		if status != "confirmed" {
			return httpapi.Error(409, "INVALID_STATE", "总检确认后才能创建返工单")
		}
		var already int64
		if err := tx.QueryRow(r.Context(), "SELECT COALESCE(SUM(qty),0) FROM rework_order WHERE tenant_id=$1 AND inspection_id=$2 AND status<>'cancelled'", p.TenantID, in.InspectionID).Scan(&already); err != nil {
			return err
		}
		if already+in.Qty > reworkQty {
			return httpapi.Error(400, "QUANTITY_EXCEEDED", "返工数量超过总检返工数")
		}
		id := platform.ID()
		reworkNo := strings.TrimSpace(in.ReworkNo)
		if reworkNo == "" {
			reworkNo = "RW-" + platform.ID()[:12]
		}
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), `INSERT INTO rework_order(tenant_id,id,rework_no,inspection_id,defect_record_id,task_id,responsibility_process_id,employee_id,qty,due_at,status,reason,created_at,version,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',$11,$12,1,$12)`, p.TenantID, id, reworkNo, in.InspectionID, in.DefectRecordID, in.TaskID, in.ResponsibilityProcessID, in.EmployeeID, in.Qty, in.DueAt, strings.TrimSpace(in.Reason), now); err != nil {
			if isUniqueViolation(err) {
				return httpapi.Error(409, "DUPLICATE_REWORK_NO", "返工单号已存在")
			}
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create", "rework_order", id, nil, in, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "rework_no": reworkNo, "status": "pending", "version": 1})
	})

	api.Handle("GET /api/v1/rework-orders/{id}", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var order map[string]any
		if err := api.DB.QueryRow(r.Context(), "SELECT row_to_json(t) FROM (SELECT * FROM rework_order WHERE tenant_id=$1 AND id=$2) t", p.TenantID, r.PathValue("id")).Scan(&order); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "返工单不存在")
			}
			return err
		}
		rows, err := api.DB.Query(r.Context(), `SELECT id,inspected_qty,passed_qty,rework_qty,scrapped_qty,inspector_employee_id,COALESCE(remark,''),created_at FROM reinspection WHERE tenant_id=$1 AND rework_order_id=$2 ORDER BY created_at`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		reinspections := []map[string]any{}
		for rows.Next() {
			var id, inspector, remark string
			var inspected, passed, rework, scrapped, createdAt int64
			if err := rows.Scan(&id, &inspected, &passed, &rework, &scrapped, &inspector, &remark, &createdAt); err != nil {
				return err
			}
			reinspections = append(reinspections, map[string]any{"id": id, "inspected_qty": inspected, "passed_qty": passed, "rework_qty": rework, "scrapped_qty": scrapped, "inspector_employee_id": inspector, "remark": remark, "created_at": createdAt})
		}
		return httpapi.JSON(w, 200, map[string]any{"order": order, "reinspections": reinspections})
	})

	api.Handle("POST /api/v1/rework-orders/{id}/assign", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			EmployeeID string  `json:"employee_id"`
			TaskID     *string `json:"task_id"`
			DueAt      *int64  `json:"due_at"`
			Reason     string  `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.EmployeeID == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "请选择返工人员")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var status string
		var version int64
		if err := tx.QueryRow(r.Context(), "SELECT status,version FROM rework_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &version); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "返工单不存在")
		}
		if status == "completed" || status == "cancelled" {
			return httpapi.Error(409, "INVALID_STATE", "已完成或已取消的返工单不能分派")
		}
		if _, err := tx.Exec(r.Context(), `UPDATE rework_order SET employee_id=$1,task_id=COALESCE($2,task_id),due_at=COALESCE($3,due_at),status='assigned',version=version+1,updated_at=$4 WHERE tenant_id=$5 AND id=$6`, in.EmployeeID, in.TaskID, in.DueAt, platform.Now(), p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "assigned", "version": version + 1})
	})

	api.Handle("POST /api/v1/rework-orders/{id}/report", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Qty    int64  `json:"qty"`
			Remark string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Qty <= 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "返工报工数量必须大于零")
		}
		if p.EmployeeID == "" {
			return httpapi.Error(400, "NO_EMPLOYEE_PROFILE", "当前账号未关联员工档案")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var status string
		var orderQty, reportedQty, version int64
		if err := tx.QueryRow(r.Context(), "SELECT status,qty,reported_qty,version FROM rework_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &orderQty, &reportedQty, &version); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "返工单不存在")
		}
		if status == "completed" || status == "cancelled" {
			return httpapi.Error(409, "INVALID_STATE", "已完成或已取消的返工单不能报工")
		}
		if reportedQty+in.Qty > orderQty {
			return httpapi.Error(400, "QUANTITY_EXCEEDED", "返工报工数量超过返工单数量")
		}
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), "UPDATE rework_order SET reported_qty=reported_qty+$1,reported_at=$2,reported_by=$3,status='reported',version=version+1,updated_at=$2 WHERE tenant_id=$4 AND id=$5", in.Qty, now, p.EmployeeID, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "report", "rework_order", r.PathValue("id"), map[string]any{"reported_qty": reportedQty}, map[string]any{"reported_qty": reportedQty + in.Qty}, in.Remark); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "reported_qty": reportedQty + in.Qty, "status": "reported", "version": version + 1})
	})

	api.Handle("POST /api/v1/rework-orders/{id}/reinspections", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			InspectedQty        int64  `json:"inspected_qty"`
			PassedQty           int64  `json:"passed_qty"`
			ReworkQty           int64  `json:"rework_qty"`
			ScrappedQty         int64  `json:"scrapped_qty"`
			InspectorEmployeeID string `json:"inspector_employee_id"`
			Remark              string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.InspectedQty <= 0 || in.PassedQty < 0 || in.ReworkQty < 0 || in.ScrappedQty < 0 || in.InspectedQty != in.PassedQty+in.ReworkQty+in.ScrappedQty {
			return httpapi.Error(400, "INSPECTION_QUANTITY_MISMATCH", "复检数量必须等于合格+返工+报废")
		}
		if in.InspectorEmployeeID == "" {
			in.InspectorEmployeeID = p.EmployeeID
		}
		if in.InspectorEmployeeID == "" {
			return httpapi.Error(400, "NO_EMPLOYEE_PROFILE", "当前账号未关联员工档案")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var inspectionID *string
		var status string
		var orderQty, reportedQty int64
		if err := tx.QueryRow(r.Context(), "SELECT inspection_id,status,qty,reported_qty FROM rework_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&inspectionID, &status, &orderQty, &reportedQty); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "返工单不存在")
		}
		if status == "completed" || status == "cancelled" {
			return httpapi.Error(409, "INVALID_STATE", "该返工单已结束")
		}
		if reportedQty <= 0 {
			return httpapi.Error(409, "REPORT_REQUIRED", "请先完成返工报工，再进行复检")
		}
		var doneQty int64
		if err := tx.QueryRow(r.Context(), "SELECT COALESCE(SUM(inspected_qty),0) FROM reinspection WHERE tenant_id=$1 AND rework_order_id=$2", p.TenantID, r.PathValue("id")).Scan(&doneQty); err != nil {
			return err
		}
		if doneQty+in.InspectedQty > reportedQty {
			return httpapi.Error(400, "QUANTITY_EXCEEDED", "复检数量超过已报工数量")
		}
		id := platform.ID()
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), `INSERT INTO reinspection(tenant_id,id,rework_order_id,inspected_qty,passed_qty,rework_qty,scrapped_qty,inspector_employee_id,inspection_id,remark,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`, p.TenantID, id, r.PathValue("id"), in.InspectedQty, in.PassedQty, in.ReworkQty, in.ScrappedQty, in.InspectorEmployeeID, inspectionID, optionalText(&in.Remark), now); err != nil {
			return err
		}
		if in.PassedQty > 0 && inspectionID != nil {
			var inspectionNo, workOrderID, bundleID string
			var originalInspected int64
			if err := tx.QueryRow(r.Context(), "SELECT inspection_no,work_order_id,bundle_id,inspected_qty FROM final_inspection WHERE tenant_id=$1 AND id=$2", p.TenantID, *inspectionID).Scan(&inspectionNo, &workOrderID, &bundleID, &originalInspected); err != nil {
				return err
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO final_inspection(tenant_id,id,inspection_no,work_order_id,bundle_id,inspected_qty,passed_qty,rework_qty,scrapped_qty,inspector_employee_id,inspection_date,handling_result,status,original_inspection_id,remark,created_at,created_by,created_node,version,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,$6,0,0,$7,$8,'rework_passed','confirmed',$9,$10,$8,$11,$12,1,$8)`, p.TenantID, platform.ID(), inspectionNo+"-R"+platform.ID()[:6], workOrderID, bundleID, in.PassedQty, in.InspectorEmployeeID, now, inspectionID, optionalText(&in.Remark), p.UserID, api.Config.NodeID); err != nil {
				return err
			}
		}
		nextStatus := status
		if doneQty+in.InspectedQty >= orderQty {
			nextStatus = "completed"
		} else {
			nextStatus = "assigned"
		}
		if _, err := tx.Exec(r.Context(), "UPDATE rework_order SET status=$1,version=version+1,updated_at=$2 WHERE tenant_id=$3 AND id=$4", nextStatus, now, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "reinspect", "rework_order", r.PathValue("id"), nil, in, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "passed_qty": in.PassedQty, "status": nextStatus})
	})
}
