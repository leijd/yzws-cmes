package production

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/orgapp"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type reportInput struct {
	TaskID            string           `json:"task_id"`
	BundleID          string           `json:"bundle_id"`
	BundlePartID      string           `json:"bundle_part_id"`
	TicketID          string           `json:"ticket_id"`
	DeviceCode        *string          `json:"device_code"`
	ClientOccurredAt  *int64           `json:"client_occurred_at"`
	GoodQty           int64            `json:"good_qty"`
	DefectQty         int64            `json:"defect_qty"`
	DefectItems       []map[string]any `json:"defect_items"`
	ActualWorkSeconds *int64           `json:"actual_work_seconds"`
	AttachmentIDs     []string         `json:"attachment_ids"`
	ReportKind        string           `json:"report_kind"`
	OccurredAt        int64            `json:"occurred_at"`
	Remark            *string          `json:"remark"`
}

func registerReports(api *httpapi.API) {
	api.Handle("POST /api/v1/work-reports", "report:create", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createReport(api, w, r, p)
	})
	api.Handle("GET /api/v1/work-reports", "report:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listReports(api, w, r, p, "")
	})
	api.Handle("GET /api/v1/report-reviews", "report:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listReports(api, w, r, p, "pending")
	})
	api.Handle("GET /api/v1/work-reports/{id}", "report:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var item map[string]any
		err := api.DB.QueryRow(r.Context(), "SELECT row_to_json(t) FROM (SELECT * FROM work_report WHERE tenant_id=$1 AND id=$2) t", p.TenantID, r.PathValue("id")).Scan(&item)
		if err == pgx.ErrNoRows {
			return httpapi.Error(404, "NOT_FOUND", "报工不存在")
		}
		if err != nil {
			return err
		}
		/* 详情同样执行组织范围过滤（设计 5.7 第 5 条） */
		access, err := orgapp.ComputeAccess(r.Context(), api.DB, p)
		if err != nil {
			return err
		}
		if !access.All || access.Deny != nil {
			var workshopID, lineID, teamID *string
			employeeID, _ := item["employee_id"].(string)
			if employeeID != "" {
				_ = api.DB.QueryRow(r.Context(), `SELECT workshop_org_id,line_org_id,team_org_id FROM employee WHERE tenant_id=$1 AND id=$2`, p.TenantID, employeeID).Scan(&workshopID, &lineID, &teamID)
			}
			if !access.AllowEmployeeOrg(workshopID, lineID, teamID, employeeID) {
				api.RecordDenied(r.Context(), p, "work_report", r.PathValue("id"), "超出组织数据范围查看报工")
				return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "无权查看该报工记录")
			}
		}
		return httpapi.JSON(w, 200, item)
	})
	api.Handle("POST /api/v1/report-reviews/{report_id}/approve", "review:approve", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return reviewReport(api, w, r, p, "approve")
	})
	api.Handle("POST /api/v1/report-reviews/{report_id}/reject", "review:approve", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return reviewReport(api, w, r, p, "reject")
	})
	api.Handle("POST /api/v1/report-reviews/{report_id}/correct-approve", "review:correct", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return reviewReport(api, w, r, p, "correct_approve")
	})
	api.Handle("POST /api/v1/work-reports/{id}/withdraw", "report:create", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return reviewReport(api, w, r, p, "withdraw")
	})
	api.Handle("POST /api/v1/work-reports/{id}/reverse", "report:reverse", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return reverseReport(api, w, r, p)
	})
	api.Handle("GET /api/v1/report-reviews/{report_id}/history", "review:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,action,actor_type,actor_id,reason,before_values,after_values,rule_version,created_node,created_at FROM report_review WHERE tenant_id=$1 AND report_id=$2 ORDER BY created_at,id`, p.TenantID, r.PathValue("report_id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, action, actorType, node string
			var actor, reason, nodeID *string
			var before, after []byte
			var rule *int64
			var created int64
			if err := rows.Scan(&id, &action, &actorType, &actor, &reason, &before, &after, &rule, &node, &created); err != nil {
				return err
			}
			nodeID = &node
			items = append(items, map[string]any{"id": id, "action": action, "actor_type": actorType, "actor_id": actor, "reason": reason, "before_values": json.RawMessage(before), "after_values": json.RawMessage(after), "rule_version": rule, "created_node": nodeID, "created_at": created})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
}

