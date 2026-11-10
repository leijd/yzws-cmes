package production

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type payrollItemSpec struct {
	code, itemType, name string
	deduction, notGross  bool
}

var payrollItemSpecs = []payrollItemSpec{
	{code: "piecework", itemType: "piecework", name: "计件工资"},
	{code: "base_salary", itemType: "base", name: "底薪"},
	{code: "position_allowance", itemType: "allowance", name: "岗位补贴"},
	{code: "attendance_bonus", itemType: "bonus", name: "全勤奖"},
	{code: "meal_allowance", itemType: "allowance", name: "餐补"},
	{code: "defect_deduction", itemType: "deduction", name: "次品扣款", deduction: true},
	{code: "late_deduction", itemType: "deduction", name: "迟到扣款", deduction: true},
	{code: "advance_deduction", itemType: "deduction", name: "借支扣款", deduction: true},
	{code: "social_insurance", itemType: "social_insurance", name: "社保扣款", deduction: true, notGross: true},
	{code: "tax", itemType: "tax", name: "个税", deduction: true, notGross: true},
	{code: "manual_adjustment", itemType: "manual", name: "手工调整"},
}

func payrollSpecByCode(code string) (payrollItemSpec, bool) {
	for _, spec := range payrollItemSpecs {
		if spec.code == code {
			return spec, true
		}
	}
	return payrollItemSpec{}, false
}

