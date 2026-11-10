package production

import (
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerQuality(api *httpapi.API) {
	api.Handle("GET /api/v1/bundles", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,work_order_id,bundle_no,COALESCE(bed_no,''),color,size,current_qty,status FROM bundle WHERE tenant_id=$1 ORDER BY work_order_id,bundle_no::integer`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, order, no, bed, color, size, status string
			var qty int64
			if err := rows.Scan(&id, &order, &no, &bed, &color, &size, &qty, &status); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "work_order_id": order, "bundle_no": no, "bed_no": bed, "color": color, "size": size, "current_qty": qty, "status": status})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("POST /api/v1/final-inspections", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createInspection(api, w, r, p)
	})
	api.Handle("GET /api/v1/final-inspections", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,inspection_no,work_order_id,bundle_id,inspected_qty,passed_qty,rework_qty,scrapped_qty,status,version FROM final_inspection WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 200`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, order, bundle, status string
			var inspected, passed, rework, scrapped, version int64
			if err := rows.Scan(&id, &no, &order, &bundle, &inspected, &passed, &rework, &scrapped, &status, &version); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "inspection_no": no, "work_order_id": order, "bundle_id": bundle, "inspected_qty": inspected, "passed_qty": passed, "rework_qty": rework, "scrapped_qty": scrapped, "status": status, "version": version})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("POST /api/v1/final-inspections/{id}/confirm", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version int64  `json:"version"`
			Remark  string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		result, err := api.DB.Exec(r.Context(), `UPDATE final_inspection SET status='confirmed',version=version+1 WHERE tenant_id=$1 AND id=$2 AND version=$3 AND status='pending'`, p.TenantID, r.PathValue("id"), in.Version)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(409, "INVALID_STATE", "总检已确认或版本不一致")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "confirmed", "version": in.Version + 1})
	})
	api.Handle("POST /api/v1/finished-receipts", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createFinishedReceipt(api, w, r, p)
	})
	api.Handle("GET /api/v1/finished-receipts", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT fr.id,fr.receipt_no,fr.work_order_id,fr.warehouse_id,fr.status,fr.version,COALESCE(SUM(l.qty),0) FROM finished_receipt fr LEFT JOIN finished_receipt_line l ON l.tenant_id=fr.tenant_id AND l.receipt_id=fr.id WHERE fr.tenant_id=$1 GROUP BY fr.tenant_id,fr.id ORDER BY fr.created_at DESC`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, order, warehouse, status string
			var version, qty int64
			if err := rows.Scan(&id, &no, &order, &warehouse, &status, &version, &qty); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "receipt_no": no, "work_order_id": order, "warehouse_id": warehouse, "status": status, "version": version, "qty": qty})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	for action, status := range map[string]string{"submit": "pending_quality", "confirm-quality": "pending_review"} {
		action, status := action, status
		api.Handle("POST /api/v1/finished-receipts/{id}/"+action, "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
			return transitionReceipt(api, w, r, p, action, status)
		})
	}
	api.Handle("POST /api/v1/finished-receipts/{id}/approve", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return transitionReceipt(api, w, r, p, "approve", "approved")
	})
	api.Handle("POST /api/v1/shipments", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createShipment(api, w, r, p)
	})
	api.Handle("GET /api/v1/shipments", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT s.id,s.shipment_no,s.project_id,s.status,s.version,COALESCE(SUM(l.qty),0) FROM shipment s LEFT JOIN shipment_line l ON l.tenant_id=s.tenant_id AND l.shipment_id=s.id WHERE s.tenant_id=$1 GROUP BY s.tenant_id,s.id ORDER BY s.created_at DESC`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, project, status string
			var version, qty int64
			if err := rows.Scan(&id, &no, &project, &status, &version, &qty); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "shipment_no": no, "project_id": project, "status": status, "version": version, "qty": qty})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("POST /api/v1/shipments/{id}/confirm", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return confirmShipment(api, w, r, p)
	})
}

type inspectionInput struct {
	InspectionNo        string  `json:"inspection_no"`
	WorkOrderID         string  `json:"work_order_id"`
	BundleID            string  `json:"bundle_id"`
	InspectedQty        int64   `json:"inspected_qty"`
	PassedQty           int64   `json:"passed_qty"`
	ReworkQty           int64   `json:"rework_qty"`
	ScrappedQty         int64   `json:"scrapped_qty"`
	InspectorEmployeeID string  `json:"inspector_employee_id"`
	InspectionDate      int64   `json:"inspection_date"`
	Remark              *string `json:"remark"`
}

