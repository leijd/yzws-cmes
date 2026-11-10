package production

import (
	"encoding/json"
	"fmt"
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
)

func registerQueries(api *httpapi.API) {
	workbench := func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		metrics := map[string]any{}
		for key, query := range map[string]string{
			"pending_release":  "SELECT COUNT(*) FROM work_order WHERE tenant_id=$1 AND status='pending_release'",
			"in_production":    "SELECT COUNT(*) FROM work_order WHERE tenant_id=$1 AND status='in_production'",
			"unassigned_tasks": "SELECT COUNT(*) FROM production_task WHERE tenant_id=$1 AND status='unassigned'",
			"pending_reviews":  "SELECT COUNT(*) FROM work_report WHERE tenant_id=$1 AND review_status='pending'",
			"today_wages":      "SELECT COALESCE(SUM(amount),0)::text FROM piecework_entry WHERE tenant_id=$1 AND to_char(to_timestamp(created_at/1000000),'YYYY-MM-DD')=to_char(CURRENT_DATE,'YYYY-MM-DD')",
		} {
			var value any
			if err := api.DB.QueryRow(r.Context(), query, p.TenantID).Scan(&value); err != nil {
				return err
			}
			metrics[key] = value
		}
		return httpapi.JSON(w, 200, map[string]any{"metrics": metrics, "data_version": platform.Now(), "offline": false})
	}
	api.Handle("GET /api/v1/workbench", "workbench:read", workbench)
	api.Handle("GET /api/v1/progress/workbench", "progress:read", workbench)
	api.Handle("GET /api/v1/progress/work-orders", "progress:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listWorkOrders(api, w, r, p)
	})
	api.Handle("GET /api/v1/progress/work-orders/{id}", "progress:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var planned int64
		if err := api.DB.QueryRow(r.Context(), "SELECT planned_product_qty FROM work_order WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&planned); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "工单不存在")
		}
		rows, err := api.DB.Query(r.Context(), `SELECT s.product_process_id,s.process_code,s.process_name,s.planned_qty,COALESCE(SUM(t.approved_good_qty),0),COALESCE(SUM(t.pending_qty),0) FROM work_order_process_snapshot s LEFT JOIN production_task t ON t.tenant_id=s.tenant_id AND t.work_order_id=s.work_order_id AND t.product_process_id=s.product_process_id AND t.parent_task_id IS NOT NULL WHERE s.tenant_id=$1 AND s.work_order_id=$2 GROUP BY s.product_process_id,s.process_code,s.process_name,s.planned_qty,s.sequence_no ORDER BY s.sequence_no`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		var totalPlanned, totalGood int64
		for rows.Next() {
			var id, code, name string
			var plan, good, pending int64
			if err := rows.Scan(&id, &code, &name, &plan, &good, &pending); err != nil {
				return err
			}
			ratio := 0.0
			if plan > 0 {
				ratio = float64(good) / float64(plan)
			}
			items = append(items, map[string]any{"product_process_id": id, "process_code": code, "process_name": name, "planned_qty": plan, "approved_good_qty": good, "pending_qty": pending, "progress_ratio": ratio})
			totalPlanned += plan
			totalGood += good
		}
		overall := 0.0
		if totalPlanned > 0 {
			overall = float64(totalGood) / float64(totalPlanned)
		}
		productProgress := 0.0
		if len(items) > 0 {
			last := items[len(items)-1]
			productProgress = last["progress_ratio"].(float64)
		}
		return httpapi.JSON(w, 200, map[string]any{"work_order_id": r.PathValue("id"), "process_progress": items, "product_progress": map[string]any{"ratio": productProgress, "planned_product_qty": planned}, "overall_progress": overall, "receipt_progress": 0, "pending_count": 0, "data_version": platform.Now()})
	})
	api.Handle("GET /api/v1/progress/processes", "progress:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return progressGroup(api, w, r, p, "process")
	})
	api.Handle("GET /api/v1/progress/employees", "progress:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return progressGroup(api, w, r, p, "employee")
	})
	api.Handle("GET /api/v1/statistics/output", "statistics:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return statistics(api, w, r, p, "output")
	})
	api.Handle("GET /api/v1/statistics/wages", "statistics:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return statistics(api, w, r, p, "wages")
	})
	api.Handle("GET /api/v1/statistics/worktime", "statistics:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return statistics(api, w, r, p, "worktime")
	})
	api.Handle("GET /api/v1/piecework/mine", "payroll:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT pe.id,pe.report_id,pe.qty,pe.unit_price::text,pe.coefficient::text,pe.amount::text,pe.entry_kind,pe.payroll_period,pe.created_at FROM piecework_entry pe WHERE pe.tenant_id=$1 AND pe.employee_id=$2 ORDER BY pe.created_at DESC LIMIT 200`, p.TenantID, p.EmployeeID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, report, qty, price, coefficient, amount, kind, period string
			var created int64
			if err := rows.Scan(&id, &report, &qty, &price, &coefficient, &amount, &kind, &period, &created); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "report_id": report, "qty": qty, "unit_price": price, "coefficient": coefficient, "amount": amount, "entry_kind": kind, "payroll_period": period, "created_at": created})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	registerSettings(api)
	registerSync(api)
	registerAttachments(api)
}

