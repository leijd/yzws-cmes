package production

import (
	"net/http"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func registerPayroll(api *httpapi.API) {
	api.Handle("GET /api/v1/my/wages", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if p.EmployeeID == "" {
			return httpapi.Error(400, "NO_EMPLOYEE_PROFILE", "当前账号未关联员工档案，无法查询工资")
		}
		period := r.URL.Query().Get("period")
		if period == "" {
			now := platform.Now()
			period = time.UnixMicro(now).Format("2006-01")
		}
		var employeeName string
		if err := api.DB.QueryRow(r.Context(), "SELECT name FROM employee WHERE tenant_id=$1 AND id=$2", p.TenantID, p.EmployeeID).Scan(&employeeName); err != nil {
			return httpapi.Error(400, "NO_EMPLOYEE_PROFILE", "当前账号未关联员工档案，无法查询工资")
		}
		entries := []map[string]any{}
		rows, err := api.DB.Query(r.Context(), `SELECT pe.created_at,pe.qty,pe.unit_price::text,pe.amount::text,pe.entry_kind,s.process_name
			FROM piecework_entry pe JOIN work_report wr ON wr.tenant_id=pe.tenant_id AND wr.id=pe.report_id
			JOIN production_task t ON t.tenant_id=wr.tenant_id AND t.id=wr.task_id
			JOIN work_order_process_snapshot s ON s.tenant_id=t.tenant_id AND s.work_order_id=t.work_order_id AND s.product_process_id=t.product_process_id
			WHERE pe.tenant_id=$1 AND pe.employee_id=$2 AND pe.payroll_period=$3 ORDER BY pe.created_at DESC`, p.TenantID, p.EmployeeID, period)
		if err != nil {
			return err
		}
		defer rows.Close()
		total := decimal.Zero
		for rows.Next() {
			var createdAt, qty int64
			var unitPrice, amount, kind, processName string
			if err := rows.Scan(&createdAt, &qty, &unitPrice, &amount, &kind, &processName); err != nil {
				return err
			}
			amt, _ := decimal.NewFromString(amount)
			total = total.Add(amt)
			entries = append(entries, map[string]any{"created_at": createdAt, "process_name": processName, "qty": qty, "unit_price": unitPrice, "amount": amount, "entry_kind": kind})
		}
		rows.Close()
		var sheet map[string]any
		var sheetID string
		var periodValue, gross, net, paid, outstanding, status string
		err = api.DB.QueryRow(r.Context(), `SELECT id,payroll_period,gross_amount::text,net_amount::text,paid_amount::text,outstanding_amount::text,status
			FROM payroll_sheet WHERE tenant_id=$1 AND employee_id=$2 AND payroll_period=$3 ORDER BY version DESC LIMIT 1`, p.TenantID, p.EmployeeID, period).Scan(&sheetID, &periodValue, &gross, &net, &paid, &outstanding, &status)
		if err == nil {
			payments := []map[string]any{}
			prows, err := api.DB.Query(r.Context(), `SELECT amount::text,paid_at,payment_method FROM payment WHERE tenant_id=$1 AND payroll_sheet_id=$2 ORDER BY paid_at DESC`, p.TenantID, sheetID)
			if err != nil {
				return err
			}
			for prows.Next() {
				var amount, method string
				var paidAt int64
				if err := prows.Scan(&amount, &paidAt, &method); err != nil {
					prows.Close()
					return err
				}
				payments = append(payments, map[string]any{"amount": amount, "paid_at": paidAt, "payment_method": method})
			}
			prows.Close()
			sheet = map[string]any{"payroll_period": periodValue, "gross_amount": gross, "net_amount": net, "paid_amount": paid, "outstanding_amount": outstanding, "status": status, "payments": payments}
		} else if err != pgx.ErrNoRows {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"period": period, "employee_name": employeeName, "piecework_total": total.StringFixed(6), "piecework": entries, "sheet": sheet})
	})
	api.Handle("POST /api/v1/payroll-sheets/generate", "payroll:confirm", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Period        string   `json:"period"`
			EmployeeIDs   []string `json:"employee_ids"`
			SourceVersion string   `json:"source_version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if len(in.Period) != 7 {
			return httpapi.Error(400, "VALIDATION_ERROR", "工资期间格式应为 YYYY-MM")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		results := []map[string]any{}
		for _, employeeID := range in.EmployeeIDs {
			var amountText string
			var pieceQty, count int64
			err = tx.QueryRow(r.Context(), `SELECT COALESCE(SUM(amount) FILTER (WHERE entry_kind IN ('confirmed','reversal','quality_adjustment','manual_adjustment')),0)::text,COALESCE(SUM(qty) FILTER (WHERE entry_kind IN ('confirmed','reversal')),0),COUNT(*) FROM piecework_entry WHERE tenant_id=$1 AND employee_id=$2 AND payroll_period=$3`, p.TenantID, employeeID, in.Period).Scan(&amountText, &pieceQty, &count)
			if err != nil {
				return err
			}
			if count == 0 {
				results = append(results, map[string]any{"employee_id": employeeID, "status": "skipped", "message": "本期间无计件来源"})
				continue
			}
			var exists bool
			if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM payroll_sheet WHERE tenant_id=$1 AND employee_id=$2 AND payroll_period=$3)", p.TenantID, employeeID, in.Period).Scan(&exists); err != nil {
				return err
			}
			if exists {
				results = append(results, map[string]any{"employee_id": employeeID, "status": "failed", "message": "工资草稿已存在"})
				continue
			}
			var attendance int64
			if err = tx.QueryRow(r.Context(), `SELECT COUNT(DISTINCT to_char(to_timestamp(wr.occurred_at/1000000.0) AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD')) FROM piecework_entry pe JOIN work_report wr ON wr.tenant_id=pe.tenant_id AND wr.id=pe.report_id WHERE pe.tenant_id=$1 AND pe.employee_id=$2 AND pe.payroll_period=$3 AND pe.entry_kind IN ('confirmed','reversal')`, p.TenantID, employeeID, in.Period).Scan(&attendance); err != nil {
				return err
			}
			var baseText string
			if err = tx.QueryRow(r.Context(), "SELECT COALESCE(base_salary,0)::text FROM employee WHERE tenant_id=$1 AND id=$2", p.TenantID, employeeID).Scan(&baseText); err != nil {
				return err
			}
			base, err := decimal.NewFromString(baseText)
			if err != nil {
				return err
			}
			amount, err := decimal.NewFromString(amountText)
			if err != nil {
				return err
			}
			id := platform.ID()
			snapshot := platform.JSONHash(map[string]any{"period": in.Period, "employee_id": employeeID, "source_version": in.SourceVersion, "amount": amountText, "count": count, "qty": pieceQty})
			now := platform.Now()
			if _, err = tx.Exec(r.Context(), `INSERT INTO payroll_sheet(tenant_id,id,employee_id,payroll_period,attendance_days,base_salary,gross_amount,net_amount,outstanding_amount,source_snapshot_hash,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,0,0,0,$7,$8,$9,$10,$8,$9,$10)`, p.TenantID, id, employeeID, in.Period, attendance, base.StringFixed(6), snapshot, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			if err = insertPayrollItems(r.Context(), tx, p, id, amount, pieceQty, base); err != nil {
				return err
			}
			if err = recalcPayrollSheet(r.Context(), tx, p.TenantID, id); err != nil {
				return err
			}
			results = append(results, map[string]any{"employee_id": employeeID, "status": "succeeded", "payroll_sheet_id": id, "piecework_amount": amount.StringFixed(6), "piecework_qty": pieceQty, "attendance_days": attendance})
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"results": results})
	})
	api.Handle("GET /api/v1/payroll-sheets", "payroll:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listPayrollWide(api, w, r, p)
	})
	api.Handle("GET /api/v1/payroll-sheets/{id}", "payroll:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var sheet map[string]any
		if err := api.DB.QueryRow(r.Context(), "SELECT row_to_json(t) FROM (SELECT * FROM payroll_sheet WHERE tenant_id=$1 AND id=$2) t", p.TenantID, r.PathValue("id")).Scan(&sheet); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "工资单不存在")
		}
		rows, err := api.DB.Query(r.Context(), "SELECT item_type,item_code,item_name,qty::text,amount::text,source_reference FROM payroll_item WHERE tenant_id=$1 AND payroll_sheet_id=$2", p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var itemType, code, name, amount string
			var qty, source *string
			if err := rows.Scan(&itemType, &code, &name, &qty, &amount, &source); err != nil {
				return err
			}
			items = append(items, map[string]any{"item_type": itemType, "item_code": code, "item_name": name, "qty": qty, "amount": amount, "source_reference": source})
		}
		return httpapi.JSON(w, 200, map[string]any{"sheet": sheet, "items": items})
	})
	api.Handle("POST /api/v1/payroll-sheets/{id}/confirm", "payroll:confirm", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version            int64  `json:"version"`
			SourceSnapshotHash string `json:"source_snapshot_hash"`
			SecondFactor       string `json:"second_factor"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		result, err := api.DB.Exec(r.Context(), `UPDATE payroll_sheet SET status='confirmed',confirmed_by=$1,confirmed_at=$2,version=version+1,updated_at=$2,updated_by=$1,updated_node=$3 WHERE tenant_id=$4 AND id=$5 AND version=$6 AND status='draft' AND source_snapshot_hash=$7`, p.UserID, platform.Now(), api.Config.NodeID, p.TenantID, r.PathValue("id"), in.Version, in.SourceSnapshotHash)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(409, "INVALID_STATE", "工资单状态、版本或来源快照不一致")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "confirmed", "version": in.Version + 1})
	})
	api.Handle("POST /api/v1/payroll-sheets/{id}/payments", "payroll:pay", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error { return addPayment(api, w, r, p) })
	api.Handle("GET /api/v1/payroll-sheets/{id}/payments", "payroll:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,amount::text,paid_at,payment_method,operator_employee_id,external_reference,reversal_of_id,remark FROM payment WHERE tenant_id=$1 AND payroll_sheet_id=$2 ORDER BY created_at`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, amount, method, operator string
			var paid int64
			var reference, reversal, remark *string
			if err := rows.Scan(&id, &amount, &paid, &method, &operator, &reference, &reversal, &remark); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "amount": amount, "paid_at": paid, "payment_method": method, "operator_employee_id": operator, "external_reference": reference, "reversal_of_id": reversal, "remark": remark})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
}

