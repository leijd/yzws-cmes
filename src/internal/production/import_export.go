package production

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

type importRow struct {
	EmployeeNo, Name, Position, Workshop, Line, Status string
}

func registerImportExport(api *httpapi.API) {
	api.Handle("GET /api/v1/export-jobs", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,job_type,filter_snapshot::text,status,progress,COALESCE(error,''),created_at FROM export_job WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 100`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, jobType, filter, status, errText string
			var progress int
			var createdAt int64
			if err := rows.Scan(&id, &jobType, &filter, &status, &progress, &errText, &createdAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "job_type": jobType, "filter_snapshot": filter, "status": status, "progress": progress, "error": errText, "created_at": createdAt})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/export-jobs", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			JobType        string          `json:"job_type"`
			Format         string          `json:"format"`
			FilterSnapshot json.RawMessage `json:"filter_snapshot"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.JobType != "employees" && in.JobType != "wages" {
			return httpapi.Error(400, "VALIDATION_ERROR", "导出类型只支持 employees 或 wages")
		}
		filter := in.FilterSnapshot
		if len(filter) == 0 {
			filter = json.RawMessage(`{}`)
		}
		id, now := platform.ID(), platform.Now()
		headers, records, err := exportRecords(r.Context(), api, p.TenantID, in.JobType, filter)
		if err != nil {
			return err
		}
		var buffer bytes.Buffer
		writer := csv.NewWriter(&buffer)
		_ = writer.Write(headers)
		for _, record := range records {
			_ = writer.Write(record)
		}
		writer.Flush()
		content := buffer.Bytes()
		digest := sha256.Sum256(content)
		storageKey := id
		dir := filepath.Join(api.Config.AttachmentDir, storageKey)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "content"), content, 0600); err != nil {
			return err
		}
		attachmentID := platform.ID()
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if _, err := tx.Exec(r.Context(), `INSERT INTO attachment(tenant_id,id,sha256,mime_type,size_bytes,storage_key,process_status,sync_status,created_by,created_node,created_at,updated_at) VALUES($1,$2,$3,'text/csv',$4,$5,'ready','local',$6,$7,$8,$8)`, p.TenantID, attachmentID, hex.EncodeToString(digest[:]), len(content), storageKey, p.UserID, api.Config.NodeID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO export_job(tenant_id,id,job_type,filter_snapshot,requested_by,status,progress,storage_key,expires_at,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'succeeded',100,$6,$7,$8,$8)`, p.TenantID, id, in.JobType, filter, p.UserID, attachmentID, now+int64(7*24*time.Hour/time.Microsecond), now); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "export", "export_job", id, nil, map[string]any{"job_type": in.JobType, "rows": len(records)}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "status": "succeeded", "rows": len(records), "attachment_id": attachmentID})
	})

	api.Handle("GET /api/v1/export-jobs/{id}", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var id, jobType, status, storageKey, errText string
		var progress int
		var createdAt int64
		if err := api.DB.QueryRow(r.Context(), "SELECT id,job_type,status,progress,COALESCE(storage_key,''),COALESCE(error,''),created_at FROM export_job WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&id, &jobType, &status, &progress, &storageKey, &errText, &createdAt); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "导出任务不存在")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": id, "job_type": jobType, "status": status, "progress": progress, "attachment_id": storageKey, "error": errText, "created_at": createdAt, "download": "/api/v1/export-jobs/" + id + "/content"})
	})

	api.Handle("GET /api/v1/export-jobs/{id}/content", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var attachmentID, jobType string
		if err := api.DB.QueryRow(r.Context(), "SELECT COALESCE(storage_key,''),job_type FROM export_job WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&attachmentID, &jobType); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "导出任务不存在")
		}
		var storageKey string
		if err := api.DB.QueryRow(r.Context(), "SELECT storage_key FROM attachment WHERE tenant_id=$1 AND id=$2", p.TenantID, attachmentID).Scan(&storageKey); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "导出文件不存在")
		}
		path := filepath.Join(api.Config.AttachmentDir, storageKey, "content")
		file, err := os.Open(path)
		if err != nil {
			return httpapi.Error(404, "NOT_FOUND", "导出文件不存在")
		}
		defer file.Close()
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.csv"`, jobType, r.PathValue("id")))
		_, err = io.Copy(w, file)
		return err
	})

	api.Handle("GET /api/v1/import-jobs", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,job_type,status,total_rows,success_rows,error_rows,COALESCE(result_attachment_id,''),created_at FROM import_job WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 100`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, jobType, status, result string
			var total, success, failed int
			var createdAt int64
			if err := rows.Scan(&id, &jobType, &status, &total, &success, &failed, &result, &createdAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "job_type": jobType, "status": status, "total_rows": total, "success_rows": success, "error_rows": failed, "result_attachment_id": result, "created_at": createdAt})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/import-jobs", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			JobType            string          `json:"job_type"`
			SourceAttachmentID string          `json:"source_attachment_id"`
			Options            json.RawMessage `json:"options"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.JobType != "employees" {
			return httpapi.Error(400, "VALIDATION_ERROR", "导入类型目前只支持 employees")
		}
		if in.SourceAttachmentID == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "请先上传导入文件")
		}
		id, now := platform.ID(), platform.Now()
		total, success, failed, err := validateEmployeeImport(r.Context(), api, p.TenantID, in.SourceAttachmentID, id)
		status := "validated"
		if err != nil {
			status = "failed"
		}
		if failed > 0 {
			status = "validated"
		}
		if _, err := api.DB.Exec(r.Context(), `INSERT INTO import_job(tenant_id,id,job_type,source_attachment_id,requested_by,status,total_rows,success_rows,error_rows,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)`, p.TenantID, id, in.JobType, in.SourceAttachmentID, p.UserID, status, total, success, failed, now); err != nil {
			return err
		}
		result := map[string]any{"id": id, "status": status, "total_rows": total, "success_rows": success, "error_rows": failed}
		if err != nil {
			result["error"] = err.Error()
		}
		return httpapi.JSON(w, 201, result)
	})

	api.Handle("GET /api/v1/import-jobs/{id}", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var id, jobType, status, source, result string
		var total, success, failed int
		var createdAt int64
		if err := api.DB.QueryRow(r.Context(), `SELECT id,job_type,status,total_rows,success_rows,error_rows,source_attachment_id,COALESCE(result_attachment_id,''),created_at FROM import_job WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id")).Scan(&id, &jobType, &status, &total, &success, &failed, &source, &result, &createdAt); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "导入任务不存在")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": id, "job_type": jobType, "status": status, "total_rows": total, "success_rows": success, "error_rows": failed, "source_attachment_id": source, "result_attachment_id": result, "created_at": createdAt})
	})

	api.Handle("GET /api/v1/import-jobs/{id}/errors", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT row_no,COALESCE(field_key,''),error_code,message,COALESCE(raw_value,'') FROM import_job_error WHERE tenant_id=$1 AND import_job_id=$2 ORDER BY row_no LIMIT 1000`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var rowNo int
			var field, code, message, raw string
			if err := rows.Scan(&rowNo, &field, &code, &message, &raw); err != nil {
				return err
			}
			items = append(items, map[string]any{"row_no": rowNo, "field": field, "error_code": code, "message": message, "raw_value": raw})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/import-jobs/{id}/validate", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var source string
		if err := api.DB.QueryRow(r.Context(), "SELECT source_attachment_id FROM import_job WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&source); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "导入任务不存在")
		}
		total, success, failed, err := validateEmployeeImport(r.Context(), api, p.TenantID, source, r.PathValue("id"))
		if err != nil {
			return err
		}
		status := "validated"
		if _, err := api.DB.Exec(r.Context(), "UPDATE import_job SET status=$1,total_rows=$2,success_rows=$3,error_rows=$4,updated_at=$5 WHERE tenant_id=$6 AND id=$7", status, total, success, failed, platform.Now(), p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": status, "total_rows": total, "success_rows": success, "error_rows": failed})
	})

	api.Handle("POST /api/v1/import-jobs/{id}/commit", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var source, status string
		if err := api.DB.QueryRow(r.Context(), "SELECT source_attachment_id,status FROM import_job WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&source, &status); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "导入任务不存在")
		}
		if status == "committed" {
			return httpapi.Error(409, "ALREADY_COMMITTED", "该导入任务已提交")
		}
		rows, err := readEmployeeImport(r.Context(), api, p.TenantID, source)
		if err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		now := platform.Now()
		inserted := 0
		for _, row := range rows {
			if strings.TrimSpace(row.EmployeeNo) == "" || strings.TrimSpace(row.Name) == "" {
				continue
			}
			var exists bool
			if err := tx.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND employee_no=$2)", p.TenantID, row.EmployeeNo).Scan(&exists); err != nil {
				return err
			}
			if exists {
				continue
			}
			status := row.Status
			if status != "disabled" {
				status = "active"
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO employee(tenant_id,id,employee_no,name,position,workshop,production_line,status,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$9,$10,$11)`, p.TenantID, platform.ID(), row.EmployeeNo, row.Name, optionalText(&row.Position), optionalText(&row.Workshop), optionalText(&row.Line), status, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			inserted++
		}
		if _, err := tx.Exec(r.Context(), "UPDATE import_job SET status='committed',success_rows=$1,updated_at=$2 WHERE tenant_id=$3 AND id=$4", inserted, now, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "import_commit", "import_job", r.PathValue("id"), nil, map[string]any{"inserted": inserted}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "committed", "inserted": inserted})
	})
}

func exportRecords(ctx context.Context, api *httpapi.API, tenantID, jobType string, filter json.RawMessage) ([]string, [][]string, error) {
	switch jobType {
	case "employees":
		rows, err := api.DB.Query(ctx, `SELECT employee_no,name,COALESCE(position,''),COALESCE(workshop,''),COALESCE(production_line,''),status FROM employee WHERE tenant_id=$1 ORDER BY employee_no`, tenantID)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		records := [][]string{}
		for rows.Next() {
			var no, name, position, workshop, line, status string
			if err := rows.Scan(&no, &name, &position, &workshop, &line, &status); err != nil {
				return nil, nil, err
			}
			records = append(records, []string{no, name, position, workshop, line, status})
		}
		return []string{"工号", "姓名", "岗位", "车间", "产线", "状态"}, records, nil
	case "wages":
		var options struct {
			Period string `json:"period"`
		}
		_ = json.Unmarshal(filter, &options)
		if options.Period == "" {
			options.Period = time.Now().Format("2006-01")
		}
		rows, err := api.DB.Query(ctx, `SELECT e.employee_no,e.name,s.gross_amount::text,s.net_amount::text,s.paid_amount::text,s.outstanding_amount::text,s.status FROM payroll_sheet s JOIN employee e ON e.tenant_id=s.tenant_id AND e.id=s.employee_id WHERE s.tenant_id=$1 AND s.payroll_period=$2 ORDER BY e.employee_no`, tenantID, options.Period)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		records := [][]string{}
		for rows.Next() {
			var no, name, gross, net, paid, outstanding, status string
			if err := rows.Scan(&no, &name, &gross, &net, &paid, &outstanding, &status); err != nil {
				return nil, nil, err
			}
			records = append(records, []string{no, name, gross, net, paid, outstanding, status})
		}
		return []string{"工号", "姓名", "应发", "实发", "已发", "未发", "状态"}, records, nil
	}
	return nil, nil, httpapi.Error(400, "VALIDATION_ERROR", "不支持的导出类型")
}

func readEmployeeImport(ctx context.Context, api *httpapi.API, tenantID, attachmentID string) ([]importRow, error) {
	var storageKey string
	if err := api.DB.QueryRow(ctx, "SELECT storage_key FROM attachment WHERE tenant_id=$1 AND id=$2", tenantID, attachmentID).Scan(&storageKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, httpapi.Error(404, "NOT_FOUND", "导入文件不存在")
		}
		return nil, err
	}
	file, err := os.Open(filepath.Join(api.Config.AttachmentDir, storageKey, "content"))
	if err != nil {
		return nil, httpapi.Error(404, "NOT_FOUND", "导入文件内容不存在")
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, httpapi.Error(400, "INVALID_CSV", "CSV 文件为空或格式错误")
	}
	index := map[string]int{}
	for i, name := range header {
		index[strings.TrimSpace(strings.TrimPrefix(name, "\ufeff"))] = i
	}
	column := func(row []string, keys ...string) string {
		for _, key := range keys {
			if i, ok := index[key]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
		}
		return ""
	}
	if _, ok := index["工号"]; !ok {
		if _, ok := index["employee_no"]; !ok {
			return nil, httpapi.Error(400, "INVALID_HEADER", "缺少工号列")
		}
	}
	rows := []importRow{}
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			continue
		}
		rows = append(rows, importRow{
			EmployeeNo: column(record, "工号", "employee_no"),
			Name:       column(record, "姓名", "name"),
			Position:   column(record, "岗位", "position"),
			Workshop:   column(record, "车间", "workshop"),
			Line:       column(record, "产线", "production_line"),
			Status:     column(record, "状态", "status"),
		})
	}
	return rows, nil
}

func validateEmployeeImport(ctx context.Context, api *httpapi.API, tenantID, attachmentID, jobID string) (int, int, int, error) {
	rows, err := readEmployeeImport(ctx, api, tenantID, attachmentID)
	if err != nil {
		return 0, 0, 0, err
	}
	if _, err := api.DB.Exec(ctx, "DELETE FROM import_job_error WHERE tenant_id=$1 AND import_job_id=$2", tenantID, jobID); err != nil {
		return 0, 0, 0, err
	}
	seen := map[string]bool{}
	success, failed := 0, 0
	for i, row := range rows {
		rowNo := i + 2
		problems := []string{}
		if strings.TrimSpace(row.EmployeeNo) == "" {
			problems = append(problems, "工号不能为空")
		}
		if strings.TrimSpace(row.Name) == "" {
			problems = append(problems, "姓名不能为空")
		}
		if row.EmployeeNo != "" && seen[row.EmployeeNo] {
			problems = append(problems, "文件内工号重复")
		}
		if row.EmployeeNo != "" {
			seen[row.EmployeeNo] = true
			var exists bool
			if err := api.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND employee_no=$2)", tenantID, row.EmployeeNo).Scan(&exists); err != nil {
				return 0, 0, 0, err
			}
			if exists {
				problems = append(problems, "工号已存在")
			}
		}
		if len(problems) == 0 {
			success++
			continue
		}
		failed++
		for _, problem := range problems {
			if _, err := api.DB.Exec(ctx, `INSERT INTO import_job_error(tenant_id,id,import_job_id,row_no,field_key,error_code,message,raw_value) VALUES($1,$2,$3,$4,'employee_no','INVALID_ROW',$5,$6)`, tenantID, platform.ID(), jobID, rowNo, problem, row.EmployeeNo); err != nil {
				return 0, 0, 0, err
			}
		}
	}
	return len(rows), success, failed, nil
}
