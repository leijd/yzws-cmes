package production

import (
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type materialIssueInput struct {
	IssueNo            string  `json:"issue_no"`
	IssueDate          string  `json:"issue_date"`
	WorkOrderID        string  `json:"work_order_id"`
	WarehouseID        string  `json:"warehouse_id"`
	MaterialID         string  `json:"material_id"`
	Qty                string  `json:"qty"`
	Purpose            *string `json:"purpose"`
	OperatorEmployeeID *string `json:"operator_employee_id"`
	Remark             *string `json:"remark"`
}

func registerMaterialIssues(api *httpapi.API) {
	api.Handle("GET /api/v1/material-issues", "masterdata:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listMaterialIssues(api, w, r, p)
	})
	api.Handle("POST /api/v1/material-issues", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createMaterialIssue(api, w, r, p)
	})
	api.Handle("POST /api/v1/material-issues/{id}/reverse", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return reverseMaterialIssue(api, w, r, p)
	})
}

func listMaterialIssues(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	rows, err := api.DB.Query(r.Context(), `SELECT mi.id,mi.issue_no,mi.issue_date,mi.work_order_id,w.work_order_code,mi.project_id,p.project_code,
		mi.warehouse_id,wh.code,wh.name,mi.material_id,m.code,m.name,m.unit,mi.qty::text,mi.unit,COALESCE(mi.purpose,''),
		COALESCE(e.name,''),mi.status,mi.version
		FROM material_issue mi
		JOIN work_order w ON w.tenant_id=mi.tenant_id AND w.id=mi.work_order_id
		JOIN project p ON p.tenant_id=mi.tenant_id AND p.id=mi.project_id
		JOIN warehouse wh ON wh.tenant_id=mi.tenant_id AND wh.id=mi.warehouse_id
		JOIN material m ON m.tenant_id=mi.tenant_id AND m.id=mi.material_id
		LEFT JOIN employee e ON e.tenant_id=mi.tenant_id AND e.id=mi.operator_employee_id
		WHERE mi.tenant_id=$1 ORDER BY mi.issue_date DESC,mi.id DESC LIMIT 200`, p.TenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, issueNo, issueDate, workOrderID, workOrderCode, projectID, projectCode string
		var warehouseID, warehouseCode, warehouseName, materialID, materialCode, materialName, unit, qty, issueUnit, purpose, operator, status string
		var version int64
		if err := rows.Scan(&id, &issueNo, &issueDate, &workOrderID, &workOrderCode, &projectID, &projectCode,
			&warehouseID, &warehouseCode, &warehouseName, &materialID, &materialCode, &materialName, &unit, &qty, &issueUnit, &purpose,
			&operator, &status, &version); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"id": id, "issue_no": issueNo, "issue_date": issueDate,
			"work_order_id": workOrderID, "work_order_code": workOrderCode,
			"project_id": projectID, "project_code": projectCode,
			"warehouse_id": warehouseID, "warehouse_code": warehouseCode, "warehouse_name": warehouseName,
			"material_id": materialID, "material_code": materialCode, "material_name": materialName,
			"unit": unit, "qty": qty, "issue_unit": issueUnit, "purpose": purpose,
			"operator_name": operator, "status": status, "version": version,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "has_more": false})
}