func createInspection(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in inspectionInput
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.InspectorEmployeeID == "" {
		in.InspectorEmployeeID = p.EmployeeID
	}
	if in.InspectionDate == 0 {
		in.InspectionDate = platform.Now()
	}
	if in.InspectionNo == "" || in.WorkOrderID == "" || in.BundleID == "" || in.InspectedQty <= 0 || in.PassedQty < 0 || in.ReworkQty < 0 || in.ScrappedQty < 0 || in.InspectedQty != in.PassedQty+in.ReworkQty+in.ScrappedQty {
		return httpapi.Error(400, "INSPECTION_QUANTITY_MISMATCH", "总检数必须等于合格、返工和报废之和")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var bundleQty int64
	if err = tx.QueryRow(r.Context(), "SELECT current_qty FROM bundle WHERE tenant_id=$1 AND id=$2 AND work_order_id=$3 FOR UPDATE", p.TenantID, in.BundleID, in.WorkOrderID).Scan(&bundleQty); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "扎不存在或与工单不匹配")
	}
	var used int64
	if err = tx.QueryRow(r.Context(), "SELECT COALESCE(SUM(inspected_qty),0) FROM final_inspection WHERE tenant_id=$1 AND bundle_id=$2 AND status<>'reversed'", p.TenantID, in.BundleID).Scan(&used); err != nil {
		return err
	}
	if in.InspectedQty > bundleQty-used {
		appErr := httpapi.Error(400, "QUANTITY_EXCEEDED", "总检数量超过扎可检余额")
		appErr.Details = map[string]any{"bundle_qty": bundleQty, "inspected": used, "remaining": bundleQty - used}
		return appErr
	}
	id := platform.ID()
	_, err = tx.Exec(r.Context(), `INSERT INTO final_inspection(tenant_id,id,inspection_no,work_order_id,bundle_id,inspected_qty,passed_qty,rework_qty,scrapped_qty,inspector_employee_id,inspection_date,remark,created_at,created_by,created_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, p.TenantID, id, in.InspectionNo, in.WorkOrderID, in.BundleID, in.InspectedQty, in.PassedQty, in.ReworkQty, in.ScrappedQty, in.InspectorEmployeeID, in.InspectionDate, in.Remark, platform.Now(), p.UserID, api.Config.NodeID)
	if err != nil {
		return httpapi.Error(409, "DUPLICATE_INSPECTION", "总检单号已存在")
	}
	if err = refreshWorkOrderProgress(r.Context(), tx, p.TenantID, in.WorkOrderID); err != nil {
		return err
	}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "inspect", "final_inspection", id, nil, in, ""); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 201, map[string]any{"id": id, "status": "pending", "version": 1, "available_qty": bundleQty - used - in.InspectedQty})
}

type receiptLineInput struct {
	BundleID     string `json:"bundle_id"`
	InspectionID string `json:"inspection_id"`
	Qty          int64  `json:"qty"`
}

func createFinishedReceipt(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		ReceiptNo   string             `json:"receipt_no"`
		WorkOrderID string             `json:"work_order_id"`
		WarehouseID string             `json:"warehouse_id"`
		ReceiptDate int64              `json:"receipt_date"`
		Lines       []receiptLineInput `json:"lines"`
		Remark      *string            `json:"remark"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.ReceiptNo == "" || in.WorkOrderID == "" || in.WarehouseID == "" || len(in.Lines) == 0 {
		return httpapi.Error(400, "VALIDATION_ERROR", "入库单号、工单、仓库和明细必填")
	}
	if in.ReceiptDate == 0 {
		in.ReceiptDate = platform.Now()
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	id := platform.ID()
	if _, err = tx.Exec(r.Context(), `INSERT INTO finished_receipt(tenant_id,id,receipt_no,work_order_id,warehouse_id,receipt_date,created_employee_id,remark,created_at,created_by,created_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, p.TenantID, id, in.ReceiptNo, in.WorkOrderID, in.WarehouseID, in.ReceiptDate, p.EmployeeID, in.Remark, platform.Now(), p.UserID, api.Config.NodeID); err != nil {
		return httpapi.Error(409, "DUPLICATE_RECEIPT", "入库单号已存在")
	}
	for _, line := range in.Lines {
		if line.Qty <= 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "入库数量必须大于零")
		}
		var color, size string
		var passed int64
		err = tx.QueryRow(r.Context(), `SELECT b.color,b.size,fi.passed_qty FROM final_inspection fi JOIN bundle b ON b.tenant_id=fi.tenant_id AND b.id=fi.bundle_id WHERE fi.tenant_id=$1 AND fi.id=$2 AND fi.bundle_id=$3 AND fi.work_order_id=$4 AND fi.status='confirmed' FOR SHARE OF fi`, p.TenantID, line.InspectionID, line.BundleID, in.WorkOrderID).Scan(&color, &size, &passed)
		if err != nil {
			return httpapi.Error(400, "INVALID_INSPECTION", "总检不存在、未确认或与扎不匹配")
		}
		var received int64
		if err = tx.QueryRow(r.Context(), `SELECT COALESCE(SUM(l.qty),0) FROM finished_receipt_line l JOIN finished_receipt fr ON fr.tenant_id=l.tenant_id AND fr.id=l.receipt_id WHERE l.tenant_id=$1 AND l.bundle_id=$2 AND fr.status<>'reversed'`, p.TenantID, line.BundleID).Scan(&received); err != nil {
			return err
		}
		available := passed - received
		if line.Qty > available {
			appErr := httpapi.Error(400, "QUANTITY_EXCEEDED", "入库数量超过总检合格可用余额")
			appErr.Details = map[string]any{"bundle_id": line.BundleID, "available": available, "requested": line.Qty}
			return appErr
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO finished_receipt_line(tenant_id,id,receipt_id,bundle_id,inspection_id,color,size,qty,available_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, p.TenantID, platform.ID(), id, line.BundleID, line.InspectionID, color, size, line.Qty, available); err != nil {
			return err
		}
	}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create_receipt", "finished_receipt", id, nil, in, ""); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 201, map[string]any{"id": id, "status": "draft", "version": 1})
}
func transitionReceipt(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, action, newStatus string) error {
	var in struct {
		Version int64  `json:"version"`
		Remark  string `json:"remark"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var status, warehouse string
	if err = tx.QueryRow(r.Context(), "SELECT status,warehouse_id FROM finished_receipt WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &warehouse); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "入库单不存在")
	}
	expected := map[string]string{"submit": "draft", "confirm-quality": "pending_quality", "approve": "pending_review"}[action]
	if status != expected {
		return httpapi.Error(409, "INVALID_STATE", "入库单当前状态不允许此操作")
	}
	if action == "approve" {
		rows, queryErr := tx.Query(r.Context(), "SELECT id,bundle_id,qty FROM finished_receipt_line WHERE tenant_id=$1 AND receipt_id=$2", p.TenantID, r.PathValue("id"))
		if queryErr != nil {
			return queryErr
		}
		type line struct {
			id, bundle string
			qty        int64
		}
		lines := []line{}
		for rows.Next() {
			var v line
			if err := rows.Scan(&v.id, &v.bundle, &v.qty); err != nil {
				rows.Close()
				return err
			}
			lines = append(lines, v)
		}
		rows.Close()
		for _, v := range lines {
			if _, err = tx.Exec(r.Context(), `INSERT INTO inventory_ledger(tenant_id,id,warehouse_id,inventory_type,source_type,source_id,bundle_id,qty_delta,occurred_at,created_at) VALUES($1,$2,$3,'finished','finished_receipt_line',$4,$5,$6,$7,$7)`, p.TenantID, platform.ID(), warehouse, v.id, v.bundle, v.qty, platform.Now()); err != nil {
				return err
			}
		}
	}
	confirmation := ""
	if action == "confirm-quality" {
		confirmation = ",quality_confirmed_by=$2,quality_confirmed_at=$3"
	} else if action == "approve" {
		confirmation = ",reviewed_by=$2,reviewed_at=$3"
	}
	rowsAffected := int64(0)
	if action == "submit" {
		result, execErr := tx.Exec(r.Context(), "UPDATE finished_receipt SET status=$1,version=version+1 WHERE tenant_id=$2 AND id=$3 AND version=$4", newStatus, p.TenantID, r.PathValue("id"), in.Version)
		err, rowsAffected = execErr, result.RowsAffected()
	} else {
		query := "UPDATE finished_receipt SET status=$1,version=version+1" + confirmation + " WHERE tenant_id=$4 AND id=$5 AND version=$6"
		result, execErr := tx.Exec(r.Context(), query, newStatus, p.UserID, platform.Now(), p.TenantID, r.PathValue("id"), in.Version)
		err, rowsAffected = execErr, result.RowsAffected()
	}
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return httpapi.Error(409, "VERSION_CONFLICT", "入库单已变更")
	}
	var receiptWorkOrder string
	if err = tx.QueryRow(r.Context(), "SELECT work_order_id FROM finished_receipt WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&receiptWorkOrder); err != nil {
		return err
	}
	if err = refreshWorkOrderProgress(r.Context(), tx, p.TenantID, receiptWorkOrder); err != nil {
		return err
	}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, action, "finished_receipt", r.PathValue("id"), map[string]any{"status": status}, map[string]any{"status": newStatus}, in.Remark); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": newStatus, "version": in.Version + 1})
}