func insertPayrollItems(ctx context.Context, tx pgx.Tx, p auth.Principal, sheetID string, pieceAmount decimal.Decimal, pieceQty int64, base decimal.Decimal) error {
	for _, spec := range payrollItemSpecs {
		amount := decimal.Zero
		var qty *int64
		switch spec.code {
		case "piecework":
			amount = pieceAmount
			qty = &pieceQty
		case "base_salary":
			amount = base
		}
		if _, err := tx.Exec(ctx, `INSERT INTO payroll_item(tenant_id,id,payroll_sheet_id,item_type,item_code,item_name,qty,amount,source_reference) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, p.TenantID, platform.ID(), sheetID, spec.itemType, spec.code, spec.name, qty, amount.StringFixed(6), nil); err != nil {
			return err
		}
	}
	return nil
}

func recalcPayrollSheet(ctx context.Context, tx pgx.Tx, tenantID, sheetID string) error {
	rows, err := tx.Query(ctx, "SELECT item_code,amount::text FROM payroll_item WHERE tenant_id=$1 AND payroll_sheet_id=$2", tenantID, sheetID)
	if err != nil {
		return err
	}
	gross, social, tax := decimal.Zero, decimal.Zero, decimal.Zero
	for rows.Next() {
		var code, text string
		if err := rows.Scan(&code, &text); err != nil {
			rows.Close()
			return err
		}
		amount, err := decimal.NewFromString(text)
		if err != nil {
			rows.Close()
			return err
		}
		switch code {
		case "social_insurance":
			social = social.Add(amount)
		case "tax":
			tax = tax.Add(amount)
		default:
			gross = gross.Add(amount)
		}
	}
	rows.Close()
	var adjustmentText, paidText, status string
	if err := tx.QueryRow(ctx, "SELECT COALESCE(SUM(amount),0)::text FROM payroll_adjustment WHERE tenant_id=$1 AND payroll_sheet_id=$2", tenantID, sheetID).Scan(&adjustmentText); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT COALESCE(SUM(CASE WHEN reversal_of_id IS NULL THEN amount ELSE -amount END),0)::text FROM payment WHERE tenant_id=$1 AND payroll_sheet_id=$2", tenantID, sheetID).Scan(&paidText); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT status FROM payroll_sheet WHERE tenant_id=$1 AND id=$2", tenantID, sheetID).Scan(&status); err != nil {
		return err
	}
	adjustment, err := decimal.NewFromString(adjustmentText)
	if err != nil {
		return err
	}
	paid, err := decimal.NewFromString(paidText)
	if err != nil {
		return err
	}
	gross = gross.Add(adjustment)
	net := gross.Add(social).Add(tax)
	outstanding := net.Sub(paid)
	switch status {
	case "confirmed", "partially_paid", "paid":
		status = "confirmed"
		if paid.IsPositive() && outstanding.IsPositive() {
			status = "partially_paid"
		} else if paid.IsPositive() {
			status = "paid"
		}
	}
	_, err = tx.Exec(ctx, "UPDATE payroll_sheet SET gross_amount=$1,net_amount=$2,paid_amount=$3,outstanding_amount=$4,status=$5 WHERE tenant_id=$6 AND id=$7", gross.StringFixed(6), net.StringFixed(6), paid.StringFixed(6), outstanding.StringFixed(6), status, tenantID, sheetID)
	return err
}

func maskBankCard(card *string) string {
	if card == nil || strings.TrimSpace(*card) == "" {
		return "—"
	}
	value := strings.TrimSpace(*card)
	if len(value) <= 4 {
		return value
	}
	return "**** " + value[len(value)-4:]
}

func listPayrollWide(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	args := []any{p.TenantID}
	where := "s.tenant_id=$1"
	for param, column := range map[string]string{"period": "s.payroll_period", "status": "s.status", "workshop": "e.workshop"} {
		if value := strings.TrimSpace(r.URL.Query().Get(param)); value != "" {
			args = append(args, value)
			where += fmt.Sprintf(" AND %s=$%d", column, len(args))
		}
	}
	sumOf := func(code string) string {
		return fmt.Sprintf("(SELECT COALESCE(SUM(i.amount),0)::text FROM payroll_item i WHERE i.tenant_id=s.tenant_id AND i.payroll_sheet_id=s.id AND i.item_code='%s')", code)
	}
	pieceQty := "(SELECT COALESCE(SUM(i.qty),0)::text FROM payroll_item i WHERE i.tenant_id=s.tenant_id AND i.payroll_sheet_id=s.id AND i.item_type='piecework')"
	pieceAmount := "(SELECT COALESCE(SUM(i.amount),0)::text FROM payroll_item i WHERE i.tenant_id=s.tenant_id AND i.payroll_sheet_id=s.id AND i.item_type='piecework')"
	columns := []string{"s.id", "e.employee_no", "e.name", "COALESCE(e.position,'')", "COALESCE(e.workshop,'')", "COALESCE(e.production_line,'')", "s.attendance_days::text",
		pieceQty, pieceAmount, sumOf("base_salary"), sumOf("position_allowance"), sumOf("attendance_bonus"), sumOf("meal_allowance"), sumOf("defect_deduction"), sumOf("late_deduction"), sumOf("advance_deduction"),
		"s.gross_amount::text", sumOf("social_insurance"), sumOf("tax"), "s.net_amount::text", "s.paid_amount::text", "s.outstanding_amount::text", "e.bank_card", "s.status"}
	rows, err := api.DB.Query(r.Context(), fmt.Sprintf(`SELECT %s FROM payroll_sheet s JOIN employee e ON e.tenant_id=s.tenant_id AND e.id=s.employee_id WHERE %s ORDER BY s.payroll_period DESC,e.employee_no,s.id LIMIT 500`, strings.Join(columns, ","), where), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, employeeNo, name, position, workshop, line, attendance, pieceQtyText, pieceAmountText string
		var base, positionAllowance, attendanceBonus, meal, defect, late, advance string
		var gross, social, tax, net, paid, outstanding string
		var bank *string
		var status string
		if err := rows.Scan(&id, &employeeNo, &name, &position, &workshop, &line, &attendance, &pieceQtyText, &pieceAmountText, &base, &positionAllowance, &attendanceBonus, &meal, &defect, &late, &advance, &gross, &social, &tax, &net, &paid, &outstanding, &bank, &status); err != nil {
			return err
		}
		signature := "未确认"
		switch status {
		case "confirmed", "partially_paid", "paid", "locked":
			signature = "已确认"
		}
		items = append(items, map[string]any{
			"id": id, "employee_no": employeeNo, "employee_name": name, "position": position, "workshop": workshop, "production_line": line,
			"attendance_days": attendance, "piecework_qty": pieceQtyText, "piecework_amount": pieceAmountText, "base_salary": base,
			"position_allowance": positionAllowance, "attendance_bonus": attendanceBonus, "meal_allowance": meal,
			"defect_deduction": defect, "late_deduction": late, "advance_deduction": advance, "gross_amount": gross,
			"social_insurance": social, "tax": tax, "net_amount": net, "paid_amount": paid, "outstanding_amount": outstanding,
			"bank_card_masked": maskBankCard(bank), "signature_status": signature, "status": status,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return httpapi.JSON(w, 200, map[string]any{"items": items, "formula": map[string]string{
		"gross_amount": "应发 = 计件 + 底薪 + 岗位补贴 + 全勤奖 + 餐补 + 手工调整 + 扣款项（扣款以负数记录）+ 确认后调整",
		"net_amount":   "实发 = 应发 - 社保 - 个税",
		"outstanding":  "未发 = 实发 - 有效付款流水合计",
	}})
}

func registerPayrollWide(api *httpapi.API) {
	api.Handle("PUT /api/v1/payroll-sheets/{id}/items/{code}", "payroll:confirm", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Amount string `json:"amount"`
			Reason string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		spec, ok := payrollSpecByCode(r.PathValue("code"))
		if !ok || spec.code == "piecework" {
			return httpapi.Error(400, "INVALID_PAYROLL_ITEM", "该工资项目不可手工维护")
		}
		amount, err := decimal.NewFromString(strings.TrimSpace(in.Amount))
		if err != nil {
			return httpapi.Error(400, "INVALID_AMOUNT", "金额格式不正确")
		}
		if spec.deduction {
			amount = amount.Abs().Neg()
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		sheetID := r.PathValue("id")
		var status string
		if err := tx.QueryRow(r.Context(), "SELECT status FROM payroll_sheet WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, sheetID).Scan(&status); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "工资单不存在")
		}
		if status != "draft" {
			return httpapi.Error(409, "INVALID_STATE", "已确认的工资单只能通过工资调整修改")
		}
		result, err := tx.Exec(r.Context(), "UPDATE payroll_item SET amount=$1,source_reference=$2 WHERE tenant_id=$3 AND payroll_sheet_id=$4 AND item_code=$5", amount.StringFixed(6), optionalText(&in.Reason), p.TenantID, sheetID, spec.code)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			if _, err := tx.Exec(r.Context(), `INSERT INTO payroll_item(tenant_id,id,payroll_sheet_id,item_type,item_code,item_name,qty,amount,source_reference) VALUES($1,$2,$3,$4,$5,$6,NULL,$7,$8)`, p.TenantID, platform.ID(), sheetID, spec.itemType, spec.code, spec.name, amount.StringFixed(6), optionalText(&in.Reason)); err != nil {
				return err
			}
		}
		if err := recalcPayrollSheet(r.Context(), tx, p.TenantID, sheetID); err != nil {
			return err
		}
		response, err := payrollTotals(r.Context(), tx, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		response["item_code"] = spec.code
		response["amount"] = amount.StringFixed(6)
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "edit_payroll_item", "payroll_sheet", sheetID, nil, response, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
	})

	api.Handle("POST /api/v1/payroll-sheets/{id}/adjustments", "payroll:confirm", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			AdjustmentType string `json:"adjustment_type"`
			Amount         string `json:"amount"`
			Reason         string `json:"reason"`
			TargetPeriod   string `json:"target_period"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.AdjustmentType != "quality" && in.AdjustmentType != "report" && in.AdjustmentType != "manual" && in.AdjustmentType != "correction" {
			return httpapi.Error(400, "VALIDATION_ERROR", "调整类型只能是 quality/report/manual/correction")
		}
		amount, err := decimal.NewFromString(strings.TrimSpace(in.Amount))
		if err != nil || amount.IsZero() {
			return httpapi.Error(400, "INVALID_AMOUNT", "调整金额必须是非零的十进制数")
		}
		in.Reason = strings.TrimSpace(in.Reason)
		if in.Reason == "" {
			return httpapi.Error(400, "REASON_REQUIRED", "工资调整必须填写原因")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		sheetID := r.PathValue("id")
		var status, period string
		if err := tx.QueryRow(r.Context(), "SELECT status,payroll_period FROM payroll_sheet WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, sheetID).Scan(&status, &period); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "工资单不存在")
		}
		if status == "draft" {
			return httpapi.Error(409, "INVALID_STATE", "草稿工资单请直接维护工资项目")
		}
		target := in.TargetPeriod
		if target == "" {
			target = period
		}
		if len(target) != 7 {
			return httpapi.Error(400, "VALIDATION_ERROR", "计入期间格式应为 YYYY-MM")
		}
		id := platform.ID()
		if _, err := tx.Exec(r.Context(), `INSERT INTO payroll_adjustment(tenant_id,id,payroll_sheet_id,adjustment_type,amount,reason,target_period,operator_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, p.TenantID, id, sheetID, in.AdjustmentType, amount.StringFixed(6), in.Reason, target, p.UserID, platform.Now()); err != nil {
			return err
		}
		if err := recalcPayrollSheet(r.Context(), tx, p.TenantID, sheetID); err != nil {
			return err
		}
		response, err := payrollTotals(r.Context(), tx, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		response["adjustment_id"] = id
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "payroll_adjustment", "payroll_sheet", sheetID, nil, map[string]any{"adjustment_id": id, "amount": amount.StringFixed(6), "adjustment_type": in.AdjustmentType}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, response)
	})

	api.Handle("GET /api/v1/payroll-sheets/{id}/calculation", "payroll:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		sheetID := r.PathValue("id")
		totals, err := payrollTotals(r.Context(), api.DB, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		items, err := queryMaps(r.Context(), api.DB, `SELECT item_type,item_code,item_name,qty::text,amount::text FROM payroll_item WHERE tenant_id=$1 AND payroll_sheet_id=$2 ORDER BY item_type,item_code`, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		adjustments, err := queryMaps(r.Context(), api.DB, `SELECT id,adjustment_type,amount::text,reason,target_period,created_at FROM payroll_adjustment WHERE tenant_id=$1 AND payroll_sheet_id=$2 ORDER BY created_at`, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		payments, err := queryMaps(r.Context(), api.DB, `SELECT id,amount::text,paid_at,payment_method,reversal_of_id FROM payment WHERE tenant_id=$1 AND payroll_sheet_id=$2 ORDER BY paid_at`, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		totals["items"] = items
		totals["adjustments"] = adjustments
		totals["payments"] = payments
		totals["formula"] = map[string]string{
			"gross_amount": "应发 = 计件 + 底薪 + 岗位补贴 + 全勤奖 + 餐补 + 手工调整 + 扣款项（负数）+ 确认后调整合计",
			"net_amount":   "实发 = 应发 - 社保 - 个税",
			"outstanding":  "未发 = 实发 - 有效付款流水合计",
		}
		return httpapi.JSON(w, 200, totals)
	})
}

func payrollTotals(ctx context.Context, q dbQuerier, tenantID, sheetID string) (map[string]any, error) {
	var id, status, gross, net, paid, outstanding string
	err := q.QueryRow(ctx, "SELECT id,status,gross_amount::text,net_amount::text,paid_amount::text,outstanding_amount::text FROM payroll_sheet WHERE tenant_id=$1 AND id=$2", tenantID, sheetID).Scan(&id, &status, &gross, &net, &paid, &outstanding)
	if err != nil {
		return nil, httpapi.Error(404, "NOT_FOUND", "工资单不存在")
	}
	return map[string]any{"id": id, "status": status, "gross_amount": gross, "net_amount": net, "paid_amount": paid, "outstanding_amount": outstanding, "sync_status": "pending"}, nil
}

func queryMaps(ctx context.Context, q dbQuerier, sql string, args ...any) ([]map[string]any, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := rows.FieldDescriptions()
	items := []map[string]any{}
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		item := map[string]any{}
		for i, column := range columns {
			item[string(column.Name)] = values[i]
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
