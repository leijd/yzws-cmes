package production

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

var businessZone = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

func registerProgress(api *httpapi.API) {
	api.Handle("GET /api/v1/progress/styles", "progress:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "s.tenant_id=$1"
		if status := strings.TrimSpace(r.URL.Query().Get("business_status")); status != "" {
			args = append(args, status)
			where += fmt.Sprintf(" AND s.business_status=$%d", len(args))
		}
		if level := strings.TrimSpace(r.URL.Query().Get("warning_level")); level != "" {
			args = append(args, level)
			where += fmt.Sprintf(" AND s.warning_level=$%d", len(args))
		}
		if code := strings.TrimSpace(r.URL.Query().Get("project_code")); code != "" {
			args = append(args, "%"+code+"%")
			where += fmt.Sprintf(" AND pj.project_code ILIKE $%d", len(args))
		}
		rows, err := api.DB.Query(r.Context(), fmt.Sprintf(`SELECT pj.id,pj.project_code,pr.product_code,pr.name,COALESCE(c.name,''),s.order_qty,s.cut_qty,s.wip_qty,s.received_qty,s.shipped_qty,s.progress_ratio::text,COALESCE(s.current_node_text,''),COALESCE(ws.name,''),COALESCE(ls.name,''),COALESCE(s.planned_delivery_date,''),s.remaining_days,s.avg_receipt_qty_4d::text,s.estimated_days::text,COALESCE(s.warning_level,''),COALESCE(s.business_status,''),s.data_version FROM style_progress_projection s JOIN project pj ON pj.tenant_id=s.tenant_id AND pj.id=s.project_id JOIN product pr ON pr.tenant_id=pj.tenant_id AND pr.id=pj.product_id LEFT JOIN customer c ON c.tenant_id=pj.tenant_id AND c.id=pj.customer_id LEFT JOIN org_unit ws ON ws.tenant_id=s.tenant_id AND ws.id=s.workshop_org_unit_id LEFT JOIN org_unit ls ON ls.tenant_id=s.tenant_id AND ls.id=s.line_org_unit_id WHERE %s ORDER BY pj.project_code`, where), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, projectCode, productCode, productName, customer, currentNode, workshop, line, delivery, warning, status string
			var orderQty, cutQty, wip, received, shipped, dataVersion int64
			var ratio, avg, estimated *string
			var remaining *int64
			if err := rows.Scan(&id, &projectCode, &productCode, &productName, &customer, &orderQty, &cutQty, &wip, &received, &shipped, &ratio, &currentNode, &workshop, &line, &delivery, &remaining, &avg, &estimated, &warning, &status, &dataVersion); err != nil {
				return err
			}
			items = append(items, map[string]any{"project_id": id, "project_code": projectCode, "product_code": productCode, "product_name": productName, "customer_name": customer, "order_qty": orderQty, "cut_qty": cutQty, "wip_qty": wip, "received_qty": received, "shipped_qty": shipped, "progress_ratio": ratio, "current_node": currentNode, "workshop_name": workshop, "line_name": line, "planned_delivery_date": delivery, "remaining_days": remaining, "avg_receipt_qty_4d": avg, "estimated_days": estimated, "warning_level": warning, "business_status": status, "data_version": dataVersion})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("GET /api/v1/progress/styles/{projectId}", "progress:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var orderQty, cutQty, wip, received, shipped, dataVersion int64
		var ratio, avg, estimated *string
		var remaining *int64
		var warning, status, currentNode, delivery string
		err := api.DB.QueryRow(r.Context(), `SELECT s.order_qty,s.cut_qty,s.wip_qty,s.received_qty,s.shipped_qty,s.progress_ratio::text,COALESCE(s.current_node_text,''),COALESCE(s.planned_delivery_date,''),s.remaining_days,s.avg_receipt_qty_4d::text,s.estimated_days::text,COALESCE(s.warning_level,''),COALESCE(s.business_status,''),s.data_version FROM style_progress_projection s WHERE s.tenant_id=$1 AND s.project_id=$2`, p.TenantID, r.PathValue("projectId")).Scan(&orderQty, &cutQty, &wip, &received, &shipped, &ratio, &currentNode, &delivery, &remaining, &avg, &estimated, &warning, &status, &dataVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "项目进度不存在，请先刷新进度投影")
		}
		if err != nil {
			return err
		}
		rows, err := api.DB.Query(r.Context(), `SELECT id,work_order_code,status,planned_product_qty,review_status FROM work_order WHERE tenant_id=$1 AND project_id=$2 ORDER BY work_order_code`, p.TenantID, r.PathValue("projectId"))
		if err != nil {
			return err
		}
		defer rows.Close()
		orders := []map[string]any{}
		for rows.Next() {
			var id, code, woStatus, review string
			var planned int64
			if err := rows.Scan(&id, &code, &woStatus, &planned, &review); err != nil {
				return err
			}
			orders = append(orders, map[string]any{"id": id, "work_order_code": code, "status": woStatus, "planned_product_qty": planned, "review_status": review})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"project_id": r.PathValue("projectId"), "order_qty": orderQty, "cut_qty": cutQty, "wip_qty": wip, "received_qty": received, "shipped_qty": shipped, "progress_ratio": ratio, "current_node": currentNode, "planned_delivery_date": delivery, "remaining_days": remaining, "avg_receipt_qty_4d": avg, "estimated_days": estimated, "warning_level": warning, "business_status": status, "data_version": dataVersion, "work_orders": orders})
	})

	api.Handle("GET /api/v1/progress/work-orders/{id}/processes", "progress:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT sequence_no,process_code,process_name,COALESCE(process_category,''),COALESCE(equipment_name,''),planned_qty,approved_good_qty,progress_ratio::text,in_progress_bundle_count,pending_bundle_count,current_employee_ids::text,business_status,over_qty_flag,previous_incomplete_flag,COALESCE(remark,''),data_version FROM process_progress_projection WHERE tenant_id=$1 AND work_order_id=$2 ORDER BY sequence_no`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		employeeIDs := map[string]bool{}
		type processRow struct {
			item      map[string]any
			employees []string
		}
		collected := []processRow{}
		for rows.Next() {
			var sequence int
			var code, name, category, equipment, ratio, businessStatus, remark, employeesJSON string
			var planned, approved, inProgress, pending, dataVersion int64
			var overQty, earlyFlow bool
			if err := rows.Scan(&sequence, &code, &name, &category, &equipment, &planned, &approved, &ratio, &inProgress, &pending, &employeesJSON, &businessStatus, &overQty, &earlyFlow, &remark, &dataVersion); err != nil {
				return err
			}
			var employees []string
			if err := json.Unmarshal([]byte(employeesJSON), &employees); err != nil {
				return err
			}
			for _, id := range employees {
				employeeIDs[id] = true
			}
			collected = append(collected, processRow{item: map[string]any{"sequence_no": sequence, "process_code": code, "process_name": name, "process_category": category, "equipment_name": equipment, "planned_qty": planned, "approved_good_qty": approved, "progress_ratio": ratio, "in_progress_bundle_count": inProgress, "pending_bundle_count": pending, "business_status": businessStatus, "over_qty_flag": overQty, "previous_incomplete_flag": earlyFlow, "remark": remark, "data_version": dataVersion}, employees: employees})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		names, err := employeeNames(r.Context(), api.DB, p.TenantID, employeeIDs)
		if err != nil {
			return err
		}
		for _, row := range collected {
			authors := []map[string]any{}
			for _, id := range row.employees {
				authors = append(authors, map[string]any{"employee_id": id, "name": names[id]})
			}
			row.item["current_employees"] = authors
			items = append(items, row.item)
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("GET /api/v1/progress/bundle-processes", "progress:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "b.tenant_id=$1"
		for param, column := range map[string]string{"work_order_id": "b.work_order_id", "bundle_no": "b.bundle_no", "process_code": "s.process_code", "employee_id": "rp.employee_id"} {
			if value := strings.TrimSpace(r.URL.Query().Get(param)); value != "" {
				args = append(args, value)
				where += fmt.Sprintf(" AND %s=$%d", column, len(args))
			}
		}
		where, args = appendEmployeeScope(p, args, where, "rp.employee_id")
		rows, err := api.DB.Query(r.Context(), fmt.Sprintf(`SELECT b.id,b.work_order_id,b.bundle_no,b.color,b.size,b.current_qty,s.sequence_no,s.process_code,s.process_name,COALESCE(rp.employee_id,''),COALESCE(e.employee_no,''),COALESCE(e.name,''),COALESCE(rp.good_qty,0),COALESCE(rp.defect_qty,0),COALESCE(rp.review_status,''),COALESCE(rp.occurred_at,0) FROM bundle b JOIN work_order_process_snapshot s ON s.tenant_id=b.tenant_id AND s.work_order_id=b.work_order_id LEFT JOIN LATERAL (SELECT wr.employee_id,wr.good_qty,wr.defect_qty,wr.review_status,wr.occurred_at FROM work_report wr WHERE wr.tenant_id=b.tenant_id AND wr.bundle_id=b.id AND wr.process_snapshot_id=s.id AND wr.report_kind<>'reversal' ORDER BY wr.occurred_at DESC LIMIT 1) rp ON TRUE LEFT JOIN employee e ON e.tenant_id=b.tenant_id AND e.id=rp.employee_id WHERE %s ORDER BY b.work_order_id,b.bundle_no::integer,s.sequence_no`, where), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, workOrderID, bundleNo, color, size, employeeID, employeeNo, employeeName, reviewStatus, processCode, processName string
			var bundleQty, goodQty, defectQty, occurredAt int64
			var sequence int
			if err := rows.Scan(&id, &workOrderID, &bundleNo, &color, &size, &bundleQty, &sequence, &processCode, &processName, &employeeID, &employeeNo, &employeeName, &goodQty, &defectQty, &reviewStatus, &occurredAt); err != nil {
				return err
			}
			status, label := "not_started", "未开始"
			switch reviewStatus {
			case "pending":
				status, label = "in_progress", "进行中"
			case "approved":
				status, label = "completed", "已通过"
			case "rejected":
				status, label = "rejected", "已驳回"
			}
			if employeeID == "" {
				employeeNo, employeeName = "", "待报工"
			}
			items = append(items, map[string]any{"bundle_id": id, "work_order_id": workOrderID, "bundle_no": bundleNo, "color": color, "size": size, "bundle_qty": bundleQty, "sequence_no": sequence, "process_code": processCode, "process_name": processName, "employee_no": employeeNo, "employee_name": employeeName, "good_qty": goodQty, "defect_qty": defectQty, "review_status": label, "status": status, "reported_at": occurredAt})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/progress/rebuild", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		rows, err := tx.Query(r.Context(), "SELECT id FROM work_order WHERE tenant_id=$1 ORDER BY id", p.TenantID)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			if err := refreshWorkOrderProgress(r.Context(), tx, p.TenantID, id); err != nil {
				return err
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"work_orders": len(ids), "data_version": platform.Now()})
	})
}

