package production

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerTickets(api *httpapi.API) {
	loadTicket := func(w http.ResponseWriter, r *http.Request, p auth.Principal, ticketID string) error {
		var ticket map[string]any
		if err := api.DB.QueryRow(r.Context(), `SELECT row_to_json(t) FROM (SELECT tk.id,tk.bundle_id,tk.bundle_part_id,tk.public_code,tk.status,tk.print_status,tk.print_count,tk.print_revision,tk.last_print_job_id,tk.voided_at,COALESCE(tk.voided_reason,'') AS voided_reason,tk.printed_at,
			b.bundle_no,b.color,b.size,b.current_qty,b.work_order_id,COALESCE(bp.part_code,'') AS part_code,COALESCE(bp.part_name,'') AS part_name,COALESCE(bp.qty,0) AS part_qty
			FROM ticket tk JOIN bundle b ON b.tenant_id=tk.tenant_id AND b.id=tk.bundle_id LEFT JOIN bundle_part bp ON bp.tenant_id=tk.tenant_id AND bp.id=tk.bundle_part_id
			WHERE tk.tenant_id=$1 AND tk.id=$2) t`, p.TenantID, ticketID).Scan(&ticket); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "TICKET_NOT_FOUND", "菲票不存在")
			}
			return err
		}
		return httpapi.JSON(w, 200, ticket)
	}

	api.Handle("GET /api/v1/tickets/{id}", "print:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return loadTicket(w, r, p, r.PathValue("id"))
	})

	api.Handle("POST /api/v1/tickets/{id}/void", "print:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Reason  string `json:"reason"`
			Version *int64 `json:"version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.Reason) == "" {
			return httpapi.Error(400, "REASON_REQUIRED", "作废必须填写原因")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var status string
		var revision, count int64
		if err := tx.QueryRow(r.Context(), "SELECT status,print_revision,print_count FROM ticket WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &revision, &count); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "TICKET_NOT_FOUND", "菲票不存在")
			}
			return err
		}
		if status == "voided" {
			return httpapi.Error(409, "TICKET_VOIDED", "菲票已作废")
		}
		if in.Version != nil && *in.Version != revision {
			return httpapi.Error(409, "VERSION_CONFLICT", "菲票已变更，请刷新后重试")
		}
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), "UPDATE ticket SET status='voided',print_status='voided',voided_at=$1,voided_reason=$2,updated_at=$1 WHERE tenant_id=$3 AND id=$4", now, strings.TrimSpace(in.Reason), p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "void", "ticket", r.PathValue("id"), map[string]any{"status": status}, map[string]any{"status": "voided", "reason": in.Reason}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "voided", "print_revision": revision, "print_count": count})
	})

	resolve := func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		payload := strings.TrimSpace(r.URL.Query().Get("payload"))
		if payload == "" {
			var in struct {
				Payload  string `json:"payload"`
				DeviceID string `json:"device_id"`
			}
			if err := httpapi.Decode(r, &in); err != nil {
				return err
			}
			payload = strings.TrimSpace(in.Payload)
		}
		if payload == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "扫码内容不能为空")
		}
		payload = normalizeScanPayload(payload)
		var ticketID, status, printStatus, bundleID string
		var revision int64
		err := api.DB.QueryRow(r.Context(), `SELECT tk.id,tk.status,tk.print_status,tk.bundle_id,tk.print_revision
			FROM ticket tk JOIN bundle b ON b.tenant_id=tk.tenant_id AND b.id=tk.bundle_id
			WHERE tk.tenant_id=$1 AND (
				tk.public_code=$2 OR tk.id=$2 OR (
					b.bundle_no=$2 AND (SELECT COUNT(*) FROM bundle b2 WHERE b2.tenant_id=$1 AND b2.bundle_no=$2)=1
				)
			)
			ORDER BY CASE WHEN tk.public_code=$2 OR tk.id=$2 THEN 0 ELSE 1 END,
				CASE WHEN COALESCE(tk.part_code,'')='whole' THEN 0 ELSE 1 END,
				tk.created_at,tk.id
			LIMIT 1`, p.TenantID, payload).Scan(&ticketID, &status, &printStatus, &bundleID, &revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "INVALID_TICKET", "未找到对应的菲票或扎")
		}
		if err != nil {
			return err
		}
		if status == "voided" {
			return httpapi.Error(409, "TICKET_VOIDED", "菲票已作废，不允许继续操作")
		}
		var result map[string]any
		if err := api.DB.QueryRow(r.Context(), `SELECT row_to_json(t) FROM (SELECT tk.id AS ticket_id,tk.status AS ticket_status,tk.print_status,tk.print_revision,tk.public_code,
			b.id AS bundle_id,b.bundle_no,b.color,b.size,b.current_qty,b.work_order_id,COALESCE(NULLIF(b.bed_no,''),co.bed_no,'') AS bed_no,
			COALESCE(bp.part_code,'') AS part_code,COALESCE(bp.part_name,'') AS part_name,COALESCE(bp.qty,0) AS part_qty,
			wo.work_order_code,COALESCE(wo.report_mode,'B') AS report_mode,pj.project_code,cu.code AS customer_code,cu.name AS customer_name,
			pr.product_code,pr.name AS product_name,COALESCE(pr.spec,'') AS product_spec,
			COALESCE(co.cutting_no,'') AS cutting_no,COALESCE(co.marker_no,'') AS marker_no,COALESCE(co.fabric_layers,0) AS fabric_layers,COALESCE(co.cutting_date,0) AS cutting_date,
			COALESCE(cutter.name,'') AS cutter_name,tn.code AS tenant_code,tn.name AS tenant_name
			FROM ticket tk JOIN bundle b ON b.tenant_id=tk.tenant_id AND b.id=tk.bundle_id
			LEFT JOIN bundle_part bp ON bp.tenant_id=tk.tenant_id AND bp.id=tk.bundle_part_id
			LEFT JOIN cutting_detail cd ON cd.tenant_id=b.tenant_id AND cd.id=b.cutting_detail_id
			LEFT JOIN cutting_order co ON co.tenant_id=cd.tenant_id AND co.id=cd.cutting_order_id
			LEFT JOIN employee cutter ON cutter.tenant_id=co.tenant_id AND cutter.id=co.cutter_id
			JOIN work_order wo ON wo.tenant_id=b.tenant_id AND wo.id=b.work_order_id
			JOIN project pj ON pj.tenant_id=wo.tenant_id AND pj.id=wo.project_id
			JOIN customer cu ON cu.tenant_id=pj.tenant_id AND cu.id=pj.customer_id
			JOIN product pr ON pr.tenant_id=pj.tenant_id AND pr.id=pj.product_id
			JOIN tenant tn ON tn.id=b.tenant_id
			WHERE tk.tenant_id=$1 AND tk.id=$2) t`, p.TenantID, ticketID).Scan(&result); err != nil {
			return err
		}
		rows, err := api.DB.Query(r.Context(), `SELECT t.id,t.version,s.sequence_no,s.process_code,s.process_name,
			t.planned_qty,t.reported_qty,t.planned_qty-t.reported_qty,
			COALESCE((SELECT SUM(a.planned_qty) FROM task_bundle_assignment a WHERE a.tenant_id=t.tenant_id AND a.bundle_id=$2 AND a.task_id=t.id AND a.status='active'),0)
			FROM production_task t
			JOIN work_order_process_snapshot s ON s.tenant_id=t.tenant_id AND s.work_order_id=t.work_order_id AND s.product_process_id=t.product_process_id
			WHERE t.tenant_id=$1 AND t.work_order_id=(SELECT work_order_id FROM bundle WHERE tenant_id=$1 AND id=$2) AND t.parent_task_id IS NULL
			ORDER BY s.sequence_no`, p.TenantID, bundleID)
		if err != nil {
			return err
		}
		processes := []map[string]any{}
		tasks := []map[string]any{}
		for rows.Next() {
			var taskID, code, name string
			var sequence int
			var version, planned, reported, remaining, assigned int64
			if err := rows.Scan(&taskID, &version, &sequence, &code, &name, &planned, &reported, &remaining, &assigned); err != nil {
				rows.Close()
				return err
			}
			process := map[string]any{"task_id": taskID, "sequence_no": sequence, "process_code": code, "process_name": name}
			processes = append(processes, process)
			tasks = append(tasks, map[string]any{
				"task_id": taskID, "version": version, "sequence_no": sequence,
				"process_code": code, "process_name": name,
				"planned_qty": planned, "reported_qty": reported, "remaining_qty": remaining,
				"assigned_qty": assigned,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		result["processes"] = processes
		result["tasks"] = tasks
		result["can_assign"] = p.Permissions["task:assign"]
		result["can_report"] = p.Permissions["report:create"]
		result["employee_id"] = p.EmployeeID
		if p.Permissions["task:assign"] {
			employeeRows, err := api.DB.Query(r.Context(), `SELECT id,employee_no,name FROM employee WHERE tenant_id=$1 AND status='active' ORDER BY employee_no,id`, p.TenantID)
			if err != nil {
				return err
			}
			employees := []map[string]any{}
			for employeeRows.Next() {
				var employeeID, employeeNo, employeeName string
				if err := employeeRows.Scan(&employeeID, &employeeNo, &employeeName); err != nil {
					employeeRows.Close()
					return err
				}
				employees = append(employees, map[string]any{"id": employeeID, "employee_no": employeeNo, "name": employeeName})
			}
			if err := employeeRows.Err(); err != nil {
				employeeRows.Close()
				return err
			}
			employeeRows.Close()
			result["employees"] = employees
		}
		return httpapi.JSON(w, 200, result)
	}
	api.Handle("GET /api/v1/scan/resolve", "task:read", resolve)
	api.Handle("POST /api/v1/scan/resolve", "task:read", resolve)
}

// normalizeScanPayload 兼容四种扫码内容：菲票公开码、扎号、扫码落地页 URL
// （/s/{tenant}/{code}），以及包含 public_code/code 的 JSON 文本。扎号仅在租户内唯一时解析。
func normalizeScanPayload(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if strings.HasPrefix(raw, "{") {
		var body struct {
			PublicCode string `json:"public_code"`
			Code       string `json:"code"`
			Payload    string `json:"payload"`
		}
		if json.Unmarshal([]byte(raw), &body) == nil {
			switch {
			case strings.TrimSpace(body.PublicCode) != "":
				return strings.TrimSpace(body.PublicCode)
			case strings.TrimSpace(body.Code) != "":
				return strings.TrimSpace(body.Code)
			case strings.TrimSpace(body.Payload) != "":
				return normalizeScanPayload(body.Payload)
			}
		}
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		for _, key := range []string{"code", "public_code", "payload"} {
			if value := strings.TrimSpace(parsed.Query().Get(key)); value != "" {
				return normalizeScanPayload(value)
			}
		}
		if path := strings.Trim(parsed.Path, "/"); path != "" {
			segments := strings.Split(path, "/")
			return segments[len(segments)-1]
		}
	}
	return raw
}
