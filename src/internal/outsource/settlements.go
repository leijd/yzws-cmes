package outsource

import (
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* registerSettlements 委外结算（设计 4.25.8）：confirmed 后金额锁定；
 * direction=payable 本厂委托应付 / receivable 本厂受托应收。
 * 单价与金额属商业敏感数据，不进入任何跨租户投影。 */
func registerSettlements(api *httpapi.API) {
	api.Handle("GET /api/v1/outsource/settlements", "outsource:read,outsource:settle", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "s.tenant_id=$1"
		if status := r.URL.Query().Get("status"); status != "" {
			args = append(args, status)
			where += " AND s.status=$2"
		}
		rows, err := api.DB.Query(r.Context(), `SELECT s.id,s.settlement_no,COALESCE(o.order_no,''),COALESCE(pt.name,''),s.direction,
			s.qualified_qty,s.unit_price,s.amount,s.deduction_amount,s.status,COALESCE(s.period_start,''),COALESCE(s.period_end,''),s.created_at,s.version
			FROM outsourcing_settlement s
			LEFT JOIN outsourcing_order o ON o.tenant_id=s.tenant_id AND o.id=s.outsourcing_order_id
			LEFT JOIN outsourcing_partner pt ON pt.tenant_id=s.tenant_id AND pt.id=s.partner_id
			WHERE `+where+` ORDER BY s.created_at DESC LIMIT 200`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, sno, orderNo, partnerName, direction, status, periodStart, periodEnd string
			var qty, price, amount, deduction float64
			var createdAt, version int64
			if err := rows.Scan(&id, &sno, &orderNo, &partnerName, &direction, &qty, &price, &amount, &deduction, &status,
				&periodStart, &periodEnd, &createdAt, &version); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "settlement_no": sno, "order_no": orderNo, "partner_name": partnerName,
				"direction": direction, "qualified_qty": qty, "unit_price": price, "amount": amount, "deduction_amount": deduction,
				"status": status, "period_start": periodStart, "period_end": periodEnd, "created_at": createdAt, "version": version})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/outsource/settlements", "outsource:settle", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			OutsourcingOrderID string  `json:"outsourcing_order_id"`
			WorkOrderID        string  `json:"work_order_id"`
			Direction          string  `json:"direction"`
			PeriodStart        string  `json:"period_start"`
			PeriodEnd          string  `json:"period_end"`
			QualifiedQty       float64 `json:"qualified_qty"`
			UnitPrice          float64 `json:"unit_price"`
			DeductionAmount    float64 `json:"deduction_amount"`
			Remark             string  `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Direction != "payable" && in.Direction != "receivable" {
			return httpapi.Error(http.StatusBadRequest, "DIRECTION_INVALID", "结算方向仅支持 payable/receivable")
		}
		if in.QualifiedQty < 0 || in.UnitPrice < 0 || in.DeductionAmount < 0 {
			return httpapi.Error(http.StatusBadRequest, "AMOUNT_INVALID", "数量、单价、扣款不能为负")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		/* 关联委外单时校验归属并带入厂商 */
		var partnerID *string
		if in.OutsourcingOrderID != "" {
			var pid string
			var oStatus string
			if err := tx.QueryRow(ctx, `SELECT partner_id,status FROM outsourcing_order WHERE tenant_id=$1 AND id=$2`,
				p.TenantID, in.OutsourcingOrderID).Scan(&pid, &oStatus); err != nil {
				return httpapi.Error(http.StatusBadRequest, "NOT_FOUND", "委外单不存在")
			}
			if oStatus != "completed" && oStatus != "partially_returned" {
				return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "委外单须至少部分回货后才能结算")
			}
			partnerID = &pid
		}
		amount := in.QualifiedQty*in.UnitPrice - in.DeductionAmount
		if amount < 0 {
			amount = 0
		}
		sno, err := nextSeqNo(ctx, tx, p.TenantID, "outsourcing_settlement", "settlement_no", "JS")
		if err != nil {
			return err
		}
		id := platform.ID()
		now := platform.Now()
		if _, err := tx.Exec(ctx, `INSERT INTO outsourcing_settlement(tenant_id,id,settlement_no,outsourcing_order_id,work_order_id,partner_id,
			direction,period_start,period_end,qualified_qty,unit_price,amount,deduction_amount,status,remark,
			created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,$7,NULLIF($8,''),NULLIF($9,''),$10,$11,$12,$13,'draft',NULLIF($14,''),
			$15,$16,$17,$15,$16,$17,1)`,
			p.TenantID, id, sno, in.OutsourcingOrderID, in.WorkOrderID, partnerID, in.Direction, in.PeriodStart, in.PeriodEnd,
			in.QualifiedQty, in.UnitPrice, amount, in.DeductionAmount, in.Remark, now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "create_outsource_settlement", "outsourcing_settlement", id, nil,
			map[string]any{"settlement_no": sno, "amount": amount, "direction": in.Direction}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "settlement_no": sno})
	})

	transition := func(action string) func(http.ResponseWriter, *http.Request, auth.Principal) error {
		return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
			ctx := r.Context()
			tx, err := api.DB.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			var status, orderID *string
			var s string
			err = tx.QueryRow(ctx, `SELECT status,outsourcing_order_id FROM outsourcing_settlement WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
				p.TenantID, r.PathValue("id")).Scan(&s, &orderID)
			if err == pgx.ErrNoRows {
				return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "结算单不存在")
			}
			if err != nil {
				return err
			}
			status = &s
			var next string
			switch action {
			case "confirm":
				if *status != "draft" {
					return httpapi.Error(http.StatusConflict, "SETTLEMENT_LOCKED", "结算单已确认，金额与数量不可修改")
				}
				next = "confirmed"
			case "invoice":
				if *status != "confirmed" {
					return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "结算单须先确认再登记开票")
				}
				next = "invoiced"
			case "pay":
				if *status != "confirmed" && *status != "invoiced" {
					return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "结算单须先确认再登记收付款")
				}
				next = "paid"
			}
			now := platform.Now()
			if _, err := tx.Exec(ctx, `UPDATE outsourcing_settlement SET status=$3,
				confirmed_by=CASE WHEN $3='confirmed' THEN $4 ELSE confirmed_by END,
				confirmed_at=CASE WHEN $3='confirmed' THEN $5 ELSE confirmed_at END,
				paid_at=CASE WHEN $3='paid' THEN $5 ELSE paid_at END,
				updated_at=$5,updated_by=$4,updated_node=$6,version=version+1
				WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id"), next, p.UserID, now, api.Config.NodeID); err != nil {
				return err
			}
			/* 应付结算完成后，关联委外单转 settled */
			if next == "paid" && orderID != nil {
				if _, err := tx.Exec(ctx, `UPDATE outsourcing_order SET status='settled',closed_at=COALESCE(closed_at,$3),
					updated_at=$3,updated_by=$4,updated_node=$5,version=version+1
					WHERE tenant_id=$1 AND id=$2 AND status='completed'`, p.TenantID, *orderID, now, p.UserID, api.Config.NodeID); err != nil {
					return err
				}
			}
			if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, action+"_settlement", "outsourcing_settlement", r.PathValue("id"),
				map[string]any{"status": *status}, map[string]any{"status": next}, ""); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true, "status": next})
		}
	}
	api.Handle("POST /api/v1/outsource/settlements/{id}/confirm", "outsource:settle", transition("confirm"))
	api.Handle("POST /api/v1/outsource/settlements/{id}/invoice", "outsource:settle", transition("invoice"))
	api.Handle("POST /api/v1/outsource/settlements/{id}/pay", "outsource:settle", transition("pay"))
}
