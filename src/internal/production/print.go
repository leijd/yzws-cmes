package production

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

var printConnectionTypes = map[string]bool{"usb": true, "network": true, "bluetooth": true, "cloud": true, "system": true}

func registerPrint(api *httpapi.API) {
	api.Handle("POST /api/v1/printers", "print:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name              string          `json:"name"`
			Model             *string         `json:"model"`
			ConnectionType    string          `json:"connection_type"`
			Address           *string         `json:"address"`
			Capabilities      json.RawMessage `json:"capabilities"`
			DefaultTemplateID *string         `json:"default_template_id"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || !printConnectionTypes[in.ConnectionType] {
			return httpapi.Error(400, "VALIDATION_ERROR", "设备名称必填，连接方式只能是 usb/network/bluetooth/cloud/system")
		}
		capabilities := []byte("{}")
		if len(in.Capabilities) > 0 {
			capabilities = in.Capabilities
		}
		id, now := platform.ID(), platform.Now()
		if _, err := api.DB.Exec(r.Context(), `INSERT INTO printer(tenant_id,id,name,model,connection_type,address,capabilities,default_template_id,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,$9)`, p.TenantID, id, in.Name, in.Model, in.ConnectionType, in.Address, capabilities, in.DefaultTemplateID, now); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "status": "active", "version": 1})
	})

	api.Handle("GET /api/v1/printers", "print:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,name,COALESCE(model,''),connection_type,COALESCE(default_template_id,''),status,version FROM printer WHERE tenant_id=$1 ORDER BY name,id`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, name, model, connection, template, status string
			var version int64
			if err := rows.Scan(&id, &name, &model, &connection, &template, &status, &version); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "name": name, "model": model, "connection_type": connection, "default_template_id": template, "status": status, "version": version})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/print-templates", "print:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name         string          `json:"name"`
			TemplateType string          `json:"template_type"`
			PaperWidth   *string         `json:"paper_width"`
			PaperHeight  *string         `json:"paper_height"`
			Content      json.RawMessage `json:"content"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || in.TemplateType == "" || len(in.Content) == 0 || !json.Valid(in.Content) {
			return httpapi.Error(400, "VALIDATION_ERROR", "模板名称、类型和有效的 JSON 内容必填")
		}
		id, now := platform.ID(), platform.Now()
		if _, err := api.DB.Exec(r.Context(), `INSERT INTO print_template(tenant_id,id,name,template_type,paper_width,paper_height,content,version,status,created_at,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,1,'active',$8,$9)`, p.TenantID, id, in.Name, in.TemplateType, in.PaperWidth, in.PaperHeight, []byte(in.Content), now, p.UserID); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "version": 1, "status": "active"})
	})

	api.Handle("GET /api/v1/print-templates", "print:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,name,template_type,version,status FROM print_template WHERE tenant_id=$1 ORDER BY name,version DESC`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, name, templateType, status string
			var version int64
			if err := rows.Scan(&id, &name, &templateType, &version, &status); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "name": name, "template_type": templateType, "version": version, "status": status})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/print-jobs", "print:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			PrinterID  string `json:"printer_id"`
			TemplateID string `json:"template_id"`
			TicketID   string `json:"ticket_id"`
			Copies     int    `json:"copies"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Copies == 0 {
			in.Copies = 1
		}
		if in.PrinterID == "" || in.TemplateID == "" || in.TicketID == "" || in.Copies < 1 || in.Copies > 50 {
			return httpapi.Error(400, "VALIDATION_ERROR", "打印机、模板、菲票必填，份数须在 1～50 之间")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if err := requirePrinterAndTemplate(r.Context(), tx, p.TenantID, in.PrinterID, in.TemplateID); err != nil {
			return err
		}
		var printStatus string
		if err := tx.QueryRow(r.Context(), "SELECT print_status FROM ticket WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, in.TicketID).Scan(&printStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "菲票不存在")
			}
			return err
		}
		if printStatus == "voided" {
			return httpapi.Error(409, "INVALID_STATE", "菲票已作废，不能打印")
		}
		payload, err := renderTicketPayload(r.Context(), tx, p.TenantID, in.TicketID)
		if err != nil {
			return err
		}
		id := platform.ID()
		if _, err := tx.Exec(r.Context(), `INSERT INTO print_job(tenant_id,id,printer_id,template_id,owner_type,owner_id,rendered_payload,copies,status,requested_by,requested_at) VALUES($1,$2,$3,$4,'ticket',$5,$6,$7,'queued',$8,$9)`, p.TenantID, id, in.PrinterID, in.TemplateID, in.TicketID, payload, in.Copies, p.UserID, platform.Now()); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "status": "queued", "copies": in.Copies})
	})

	api.Handle("GET /api/v1/print-jobs/{id}", "print:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var id, printerID, templateID, ownerType, ownerID, status, requestedBy string
		var originalID, errText *string
		var copies, retry, requestedAt int64
		var payload string
		err := api.DB.QueryRow(r.Context(), `SELECT id,printer_id,template_id,owner_type,owner_id,original_job_id,rendered_payload,copies,status,error,retry_count,requested_by,requested_at FROM print_job WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id")).Scan(&id, &printerID, &templateID, &ownerType, &ownerID, &originalID, &payload, &copies, &status, &errText, &retry, &requestedBy, &requestedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "打印任务不存在")
		}
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": id, "printer_id": printerID, "template_id": templateID, "owner_type": ownerType, "owner_id": ownerID, "original_job_id": originalID, "rendered_payload": json.RawMessage(payload), "copies": copies, "status": status, "error": errText, "retry_count": retry, "requested_by": requestedBy, "requested_at": requestedAt})
	})

	api.Handle("POST /api/v1/print-jobs/{id}/receipt", "print:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Result string  `json:"result"`
			Error  *string `json:"error"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Result != "succeeded" && in.Result != "failed" {
			return httpapi.Error(400, "VALIDATION_ERROR", "回执结果只能是 succeeded 或 failed")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		id := r.PathValue("id")
		var status, ownerID string
		var copies int64
		err = tx.QueryRow(r.Context(), "SELECT status,owner_id,copies FROM print_job WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, id).Scan(&status, &ownerID, &copies)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "打印任务不存在")
		}
		if err != nil {
			return err
		}
		if status != "queued" && status != "sent" {
			return httpapi.Error(409, "INVALID_STATE", "打印任务已结束，不能再次登记回执")
		}
		if _, err := tx.Exec(r.Context(), "UPDATE print_job SET status=$1,error=$2 WHERE tenant_id=$3 AND id=$4", in.Result, in.Error, p.TenantID, id); err != nil {
			return err
		}
		if in.Result == "succeeded" {
			if _, err := tx.Exec(r.Context(), "UPDATE ticket SET print_status='printed',print_count=print_count+$1,last_print_job_id=$2 WHERE tenant_id=$3 AND id=$4", copies, id, p.TenantID, ownerID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(r.Context(), "UPDATE ticket SET print_status='failed' WHERE tenant_id=$1 AND id=$2 AND print_status='unprinted'", p.TenantID, ownerID); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "print_receipt", "print_job", id, nil, map[string]any{"result": in.Result}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": id, "status": in.Result})
	})

	api.Handle("POST /api/v1/print-jobs/{id}/reprint", "print:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Reason string `json:"reason"`
			Copies int    `json:"copies"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		in.Reason = strings.TrimSpace(in.Reason)
		if in.Reason == "" {
			return httpapi.Error(400, "REASON_REQUIRED", "补打必须填写原因")
		}
		if in.Copies == 0 {
			in.Copies = 1
		}
		if in.Copies < 1 || in.Copies > 50 {
			return httpapi.Error(400, "VALIDATION_ERROR", "份数须在 1～50 之间")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		originalID := r.PathValue("id")
		var printerID, templateID, ownerType, ownerID, status string
		err = tx.QueryRow(r.Context(), "SELECT printer_id,template_id,owner_type,owner_id,status FROM print_job WHERE tenant_id=$1 AND id=$2", p.TenantID, originalID).Scan(&printerID, &templateID, &ownerType, &ownerID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "打印任务不存在")
		}
		if err != nil {
			return err
		}
		if ownerType != "ticket" || status != "succeeded" {
			return httpapi.Error(409, "TICKET_REPRINT_REQUIRED", "只有已成功打印的菲票可以补打")
		}
		var printStatus string
		var revision int64
		if err := tx.QueryRow(r.Context(), "SELECT print_status,print_revision FROM ticket WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, ownerID).Scan(&printStatus, &revision); err != nil {
			return err
		}
		if printStatus == "voided" {
			return httpapi.Error(409, "INVALID_STATE", "菲票已作废，不能补打")
		}
		if err := requirePrinterAndTemplate(r.Context(), tx, p.TenantID, printerID, templateID); err != nil {
			return err
		}
		payload, err := renderTicketPayload(r.Context(), tx, p.TenantID, ownerID)
		if err != nil {
			return err
		}
		now := platform.Now()
		newID := platform.ID()
		if _, err := tx.Exec(r.Context(), `INSERT INTO print_job(tenant_id,id,printer_id,template_id,owner_type,owner_id,original_job_id,rendered_payload,copies,status,requested_by,requested_at) VALUES($1,$2,$3,$4,'ticket',$5,$6,$7,$8,'queued',$9,$10)`, p.TenantID, newID, printerID, templateID, ownerID, originalID, payload, in.Copies, p.UserID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE ticket SET print_revision=print_revision+1,voided_at=$1,voided_reason=$2 WHERE tenant_id=$3 AND id=$4", now, in.Reason, p.TenantID, ownerID); err != nil {
			return err
		}
		response := map[string]any{"id": newID, "original_job_id": originalID, "ticket_id": ownerID, "print_revision": revision + 1, "status": "queued"}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "reprint", "ticket", ownerID, map[string]any{"print_revision": revision}, response, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, response)
	})
}