func appendEmployeeScope(p auth.Principal, args []any, where, column string) (string, []any) {
	if p.Permissions["progress:detail_all"] {
		return where, args
	}
	if p.EmployeeID == "" {
		return where + " AND FALSE", args
	}
	args = append(args, p.EmployeeID)
	self := fmt.Sprintf("$%d", len(args))
	if p.Permissions["progress:detail_team"] {
		return where + fmt.Sprintf(` AND %[1]s IN (SELECT e.id FROM employee e WHERE e.tenant_id=$1 AND (e.id=%[2]s OR e.manager_employee_id=%[2]s OR (e.org_unit_id IS NOT NULL AND e.org_unit_id=(SELECT me.org_unit_id FROM employee me WHERE me.tenant_id=$1 AND me.id=%[2]s))))`, column, self), args
	}
	return where + fmt.Sprintf(" AND %s=%s", column, self), args
}

func refreshProgressForTask(ctx context.Context, tx pgx.Tx, tenantID, taskID string) error {
	var workOrderID string
	if err := tx.QueryRow(ctx, "SELECT work_order_id FROM production_task WHERE tenant_id=$1 AND id=$2", tenantID, taskID).Scan(&workOrderID); err != nil {
		return err
	}
	return refreshWorkOrderProgress(ctx, tx, tenantID, workOrderID)
}

