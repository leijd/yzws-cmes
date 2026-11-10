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

func registerShipmentReturns(api *httpapi.API) {
	api.Handle("GET /api/v1/shipment-returns", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listShipmentReturns(api, w, r, p)
	})
	api.Handle("GET /api/v1/shipments/{id}", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return getShipment(api, w, r, p)
	})
	api.Handle("POST /api/v1/shipments/{id}/returns", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createShipmentReturn(api, w, r, p)
	})
}

func listShipmentReturns(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	rows, err := api.DB.Query(r.Context(), `SELECT sr.id,sr.return_no,sr.shipment_id,s.shipment_no,sr.project_id,pj.project_code,sr.work_order_id,wo.work_order_code,
		sr.bundle_id,b.bundle_no,b.color,b.size,sr.qty,sr.reason,sr.status,COALESCE(e.name,''),sr.version,sr.created_at
		FROM shipment_return sr
		JOIN shipment s ON s.tenant_id=sr.tenant_id AND s.id=sr.shipment_id
		JOIN project pj ON pj.tenant_id=sr.tenant_id AND pj.id=sr.project_id
		JOIN work_order wo ON wo.tenant_id=sr.tenant_id AND wo.id=sr.work_order_id
		JOIN bundle b ON b.tenant_id=sr.tenant_id AND b.id=sr.bundle_id
		LEFT JOIN employee e ON e.tenant_id=sr.tenant_id AND e.id=sr.operator_employee_id
		WHERE sr.tenant_id=$1 ORDER BY sr.created_at DESC LIMIT 200`, p.TenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, no, shipmentID, shipmentNo, projectID, projectCode, workOrderID, workOrderCode, bundleID, bundleNo, color, size, reason, status, operator string
		var qty, version, createdAt int64
		if err := rows.Scan(&id, &no, &shipmentID, &shipmentNo, &projectID, &projectCode, &workOrderID, &workOrderCode, &bundleID, &bundleNo, &color, &size, &qty, &reason, &status, &operator, &version, &createdAt); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"id": id, "return_no": no, "shipment_id": shipmentID, "shipment_no": shipmentNo,
			"project_id": projectID, "project_code": projectCode, "work_order_id": workOrderID, "work_order_code": workOrderCode,
			"bundle_id": bundleID, "bundle_no": bundleNo, "color": color, "size": size,
			"qty": qty, "reason": reason, "status": status, "operator_name": operator, "version": version, "created_at": createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func getShipment(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var header map[string]any
	if err := api.DB.QueryRow(r.Context(), `SELECT row_to_json(t) FROM (SELECT s.*,p.project_code,COALESCE((SELECT SUM(sr.qty) FROM shipment_return sr WHERE sr.tenant_id=s.tenant_id AND sr.shipment_id=s.id AND sr.status='posted'),0) AS returned_qty FROM shipment s JOIN project p ON p.tenant_id=s.tenant_id AND p.id=s.project_id WHERE s.tenant_id=$1 AND s.id=$2) t`, p.TenantID, r.PathValue("id")).Scan(&header); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "出货单不存在")
		}
		return err
	}
	rows, err := api.DB.Query(r.Context(), `SELECT l.id,l.bundle_id,COALESCE(b.bundle_no,''),l.work_order_id,wo.work_order_code,l.color,l.size,l.qty,
		COALESCE((SELECT SUM(sr.qty) FROM shipment_return sr WHERE sr.tenant_id=l.tenant_id AND sr.shipment_id=l.shipment_id AND sr.bundle_id=l.bundle_id AND sr.status='posted'),0) AS returned_qty
		FROM shipment_line l LEFT JOIN bundle b ON b.tenant_id=l.tenant_id AND b.id=l.bundle_id
		JOIN work_order wo ON wo.tenant_id=l.tenant_id AND wo.id=l.work_order_id
		WHERE l.tenant_id=$1 AND l.shipment_id=$2 ORDER BY l.id`, p.TenantID, r.PathValue("id"))
	if err != nil {
		return err
	}
	defer rows.Close()
	lines := []map[string]any{}
	for rows.Next() {
		var id, bundleID, bundleNo, workOrderID, workOrderCode, color, size string
		var qty, returned int64
		if err := rows.Scan(&id, &bundleID, &bundleNo, &workOrderID, &workOrderCode, &color, &size, &qty, &returned); err != nil {
			return err
		}
		lines = append(lines, map[string]any{"id": id, "bundle_id": bundleID, "bundle_no": bundleNo, "work_order_id": workOrderID, "work_order_code": workOrderCode, "color": color, "size": size, "qty": qty, "returned_qty": returned, "returnable_qty": qty - returned})
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"header": header, "lines": lines})
}