func addPayment(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	key, err := httpapi.IdempotencyKey(r)
	if err != nil {
		return err
	}
	var in struct {
		Version           int64   `json:"version"`
		Amount            string  `json:"amount"`
		PaidAt            int64   `json:"paid_at"`
		PaymentMethod     string  `json:"payment_method"`
		ExternalReference *string `json:"external_reference"`
		SecondFactor      string  `json:"second_factor"`
		Remark            *string `json:"remark"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	amount, err := decimal.NewFromString(in.Amount)
	if err != nil || !amount.IsPositive() {
		return httpapi.Error(400, "INVALID_AMOUNT", "付款金额必须大于零")
	}
	if in.PaidAt == 0 {
		in.PaidAt = platform.Now()
	}
	if in.PaymentMethod == "" {
		return httpapi.Error(400, "VALIDATION_ERROR", "付款方式必填")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var outstanding, status string
	if err = tx.QueryRow(r.Context(), "SELECT outstanding_amount::text,status FROM payroll_sheet WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&outstanding, &status); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "工资单不存在")
	}
	if status != "confirmed" && status != "partially_paid" {
		return httpapi.Error(409, "INVALID_STATE", "仅已确认工资单可付款")
	}
	balance, _ := decimal.NewFromString(outstanding)
	if amount.GreaterThan(balance) {
		return httpapi.Error(400, "PAYMENT_EXCEEDED", "付款金额超过未发余额")
	}
	newBalance := balance.Sub(amount)
	newStatus := "partially_paid"
	if newBalance.IsZero() {
		newStatus = "paid"
	}
	id := platform.ID()
	if _, err = tx.Exec(r.Context(), `INSERT INTO payment(tenant_id,id,payroll_sheet_id,amount,paid_at,payment_method,operator_employee_id,external_reference,remark,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, p.TenantID, id, r.PathValue("id"), amount.StringFixed(6), in.PaidAt, in.PaymentMethod, p.EmployeeID, in.ExternalReference, in.Remark, platform.Now()); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE payroll_sheet SET status=$1,paid_amount=paid_amount+$2,outstanding_amount=outstanding_amount-$2,version=version+1,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$6 AND id=$7 AND version=$8`, newStatus, amount.StringFixed(6), platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), in.Version); err != nil {
		return err
	}
	if err = recalcPayrollSheet(r.Context(), tx, p.TenantID, r.PathValue("id")); err != nil {
		return err
	}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "pay", "payroll_sheet", r.PathValue("id"), nil, map[string]any{"payment_id": id, "amount": amount.StringFixed(6)}, ""); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	_ = key
	return httpapi.JSON(w, 201, map[string]any{"payment_id": id, "status": newStatus, "outstanding_amount": newBalance.StringFixed(6)})
}

var _ = pgx.ErrNoRows