func refreshWorkOrderProgress(ctx context.Context, tx pgx.Tx, tenantID, workOrderID string) error {
	now, eventID := platform.Now(), platform.ID()
	var planned int64
	var projectID string
	if err := tx.QueryRow(ctx, "SELECT planned_product_qty,project_id FROM work_order WHERE tenant_id=$1 AND id=$2", tenantID, workOrderID).Scan(&planned, &projectID); err != nil {
		return err
	}
	var bundleCount int64
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM bundle WHERE tenant_id=$1 AND work_order_id=$2", tenantID, workOrderID).Scan(&bundleCount); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT s.id,s.sequence_no,s.process_code,s.process_name,COALESCE(s.process_category,''),COALESCE(s.equipment_name,''),
		(SELECT COALESCE(SUM(ct.approved_good_qty),0) FROM production_task ct WHERE ct.tenant_id=s.tenant_id AND ct.work_order_id=s.work_order_id AND ct.product_process_id=s.product_process_id AND ct.parent_task_id IS NOT NULL),
		(SELECT COUNT(DISTINCT wr.bundle_id) FROM work_report wr WHERE wr.tenant_id=s.tenant_id AND wr.process_snapshot_id=s.id AND wr.report_kind<>'reversal' AND wr.review_status='pending'),
		(SELECT COUNT(DISTINCT wr.bundle_id) FROM work_report wr WHERE wr.tenant_id=s.tenant_id AND wr.process_snapshot_id=s.id AND wr.report_kind<>'reversal' AND wr.review_status IN ('pending','approved')),
		COALESCE((SELECT jsonb_agg(DISTINCT wr.employee_id) FROM work_report wr WHERE wr.tenant_id=s.tenant_id AND wr.process_snapshot_id=s.id AND wr.report_kind<>'reversal' AND wr.review_status='pending'),'[]'::jsonb)::text
		FROM work_order_process_snapshot s
		WHERE s.tenant_id=$1 AND s.work_order_id=$2 ORDER BY s.sequence_no`, tenantID, workOrderID)
	if err != nil {
		return err
	}
	type snapshotRow struct {
		id, code, name, category, equipment, employees string
		sequence                                       int
		approved, inProgress, reported                 int64
	}
	snapshots := []snapshotRow{}
	for rows.Next() {
		var v snapshotRow
		if err := rows.Scan(&v.id, &v.sequence, &v.code, &v.name, &v.category, &v.equipment, &v.approved, &v.inProgress, &v.reported, &v.employees); err != nil {
			rows.Close()
			return err
		}
		snapshots = append(snapshots, v)
	}
	rows.Close()
	var previousApproved int64
	hasPrevious := false
	for _, s := range snapshots {
		var ratio *string
		if planned > 0 {
			value := decimal.NewFromInt(s.approved).Div(decimal.NewFromInt(planned)).Round(6).StringFixed(6)
			ratio = &value
		}
		pending := bundleCount - s.reported
		if pending < 0 {
			pending = 0
		}
		status := "not_started"
		if planned > 0 && s.approved >= planned {
			status = "completed"
		} else if s.approved > 0 || s.inProgress > 0 || s.reported > 0 {
			status = "in_progress"
		}
		over := planned > 0 && s.approved > planned
		early := s.reported > 0 && hasPrevious && planned > 0 && previousApproved < planned
		remark := ""
		if over {
			remark = fmt.Sprintf("超出 %d 件", s.approved-planned)
		} else if early {
			remark = "提前流转"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO process_progress_projection(tenant_id,id,work_order_id,process_snapshot_id,sequence_no,process_code,process_name,process_category,equipment_name,planned_qty,approved_good_qty,progress_ratio,in_progress_bundle_count,pending_bundle_count,current_employee_ids,business_status,over_qty_flag,previous_incomplete_flag,remark,last_event_id,data_version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$22) ON CONFLICT(tenant_id,work_order_id,process_snapshot_id) DO UPDATE SET sequence_no=EXCLUDED.sequence_no,process_code=EXCLUDED.process_code,process_name=EXCLUDED.process_name,process_category=EXCLUDED.process_category,equipment_name=EXCLUDED.equipment_name,planned_qty=EXCLUDED.planned_qty,approved_good_qty=EXCLUDED.approved_good_qty,progress_ratio=EXCLUDED.progress_ratio,in_progress_bundle_count=EXCLUDED.in_progress_bundle_count,pending_bundle_count=EXCLUDED.pending_bundle_count,current_employee_ids=EXCLUDED.current_employee_ids,business_status=EXCLUDED.business_status,over_qty_flag=EXCLUDED.over_qty_flag,previous_incomplete_flag=EXCLUDED.previous_incomplete_flag,remark=EXCLUDED.remark,last_event_id=EXCLUDED.last_event_id,data_version=EXCLUDED.data_version,updated_at=EXCLUDED.updated_at`, tenantID, platform.ID(), workOrderID, s.id, s.sequence, s.code, s.name, s.category, s.equipment, planned, s.approved, ratio, s.inProgress, pending, s.employees, status, over, early, remark, eventID, now, now); err != nil {
			return err
		}
		previousApproved = s.approved
		hasPrevious = true
	}
	return refreshStyleProgress(ctx, tx, tenantID, projectID, eventID, now)
}

