package production

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type materialReceiptLineInput struct {
	MaterialID    string  `json:"material_id"`
	BedNo         *string `json:"bed_no"`
	ColorNo       *string `json:"color_no"`
	BatchNo       *string `json:"batch_no"`
	ReceivedQty   string  `json:"received_qty"`
	PassQty       string  `json:"pass_qty"`
	ConcessionQty string  `json:"concession_qty"`
	PendingQty    string  `json:"pending_qty"`
	AcceptedQty   string  `json:"accepted_qty"`
	UnitPrice     string  `json:"unit_price"`
	Remark        *string `json:"remark"`
}

type materialReceiptInput struct {
	ReceiptNo          string                     `json:"receipt_no"`
	ReceiptDate        int64                      `json:"receipt_date"`
	ProjectID          string                     `json:"project_id"`
	WarehouseID        string                     `json:"warehouse_id"`
	SupplierID         string                     `json:"supplier_id"`
	ReceiverEmployeeID string                     `json:"receiver_employee_id"`
	Lines              []materialReceiptLineInput `json:"lines"`
}

func writeMaterialReceiptLines(ctx context.Context, tx pgx.Tx, tenantID, receiptID string, lines []materialReceiptLineInput) error {
	for index, line := range lines {
		if strings.TrimSpace(line.MaterialID) == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", fmt.Sprintf("第 %d 行缺少物料", index+1))
		}
		received, err := decimal.NewFromString(strings.TrimSpace(line.ReceivedQty))
		if err != nil || !received.IsPositive() {
			return httpapi.Error(400, "INVALID_QUANTITY", fmt.Sprintf("第 %d 行来料数量必须大于 0", index+1))
		}
		pass, err := decimal.NewFromString(strings.TrimSpace(defaultString(line.PassQty, "0")))
		if err != nil || pass.IsNegative() {
			return httpapi.Error(400, "INVALID_QUANTITY", fmt.Sprintf("第 %d 行合格数量不合法", index+1))
		}
		concession, _ := decimal.NewFromString(strings.TrimSpace(defaultString(line.ConcessionQty, "0")))
		pending, _ := decimal.NewFromString(strings.TrimSpace(defaultString(line.PendingQty, "0")))
		accepted := pass
		if strings.TrimSpace(line.AcceptedQty) != "" {
			accepted, err = decimal.NewFromString(strings.TrimSpace(line.AcceptedQty))
			if err != nil || accepted.IsNegative() {
				return httpapi.Error(400, "INVALID_QUANTITY", fmt.Sprintf("第 %d 行实收数量不合法", index+1))
			}
		}
		reject := received.Sub(pass)
		if reject.IsNegative() {
			return httpapi.Error(400, "INVALID_QUANTITY", fmt.Sprintf("第 %d 行合格数量不能大于来料数量", index+1))
		}
		if accepted.GreaterThan(pass) {
			return httpapi.Error(400, "INVALID_QUANTITY", fmt.Sprintf("第 %d 行实收不能大于合格数量", index+1))
		}
		if accepted.LessThan(pass) && optionalText(line.Remark) == nil {
			return httpapi.Error(400, "REMARK_REQUIRED", fmt.Sprintf("第 %d 行实收小于合格数量时必须说明让步或差异", index+1))
		}
		price := decimal.Zero
		if strings.TrimSpace(line.UnitPrice) != "" {
			price, err = decimal.NewFromString(strings.TrimSpace(line.UnitPrice))
			if err != nil || price.IsNegative() {
				return httpapi.Error(400, "INVALID_PRICE", fmt.Sprintf("第 %d 行单价不合法", index+1))
			}
		}
		amount := accepted.Mul(price)
		if _, err := tx.Exec(ctx, `INSERT INTO material_receipt_line(tenant_id,id,receipt_id,material_id,bed_no,color_no,batch_no,received_qty,pass_qty,reject_qty,concession_qty,pending_qty,accepted_qty,unit_price,amount,remark,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17)`, tenantID, platform.ID(), receiptID, line.MaterialID, optionalText(line.BedNo), optionalText(line.ColorNo), optionalText(line.BatchNo),
			received.StringFixed(6), pass.StringFixed(6), reject.StringFixed(6), concession.StringFixed(6), pending.StringFixed(6), accepted.StringFixed(6), price.StringFixed(6), amount.StringFixed(6), optionalText(line.Remark), platform.Now()); err != nil {
			return err
		}
	}
	return nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func registerMaterialReceipts(api *httpapi.API) {
	api.Handle("GET /api/v1/material-receipts", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT r.id,r.receipt_no,r.receipt_date,r.status,r.version,pj.project_code,pj.id,pr.product_code,pr.name,
			COALESCE(s.name,''),COALESCE(w.name,''),COALESCE(e.name,''),
			(SELECT COUNT(*) FROM material_receipt_line l WHERE l.tenant_id=r.tenant_id AND l.receipt_id=r.id),
			(SELECT COALESCE(SUM(l.received_qty),0)::text FROM material_receipt_line l WHERE l.tenant_id=r.tenant_id AND l.receipt_id=r.id),
			(SELECT COALESCE(SUM(l.pass_qty),0)::text FROM material_receipt_line l WHERE l.tenant_id=r.tenant_id AND l.receipt_id=r.id),
			(SELECT COALESCE(SUM(l.amount),0)::text FROM material_receipt_line l WHERE l.tenant_id=r.tenant_id AND l.receipt_id=r.id)
			FROM material_receipt r
			JOIN project pj ON pj.tenant_id=r.tenant_id AND pj.id=r.project_id
			JOIN product pr ON pr.tenant_id=pj.tenant_id AND pr.id=pj.product_id
			LEFT JOIN supplier s ON s.tenant_id=r.tenant_id AND s.id=r.supplier_id
			LEFT JOIN warehouse w ON w.tenant_id=r.tenant_id AND w.id=r.warehouse_id
			LEFT JOIN employee e ON e.tenant_id=r.tenant_id AND e.id=r.receiver_employee_id
			WHERE r.tenant_id=$1 ORDER BY r.created_at DESC`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, status, projectCode, projectID, productCode, productName, supplier, warehouse, receiver string
			var receiptDate, version, lineCount int64
			var received, pass, amount string
			if err := rows.Scan(&id, &no, &receiptDate, &status, &version, &projectCode, &projectID, &productCode, &productName, &supplier, &warehouse, &receiver, &lineCount, &received, &pass, &amount); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "receipt_no": no, "receipt_date": receiptDate, "status": status, "version": version, "project_id": projectID, "project_code": projectCode, "product_code": productCode, "product_name": productName, "supplier_name": supplier, "warehouse_name": warehouse, "receiver_name": receiver, "line_count": lineCount, "received_total": received, "pass_total": pass, "amount_total": amount})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/material-receipts", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in materialReceiptInput
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.ReceiptNo) == "" || in.ProjectID == "" || in.WarehouseID == "" || in.SupplierID == "" || len(in.Lines) == 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "入库单号、项目、仓库、供应商和至少一行明细必填")
		}
		if strings.TrimSpace(in.ReceiverEmployeeID) == "" {
			in.ReceiverEmployeeID = p.EmployeeID
		}
		if strings.TrimSpace(in.ReceiverEmployeeID) == "" {
			return httpapi.Error(400, "NO_EMPLOYEE_PROFILE", "请选择收货人")
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
		if _, err := tx.Exec(r.Context(), `INSERT INTO material_receipt(tenant_id,id,receipt_no,receipt_date,project_id,warehouse_id,supplier_id,receiver_employee_id,status,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,'draft',$9,$10,$11,$9,$10,$11,1)`, p.TenantID, id, strings.TrimSpace(in.ReceiptNo), in.ReceiptDate, in.ProjectID, in.WarehouseID, in.SupplierID, in.ReceiverEmployeeID, platform.Now(), p.UserID, api.Config.NodeID); err != nil {
			if isUniqueViolation(err) {
				return httpapi.Error(409, "DUPLICATE_RECEIPT_NO", "入库单号已存在")
			}
			return err
		}
		if err := writeMaterialReceiptLines(r.Context(), tx, p.TenantID, id, in.Lines); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create", "material_receipt", id, nil, in, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "version": 1, "status": "draft"})
	})

	api.Handle("GET /api/v1/material-receipts/{id}", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var header map[string]any
		if err := api.DB.QueryRow(r.Context(), `SELECT row_to_json(t) FROM (SELECT r.id,r.receipt_no,r.receipt_date,r.project_id,pj.project_code,pr.product_code,pr.name AS product_name,r.warehouse_id,w.name AS warehouse_name,r.supplier_id,COALESCE(s.name,'') AS supplier_name,r.receiver_employee_id,COALESCE(e.name,'') AS receiver_name,r.status,r.version FROM material_receipt r JOIN project pj ON pj.tenant_id=r.tenant_id AND pj.id=r.project_id JOIN product pr ON pr.tenant_id=pj.tenant_id AND pr.id=pj.product_id LEFT JOIN warehouse w ON w.tenant_id=r.tenant_id AND w.id=r.warehouse_id LEFT JOIN supplier s ON s.tenant_id=r.tenant_id AND s.id=r.supplier_id LEFT JOIN employee e ON e.tenant_id=r.tenant_id AND e.id=r.receiver_employee_id WHERE r.tenant_id=$1 AND r.id=$2) t`, p.TenantID, r.PathValue("id")).Scan(&header); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "入库单不存在")
			}
			return err
		}
		rows, err := api.DB.Query(r.Context(), `SELECT l.id,l.material_id,m.code,m.name,COALESCE(m.category,''),COALESCE(m.specification,''),m.unit,COALESCE(l.bed_no,''),COALESCE(l.color_no,''),COALESCE(l.batch_no,''),
			l.received_qty::text,l.pass_qty::text,l.reject_qty::text,l.concession_qty::text,l.pending_qty::text,l.accepted_qty::text,l.unit_price::text,l.amount::text,COALESCE(l.remark,''),
			COALESCE((SELECT i.result FROM iqc_result i WHERE i.tenant_id=l.tenant_id AND i.receipt_line_id=l.id ORDER BY i.inspected_at DESC LIMIT 1),''),
			COALESCE((SELECT e2.name FROM iqc_result i JOIN employee e2 ON e2.tenant_id=i.tenant_id AND e2.id=i.inspector_employee_id WHERE i.tenant_id=l.tenant_id AND i.receipt_line_id=l.id ORDER BY i.inspected_at DESC LIMIT 1),'')
			FROM material_receipt_line l JOIN material m ON m.tenant_id=l.tenant_id AND m.id=l.material_id
			WHERE l.tenant_id=$1 AND l.receipt_id=$2 ORDER BY l.created_at,l.id`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		lines := []map[string]any{}
		for rows.Next() {
			var id, materialID, code, name, category, spec, unit, bed, color, batch, received, pass, reject, concession, pending, accepted, price, amount, remark, iqcResult, inspector string
			if err := rows.Scan(&id, &materialID, &code, &name, &category, &spec, &unit, &bed, &color, &batch, &received, &pass, &reject, &concession, &pending, &accepted, &price, &amount, &remark, &iqcResult, &inspector); err != nil {
				return err
			}
			lines = append(lines, map[string]any{"id": id, "material_id": materialID, "material_code": code, "material_name": name, "category": category, "specification": spec, "unit": unit, "bed_no": bed, "color_no": color, "batch_no": batch, "received_qty": received, "pass_qty": pass, "reject_qty": reject, "concession_qty": concession, "pending_qty": pending, "accepted_qty": accepted, "unit_price": price, "amount": amount, "remark": remark, "iqc_result": iqcResult, "inspector_name": inspector})
		}
		return httpapi.JSON(w, 200, map[string]any{"header": header, "lines": lines})
	})

	api.Handle("PATCH /api/v1/material-receipts/{id}", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in materialReceiptInput
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
		if err := tx.QueryRow(r.Context(), "SELECT status,version FROM material_receipt WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &version); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "入库单不存在")
		}
		if status != "draft" {
			return httpapi.Error(409, "INVALID_STATE", "只有草稿入库单可以编辑")
		}
		if in.ProjectID == "" || in.WarehouseID == "" || in.SupplierID == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "项目、仓库、供应商必填")
		}
		if strings.TrimSpace(in.ReceiverEmployeeID) == "" {
			in.ReceiverEmployeeID = p.EmployeeID
		}
		if in.ReceiptDate == 0 {
			in.ReceiptDate = platform.Now()
		}
		if _, err := tx.Exec(r.Context(), `UPDATE material_receipt SET receipt_date=$1,project_id=$2,warehouse_id=$3,supplier_id=$4,receiver_employee_id=$5,version=version+1,updated_at=$6,updated_by=$7,updated_node=$8 WHERE tenant_id=$9 AND id=$10`, in.ReceiptDate, in.ProjectID, in.WarehouseID, in.SupplierID, in.ReceiverEmployeeID, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM iqc_result WHERE tenant_id=$1 AND receipt_line_id IN (SELECT id FROM material_receipt_line WHERE tenant_id=$1 AND receipt_id=$2)", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM material_receipt_line WHERE tenant_id=$1 AND receipt_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := writeMaterialReceiptLines(r.Context(), tx, p.TenantID, r.PathValue("id"), in.Lines); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "version": version + 1})
	})

	api.Handle("DELETE /api/v1/material-receipts/{id}", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var status string
		if err := tx.QueryRow(r.Context(), "SELECT status FROM material_receipt WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "入库单不存在")
		}
		if status != "draft" {
			return httpapi.Error(409, "INVALID_STATE", "只有草稿入库单可以删除")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM iqc_result WHERE tenant_id=$1 AND receipt_line_id IN (SELECT id FROM material_receipt_line WHERE tenant_id=$1 AND receipt_id=$2)", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM material_receipt_line WHERE tenant_id=$1 AND receipt_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM material_receipt WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})

	api.Handle("POST /api/v1/material-receipts/{id}/submit", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
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
		var status, warehouseID string
		var version int64
		if err := tx.QueryRow(r.Context(), "SELECT status,warehouse_id,version FROM material_receipt WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &warehouseID, &version); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "入库单不存在")
		}
		if status != "draft" {
			return httpapi.Error(409, "INVALID_STATE", "只有草稿入库单可以提交")
		}
		if in.Version != 0 && in.Version != version {
			return httpapi.Error(409, "VERSION_CONFLICT", "入库单已变更，请刷新后重试")
		}
		rows, err := tx.Query(r.Context(), "SELECT id,material_id,accepted_qty::text FROM material_receipt_line WHERE tenant_id=$1 AND receipt_id=$2 AND accepted_qty>0", p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		type stockLine struct{ id, materialID, qty string }
		lines := []stockLine{}
		for rows.Next() {
			var line stockLine
			if err := rows.Scan(&line.id, &line.materialID, &line.qty); err != nil {
				rows.Close()
				return err
			}
			lines = append(lines, line)
		}
		rows.Close()
		if len(lines) == 0 {
			return httpapi.Error(400, "NO_ACCEPTED_QTY", "没有实收入库数量，无法提交")
		}
		now := platform.Now()
		for _, line := range lines {
			if _, err := tx.Exec(r.Context(), `INSERT INTO inventory_ledger(tenant_id,id,warehouse_id,inventory_type,source_type,source_id,material_id,qty_delta,occurred_at,created_at) VALUES($1,$2,$3,'material','material_receipt_line',$4,$5,$6,$7,$7)`, p.TenantID, platform.ID(), warehouseID, line.id, line.materialID, line.qty, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), "UPDATE material_receipt SET status='submitted',version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5", now, p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "submit", "material_receipt", r.PathValue("id"), map[string]any{"status": status}, map[string]any{"status": "submitted"}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "submitted", "version": version + 1})
	})

	api.Handle("POST /api/v1/material-receipts/{id}/reverse", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version int64  `json:"version"`
			Reason  string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.Reason) == "" {
			return httpapi.Error(400, "REASON_REQUIRED", "冲销必须填写原因")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var status string
		if err := tx.QueryRow(r.Context(), "SELECT status FROM material_receipt WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "入库单不存在")
		}
		if status != "submitted" {
			return httpapi.Error(409, "INVALID_STATE", "只有已提交入库单可以冲销")
		}
		rows, err := tx.Query(r.Context(), `SELECT il.id,il.warehouse_id,il.material_id,il.qty_delta::text,il.source_id FROM inventory_ledger il
			WHERE il.tenant_id=$1 AND il.source_type='material_receipt_line' AND il.reversal_of_id IS NULL
			AND il.source_id IN (SELECT id FROM material_receipt_line WHERE tenant_id=$1 AND receipt_id=$2)`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		type ledger struct{ id, warehouse, material, qty, lineID string }
		ledgers := []ledger{}
		for rows.Next() {
			var l ledger
			if err := rows.Scan(&l.id, &l.warehouse, &l.material, &l.qty, &l.lineID); err != nil {
				rows.Close()
				return err
			}
			ledgers = append(ledgers, l)
		}
		rows.Close()
		now := platform.Now()
		for _, l := range ledgers {
			delta, _ := decimal.NewFromString(l.qty)
			if _, err := tx.Exec(r.Context(), `INSERT INTO inventory_ledger(tenant_id,id,warehouse_id,inventory_type,source_type,source_id,material_id,qty_delta,occurred_at,reversal_of_id,created_at) VALUES($1,$2,$3,'material','material_receipt_line_reversal',$4,$5,$6,$7,$8,$7)`, p.TenantID, platform.ID(), l.warehouse, l.lineID, l.material, delta.Neg().StringFixed(6), now, l.id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), "UPDATE material_receipt SET status='reversed',version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5", now, p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "reversed", "version": in.Version + 1})
	})

	api.Handle("POST /api/v1/material-receipt-lines/{id}/iqc", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Result              string   `json:"result"`
			InspectorEmployeeID string   `json:"inspector_employee_id"`
			DefectDescription   *string  `json:"defect_description"`
			AttachmentIDs       []string `json:"attachment_ids"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Result != "pass" && in.Result != "fail" && in.Result != "concession" {
			return httpapi.Error(400, "VALIDATION_ERROR", "IQC 结果只能为 pass/fail/concession")
		}
		if in.InspectorEmployeeID == "" {
			in.InspectorEmployeeID = p.EmployeeID
		}
		if in.InspectorEmployeeID == "" {
			return httpapi.Error(400, "NO_EMPLOYEE_PROFILE", "当前账号未关联员工档案")
		}
		attachments := in.AttachmentIDs
		if attachments == nil {
			attachments = []string{}
		}
		body, _ := json.Marshal(attachments)
		id := platform.ID()
		if _, err := api.DB.Exec(r.Context(), `INSERT INTO iqc_result(tenant_id,id,receipt_line_id,result,inspector_employee_id,inspected_at,defect_description,attachment_ids,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$6)`, p.TenantID, id, r.PathValue("id"), in.Result, in.InspectorEmployeeID, platform.Now(), optionalText(in.DefectDescription), string(body)); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "result": in.Result})
	})
}
