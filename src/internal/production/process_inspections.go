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

func registerProcessInspections(api *httpapi.API) {
	api.Handle("GET /api/v1/process-inspections", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listProcessInspections(api, w, r, p)
	})
	api.Handle("POST /api/v1/process-inspections", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createProcessInspection(api, w, r, p)
	})
	api.Handle("POST /api/v1/process-inspections/{id}/confirm", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return confirmProcessInspection(api, w, r, p)
	})
	api.Handle("POST /api/v1/process-inspections/{id}/rework", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createProcessRework(api, w, r, p)
	})
}

func listProcessInspections(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	rows, err := api.DB.Query(r.Context(), `SELECT pi.id,pi.inspection_no,pi.work_order_id,wo.work_order_code,pi.project_id,pj.project_code,pi.product_id,pr.product_code,pr.name,
		pi.process_snapshot_id,pi.process_code,pi.process_name,pi.bundle_id,b.bundle_no,b.color,b.size,
		pi.inspected_qty,pi.passed_qty,pi.rework_qty,pi.scrapped_qty,pi.inspector_employee_id,COALESCE(e.name,''),pi.status,COALESCE(pi.remark,''),pi.version,pi.created_at
		FROM process_inspection pi
		JOIN work_order wo ON wo.tenant_id=pi.tenant_id AND wo.id=pi.work_order_id
		JOIN project pj ON pj.tenant_id=pi.tenant_id AND pj.id=pi.project_id
		JOIN product pr ON pr.tenant_id=pi.tenant_id AND pr.id=pi.product_id
		JOIN bundle b ON b.tenant_id=pi.tenant_id AND b.id=pi.bundle_id
		LEFT JOIN employee e ON e.tenant_id=pi.tenant_id AND e.id=pi.inspector_employee_id
		WHERE pi.tenant_id=$1 ORDER BY pi.created_at DESC LIMIT 200`, p.TenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, no, workOrderID, workOrderCode, projectID, projectCode, productID, productCode, productName string
		var processSnapshotID, processCode, processName, bundleID, bundleNo, color, size, inspectorID, inspectorName, status, remark string
		var inspected, passed, rework, scrapped, version, createdAt int64
		if err := rows.Scan(&id, &no, &workOrderID, &workOrderCode, &projectID, &projectCode, &productID, &productCode, &productName,
			&processSnapshotID, &processCode, &processName, &bundleID, &bundleNo, &color, &size,
			&inspected, &passed, &rework, &scrapped, &inspectorID, &inspectorName, &status, &remark, &version, &createdAt); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"id": id, "inspection_no": no, "work_order_id": workOrderID, "work_order_code": workOrderCode,
			"project_id": projectID, "project_code": projectCode, "product_id": productID, "product_code": productCode, "product_name": productName,
			"process_snapshot_id": processSnapshotID, "process_code": processCode, "process_name": processName,
			"bundle_id": bundleID, "bundle_no": bundleNo, "color": color, "size": size,
			"inspected_qty": inspected, "passed_qty": passed, "rework_qty": rework, "scrapped_qty": scrapped,
			"inspector_employee_id": inspectorID, "inspector_name": inspectorName,
			"status": status, "remark": remark, "version": version, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func createProcessInspection(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		InspectionNo         string `json:"inspection_no"`
		WorkOrderID          string `json:"work_order_id"`
		ProcessSnapshotID    string `json:"process_snapshot_id"`
		BundleID             string `json:"bundle_id"`
		InspectedQty         int64  `json:"inspected_qty"`
		PassedQty            int64  `json:"passed_qty"`
		ReworkQty            int64  `json:"rework_qty"`
		ScrappedQty          int64  `json:"scrapped_qty"`
		InspectorEmployeeID  string `json:"inspector_employee_id"`
		Remark               string `json:"remark"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.WorkOrderID == "" || in.ProcessSnapshotID == "" || in.BundleID == "" || in.InspectedQty <= 0 || in.InspectedQty != in.PassedQty+in.ReworkQty+in.ScrappedQty {
		return httpapi.Error(400, "VALIDATION_ERROR", "工单、工序、扎和巡检数量必填，且巡检数=合格+返工+报废")
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
	var projectID, productID, processCode, processName string
	if err := tx.QueryRow(r.Context(), `SELECT w.project_id,w.product_id,s.process_code,s.process_name FROM work_order w JOIN work_order_process_snapshot s ON s.tenant_id=w.tenant_id AND s.work_order_id=w.id AND s.id=$3 WHERE w.tenant_id=$1 AND w.id=$2`, p.TenantID, in.WorkOrderID, in.ProcessSnapshotID).Scan(&projectID, &productID, &processCode, &processName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "工单或工序不存在")
		}
		return err
	}
	var bundleWorkOrder string
	if err := tx.QueryRow(r.Context(), "SELECT work_order_id FROM bundle WHERE tenant_id=$1 AND id=$2", p.TenantID, in.BundleID).Scan(&bundleWorkOrder); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "扎不存在")
	}
	if bundleWorkOrder != in.WorkOrderID {
		return httpapi.Error(400, "BUNDLE_NOT_IN_WORK_ORDER", "扎不属于该工单")
	}
	id, now := platform.ID(), platform.Now()
	inspectionNo := strings.TrimSpace(in.InspectionNo)
	if inspectionNo == "" {
		inspectionNo = "PI-" + platform.ID()[:12]
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO process_inspection(tenant_id,id,inspection_no,work_order_id,project_id,product_id,process_snapshot_id,process_code,process_name,bundle_id,inspected_qty,passed_qty,rework_qty,scrapped_qty,inspector_employee_id,inspection_date,status,remark,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,'pending',$17,$16,$18,$19,$16,$18,$19,1)`, p.TenantID, id, inspectionNo, in.WorkOrderID, projectID, productID, in.ProcessSnapshotID, processCode, processName, in.BundleID, in.InspectedQty, in.PassedQty, in.ReworkQty, in.ScrappedQty, in.InspectorEmployeeID, now, optionalText(&in.Remark), p.UserID, api.Config.NodeID); err != nil {
		if isUniqueViolation(err) {
			return httpapi.Error(409, "DUPLICATE_INSPECTION", "巡检单号已存在")
		}
		return err
	}
	if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create", "process_inspection", id, nil, in, ""); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "inspection_no": inspectionNo, "status": "pending", "version": 1})
}

func confirmProcessInspection(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var status string
	var version int64
	if err := tx.QueryRow(r.Context(), "SELECT status,version FROM process_inspection WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &version); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "巡检单不存在")
	}
	if status != "pending" || (in.Version != 0 && in.Version != version) {
		return httpapi.Error(409, "INVALID_STATE", "巡检单已确认或版本不一致")
	}
	now := platform.Now()
	if _, err := tx.Exec(r.Context(), "UPDATE process_inspection SET status='confirmed',version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5", now, p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
		return err
	}
	if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "confirm", "process_inspection", r.PathValue("id"), map[string]any{"status": status}, map[string]any{"status": "confirmed"}, ""); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "status": "confirmed", "version": version + 1})
}