func refreshStyleProgress(ctx context.Context, tx pgx.Tx, tenantID, projectID, eventID string, now int64) error {
	var orderQty int64
	var delivery *string
	var workshop, line *string
	if err := tx.QueryRow(ctx, "SELECT order_qty,required_delivery_date,workshop_org_unit_id,line_org_unit_id FROM project WHERE tenant_id=$1 AND id=$2", tenantID, projectID).Scan(&orderQty, &delivery, &workshop, &line); err != nil {
		return err
	}
	var cutQty, inspected, received, shipped, avgWindow, workOrders, activeProgress int64
	nowTime := time.UnixMicro(now).In(businessZone)
	windowStart := now - int64(4*24*time.Hour/time.Microsecond)
	if err := tx.QueryRow(ctx, `SELECT
		COALESCE((SELECT SUM(c.cut_qty) FROM cutting_order c JOIN work_order w ON w.tenant_id=c.tenant_id AND w.id=c.work_order_id WHERE c.tenant_id=$1 AND w.project_id=$2 AND c.status='confirmed'),0),
		COALESCE((SELECT SUM(fi.inspected_qty) FROM final_inspection fi JOIN work_order w ON w.tenant_id=fi.tenant_id AND w.id=fi.work_order_id WHERE fi.tenant_id=$1 AND w.project_id=$2 AND fi.status<>'reversed'),0),
		COALESCE((SELECT SUM(l.qty) FROM finished_receipt_line l JOIN finished_receipt fr ON fr.tenant_id=l.tenant_id AND fr.id=l.receipt_id JOIN work_order w ON w.tenant_id=fr.tenant_id AND w.id=fr.work_order_id WHERE fr.tenant_id=$1 AND w.project_id=$2 AND fr.status='approved'),0),
		COALESCE((SELECT SUM(sl.qty) FROM shipment_line sl JOIN shipment sh ON sh.tenant_id=sl.tenant_id AND sh.id=sl.shipment_id WHERE sh.tenant_id=$1 AND sh.project_id=$2 AND sh.status='confirmed'),0),
		COALESCE((SELECT SUM(l.qty) FROM finished_receipt_line l JOIN finished_receipt fr ON fr.tenant_id=l.tenant_id AND fr.id=l.receipt_id JOIN work_order w ON w.tenant_id=fr.tenant_id AND w.id=fr.work_order_id WHERE fr.tenant_id=$1 AND w.project_id=$2 AND fr.status='approved' AND fr.receipt_date>=$3),0),
		(SELECT COUNT(*) FROM work_order WHERE tenant_id=$1 AND project_id=$2),
		(SELECT COUNT(*) FROM process_progress_projection p JOIN work_order w ON w.tenant_id=p.tenant_id AND w.id=p.work_order_id WHERE p.tenant_id=$1 AND w.project_id=$2 AND (p.approved_good_qty>0 OR p.in_progress_bundle_count>0))`, tenantID, projectID, windowStart).Scan(&cutQty, &inspected, &received, &shipped, &avgWindow, &workOrders, &activeProgress); err != nil {
		return err
	}
	wip := cutQty - inspected
	if wip < 0 {
		wip = 0
	}
	avg := decimal.NewFromInt(avgWindow).Div(decimal.NewFromInt(4)).Round(2)
	var ratio, estimated, avgText *string
	var remainingDays *int64
	warning := "none"
	status := "设计打样"
	if orderQty > 0 {
		value := decimal.NewFromInt(received).Div(decimal.NewFromInt(orderQty)).Round(6).StringFixed(6)
		ratio = &value
	}
	if delivery != nil && *delivery != "" {
		due, err := time.ParseInLocation("2006-01-02", *delivery, businessZone)
		if err == nil {
			today := time.Date(nowTime.Year(), nowTime.Month(), nowTime.Day(), 0, 0, 0, 0, businessZone)
			days := int64(due.Sub(today).Hours() / 24)
			remainingDays = &days
		}
	}
	if avg.IsPositive() {
		text := avg.StringFixed(2)
		avgText = &text
	}
	if orderQty > 0 && avg.IsPositive() {
		remainingQty := decimal.NewFromInt(orderQty - received)
		if remainingQty.IsNegative() {
			remainingQty = decimal.Zero
		}
		value := remainingQty.Div(avg).Round(1).StringFixed(1)
		estimated = &value
	}
	shippedAll := orderQty > 0 && shipped >= orderQty
	receivedAll := orderQty > 0 && received >= orderQty
	switch {
	case shippedAll:
		status = "已出货"
	case receivedAll:
		status = "已完工"
	case activeProgress > 0:
		status = "生产中"
	case workOrders > 0:
		status = "待排产"
	}
	var slack *decimal.Decimal
	if remainingDays != nil && estimated != nil {
		value := decimal.NewFromInt(*remainingDays).Sub(decimal.RequireFromString(*estimated))
		slack = &value
	}
	switch {
	case shippedAll:
		warning = "none"
	case remainingDays == nil || !avg.IsPositive():
		warning = "none"
	case *remainingDays < 0:
		warning = "urgent"
	case slack != nil && slack.LessThanOrEqual(decimal.NewFromInt(3)):
		warning = "urgent"
	case slack != nil && slack.LessThanOrEqual(decimal.NewFromInt(5)):
		warning = "warning"
	case slack != nil:
		warning = "normal"
	}
	nodeText, err := currentNodeText(ctx, tx, tenantID, projectID)
	if err != nil {
		return err
	}
	var currentNode *string
	if nodeText != "" {
		currentNode = &nodeText
	}
	_, err = tx.Exec(ctx, `INSERT INTO style_progress_projection(tenant_id,id,project_id,order_qty,cut_qty,wip_qty,received_qty,shipped_qty,progress_ratio,current_node_text,workshop_org_unit_id,line_org_unit_id,planned_delivery_date,remaining_days,avg_receipt_qty_4d,estimated_days,warning_level,business_status,last_event_id,data_version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$21) ON CONFLICT(tenant_id,project_id) DO UPDATE SET order_qty=EXCLUDED.order_qty,cut_qty=EXCLUDED.cut_qty,wip_qty=EXCLUDED.wip_qty,received_qty=EXCLUDED.received_qty,shipped_qty=EXCLUDED.shipped_qty,progress_ratio=EXCLUDED.progress_ratio,current_node_text=EXCLUDED.current_node_text,workshop_org_unit_id=EXCLUDED.workshop_org_unit_id,line_org_unit_id=EXCLUDED.line_org_unit_id,planned_delivery_date=EXCLUDED.planned_delivery_date,remaining_days=EXCLUDED.remaining_days,avg_receipt_qty_4d=EXCLUDED.avg_receipt_qty_4d,estimated_days=EXCLUDED.estimated_days,warning_level=EXCLUDED.warning_level,business_status=EXCLUDED.business_status,last_event_id=EXCLUDED.last_event_id,data_version=EXCLUDED.data_version,updated_at=EXCLUDED.updated_at`, tenantID, platform.ID(), projectID, orderQty, cutQty, wip, received, shipped, ratio, currentNode, workshop, line, delivery, remainingDays, avgText, estimated, warning, status, eventID, now, now)
	return err
}