func progressGroup(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, group string) error {
	field, join, name := "s.process_name", "", ""
	if group == "employee" {
		field = "COALESCE(e.name,'未派工')"
		join = "LEFT JOIN employee e ON e.tenant_id=t.tenant_id AND e.id=t.employee_id"
	}
	query := fmt.Sprintf(`SELECT %s,COUNT(t.id),COALESCE(SUM(t.planned_qty),0),COALESCE(SUM(t.approved_good_qty),0),COALESCE(SUM(t.pending_qty),0) FROM production_task t JOIN work_order_process_snapshot s ON s.tenant_id=t.tenant_id AND s.work_order_id=t.work_order_id AND s.product_process_id=t.product_process_id %s WHERE t.tenant_id=$1 AND t.parent_task_id IS NOT NULL GROUP BY %s ORDER BY %s`, field, join, field, field)
	rows, err := api.DB.Query(r.Context(), query, p.TenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		if err := rows.Scan(&name, new(any), new(any), new(any), new(any)); err != nil {
			return err
		}
		_ = name
	}
	rows.Close()
	rows, err = api.DB.Query(r.Context(), query, p.TenantID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var count, planned, good, pending int64
		if err := rows.Scan(&name, &count, &planned, &good, &pending); err != nil {
			return err
		}
		ratio := 0.0
		if planned > 0 {
			ratio = float64(good) / float64(planned)
		}
		items = append(items, map[string]any{"name": name, "task_count": count, "planned_qty": planned, "approved_good_qty": good, "pending_qty": pending, "progress_ratio": ratio})
	}
	return httpapi.JSON(w, 200, map[string]any{"items": items})
}

func statistics(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, kind string) error {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "2026-11"
	}
	selectValue := "CASE WHEN wr.report_kind='reversal' THEN -wr.good_qty ELSE wr.good_qty END"
	source := "work_report wr JOIN employee e ON e.tenant_id=wr.tenant_id AND e.id=wr.employee_id"
	where := "wr.tenant_id=$1 AND wr.review_status='approved'"
	if kind == "wages" {
		selectValue = "pe.amount"
		source = "piecework_entry pe JOIN employee e ON e.tenant_id=pe.tenant_id AND e.id=pe.employee_id"
		where = "pe.tenant_id=$1 AND pe.entry_kind IN ('confirmed','reversal')"
	} else if kind == "worktime" {
		selectValue = "(CASE WHEN wr.report_kind='reversal' THEN -wr.good_qty ELSE wr.good_qty END)*wr.standard_work_seconds"
	}
	query := fmt.Sprintf(`SELECT e.name,to_char(to_timestamp(%s/1000000) AT TIME ZONE 'Asia/Shanghai','YYYY-MM-DD'),SUM(%s)::text FROM %s WHERE %s AND to_char(to_timestamp(%s/1000000) AT TIME ZONE 'Asia/Shanghai','YYYY-MM')=$2 GROUP BY e.name,2 ORDER BY e.name,2`, map[string]string{"wages": "pe.created_at", "output": "wr.occurred_at", "worktime": "wr.occurred_at"}[kind], selectValue, source, where, map[string]string{"wages": "pe.created_at", "output": "wr.occurred_at", "worktime": "wr.occurred_at"}[kind])
	rows, err := api.DB.Query(r.Context(), query, p.TenantID, period)
	if err != nil {
		return err
	}
	defer rows.Close()
	type cell struct{ name, date, value string }
	cells := []cell{}
	dates := map[string]bool{}
	for rows.Next() {
		var c cell
		if err := rows.Scan(&c.name, &c.date, &c.value); err != nil {
			return err
		}
		cells = append(cells, c)
		dates[c.date] = true
	}
	matrix := map[string]map[string]string{}
	for _, c := range cells {
		if matrix[c.name] == nil {
			matrix[c.name] = map[string]string{}
		}
		matrix[c.name][c.date] = c.value
	}
	result := []map[string]any{}
	for name, values := range matrix {
		result = append(result, map[string]any{"name": name, "values": values, "filter_token": platform.Token(12)})
	}
	columns := []string{}
	for date := range dates {
		columns = append(columns, date)
	}
	return httpapi.JSON(w, 200, map[string]any{"rows": result, "columns": columns, "period": period, "metric": kind})
}