func createProcessRework(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		ReworkNo   string  `json:"rework_no"`
		EmployeeID *string `json:"employee_id"`
		Qty        int64   `json:"qty"`
		DueAt      *int64  `json:"due_at"`
		Reason     string  `json:"reason"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.Qty <= 0 || strings.TrimSpace(in.Reason) == "" {
		return httpapi.Error(400, "VALIDATION_ERROR", "返工数量和原因必填")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var inspectionNo, workOrderID, bundleID, processSnapshotID, processCode, processName, status string
	var reworkQty int64
	if err := tx.QueryRow(r.Context(), `SELECT inspection_no,work_order_id,bundle_id,process_snapshot_id,process_code,process_name,rework_qty,status FROM process_inspection WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, p.TenantID, r.PathValue("id")).Scan(&inspectionNo, &workOrderID, &bundleID, &processSnapshotID, &processCode, &processName, &reworkQty, &status); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "巡检单不存在")
	}
	if status != "confirmed" {
		return httpapi.Error(409, "INVALID_STATE", "巡检确认后才能创建返工单")
	}
	var already int64
	if err := tx.QueryRow(r.Context(), "SELECT COALESCE(SUM(qty),0) FROM rework_order WHERE tenant_id=$1 AND process_inspection_id=$2 AND status<>'cancelled'", p.TenantID, r.PathValue("id")).Scan(&already); err != nil {
		return err
	}
	if already+in.Qty > reworkQty {
		return httpapi.Error(400, "QUANTITY_EXCEEDED", "返工数量超过巡检返工数")
	}
	id := platform.ID()
	reworkNo := strings.TrimSpace(in.ReworkNo)
	if reworkNo == "" {
		reworkNo = "RW-" + platform.ID()[:12]
	}
	now := platform.Now()
	if _, err := tx.Exec(r.Context(), `INSERT INTO rework_order(tenant_id,id,rework_no,inspection_id,process_inspection_id,work_order_id,bundle_id,process_snapshot_id,process_code,process_name,employee_id,qty,due_at,status,reason,created_at,version,updated_at,source_type)
		VALUES($1,$2,$3,NULL,$4,$5,$6,$7,$8,$9,$10,$11,$12,'pending',$13,$14,1,$14,'process_inspection')`, p.TenantID, id, reworkNo, r.PathValue("id"), workOrderID, bundleID, processSnapshotID, processCode, processName, in.EmployeeID, in.Qty, in.DueAt, strings.TrimSpace(in.Reason), now); err != nil {
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
	return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "rework_no": reworkNo, "inspection_no": inspectionNo, "process_code": processCode, "process_name": processName, "status": "pending", "version": 1})
}