func currentNodeText(ctx context.Context, tx pgx.Tx, tenantID, projectID string) (string, error) {
	rows, err := tx.Query(ctx, `SELECT p.sequence_no,p.process_name,p.business_status FROM process_progress_projection p JOIN work_order w ON w.tenant_id=p.tenant_id AND w.id=p.work_order_id WHERE p.tenant_id=$1 AND w.project_id=$2 ORDER BY p.sequence_no`, tenantID, projectID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	minSeq := -1
	names := []string{}
	seen := map[string]bool{}
	for rows.Next() {
		var sequence int
		var name, status string
		if err := rows.Scan(&sequence, &name, &status); err != nil {
			return "", err
		}
		if status == "completed" {
			continue
		}
		if minSeq == -1 || sequence < minSeq {
			minSeq = sequence
			names = names[:0]
			seen = map[string]bool{}
		}
		if sequence == minSeq && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(names, "、"), nil
}

func employeeNames(ctx context.Context, q dbQuerier, tenantID string, ids map[string]bool) (map[string]string, error) {
	names := map[string]string{}
	if len(ids) == 0 {
		return names, nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	rows, err := q.Query(ctx, "SELECT id,name FROM employee WHERE tenant_id=$1 AND id=ANY($2)", tenantID, list)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	return names, rows.Err()
}