func createShipmentReturn(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		ReturnNo           string  `json:"return_no"`
		BundleID           string  `json:"bundle_id"`
		Qty                int64   `json:"qty"`
		Reason             string  `json:"reason"`
		OperatorEmployeeID *string `json:"operator_employee_id"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.BundleID == "" || in.Qty <= 0 || strings.TrimSpace(in.Reason) == "" {
		return httpapi.Error(400, "VALIDATION_ERROR", "扎、退货数量和退货原因必填")
	}
	operator := p.EmployeeID
	if in.OperatorEmployeeID != nil && strings.TrimSpace(*in.OperatorEmployeeID) != "" {
		operator = strings.TrimSpace(*in.OperatorEmployeeID)
	}
	if operator == "" {
		return httpapi.Error(400, "NO_EMPLOYEE_PROFILE", "当前账号未关联员工档案")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var projectID, status string
	if err := tx.QueryRow(r.Context(), "SELECT project_id,status FROM shipment WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&projectID, &status); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "出货单不存在")
	}
	if status != "confirmed" {
		return httpapi.Error(409, "INVALID_STATE", "只有已确认出货单可以退货")
	}
	var lineID, workOrderID string
	var lineQty int64
	if err := tx.QueryRow(r.Context(), "SELECT id,work_order_id,qty FROM shipment_line WHERE tenant_id=$1 AND shipment_id=$2 AND bundle_id=$3", p.TenantID, r.PathValue("id"), in.BundleID).Scan(&lineID, &workOrderID, &lineQty); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "出货单中没有该扎")
	}
	var returned int64
	if err := tx.QueryRow(r.Context(), "SELECT COALESCE(SUM(qty),0) FROM shipment_return WHERE tenant_id=$1 AND shipment_id=$2 AND bundle_id=$3 AND status='posted'", p.TenantID, r.PathValue("id"), in.BundleID).Scan(&returned); err != nil {
		return err
	}
	if returned+in.Qty > lineQty {
		appErr := httpapi.Error(400, "QUANTITY_EXCEEDED", "退货数量超过该扎可退数量")
		appErr.Details = map[string]any{"shipped": lineQty, "returned": returned, "requested": in.Qty}
		return appErr
	}
	var warehouseID string
	if err := tx.QueryRow(r.Context(), "SELECT warehouse_id FROM inventory_ledger WHERE tenant_id=$1 AND source_type='shipment_line' AND source_id=$2 LIMIT 1", p.TenantID, lineID).Scan(&warehouseID); err != nil {
		return httpapi.Error(409, "WAREHOUSE_NOT_FOUND", "找不到原出货仓库")
	}
	id, now := platform.ID(), platform.Now()
	returnNo := strings.TrimSpace(in.ReturnNo)
	if returnNo == "" {
		returnNo = "TH-" + platform.ID()[:12]
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO shipment_return(tenant_id,id,return_no,shipment_id,project_id,work_order_id,bundle_id,qty,reason,status,operator_employee_id,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'posted',$10,$11,$12,$13,$11,$12,$13,1)`, p.TenantID, id, returnNo, r.PathValue("id"), projectID, workOrderID, in.BundleID, in.Qty, strings.TrimSpace(in.Reason), operator, now, p.UserID, api.Config.NodeID); err != nil {
		return httpapi.Error(409, "DUPLICATE_OR_INVALID", "退货单号重复或引用数据无效")
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO inventory_ledger(tenant_id,id,warehouse_id,inventory_type,source_type,source_id,bundle_id,qty_delta,occurred_at,created_at)
		VALUES($1,$2,$3,'finished','shipment_return',$4,$5,$6,$7,$7)`, p.TenantID, platform.ID(), warehouseID, id, in.BundleID, in.Qty, now); err != nil {
		return err
	}
	response := map[string]any{"id": id, "return_no": returnNo, "shipment_id": r.PathValue("id"), "bundle_id": in.BundleID, "qty": in.Qty, "status": "posted", "version": 1}
	if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "return", "shipment", r.PathValue("id"), nil, response, in.Reason); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusCreated, response)
}