func requirePrinterAndTemplate(ctx context.Context, tx pgx.Tx, tenantID, printerID, templateID string) error {
	var printerStatus string
	if err := tx.QueryRow(ctx, "SELECT status FROM printer WHERE tenant_id=$1 AND id=$2", tenantID, printerID).Scan(&printerStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(400, "INVALID_PRINTER", "打印机不存在")
		}
		return err
	}
	if printerStatus != "active" {
		return httpapi.Error(409, "PRINTER_UNAVAILABLE", "打印机已停用")
	}
	var templateExists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM print_template WHERE tenant_id=$1 AND id=$2)", tenantID, templateID).Scan(&templateExists); err != nil {
		return err
	}
	if !templateExists {
		return httpapi.Error(400, "INVALID_TEMPLATE", "打印模板不存在")
	}
	return nil
}

func renderTicketPayload(ctx context.Context, q dbQuerier, tenantID, ticketID string) (string, error) {
	var publicCode, partCode, partName, bundleNo, color, size, workOrderCode, productCode, productName string
	var qty int64
	err := q.QueryRow(ctx, `SELECT t.public_code,COALESCE(t.part_code,''),COALESCE(t.part_name,''),b.bundle_no,b.color,b.size,b.current_qty,w.work_order_code,pr.product_code,pr.name FROM ticket t JOIN bundle b ON b.tenant_id=t.tenant_id AND b.id=t.bundle_id JOIN work_order w ON w.tenant_id=b.tenant_id AND w.id=b.work_order_id JOIN product pr ON pr.tenant_id=w.tenant_id AND pr.id=w.product_id WHERE t.tenant_id=$1 AND t.id=$2`, tenantID, ticketID).Scan(&publicCode, &partCode, &partName, &bundleNo, &color, &size, &qty, &workOrderCode, &productCode, &productName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", httpapi.Error(404, "NOT_FOUND", "菲票不存在")
	}
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{"ticket_id": ticketID, "public_code": publicCode, "work_order_code": workOrderCode, "product_code": productCode, "product_name": productName, "bundle_no": bundleNo, "color": color, "size": size, "qty": qty, "part_code": partCode, "part_name": partName})
	return string(payload), err
}