func createShipment(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		ShipmentNo   string `json:"shipment_no"`
		ProjectID    string `json:"project_id"`
		ShipmentDate int64  `json:"shipment_date"`
		Lines        []struct {
			WorkOrderID string `json:"work_order_id"`
			BundleID    string `json:"bundle_id"`
			Color       string `json:"color"`
			Size        string `json:"size"`
			Qty         int64  `json:"qty"`
		} `json:"lines"`
		Remark *string `json:"remark"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.ShipmentNo == "" || in.ProjectID == "" || len(in.Lines) == 0 {
		return httpapi.Error(400, "VALIDATION_ERROR", "出货单号、项目和明细必填")
	}
	if in.ShipmentDate == 0 {
		in.ShipmentDate = platform.Now()
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var customer string
	if err = tx.QueryRow(r.Context(), "SELECT customer_id FROM project WHERE tenant_id=$1 AND id=$2", p.TenantID, in.ProjectID).Scan(&customer); err != nil {
		return httpapi.Error(400, "INVALID_PROJECT", "项目不存在")
	}
	id := platform.ID()
	if _, err = tx.Exec(r.Context(), `INSERT INTO shipment(tenant_id,id,shipment_no,project_id,customer_id,shipment_date,operator_employee_id,remark,created_at,created_by,created_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, p.TenantID, id, in.ShipmentNo, in.ProjectID, customer, in.ShipmentDate, p.EmployeeID, in.Remark, platform.Now(), p.UserID, api.Config.NodeID); err != nil {
		return err
	}
	for _, line := range in.Lines {
		if line.Qty <= 0 || line.BundleID == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "出货扎和数量必填")
		}
		var stock int64
		if err = tx.QueryRow(r.Context(), "SELECT COALESCE(SUM(qty_delta),0)::bigint FROM inventory_ledger WHERE tenant_id=$1 AND bundle_id=$2 AND inventory_type='finished'", p.TenantID, line.BundleID).Scan(&stock); err != nil {
			return err
		}
		if line.Qty > stock {
			return httpapi.Error(400, "QUANTITY_EXCEEDED", "出货数量超过成品库存")
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO shipment_line(tenant_id,id,shipment_id,work_order_id,bundle_id,color,size,qty) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.TenantID, platform.ID(), id, line.WorkOrderID, line.BundleID, line.Color, line.Size, line.Qty); err != nil {
			return err
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 201, map[string]any{"id": id, "status": "draft", "version": 1})
}
func confirmShipment(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
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
	if err = tx.QueryRow(r.Context(), "SELECT status FROM shipment WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status); err != nil {
		return err
	}
	if status != "draft" {
		return httpapi.Error(409, "INVALID_STATE", "出货单已确认")
	}
	rows, err := tx.Query(r.Context(), "SELECT id,bundle_id,qty FROM shipment_line WHERE tenant_id=$1 AND shipment_id=$2", p.TenantID, r.PathValue("id"))
	if err != nil {
		return err
	}
	type line struct {
		id, bundle string
		qty        int64
	}
	lines := []line{}
	for rows.Next() {
		var v line
		if err := rows.Scan(&v.id, &v.bundle, &v.qty); err != nil {
			rows.Close()
			return err
		}
		lines = append(lines, v)
	}
	rows.Close()
	for _, v := range lines {
		var warehouse string
		if err = tx.QueryRow(r.Context(), "SELECT warehouse_id FROM inventory_ledger WHERE tenant_id=$1 AND bundle_id=$2 AND qty_delta>0 ORDER BY occurred_at DESC LIMIT 1 FOR UPDATE", p.TenantID, v.bundle).Scan(&warehouse); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO inventory_ledger(tenant_id,id,warehouse_id,inventory_type,source_type,source_id,bundle_id,qty_delta,occurred_at,created_at) VALUES($1,$2,$3,'finished','shipment_line',$4,$5,$6,$7,$7)`, p.TenantID, platform.ID(), warehouse, v.id, v.bundle, -v.qty, platform.Now()); err != nil {
			return err
		}
	}
	result, err := tx.Exec(r.Context(), "UPDATE shipment SET status='confirmed',version=version+1 WHERE tenant_id=$1 AND id=$2 AND version=$3", p.TenantID, r.PathValue("id"), in.Version)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return httpapi.Error(409, "VERSION_CONFLICT", "出货单已变更")
	}
	var shipmentProject string
	if err = tx.QueryRow(r.Context(), "SELECT project_id FROM shipment WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&shipmentProject); err != nil {
		return err
	}
	if err = refreshStyleProgress(r.Context(), tx, p.TenantID, shipmentProject, platform.ID(), platform.Now()); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "confirmed", "version": in.Version + 1})
}

var _ = pgx.ErrNoRows