func createMaterialIssue(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in materialIssueInput
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.IssueNo == "" || in.WorkOrderID == "" || in.WarehouseID == "" || in.MaterialID == "" {
		return httpapi.Error(400, "VALIDATION_ERROR", "领料单号、工单、仓库和物料必填")
	}
	qty, err := decimal.NewFromString(in.Qty)
	if err != nil || !qty.GreaterThan(decimal.Zero) {
		return httpapi.Error(400, "VALIDATION_ERROR", "领料数量必须大于零")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())

	var projectID, workOrderStatus, materialUnit, materialName, materialStatus string
	if err := tx.QueryRow(r.Context(), `SELECT w.project_id,w.status,m.unit,m.name,m.status FROM work_order w JOIN material m ON m.tenant_id=w.tenant_id AND m.id=$2 WHERE w.tenant_id=$1 AND w.id=$3`, p.TenantID, in.MaterialID, in.WorkOrderID).Scan(&projectID, &workOrderStatus, &materialUnit, &materialName, &materialStatus); err != nil {
		if err == pgx.ErrNoRows {
			return httpapi.Error(404, "NOT_FOUND", "工单或物料不存在")
		}
		return err
	}
	if workOrderStatus == "cancelled" || workOrderStatus == "completed" {
		return httpapi.Error(409, "INVALID_STATE", "已完成或取消的工单不能领料")
	}
	if materialStatus != "active" {
		return httpapi.Error(409, "INVALID_STATE", "物料已停用，不能领料")
	}
	var stockText string
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(SUM(qty_delta),0)::text FROM inventory_ledger WHERE tenant_id=$1 AND inventory_type='material' AND material_id=$2 AND warehouse_id=$3`, p.TenantID, in.MaterialID, in.WarehouseID).Scan(&stockText); err != nil {
		return err
	}
	stock, _ := decimal.NewFromString(stockText)
	if stock.LessThan(qty) {
		appErr := httpapi.Error(400, "STOCK_INSUFFICIENT", "物料库存不足")
		appErr.Details = map[string]any{"material": materialName, "available": stock.StringFixed(6), "requested": qty.StringFixed(6)}
		return appErr
	}
	id, now := platform.ID(), platform.Now()
	if _, err := tx.Exec(r.Context(), `INSERT INTO material_issue(tenant_id,id,issue_no,issue_date,work_order_id,project_id,warehouse_id,material_id,qty,unit,purpose,operator_employee_id,status,remark,version,created_at,created_by,created_node,updated_at,updated_by,updated_node)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'posted',$13,1,$14,$15,$16,$14,$15,$16)`, p.TenantID, id, in.IssueNo, in.IssueDate, in.WorkOrderID, projectID, in.WarehouseID, in.MaterialID, qty.StringFixed(6), materialUnit, in.Purpose, in.OperatorEmployeeID, in.Remark, now, p.UserID, api.Config.NodeID); err != nil {
		return httpapi.Error(http.StatusConflict, "DUPLICATE_OR_INVALID_REFERENCE", "领料单号重复或引用数据无效")
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO inventory_ledger(tenant_id,id,warehouse_id,inventory_type,source_type,source_id,material_id,qty_delta,occurred_at,created_at)
		VALUES($1,$2,$3,'material','material_issue',$4,$5,$6,$7,$7)`, p.TenantID, platform.ID(), in.WarehouseID, id, in.MaterialID, qty.Neg().StringFixed(6), now); err != nil {
		return err
	}
	response := map[string]any{"id": id, "issue_no": in.IssueNo, "status": "posted", "qty": qty.StringFixed(6), "version": 1}
	if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create", "material_issue", id, nil, response, ""); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusCreated, response)
}

func reverseMaterialIssue(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		Reason string `json:"reason"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.Reason == "" {
		return httpapi.Error(400, "REASON_REQUIRED", "冲销原因必填")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var issueNo, status, warehouseID, materialID, qty string
	var version int64
	if err := tx.QueryRow(r.Context(), `SELECT issue_no,status,warehouse_id,material_id,qty::text,version FROM material_issue WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, p.TenantID, r.PathValue("id")).Scan(&issueNo, &status, &warehouseID, &materialID, &qty, &version); err != nil {
		if err == pgx.ErrNoRows {
			return httpapi.Error(404, "NOT_FOUND", "领料单不存在")
		}
		return err
	}
	if status != "posted" {
		return httpapi.Error(409, "INVALID_STATE", "只有已领料单可以冲销")
	}
	now := platform.Now()
	if _, err := tx.Exec(r.Context(), `INSERT INTO inventory_ledger(tenant_id,id,warehouse_id,inventory_type,source_type,source_id,material_id,qty_delta,occurred_at,created_at)
		VALUES($1,$2,$3,'material','material_issue_reversal',$4,$5,$6,$7,$7)`, p.TenantID, platform.ID(), warehouseID, r.PathValue("id"), materialID, qty, now); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE material_issue SET status='reversed',version=version+1,updated_at=$1,updated_by=$2,updated_node=$3,remark=COALESCE(remark,'')||' 冲销原因：'||$4 WHERE tenant_id=$5 AND id=$6`, now, p.UserID, api.Config.NodeID, in.Reason, p.TenantID, r.PathValue("id")); err != nil {
		return err
	}
	response := map[string]any{"id": r.PathValue("id"), "issue_no": issueNo, "status": "reversed", "version": version + 1}
	if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "reverse", "material_issue", r.PathValue("id"), map[string]any{"status": status}, response, in.Reason); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, response)
}