func registerSettings(api *httpapi.API) {
	api.Handle("GET /api/v1/feature-settings", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT DISTINCT ON(setting_domain,setting_key) setting_domain,setting_key,setting_value,version,effective_at FROM feature_setting WHERE tenant_id=$1 ORDER BY setting_domain,setting_key,version DESC`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var domain, key string
			var value json.RawMessage
			var version, effective int64
			if err := rows.Scan(&domain, &key, &value, &version, &effective); err != nil {
				return err
			}
			items = append(items, map[string]any{"setting_domain": domain, "setting_key": key, "setting_value": value, "version": version, "effective_at": effective})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("PUT /api/v1/feature-settings/{domain}/{key}", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Value   any    `json:"value"`
			Version int64  `json:"version"`
			Reason  string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Reason == "" {
			return httpapi.Error(400, "REASON_REQUIRED", "设置变更原因必填")
		}
		body, _ := json.Marshal(in.Value)
		id := platform.ID()
		_, err := api.DB.Exec(r.Context(), `INSERT INTO feature_setting(tenant_id,id,setting_domain,setting_key,setting_value,version,effective_at,updated_by,change_reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$7)`, p.TenantID, id, r.PathValue("domain"), r.PathValue("key"), body, in.Version+1, platform.Now(), p.UserID, in.Reason)
		if err != nil {
			return httpapi.Error(409, "VERSION_CONFLICT", "设置版本已存在")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": id, "version": in.Version + 1, "sync_status": "pending"})
	})
	api.Handle("GET /api/v1/audit-logs", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,request_id,actor_id,action,owner_type,owner_id,reason,created_node,created_at FROM audit_log WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 200`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, request, action, ownerType, ownerID, node string
			var actor, reason *string
			var created int64
			if err := rows.Scan(&id, &request, &actor, &action, &ownerType, &ownerID, &reason, &node, &created); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "request_id": request, "actor_id": actor, "action": action, "owner_type": ownerType, "owner_id": ownerID, "reason": reason, "created_node": node, "created_at": created})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
}

func registerSync(api *httpapi.API) {
	api.Handle("GET /api/v1/sync/status", "sync:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var pending, conflicts int64
		if err := api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM outbox_event WHERE tenant_id=$1 AND push_status='pending'", p.TenantID).Scan(&pending); err != nil {
			return err
		}
		if err := api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM outbox_event WHERE tenant_id=$1 AND push_status='conflict'", p.TenantID).Scan(&conflicts); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"node_id": api.Config.NodeID, "outbox_pending": pending, "conflicts": conflicts, "attachment_pending": 0, "online": true, "last_sync_at": nil})
	})
	api.Handle("POST /api/v1/sync/pull", "sync:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Cursor int64 `json:"cursor"`
			Limit  int   `json:"limit"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Limit < 1 || in.Limit > 200 {
			in.Limit = 100
		}
		rows, err := api.DB.Query(r.Context(), `SELECT event_id,origin_node_id,origin_seq,aggregate_type,aggregate_id,event_type,schema_version,occurred_at,payload,payload_sha256 FROM outbox_event WHERE tenant_id=$1 AND origin_seq>$2 ORDER BY origin_seq LIMIT $3`, p.TenantID, in.Cursor, in.Limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		events := []map[string]any{}
		var cursor int64 = in.Cursor
		for rows.Next() {
			var id, node, aggregateType, aggregateID, eventType, payload, hash string
			var seq, occurred int64
			var schema int
			if err := rows.Scan(&id, &node, &seq, &aggregateType, &aggregateID, &eventType, &schema, &occurred, &payload, &hash); err != nil {
				return err
			}
			cursor = seq
			events = append(events, map[string]any{"event_id": id, "origin_node_id": node, "origin_seq": seq, "aggregate_type": aggregateType, "aggregate_id": aggregateID, "event_type": eventType, "schema_version": schema, "occurred_at": occurred, "payload": json.RawMessage(payload), "payload_sha256": hash})
		}
		return httpapi.JSON(w, 200, map[string]any{"events": events, "next_cursor": cursor, "has_more": len(events) == in.Limit})
	})
}
