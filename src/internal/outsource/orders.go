package outsource

import (
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

type orderItemInput struct {
	ProcessID  string   `json:"process_id"`
	Segment    string   `json:"segment"`
	BundleRange string  `json:"bundle_range"`
	Qty        float64  `json:"qty"`
	UnitPrice  *float64 `json:"unit_price"`
}

/* registerOrders 委外单（设计 4.25.4~4.25.7）：明细、发出/取消、物料流水、进度填报与三视图查询。 */
func registerOrders(api *httpapi.API) {
	api.Handle("GET /api/v1/outsource/orders", "outsource:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "o.tenant_id=$1"
		if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
			args = append(args, status)
			where += fmt.Sprintf(" AND o.status=$%d", len(args))
		}
		if partnerID := strings.TrimSpace(r.URL.Query().Get("partner_id")); partnerID != "" {
			args = append(args, partnerID)
			where += fmt.Sprintf(" AND o.partner_id=$%d", len(args))
		}
		if kw := strings.TrimSpace(r.URL.Query().Get("keyword")); kw != "" {
			args = append(args, "%"+kw+"%")
			where += fmt.Sprintf(" AND o.order_no ILIKE $%d", len(args))
		}
		rows, err := api.DB.Query(r.Context(), `SELECT o.id,o.order_no,o.partner_id,pt.name,o.scope_type,o.pricing_type,
			o.total_qty,COALESCE(o.agreed_due_date,''),o.status,COALESCE(o.issued_at,0),o.created_at,o.version,
			(SELECT COALESCE(SUM(i.returned_qty),0) FROM outsourcing_order_item i WHERE i.tenant_id=o.tenant_id AND i.outsourcing_order_id=o.id)
			FROM outsourcing_order o JOIN outsourcing_partner pt ON pt.tenant_id=o.tenant_id AND pt.id=o.partner_id
			WHERE `+where+` ORDER BY o.created_at DESC LIMIT 200`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, orderNo, partnerID, partnerName, scopeType, pricingType, dueDate, status string
			var totalQty, returnedQty float64
			var issuedAt, createdAt, version int64
			if err := rows.Scan(&id, &orderNo, &partnerID, &partnerName, &scopeType, &pricingType, &totalQty, &dueDate,
				&status, &issuedAt, &createdAt, &version, &returnedQty); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "order_no": orderNo, "partner_id": partnerID, "partner_name": partnerName,
				"scope_type": scopeType, "pricing_type": pricingType, "total_qty": totalQty, "returned_qty": returnedQty,
				"agreed_due_date": dueDate, "status": status, "issued_at": issuedAt, "created_at": createdAt, "version": version})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/outsource/orders", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			PartnerID     string           `json:"partner_id"`
			ProjectID     string           `json:"project_id"`
			WorkOrderID   string           `json:"work_order_id"`
			ScopeType     string           `json:"scope_type"`
			PricingType   string           `json:"pricing_type"`
			AgreedDueDate string           `json:"agreed_due_date"`
			Remark        string           `json:"remark"`
			Items         []orderItemInput `json:"items"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if len(in.Items) == 0 {
			return httpapi.Error(http.StatusBadRequest, "ITEMS_REQUIRED", "委外明细至少一行")
		}
		if in.ScopeType != "order" && in.ScopeType != "style" && in.ScopeType != "processes" {
			return httpapi.Error(http.StatusBadRequest, "OUTSOURCE_SCOPE_INVALID", "委外范围仅支持 order/style/processes")
		}
		if in.PricingType != "piece" && in.PricingType != "lump" {
			return httpapi.Error(http.StatusBadRequest, "PRICING_INVALID", "计价方式仅支持 piece/lump")
		}
		var totalQty float64
		for _, it := range in.Items {
			if it.Qty <= 0 {
				return httpapi.Error(http.StatusBadRequest, "QTY_INVALID", "明细数量必须大于 0")
			}
			if in.ScopeType == "processes" && strings.TrimSpace(it.ProcessID) == "" {
				return httpapi.Error(http.StatusBadRequest, "OUTSOURCE_SCOPE_INVALID", "工序外发时明细必须指定工序")
			}
			if in.PricingType == "piece" && it.UnitPrice == nil {
				return httpapi.Error(http.StatusBadRequest, "PRICE_REQUIRED", "计件计价时明细必须填写单价")
			}
			totalQty += it.Qty
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var partnerTenantID *string
		var partnerStatus string
		if err := tx.QueryRow(ctx, `SELECT partner_tenant_id,status FROM outsourcing_partner WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, in.PartnerID).Scan(&partnerTenantID, &partnerStatus); err != nil {
			return httpapi.Error(http.StatusBadRequest, "PARTNER_NOT_FOUND", "外协厂商不存在")
		}
		if partnerStatus != "active" {
			return httpapi.Error(http.StatusConflict, "PARTNER_DISABLED", "外协厂商已停用")
		}
		/* 受托方为系统租户时必须存在 active 协作（设计 4.25.4） */
		var collaborationID *string
		if partnerTenantID != nil {
			cid, err := findActiveCollaboration(ctx, tx, p.TenantID, *partnerTenantID)
			if err == pgx.ErrNoRows {
				return httpapi.Error(http.StatusConflict, "COLLAB_NOT_ACTIVE", "受托方为系统租户但协作未生效，请先建立并确认协作")
			}
			if err != nil {
				return err
			}
			collaborationID = &cid
		}
		orderNo, err := nextSeqNo(ctx, tx, p.TenantID, "outsourcing_order", "order_no", "WW")
		if err != nil {
			return err
		}
		orderID := platform.ID()
		now := platform.Now()
		if _, err := tx.Exec(ctx, `INSERT INTO outsourcing_order(tenant_id,id,order_no,partner_id,collaboration_id,project_id,work_order_id,
			scope_type,pricing_type,total_qty,agreed_due_date,status,remark,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,$10,NULLIF($11,''),'draft',NULLIF($12,''),$13,$14,$15,$13,$14,$15,1)`,
			p.TenantID, orderID, orderNo, in.PartnerID, collaborationID, in.ProjectID, in.WorkOrderID,
			in.ScopeType, in.PricingType, totalQty, in.AgreedDueDate, in.Remark, now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		for _, it := range in.Items {
			if _, err := tx.Exec(ctx, `INSERT INTO outsourcing_order_item(tenant_id,id,outsourcing_order_id,process_id,segment,bundle_range,qty,unit_price,
				created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
				VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,$11,$9,$10,$11,1)`,
				p.TenantID, platform.ID(), orderID, it.ProcessID, it.Segment, it.BundleRange, it.Qty, it.UnitPrice,
				now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "create_outsource_order", "outsourcing_order", orderID, nil,
			map[string]any{"order_no": orderNo, "total_qty": totalQty, "scope_type": in.ScopeType}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": orderID, "order_no": orderNo})
	})

	api.Handle("GET /api/v1/outsource/orders/{id}", "outsource:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		orderID := r.PathValue("id")
		var order map[string]any
		var orderNo, partnerName, scopeType, pricingType, status, dueDate, remark string
		var totalQty float64
		var issuedAt, createdAt, version int64
		var collaborationID, projectID, workOrderID *string
		err := api.DB.QueryRow(ctx, `SELECT o.order_no,pt.name,o.collaboration_id,o.project_id,o.work_order_id,o.scope_type,o.pricing_type,
			o.total_qty,COALESCE(o.agreed_due_date,''),o.status,COALESCE(o.issued_at,0),COALESCE(o.remark,''),o.created_at,o.version
			FROM outsourcing_order o JOIN outsourcing_partner pt ON pt.tenant_id=o.tenant_id AND pt.id=o.partner_id
			WHERE o.tenant_id=$1 AND o.id=$2`, p.TenantID, orderID).
			Scan(&orderNo, &partnerName, &collaborationID, &projectID, &workOrderID, &scopeType, &pricingType, &totalQty,
				&dueDate, &status, &issuedAt, &remark, &createdAt, &version)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "委外单不存在")
		}
		if err != nil {
			return err
		}
		order = map[string]any{"id": orderID, "order_no": orderNo, "partner_name": partnerName, "collaboration_id": collaborationID,
			"project_id": projectID, "work_order_id": workOrderID, "scope_type": scopeType, "pricing_type": pricingType,
			"total_qty": totalQty, "agreed_due_date": dueDate, "status": status, "issued_at": issuedAt, "remark": remark,
			"created_at": createdAt, "version": version}

		items := []map[string]any{}
		rows, err := api.DB.Query(ctx, `SELECT i.id,COALESCE(i.process_id,''),COALESCE(pr.name,''),COALESCE(i.segment,''),COALESCE(i.bundle_range,''),
			i.qty,i.unit_price,i.returned_qty,i.rework_qty,i.defect_qty
			FROM outsourcing_order_item i LEFT JOIN process_definition pr ON pr.tenant_id=i.tenant_id AND pr.id=i.process_id
			WHERE i.tenant_id=$1 AND i.outsourcing_order_id=$2 ORDER BY i.created_at`, p.TenantID, orderID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, processID, processName, segment, bundleRange string
			var qty float64
			var unitPrice *float64
			var returned, rework, defect float64
			if err := rows.Scan(&id, &processID, &processName, &segment, &bundleRange, &qty, &unitPrice, &returned, &rework, &defect); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "process_id": processID, "process_name": processName, "segment": segment,
				"bundle_range": bundleRange, "qty": qty, "unit_price": unitPrice, "returned_qty": returned,
				"rework_qty": rework, "defect_qty": defect})
		}

		flows := []map[string]any{}
		frows, err := api.DB.Query(ctx, `SELECT id,flow_type,material_name,qty,COALESCE(voucher_no,''),flow_date,COALESCE(linked_ref,''),status,created_at
			FROM outsourcing_material_flow WHERE tenant_id=$1 AND outsourcing_order_id=$2 ORDER BY created_at DESC LIMIT 200`, p.TenantID, orderID)
		if err != nil {
			return err
		}
		defer frows.Close()
		for frows.Next() {
			var id, flowType, materialName, voucherNo, flowDate, linkedRef, fstatus string
			var qty float64
			var createdAt2 int64
			if err := frows.Scan(&id, &flowType, &materialName, &qty, &voucherNo, &flowDate, &linkedRef, &fstatus, &createdAt2); err != nil {
				return err
			}
			flows = append(flows, map[string]any{"id": id, "flow_type": flowType, "material_name": materialName, "qty": qty,
				"voucher_no": voucherNo, "flow_date": flowDate, "linked_ref": linkedRef, "status": fstatus, "created_at": createdAt2})
		}

		projections := []map[string]any{}
		prows, err := api.DB.Query(ctx, `SELECT id,reported_at,completed_qty,in_process_qty,quality_pass_qty,COALESCE(stage_note,''),COALESCE(reported_by,'')
			FROM outsourcing_progress_projection WHERE tenant_id=$1 AND outsourcing_order_id=$2 ORDER BY reported_at DESC LIMIT 50`, p.TenantID, orderID)
		if err != nil {
			return err
		}
		defer prows.Close()
		for prows.Next() {
			var id, stageNote, reportedBy string
			var reportedAt int64
			var completed, inProcess, pass float64
			if err := prows.Scan(&id, &reportedAt, &completed, &inProcess, &pass, &stageNote, &reportedBy); err != nil {
				return err
			}
			projections = append(projections, map[string]any{"id": id, "reported_at": reportedAt, "completed_qty": completed,
				"in_process_qty": inProcess, "quality_pass_qty": pass, "stage_note": stageNote, "reported_by": reportedBy})
		}

		settlements := []map[string]any{}
		srows, err := api.DB.Query(ctx, `SELECT id,settlement_no,qualified_qty,unit_price,amount,deduction_amount,status,created_at
			FROM outsourcing_settlement WHERE tenant_id=$1 AND outsourcing_order_id=$2 ORDER BY created_at DESC`, p.TenantID, orderID)
		if err != nil {
			return err
		}
		defer srows.Close()
		for srows.Next() {
			var id, sno, sstatus string
			var qty, price, amount, deduction float64
			var createdAt3 int64
			if err := srows.Scan(&id, &sno, &qty, &price, &amount, &deduction, &sstatus, &createdAt3); err != nil {
				return err
			}
			settlements = append(settlements, map[string]any{"id": id, "settlement_no": sno, "qualified_qty": qty, "unit_price": price,
				"amount": amount, "deduction_amount": deduction, "status": sstatus, "created_at": createdAt3})
		}

		return httpapi.JSON(w, http.StatusOK, map[string]any{"order": order, "items": items, "flows": flows,
			"projections": projections, "settlements": settlements})
	})

	/* 发出：校验协作、明细与数量（设计 5.14 发出事务） */
	api.Handle("POST /api/v1/outsource/orders/{id}/issue", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var status string
		var collaborationID *string
		if err := tx.QueryRow(ctx, `SELECT status,collaboration_id FROM outsourcing_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
			p.TenantID, r.PathValue("id")).Scan(&status, &collaborationID); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "委外单不存在")
		}
		if status != "draft" {
			return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "仅草稿委外单可发出")
		}
		if collaborationID != nil {
			var cStatus string
			if err := tx.QueryRow(ctx, `SELECT status FROM tenant_collaboration WHERE id=$1`, *collaborationID).Scan(&cStatus); err != nil {
				return err
			}
			if cStatus != "active" {
				return httpapi.Error(http.StatusConflict, "COLLAB_NOT_ACTIVE", "跨租户协作未生效或已暂停/终止，不能发出")
			}
		}
		var itemCount int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM outsourcing_order_item WHERE tenant_id=$1 AND outsourcing_order_id=$2`,
			p.TenantID, r.PathValue("id")).Scan(&itemCount); err != nil {
			return err
		}
		if itemCount == 0 {
			return httpapi.Error(http.StatusBadRequest, "ITEMS_REQUIRED", "委外明细至少一行")
		}
		now := platform.Now()
		if _, err := tx.Exec(ctx, `UPDATE outsourcing_order SET status='issued',issued_at=$3,updated_at=$3,updated_by=$4,updated_node=$5,version=version+1
			WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id"), now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "issue_outsource_order", "outsourcing_order", r.PathValue("id"),
			map[string]any{"status": "draft"}, map[string]any{"status": "issued"}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	api.Handle("POST /api/v1/outsource/orders/{id}/cancel", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		now := platform.Now()
		result, err := tx.Exec(ctx, `UPDATE outsourcing_order SET status='cancelled',updated_at=$3,updated_by=$4,updated_node=$5,version=version+1
			WHERE tenant_id=$1 AND id=$2 AND status='draft'`, p.TenantID, r.PathValue("id"), now, p.UserID, api.Config.NodeID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "仅草稿委外单可取消")
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "cancel_outsource_order", "outsourcing_order", r.PathValue("id"), nil, nil, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	/* 物料流水：发料 issue / 回货 return；回货数量守恒，超量整事务回滚（设计 5.14） */
	api.Handle("POST /api/v1/outsource/orders/{id}/material-flows", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ItemID       string  `json:"item_id"`
			FlowType     string  `json:"flow_type"`
			MaterialName string  `json:"material_name"`
			Qty          float64 `json:"qty"`
			FlowDate     string  `json:"flow_date"`
			VoucherNo    string  `json:"voucher_no"`
			Remark       string  `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.FlowType != "issue" && in.FlowType != "return" {
			return httpapi.Error(http.StatusBadRequest, "FLOW_TYPE_INVALID", "流水类型仅支持 issue/return")
		}
		if strings.TrimSpace(in.MaterialName) == "" || in.Qty <= 0 {
			return httpapi.Error(http.StatusBadRequest, "QTY_INVALID", "物料名称必填且数量必须大于 0")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		orderID := r.PathValue("id")
		var status string
		var totalQty float64
		if err := tx.QueryRow(ctx, `SELECT status,total_qty FROM outsourcing_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
			p.TenantID, orderID).Scan(&status, &totalQty); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "委外单不存在")
		}
		if status == "draft" || status == "cancelled" || status == "settled" {
			return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "当前状态不允许登记物料流水")
		}
		newStatus := status
		if in.FlowType == "issue" {
			if status == "issued" {
				newStatus = "in_transit"
			}
		} else {
			/* 回货：数量守恒校验（按明细行；未指定明细时按整单口径） */
			if in.ItemID != "" {
				result, err := tx.Exec(ctx, `UPDATE outsourcing_order_item SET returned_qty=returned_qty+$4,updated_at=$5,updated_by=$6,updated_node=$7,version=version+1
					WHERE tenant_id=$1 AND id=$2 AND outsourcing_order_id=$3 AND returned_qty+rework_qty+defect_qty+$4<=qty`,
					p.TenantID, in.ItemID, orderID, in.Qty, platform.Now(), p.UserID, api.Config.NodeID)
				if err != nil {
					return err
				}
				if result.RowsAffected() == 0 {
					return httpapi.Error(http.StatusConflict, "OUTSOURCE_QTY_EXCEEDED", "回货数量超出委外数量")
				}
			} else {
				var returnedTotal float64
				if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(returned_qty),0) FROM outsourcing_order_item
					WHERE tenant_id=$1 AND outsourcing_order_id=$2`, p.TenantID, orderID).Scan(&returnedTotal); err != nil {
					return err
				}
				if returnedTotal+in.Qty > totalQty {
					return httpapi.Error(http.StatusConflict, "OUTSOURCE_QTY_EXCEEDED", "回货数量超出委外数量")
				}
			}
			var returnedTotal float64
			if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(returned_qty+rework_qty+defect_qty),0) FROM outsourcing_order_item
				WHERE tenant_id=$1 AND outsourcing_order_id=$2`, p.TenantID, orderID).Scan(&returnedTotal); err != nil {
				return err
			}
			if returnedTotal >= totalQty {
				newStatus = "completed"
			} else {
				newStatus = "partially_returned"
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outsourcing_material_flow(tenant_id,id,outsourcing_order_id,item_id,flow_type,material_name,qty,
			flow_date,voucher_no,status,remark,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,NULLIF($9,''),'posted',NULLIF($10,''),$11,$12,$13,$11,$12,$13,1)`,
			p.TenantID, platform.ID(), orderID, in.ItemID, in.FlowType, in.MaterialName, in.Qty, in.FlowDate, in.VoucherNo, in.Remark,
			platform.Now(), p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if newStatus != status {
			if _, err := tx.Exec(ctx, `UPDATE outsourcing_order SET status=$3,closed_at=CASE WHEN $3='completed' THEN $4 ELSE closed_at END,
				updated_at=$4,updated_by=$5,updated_node=$6,version=version+1 WHERE tenant_id=$1 AND id=$2`,
				p.TenantID, orderID, newStatus, platform.Now(), p.UserID, api.Config.NodeID); err != nil {
				return err
			}
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "outsource_material_"+in.FlowType, "outsourcing_order", orderID, nil,
			map[string]any{"material": in.MaterialName, "qty": in.Qty, "status": newStatus}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"ok": true, "status": newStatus})
	})

	/* 非租户受托方手工进度填报（写投影，reported_by=本厂填报人） */
	api.Handle("POST /api/v1/outsource/orders/{id}/progress-report", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			CompletedQty   float64 `json:"completed_qty"`
			InProcessQty   float64 `json:"in_process_qty"`
			QualityPassQty float64 `json:"quality_pass_qty"`
			StageNote      string  `json:"stage_note"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var totalQty float64
		var status string
		if err := tx.QueryRow(ctx, `SELECT total_qty,status FROM outsourcing_order WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, r.PathValue("id")).Scan(&totalQty, &status); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "委外单不存在")
		}
		if status == "draft" || status == "cancelled" {
			return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "委外单未发出，不能填报进度")
		}
		if in.CompletedQty+in.InProcessQty > totalQty {
			return httpapi.Error(http.StatusConflict, "OUTSOURCE_QTY_EXCEEDED", "报数超出委外数量")
		}
		reportedBy := p.EmployeeID
		if reportedBy == "" {
			reportedBy = p.UserID
		}
		if _, err := tx.Exec(ctx, `INSERT INTO outsourcing_progress_projection(tenant_id,id,outsourcing_order_id,reported_at,
			completed_qty,in_process_qty,quality_pass_qty,stage_note,reported_by,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$4,$4)`,
			p.TenantID, platform.ID(), r.PathValue("id"), platform.Now(), in.CompletedQty, in.InProcessQty, in.QualityPassQty,
			in.StageNote, reportedBy); err != nil {
			return err
		}
		if status == "issued" || status == "in_transit" {
			if _, err := tx.Exec(ctx, `UPDATE outsourcing_order SET status='processing',updated_at=$3,updated_by=$4,updated_node=$5,version=version+1
				WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id"), platform.Now(), p.UserID, api.Config.NodeID); err != nil {
				return err
			}
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "outsource_progress_report", "outsourcing_order", r.PathValue("id"), nil,
			map[string]any{"completed": in.CompletedQty, "in_process": in.InProcessQty}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"ok": true})
	})

	/* 进度三视图：combined/own/contractor（设计 5.14、6.8） */
	api.Handle("GET /api/v1/outsource/orders/{id}/progress", "outsource:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		orderID := r.PathValue("id")
		var totalQty float64
		var workOrderID *string
		if err := api.DB.QueryRow(ctx, `SELECT total_qty,work_order_id FROM outsourcing_order WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, orderID).Scan(&totalQty, &workOrderID); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "委外单不存在")
		}
		/* 本方进度：关联工单的任务汇总 */
		own := map[string]any{"planned": 0, "reported": 0, "approved": 0}
		if workOrderID != nil {
			var planned, reported, approved float64
			if err := api.DB.QueryRow(ctx, `SELECT COALESCE(SUM(planned_qty),0),COALESCE(SUM(reported_qty),0),COALESCE(SUM(approved_good_qty),0)
				FROM production_task WHERE tenant_id=$1 AND work_order_id=$2 AND status<>'cancelled'`,
				p.TenantID, *workOrderID).Scan(&planned, &reported, &approved); err != nil {
				return err
			}
			own = map[string]any{"planned": planned, "reported": reported, "approved": approved}
		}
		/* 外包方进度：投影表最新一条 */
		contractor := map[string]any{"completed": 0, "in_process": 0, "quality_pass": 0, "reported_at": 0}
		var completed, inProcess, pass float64
		var reportedAt int64
		if err := api.DB.QueryRow(ctx, `SELECT completed_qty,in_process_qty,quality_pass_qty,reported_at
			FROM outsourcing_progress_projection WHERE tenant_id=$1 AND outsourcing_order_id=$2 ORDER BY reported_at DESC LIMIT 1`,
			p.TenantID, orderID).Scan(&completed, &inProcess, &pass, &reportedAt); err == nil {
			contractor = map[string]any{"completed": completed, "in_process": inProcess, "quality_pass": pass, "reported_at": reportedAt}
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"total_qty": totalQty, "own": own, "contractor": contractor})
	})
}
