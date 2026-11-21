package production

import (
	"context"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/orgapp"
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
	ReceiverEmployeeID *string `json:"receiver_employee_id"`
	Remark             *string `json:"remark"`
}

func registerMaterialIssues(api *httpapi.API) {
	api.Handle("GET /api/v1/material-issues", "inventory:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listMaterialIssues(api, w, r, p)
	})
	api.Handle("POST /api/v1/material-issues", "inventory:issue", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createMaterialIssue(api, w, r, p)
	})
	api.Handle("GET /api/v1/material-issues/pending-confirm", "inventory:issue_confirm,inventory:issue", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listPendingMaterialIssues(api, w, r, p, false)
	})
	api.Handle("GET /api/v1/material-issues/pending-confirm-count", "inventory:issue_confirm,inventory:issue", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listPendingMaterialIssues(api, w, r, p, true)
	})
	api.Handle("POST /api/v1/material-issues/{id}/confirm", "inventory:issue_confirm", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return confirmMaterialIssue(api, w, r, p)
	})
	api.Handle("POST /api/v1/material-issues/{id}/reverse", "inventory:issue", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return reverseMaterialIssue(api, w, r, p)
	})
}

func listMaterialIssues(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	/* 数据范围（设计 5.7）：仓库范围按 warehouse_id 过滤，同时尊重人员组织范围 */
	access, err := orgapp.ComputeAccess(r.Context(), api.DB, p)
	if err != nil {
		return err
	}
	args := []any{p.TenantID}
	where := "mi.tenant_id=$1"
	whCond, whArgs := access.WarehouseScopeSQL("mi.warehouse_id", len(args)+1)
	args = append(args, whArgs...)
	where += whCond
	orgCond, orgArgs := access.OrgScopeSQL("e", len(args)+1)
	args = append(args, orgArgs...)
	if len(orgArgs) > 0 {
		/* 领料可能没有经办员工：无经办人时保留（仓库范围已过滤），有经办人时按组织范围判断 */
		inner := strings.TrimSuffix(strings.TrimPrefix(orgCond, " AND ("), ")")
		where += " AND (mi.operator_employee_id IS NULL OR (" + inner + "))"
	}
	rows, err := api.DB.Query(r.Context(), `SELECT mi.id,mi.issue_no,mi.issue_date,mi.work_order_id,w.work_order_code,mi.project_id,p.project_code,
		mi.warehouse_id,wh.code,wh.name,mi.material_id,m.code,m.name,m.unit,mi.qty::text,mi.unit,COALESCE(mi.purpose,''),
		COALESCE(e.name,''),mi.status,mi.version,
		COALESCE(re.name,''),mi.confirm_status,mi.confirmed_at
		FROM material_issue mi
		JOIN work_order w ON w.tenant_id=mi.tenant_id AND w.id=mi.work_order_id
		JOIN project p ON p.tenant_id=mi.tenant_id AND p.id=mi.project_id
		JOIN warehouse wh ON wh.tenant_id=mi.tenant_id AND wh.id=mi.warehouse_id
		JOIN material m ON m.tenant_id=mi.tenant_id AND m.id=mi.material_id
		LEFT JOIN employee e ON e.tenant_id=mi.tenant_id AND e.id=mi.operator_employee_id
		LEFT JOIN employee re ON re.tenant_id=mi.tenant_id AND re.id=mi.receiver_employee_id
		WHERE `+where+` ORDER BY mi.issue_date DESC,mi.id DESC LIMIT 200`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, issueNo, issueDate, workOrderID, workOrderCode, projectID, projectCode string
		var warehouseID, warehouseCode, warehouseName, materialID, materialCode, materialName, unit, qty, issueUnit, purpose, operator, status, receiver, confirmStatus string
		var version int64
		var confirmedAt *int64
		if err := rows.Scan(&id, &issueNo, &issueDate, &workOrderID, &workOrderCode, &projectID, &projectCode,
			&warehouseID, &warehouseCode, &warehouseName, &materialID, &materialCode, &materialName, &unit, &qty, &issueUnit, &purpose,
			&operator, &status, &version, &receiver, &confirmStatus, &confirmedAt); err != nil {
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
			"receiver_name": receiver, "confirm_status": confirmStatus, "confirmed_at": confirmedAt,
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
	status, confirmStatus := "posted", "none"
	var receiverID *string
	if in.ReceiverEmployeeID != nil && strings.TrimSpace(*in.ReceiverEmployeeID) != "" {
		var receiverStatus string
		if err := tx.QueryRow(r.Context(), "SELECT status FROM employee WHERE tenant_id=$1 AND id=$2", p.TenantID, *in.ReceiverEmployeeID).Scan(&receiverStatus); err != nil || receiverStatus != "active" {
			return httpapi.Error(400, "INVALID_RECEIVER", "领料人不存在或已停用")
		}
		receiverID = in.ReceiverEmployeeID
		status, confirmStatus = "pending_confirm", "pending"
	}
	id, now := platform.ID(), platform.Now()
	if _, err := tx.Exec(r.Context(), `INSERT INTO material_issue(tenant_id,id,issue_no,issue_date,work_order_id,project_id,warehouse_id,material_id,qty,unit,purpose,operator_employee_id,receiver_employee_id,status,confirm_status,remark,version,created_at,created_by,created_node,updated_at,updated_by,updated_node)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,1,$17,$18,$19,$17,$18,$19)`, p.TenantID, id, in.IssueNo, in.IssueDate, in.WorkOrderID, projectID, in.WarehouseID, in.MaterialID, qty.StringFixed(6), materialUnit, in.Purpose, in.OperatorEmployeeID, receiverID, status, confirmStatus, in.Remark, now, p.UserID, api.Config.NodeID); err != nil {
		return httpapi.Error(http.StatusConflict, "DUPLICATE_OR_INVALID_REFERENCE", "领料单号重复或引用数据无效")
	}
	if status == "posted" {
		if _, err := tx.Exec(r.Context(), `INSERT INTO inventory_ledger(tenant_id,id,warehouse_id,inventory_type,source_type,source_id,material_id,qty_delta,occurred_at,created_at)
			VALUES($1,$2,$3,'material','material_issue',$4,$5,$6,$7,$7)`, p.TenantID, platform.ID(), in.WarehouseID, id, in.MaterialID, qty.Neg().StringFixed(6), now); err != nil {
			return err
		}
	} else if err := notifyMaterialIssueReceiver(r.Context(), tx, api, p.TenantID, *receiverID, id, in.IssueNo, materialName, qty.StringFixed(6)); err != nil {
		return err
	}
	response := map[string]any{"id": id, "issue_no": in.IssueNo, "status": status, "confirm_status": confirmStatus, "qty": qty.StringFixed(6), "version": 1}
	if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create", "material_issue", id, nil, response, ""); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusCreated, response)
}

func notifyMaterialIssueReceiver(ctx context.Context, tx pgx.Tx, api *httpapi.API, tenantID, employeeID, issueID, issueNo, materialName, qty string) error {
	rows, err := tx.Query(ctx, `SELECT id FROM user_account WHERE tenant_id=$1 AND employee_id=$2 AND status='active'`, tenantID, employeeID)
	if err != nil {
		return err
	}
	userIDs := []string{}
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			rows.Close()
			return err
		}
		userIDs = append(userIDs, userID)
	}
	rowErr := rows.Err()
	rows.Close()
	if rowErr != nil {
		return rowErr
	}
	now := platform.Now()
	for _, userID := range userIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO notification(tenant_id,id,user_id,notification_type,owner_type,owner_id,title,body,created_at)
			VALUES($1,$2,$3,'material_issue_pending_confirm','material_issue',$4,'待确认领料',
			'物料出库单 '||$5||' 待确认：'||$6||' '||$7||' 件。请在物料出库模块确认领取。',$8)`,
			tenantID, platform.ID(), userID, issueID, issueNo, materialName, qty, now); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, "UPDATE material_issue SET notified_at=$1 WHERE tenant_id=$2 AND id=$3", now, tenantID, issueID)
	return err
}

func listPendingMaterialIssues(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, countOnly bool) error {
	args := []any{p.TenantID}
	where := "mi.tenant_id=$1 AND mi.status='pending_confirm'"
	if !p.Permissions["inventory:issue"] {
		where += " AND mi.receiver_employee_id=$2"
		args = append(args, p.EmployeeID)
	}
	if countOnly {
		var count int64
		if err := api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM material_issue mi WHERE "+where, args...).Scan(&count); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"count": count})
	}
	rows, err := api.DB.Query(r.Context(), `SELECT mi.id,mi.issue_no,mi.issue_date,mi.work_order_id,w.work_order_code,mi.project_id,pj.project_code,
		mi.warehouse_id,wh.name,mi.material_id,m.name,m.unit,mi.qty::text,COALESCE(mi.purpose,''),mi.version,COALESCE(re.name,''),mi.notified_at
		FROM material_issue mi
		JOIN work_order w ON w.tenant_id=mi.tenant_id AND w.id=mi.work_order_id
		JOIN project pj ON pj.tenant_id=mi.tenant_id AND pj.id=mi.project_id
		JOIN warehouse wh ON wh.tenant_id=mi.tenant_id AND wh.id=mi.warehouse_id
		JOIN material m ON m.tenant_id=mi.tenant_id AND m.id=mi.material_id
		LEFT JOIN employee re ON re.tenant_id=mi.tenant_id AND re.id=mi.receiver_employee_id
		WHERE `+where+` ORDER BY mi.issue_date DESC,mi.id DESC LIMIT 200`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, issueNo, issueDate, workOrderID, workOrderCode, projectID, projectCode, warehouseID, warehouseName, materialID, materialName, unit, qty, purpose, receiver string
		var version int64
		var notifiedAt *int64
		if err := rows.Scan(&id, &issueNo, &issueDate, &workOrderID, &workOrderCode, &projectID, &projectCode, &warehouseID, &warehouseName, &materialID, &materialName, &unit, &qty, &purpose, &version, &receiver, &notifiedAt); err != nil {
			return err
		}
		items = append(items, map[string]any{"id": id, "issue_no": issueNo, "issue_date": issueDate,
			"work_order_id": workOrderID, "work_order_code": workOrderCode, "project_id": projectID, "project_code": projectCode,
			"warehouse_id": warehouseID, "warehouse_name": warehouseName, "material_id": materialID, "material_name": materialName,
			"unit": unit, "qty": qty, "purpose": purpose, "version": version, "receiver_name": receiver, "notified_at": notifiedAt})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func confirmMaterialIssue(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		Version int64  `json:"version"`
		Reason  string `json:"reason"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.Version <= 0 {
		return httpapi.Error(400, "VERSION_REQUIRED", "必须提供领料单版本")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var issueNo, status, receiverID, warehouseID, materialID, qty, createdBy string
	var version int64
	if err := tx.QueryRow(r.Context(), `SELECT issue_no,status,COALESCE(receiver_employee_id,''),warehouse_id,material_id,qty::text,version,created_by
		FROM material_issue WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, p.TenantID, r.PathValue("id")).Scan(&issueNo, &status, &receiverID, &warehouseID, &materialID, &qty, &version, &createdBy); err != nil {
		if err == pgx.ErrNoRows {
			return httpapi.Error(404, "NOT_FOUND", "领料单不存在")
		}
		return err
	}
	if status != "pending_confirm" {
		return httpapi.Error(409, "INVALID_STATE", "该领料单不需要确认或已确认")
	}
	if !p.Permissions["inventory:issue"] && receiverID != p.EmployeeID {
		return httpapi.Error(403, "FORBIDDEN", "只能确认分配给自己的领料单")
	}
	if version != in.Version {
		return httpapi.Error(409, "VERSION_CONFLICT", "领料单已变更，请刷新后重试")
	}
	var stockText string
	if err := tx.QueryRow(r.Context(), `SELECT COALESCE(SUM(qty_delta),0)::text FROM inventory_ledger WHERE tenant_id=$1 AND inventory_type='material' AND material_id=$2 AND warehouse_id=$3`, p.TenantID, materialID, warehouseID).Scan(&stockText); err != nil {
		return err
	}
	stock, _ := decimal.NewFromString(stockText)
	requested, _ := decimal.NewFromString(qty)
	if stock.LessThan(requested) {
		return httpapi.Error(400, "STOCK_INSUFFICIENT", "确认时物料库存不足，请联系仓管处理")
	}
	now := platform.Now()
	if _, err := tx.Exec(r.Context(), `INSERT INTO inventory_ledger(tenant_id,id,warehouse_id,inventory_type,source_type,source_id,material_id,qty_delta,occurred_at,created_at)
		VALUES($1,$2,$3,'material','material_issue',$4,$5,$6,$7,$7)`, p.TenantID, platform.ID(), warehouseID, r.PathValue("id"), materialID, requested.Neg().StringFixed(6), now); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `UPDATE material_issue SET status='posted',confirm_status='confirmed',confirmed_at=$1,confirmed_by=$2,confirmed_node=$3,
		version=version+1,updated_at=$1,updated_by=$2,updated_node=$3,remark=COALESCE(remark,'')||CASE WHEN $4='' THEN '' ELSE ' 确认说明：'||$4 END
		WHERE tenant_id=$5 AND id=$6`, now, p.UserID, api.Config.NodeID, in.Reason, p.TenantID, r.PathValue("id")); err != nil {
		return err
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO notification(tenant_id,id,user_id,notification_type,owner_type,owner_id,title,body,created_at)
		VALUES($1,$2,$3,'material_issue_confirmed','material_issue',$4,'领料已确认','物料出库单 '||$5||' 已由领料人确认领取。',$6)`,
		p.TenantID, platform.ID(), createdBy, r.PathValue("id"), issueNo, now); err != nil {
		return err
	}
	response := map[string]any{"id": r.PathValue("id"), "issue_no": issueNo, "status": "posted", "confirm_status": "confirmed", "version": version + 1}
	if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "confirm", "material_issue", r.PathValue("id"), nil, response, in.Reason); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, response)
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