// checkPreviousProcess 按工序拓扑校验：后道工序不能在直接前驱报工前报工；
// 并行汇合点可用量取所有汇入分支的最小值，而不是合计。
func checkPreviousProcess(ctx context.Context, tx pgx.Tx, api *httpapi.API, tenantID, taskID string, extra int64) error {
	/* 当前工序快照：取 work_order_id / product_process_id / sequence_no / prev_ids */
	var workOrderID, ppID string
	var sequenceNo int
	var prevRaw []byte
	err := tx.QueryRow(ctx, `SELECT s.work_order_id,s.product_process_id,s.sequence_no,s.prev_ids FROM production_task t JOIN work_order_process_snapshot s ON s.tenant_id=t.tenant_id AND s.work_order_id=t.work_order_id AND s.product_process_id=t.product_process_id WHERE t.tenant_id=$1 AND t.id=$2`, tenantID, taskID).Scan(&workOrderID, &ppID, &sequenceNo, &prevRaw)
	if err != nil {
		return err
	}
	var prevIDs []string
	_ = json.Unmarshal(prevRaw, &prevIDs)
	var available, current int64
	branchQty := map[string]int64{}
	hasPrevious := len(prevIDs) > 0
	if hasPrevious {
		/* 拓扑模式：逐条汇入分支统计；并行汇合取最小值，任一分支未报工则后道工序不可报 */
		rows, err := tx.Query(ctx, `SELECT p.ppid,COALESCE(SUM(t.approved_good_qty),0)
			FROM unnest($3::text[]) AS p(ppid)
			LEFT JOIN production_task t ON t.tenant_id=$1 AND t.work_order_id=$2 AND t.product_process_id=p.ppid
			GROUP BY p.ppid`, tenantID, workOrderID, prevIDs)
		if err != nil {
			return err
		}
		available = -1
		unready := []string{}
		for rows.Next() {
			var prevID string
			var qty int64
			if err := rows.Scan(&prevID, &qty); err != nil {
				rows.Close()
				return err
			}
			branchQty[prevID] = qty
			if available < 0 || qty < available {
				available = qty
			}
			if qty <= 0 {
				unready = append(unready, prevID)
			}
		}
		rowErr := rows.Err()
		rows.Close()
		if rowErr != nil {
			return rowErr
		}
		if available < 0 {
			available = 0
		}
		if len(unready) > 0 {
			appErr := httpapi.Error(400, "PREVIOUS_PROCESS_NOT_REPORTED", "前道工序尚未报工或审批，当前工序不能报工")
			appErr.Details = map[string]any{"previous_process_ids": unready, "branch_qty": branchQty}
			return appErr
		}
	} else {
		/* 兼容旧数据：无拓扑边时回退到 sequence_no 更早的非并行前道 */
		err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(t.approved_good_qty),0) FROM production_task t JOIN work_order_process_snapshot s ON s.tenant_id=t.tenant_id AND s.work_order_id=t.work_order_id AND s.product_process_id=t.product_process_id WHERE t.tenant_id=$1 AND t.work_order_id=$2 AND s.sequence_no<$3 AND (s.route_flags->>'parallel')::boolean=false`, tenantID, workOrderID, sequenceNo).Scan(&available)
		if err != nil {
			return err
		}
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM work_order_process_snapshot WHERE tenant_id=$1 AND work_order_id=$2 AND sequence_no<$3)`, tenantID, workOrderID, sequenceNo).Scan(&hasPrevious); err != nil {
			return err
		}
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(reported_qty),0) FROM production_task WHERE tenant_id=$1 AND work_order_id=$2 AND product_process_id=$3`, tenantID, workOrderID, ppID).Scan(&current); err != nil {
		return err
	}
	if hasPrevious && current+extra > available {
		appErr := httpapi.Error(400, "PREVIOUS_PROCESS_INCOMPLETE", "报工数量超过前道工序可用量（并行汇合取最小分支）")
		appErr.Details = map[string]any{"previous_available": available, "branch_qty": branchQty, "current_reported": current, "requested": extra}
		return appErr
	}
	return nil
}

func listReports(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, defaultStatus string) error {
	args := []any{p.TenantID}
	where := "wr.tenant_id=$1"
	status := r.URL.Query().Get("status")
	if status == "" {
		status = defaultStatus
	}
	if status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND wr.review_status=$%d", len(args))
	}
	if employee := r.URL.Query().Get("employee_id"); employee != "" {
		args = append(args, employee)
		where += fmt.Sprintf(" AND wr.employee_id=$%d", len(args))
	}
	/* 数据范围（设计 5.7）：按报工人员的组织归属过滤车间/产线/班组负责人可见范围 */
	access, err := orgapp.ComputeAccess(r.Context(), api.DB, p)
	if err != nil {
		return err
	}
	scopeCond, scopeArgs := access.OrgScopeSQL("e", len(args)+1)
	args = append(args, scopeArgs...)
	where += scopeCond
	rows, err := api.DB.Query(r.Context(), `SELECT wr.id,wr.task_id,s.process_name,e.name,wr.good_qty,wr.defect_qty,wr.unit_price::text,wr.review_status,wr.report_kind,wr.occurred_at,COALESCE(pe.amount::text,''),wr.idempotency_key FROM work_report wr JOIN production_task t ON t.tenant_id=wr.tenant_id AND t.id=wr.task_id JOIN work_order_process_snapshot s ON s.tenant_id=t.tenant_id AND s.work_order_id=t.work_order_id AND s.product_process_id=t.product_process_id JOIN employee e ON e.tenant_id=wr.tenant_id AND e.id=wr.employee_id LEFT JOIN piecework_entry pe ON pe.tenant_id=wr.tenant_id AND pe.report_id=wr.id AND pe.entry_kind='confirmed' WHERE `+where+" ORDER BY wr.occurred_at DESC,wr.id LIMIT 200", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, task, process, employee, price, status, kind, key string
		var good, defect, occurred int64
		var amount string
		if err := rows.Scan(&id, &task, &process, &employee, &good, &defect, &price, &status, &kind, &occurred, &amount, &key); err != nil {
			return err
		}
		var amountValue any
		if amount != "" {
			amountValue = amount
		}
		items = append(items, map[string]any{"id": id, "task_id": task, "process_name": process, "employee_name": employee, "good_qty": good, "defect_qty": defect, "unit_price": price, "review_status": status, "report_kind": kind, "occurred_at": occurred, "piecework_amount": amountValue, "idempotency_key": key})
	}
	return httpapi.JSON(w, 200, map[string]any{"items": items, "has_more": false})
}

func reviewReport(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, action string) error {
	var in struct {
		Version           int64   `json:"version"`
		Reason            string  `json:"reason"`
		EmployeeID        *string `json:"employee_id"`
		GoodQty           *int64  `json:"good_qty"`
		DefectQty         *int64  `json:"defect_qty"`
		UnitPrice         *string `json:"unit_price"`
		ActualWorkSeconds *int64  `json:"actual_work_seconds"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	reportID := r.PathValue("report_id")
	if reportID == "" {
		reportID = r.PathValue("id")
	}
	if (action == "reject" || action == "withdraw" || action == "correct_approve") && in.Reason == "" {
		return httpapi.Error(400, "REASON_REQUIRED", "必须填写原因")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var taskID, employeeID, price, status, kind string
	var good, defect, occurred int64
	if err = tx.QueryRow(r.Context(), "SELECT task_id,employee_id,unit_price::text,review_status,report_kind,good_qty,defect_qty,occurred_at FROM work_report WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, reportID).Scan(&taskID, &employeeID, &price, &status, &kind, &good, &defect, &occurred); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "报工不存在")
	}
	if status != "pending" || kind == "reversal" {
		return httpapi.Error(409, "INVALID_STATE", "仅待审批的正常报工可执行此操作")
	}
	before := map[string]any{"employee_id": employeeID, "good_qty": good, "defect_qty": defect, "unit_price": price}
	if action == "correct_approve" {
		if in.EmployeeID != nil {
			employeeID = *in.EmployeeID
		}
		if in.GoodQty != nil {
			good = *in.GoodQty
		}
		if in.DefectQty != nil {
			defect = *in.DefectQty
		}
		if in.UnitPrice != nil {
			price = *in.UnitPrice
		}
		if good < 0 || defect < 0 || good+defect <= 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "报工数量不合法")
		}
		if _, err = decimal.NewFromString(price); err != nil {
			return httpapi.Error(400, "INVALID_DECIMAL", "工价格式不正确")
		}
	}
	var planned, reported, pending int64
	if err = tx.QueryRow(r.Context(), "SELECT planned_qty,reported_qty,pending_qty FROM production_task WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, taskID).Scan(&planned, &reported, &pending); err != nil {
		return err
	}
	switch action {
	case "approve":
		if err = checkPreviousProcess(r.Context(), tx, api, p.TenantID, taskID, 0); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "UPDATE production_task SET pending_qty=pending_qty-$1,approved_good_qty=approved_good_qty+$2,version=version+1,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$6 AND id=$7", good+defect, good, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, taskID); err != nil {
			return err
		}
		if err = insertPiecework(r.Context(), tx, p.TenantID, reportID, employeeID, "confirmed", good, price, occurred, nil); err != nil {
			return err
		}
	case "reject", "withdraw":
		if _, err = tx.Exec(r.Context(), "UPDATE production_task SET reported_qty=reported_qty-$1,pending_qty=pending_qty-$1,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6", good+defect, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, taskID); err != nil {
			return err
		}
	case "correct_approve":
		newTotal := good + defect
		var oldGood, oldDefect int64
		if err = tx.QueryRow(r.Context(), "SELECT good_qty,defect_qty FROM work_report WHERE tenant_id=$1 AND id=$2", p.TenantID, reportID).Scan(&oldGood, &oldDefect); err != nil {
			return err
		}
		oldTotal := oldGood + oldDefect
		delta := newTotal - oldTotal
		if reported+delta > planned {
			return httpapi.Error(400, "QUANTITY_EXCEEDED", "纠错后的累计报工超过任务计划量")
		}
		if delta > 0 {
			if err = checkPreviousProcess(r.Context(), tx, api, p.TenantID, taskID, delta); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(r.Context(), `UPDATE work_report SET employee_id=$1,good_qty=$2,defect_qty=$3,unit_price=$4,actual_work_seconds=COALESCE($5,actual_work_seconds),review_status='approved' WHERE tenant_id=$6 AND id=$7`, employeeID, good, defect, price, in.ActualWorkSeconds, p.TenantID, reportID); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), "UPDATE production_task SET reported_qty=reported_qty+$1,pending_qty=pending_qty-$2,approved_good_qty=approved_good_qty+$3,version=version+1,updated_at=$4,updated_by=$5,updated_node=$6 WHERE tenant_id=$7 AND id=$8", delta, oldTotal, good, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, taskID); err != nil {
			return err
		}
		if err = insertPiecework(r.Context(), tx, p.TenantID, reportID, employeeID, "confirmed", good, price, occurred, nil); err != nil {
			return err
		}
	}
	newStatus := "approved"
	if action == "reject" || action == "withdraw" {
		newStatus = "rejected"
	}
	if action != "correct_approve" {
		if _, err = tx.Exec(r.Context(), "UPDATE work_report SET review_status=$1 WHERE tenant_id=$2 AND id=$3", newStatus, p.TenantID, reportID); err != nil {
			return err
		}
	}
	after := map[string]any{"employee_id": employeeID, "good_qty": good, "defect_qty": defect, "unit_price": price, "review_status": newStatus}
	if err = insertReview(r.Context(), tx, p.TenantID, reportID, action, "user", &p.UserID, before, after, 0, api.Config.NodeID); err != nil {
		return err
	}
	if err = refreshProgressForTask(r.Context(), tx, p.TenantID, taskID); err != nil {
		return err
	}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, action, "work_report", reportID, before, after, in.Reason); err != nil {
		return err
	}
	response := map[string]any{"report_id": reportID, "review_status": newStatus, "piecework_amount": nil}
	if newStatus == "approved" {
		amount, _ := decimal.NewFromString(price)
		response["piecework_amount"] = amount.Mul(decimal.NewFromInt(good)).StringFixed(6)
	}
	if err = httpapi.Outbox(r.Context(), tx, p.TenantID, api.Config.NodeID, "work_report", reportID, "work_report.reviewed", response); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 200, response)
}

func reverseReport(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	key, err := httpapi.IdempotencyKey(r)
	if err != nil {
		return err
	}
	var in struct {
		Version int64  `json:"version"`
		Reason  string `json:"reason"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.Reason == "" {
		return httpapi.Error(400, "REASON_REQUIRED", "冲销原因必填")
	}
	originalID := r.PathValue("id")
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var taskID, employeeID, price, status, kind string
	var good, defect, occurred int64
	if err = tx.QueryRow(r.Context(), "SELECT task_id,employee_id,unit_price::text,review_status,report_kind,good_qty,defect_qty,occurred_at FROM work_report WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, originalID).Scan(&taskID, &employeeID, &price, &status, &kind, &good, &defect, &occurred); err != nil {
		return httpapi.Error(404, "NOT_FOUND", "报工不存在")
	}
	if status != "approved" || kind == "reversal" {
		return httpapi.Error(409, "INVALID_STATE", "仅已审批报工可冲销")
	}
	var exists bool
	if err = tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM work_report WHERE tenant_id=$1 AND original_report_id=$2 AND report_kind='reversal')", p.TenantID, originalID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return httpapi.Error(409, "ALREADY_REVERSED", "报工已冲销")
	}
	if err = tx.QueryRow(r.Context(), "SELECT id FROM production_task WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, taskID).Scan(&taskID); err != nil {
		return err
	}
	id, now := platform.ID(), platform.Now()
	_, err = tx.Exec(r.Context(), `INSERT INTO work_report(tenant_id,id,task_id,report_kind,good_qty,defect_qty,employee_id,unit_price,standard_work_seconds,original_report_id,review_status,idempotency_key,occurred_at,created_node,created_at,bundle_id,bundle_part_id,ticket_id,process_snapshot_id,device_code,project_id,work_order_id,product_code,bundle_no,color,size,client_occurred_at) SELECT tenant_id,$1,task_id,'reversal',$2,$3,employee_id,unit_price,0,id,'approved',$4,$5,$6,$7,bundle_id,bundle_part_id,ticket_id,process_snapshot_id,device_code,project_id,work_order_id,product_code,bundle_no,color,size,client_occurred_at FROM work_report WHERE tenant_id=$8 AND id=$9`, id, good, defect, key, now, api.Config.NodeID, now, p.TenantID, originalID)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), "UPDATE production_task SET reported_qty=reported_qty-$1,approved_good_qty=approved_good_qty-$2,version=version+1,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$6 AND id=$7", good+defect, good, now, p.UserID, api.Config.NodeID, p.TenantID, taskID); err != nil {
		return err
	}
	if err = insertReview(r.Context(), tx, p.TenantID, id, "reverse", "user", &p.UserID, nil, map[string]any{"original_report_id": originalID}, 0, api.Config.NodeID); err != nil {
		return err
	}
	if err = insertPiecework(r.Context(), tx, p.TenantID, id, employeeID, "reversal", -good, price, now, &originalID); err != nil {
		return err
	}
	if err = refreshProgressForTask(r.Context(), tx, p.TenantID, taskID); err != nil {
		return err
	}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "reverse", "work_report", originalID, nil, map[string]any{"reversal_id": id}, in.Reason); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 201, map[string]any{"reversal_id": id, "original_report_id": originalID, "review_status": "approved", "sync_status": "pending"})
}

func insertReview(ctx context.Context, tx pgx.Tx, tenantID, reportID, action, actorType string, actorID *string, before, after any, ruleVersion int64, nodeID string) error {
	var beforeBody, afterBody []byte
	if before != nil {
		beforeBody, _ = json.Marshal(before)
	}
	if after != nil {
		afterBody, _ = json.Marshal(after)
	}
	var rule any
	if ruleVersion > 0 {
		rule = ruleVersion
	}
	_, err := tx.Exec(ctx, `INSERT INTO report_review(tenant_id,id,report_id,action,actor_type,actor_id,before_values,after_values,rule_version,created_node,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, tenantID, platform.ID(), reportID, action, actorType, actorID, beforeBody, afterBody, rule, nodeID, platform.Now())
	return err
}
func insertPiecework(ctx context.Context, tx pgx.Tx, tenantID, reportID, employeeID, kind string, qty int64, unitPrice string, occurredAt int64, sourceID *string) error {
	price, err := decimal.NewFromString(unitPrice)
	if err != nil {
		return err
	}
	amount := price.Mul(decimal.NewFromInt(qty)).Round(6)
	period := time.UnixMicro(occurredAt).UTC().Format("2006-01")
	_, err = tx.Exec(ctx, `INSERT INTO piecework_entry(tenant_id,id,report_id,employee_id,entry_kind,qty,unit_price,coefficient,amount,payroll_period,source_entry_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,1,$8,$9,$10,$11)`, tenantID, platform.ID(), reportID, employeeID, kind, qty, unitPrice, amount.StringFixed(6), period, sourceID, platform.Now())
	return err
}
