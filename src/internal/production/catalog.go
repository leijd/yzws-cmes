package production

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func Register(api *httpapi.API) {
	registerCatalog(api)
	registerOrders(api)
	registerTasks(api)
	registerReports(api)
	registerQuality(api)
	registerPayroll(api)
	registerIdentity(api)
	registerQueries(api)
	registerMarkers(api)
	registerWorkOrderReview(api)
	registerBundleParts(api)
	registerBundleLabels(api)
	registerAssignments(api)
	registerPrint(api)
	registerMaterialReceipts(api)
	registerInventory(api)
	registerMaterialIssues(api)
	registerProcessInspections(api)
	registerShipmentReturns(api)
	registerRework(api)
	registerTickets(api)
	registerNotifications(api)
	registerImportExport(api)
	registerBackupSync(api)
	registerCustomFields(api)
	registerProgress(api)
	registerPayrollWide(api)
}

type productInput struct {
	ProductCode          string  `json:"product_code"`
	Name                 string  `json:"name"`
	Spec                 *string `json:"spec"`
	Unit                 string  `json:"unit"`
	CategoryID           *string `json:"category_id"`
	AccessoryCombination *string `json:"accessory_combination"`
	CustomerID           *string `json:"customer_id"`
	DevelopmentDate      *string `json:"development_date"`
	SalePrice            *string `json:"sale_price"`
	Remark               *string `json:"remark"`
}

func registerCatalog(api *httpapi.API) {
	api.Handle("POST /api/v1/products", "product:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in productInput
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.ProductCode == "" || in.Name == "" || in.Unit == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "产品编号、名称和单位必填")
		}
		if in.SalePrice != nil {
			if _, err := decimal.NewFromString(*in.SalePrice); err != nil {
				return httpapi.Error(400, "INVALID_DECIMAL", "售价格式不正确")
			}
		}
		id, now := platform.ID(), platform.Now()
		_, err := api.DB.Exec(r.Context(), `INSERT INTO product(tenant_id,id,product_code,name,spec,unit,category_id,accessory_combination,customer_id,development_date,sale_price,remark,created_at,created_by,created_node,updated_at,updated_by,updated_node)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$13,$14,$15)`, p.TenantID, id, in.ProductCode, in.Name, in.Spec, in.Unit, in.CategoryID, in.AccessoryCombination, in.CustomerID, in.DevelopmentDate, in.SalePrice, in.Remark, now, p.UserID, api.Config.NodeID)
		if err != nil {
			return httpapi.Error(409, "DUPLICATE_PRODUCT", "产品编号已存在")
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "version": 1, "status": "draft", "sync_status": "pending"})
	})
	api.Handle("GET /api/v1/products", "product:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		limit, cursor := httpapi.Page(r)
		args := []any{p.TenantID}
		where := "p.tenant_id=$1"
		if cursor != "" {
			args = append(args, cursor)
			where += fmt.Sprintf(" AND p.id>$%d", len(args))
		}
		if keyword := r.URL.Query().Get("keyword"); keyword != "" {
			args = append(args, "%"+keyword+"%")
			where += fmt.Sprintf(" AND (p.name ILIKE $%d OR p.product_code ILIKE $%d)", len(args), len(args))
		}
		args = append(args, limit+1)
		rows, err := api.DB.Query(r.Context(), fmt.Sprintf(`SELECT p.id,p.product_code,p.name,p.spec,p.unit,p.status,p.version,
			COALESCE(p.image_attachment_id,(SELECT l.attachment_id FROM attachment_link l WHERE l.tenant_id=p.tenant_id AND l.owner_type='product' AND l.owner_id=p.id AND l.is_deleted=FALSE ORDER BY CASE WHEN l.usage_type='standard' THEN 0 ELSE 1 END,l.sort_no,l.id LIMIT 1)) AS image_attachment_id,
			(SELECT COUNT(*) FROM product_process pp WHERE pp.tenant_id=p.tenant_id AND pp.product_id=p.id AND pp.product_version=p.version),
			COALESCE((SELECT SUM(pp.unit_price*pp.report_ratio) FROM product_process pp WHERE pp.tenant_id=p.tenant_id AND pp.product_id=p.id AND pp.product_version=p.version),0)::text,
			(SELECT COALESCE(e.name,u.login_name) FROM product_process pp JOIN user_account u ON u.tenant_id=pp.tenant_id AND u.id=pp.created_by LEFT JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id WHERE pp.tenant_id=p.tenant_id AND pp.product_id=p.id AND pp.product_version=p.version ORDER BY pp.sequence_no LIMIT 1),
			(SELECT pp.created_at FROM product_process pp WHERE pp.tenant_id=p.tenant_id AND pp.product_id=p.id AND pp.product_version=p.version ORDER BY pp.sequence_no LIMIT 1)
			FROM product p WHERE %s ORDER BY p.id LIMIT $%d`, where, len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, unit, status, price string
			var spec, imageID, configurerName *string
			var configuredAt *int64
			var version, count int64
			if err := rows.Scan(&id, &code, &name, &spec, &unit, &status, &version, &imageID, &count, &price, &configurerName, &configuredAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "product_code": code, "name": name, "spec": spec, "unit": unit, "status": status, "version": version, "image_attachment_id": imageID, "process_count": count, "theoretical_price": price, "configurer_name": configurerName, "configured_at": configuredAt})
		}
		hasMore := len(items) > limit
		if hasMore {
			items = items[:limit]
		}
		var next any
		if hasMore {
			next = items[len(items)-1]["id"]
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items, "next_cursor": next, "has_more": hasMore})
	})
	api.Handle("GET /api/v1/products/{id}", "product:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var item map[string]any
		err := api.DB.QueryRow(r.Context(), "SELECT row_to_json(t) FROM (SELECT * FROM product WHERE tenant_id=$1 AND id=$2) t", p.TenantID, r.PathValue("id")).Scan(&item)
		if err == pgx.ErrNoRows {
			return httpapi.Error(404, "NOT_FOUND", "产品不存在")
		}
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, item)
	})
	api.Handle("PATCH /api/v1/products/{id}", "product:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version   int64   `json:"version"`
			Name      string  `json:"name"`
			Spec      *string `json:"spec"`
			Unit      string  `json:"unit"`
			SalePrice *string `json:"sale_price"`
			Remark    *string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var current int64
		if err := tx.QueryRow(r.Context(), "SELECT version FROM product WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&current); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "产品不存在")
		}
		if current != in.Version {
			return httpapi.Error(409, "VERSION_CONFLICT", "产品已变更")
		}
		if err := copyProductRoutes(r.Context(), tx, p.TenantID, r.PathValue("id"), current, current+1); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE product SET name=$1,spec=$2,unit=$3,sale_price=$4,remark=$5,version=$6,updated_at=$7,updated_by=$8,updated_node=$9 WHERE tenant_id=$10 AND id=$11`, in.Name, in.Spec, in.Unit, in.SalePrice, in.Remark, current+1, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "version": current + 1})
	})
	api.Handle("GET /api/v1/products/{id}/images", "product:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT l.id,l.attachment_id,l.usage_type,l.sort_no,a.mime_type,a.size_bytes FROM attachment_link l JOIN attachment a ON a.tenant_id=l.tenant_id AND a.id=l.attachment_id WHERE l.tenant_id=$1 AND l.owner_type='product' AND l.owner_id=$2 AND l.is_deleted=FALSE ORDER BY CASE WHEN l.usage_type='standard' THEN 0 ELSE 1 END,l.sort_no,l.id`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var linkID, attachmentID, viewType, mime string
			var sortNo int
			var size int64
			if err := rows.Scan(&linkID, &attachmentID, &viewType, &sortNo, &mime, &size); err != nil {
				return err
			}
			items = append(items, map[string]any{"link_id": linkID, "attachment_id": attachmentID, "view_type": viewType, "sort_no": sortNo, "mime_type": mime, "size_bytes": size})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("POST /api/v1/products/{id}/images", "product:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			AttachmentID string `json:"attachment_id"`
			ViewType     string `json:"view_type"`
			SortNo       int    `json:"sort_no"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if !imageViewTypes[in.ViewType] {
			return httpapi.Error(400, "INVALID_IMAGE_VIEW", "图片视角不合法")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var ready string
		if err := tx.QueryRow(r.Context(), "SELECT process_status FROM attachment WHERE tenant_id=$1 AND id=$2", p.TenantID, in.AttachmentID).Scan(&ready); err != nil || ready != "ready" {
			return httpapi.Error(400, "ATTACHMENT_PENDING", "图片不存在或尚未上传完成")
		}
		linkID := platform.ID()
		if _, err := tx.Exec(r.Context(), `INSERT INTO attachment_link(tenant_id,id,attachment_id,owner_type,owner_id,usage_type,sort_no) VALUES($1,$2,$3,'product',$4,$5,$6) ON CONFLICT DO NOTHING`, p.TenantID, linkID, in.AttachmentID, r.PathValue("id"), in.ViewType, in.SortNo); err != nil {
			return err
		}
		if in.ViewType == "standard" {
			if _, err := tx.Exec(r.Context(), "UPDATE product SET image_attachment_id=$1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6", in.AttachmentID, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
				return err
			}
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "upload_image", "product", r.PathValue("id"), nil, map[string]any{"attachment_id": in.AttachmentID, "view_type": in.ViewType}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"link_id": linkID, "attachment_id": in.AttachmentID, "view_type": in.ViewType})
	})
	api.Handle("DELETE /api/v1/products/{id}/images/{link_id}", "product:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var attachmentID, usageType string
		err = tx.QueryRow(r.Context(), "SELECT attachment_id,usage_type FROM attachment_link WHERE tenant_id=$1 AND owner_type='product' AND owner_id=$2 AND id=$3 AND is_deleted=FALSE FOR UPDATE", p.TenantID, r.PathValue("id"), r.PathValue("link_id")).Scan(&attachmentID, &usageType)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "产品图片不存在")
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE attachment_link SET is_deleted=TRUE WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("link_id")); err != nil {
			return err
		}
		if usageType == "standard" {
			if _, err := tx.Exec(r.Context(), "UPDATE product SET image_attachment_id=NULL,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5 AND image_attachment_id=$6", platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), attachmentID); err != nil {
				return err
			}
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete_image", "product", r.PathValue("id"), nil, map[string]any{"attachment_id": attachmentID, "link_id": r.PathValue("link_id"), "usage_type": usageType}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true, "link_id": r.PathValue("link_id")})
	})
	api.Handle("POST /api/v1/products/{id}/visibility", "product:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version int64 `json:"version"`
			Visible bool  `json:"visible"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var current int64
		if err := tx.QueryRow(r.Context(), "SELECT version FROM product WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&current); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "产品不存在")
		}
		if current != in.Version {
			return httpapi.Error(409, "VERSION_CONFLICT", "产品已变更")
		}
		if err := copyProductRoutes(r.Context(), tx, p.TenantID, r.PathValue("id"), current, current+1); err != nil {
			return err
		}
		status := "disabled"
		if in.Visible {
			status = "active"
		}
		if _, err := tx.Exec(r.Context(), "UPDATE product SET status=$1,version=$2,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$6 AND id=$7", status, current+1, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": status, "version": current + 1})
	})
	api.Handle("DELETE /api/v1/products/{id}", "product:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var used int
		if err := tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM work_order WHERE tenant_id=$1 AND product_id=$2", p.TenantID, r.PathValue("id")).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return httpapi.Error(409, "PRODUCT_IN_USE", "产品已投入生产，不能删除，可改为隐藏")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM product_process WHERE tenant_id=$1 AND product_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE attachment_link SET is_deleted=TRUE WHERE tenant_id=$1 AND owner_type='product' AND owner_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		result, err := tx.Exec(r.Context(), "DELETE FROM product WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(404, "NOT_FOUND", "产品不存在")
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete", "product", r.PathValue("id"), nil, nil, "无生产引用，删除产品"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})
	api.Handle("PUT /api/v1/products/{id}/processes", "product:write", saveProductProcesses)
	api.Handle("GET /api/v1/products/{id}/processes", "product:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT pp.id,pp.process_id,pd.process_code,pd.name,pp.sequence_no,pp.unit_price::text,pp.report_ratio::text,
			pp.department_id,pp.standard_work_seconds,pp.is_parallel,pp.is_critical,pp.is_required,pp.remark
			FROM product p JOIN product_process pp ON pp.tenant_id=p.tenant_id AND pp.product_id=p.id AND pp.product_version=p.version
			JOIN process_definition pd ON pd.tenant_id=pp.tenant_id AND pd.id=pp.process_id WHERE p.tenant_id=$1 AND p.id=$2 ORDER BY pp.sequence_no`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		total := decimal.Zero
		for rows.Next() {
			var id, processID, code, name, unitPrice, ratio string
			var sequence int
			var department, remark *string
			var seconds int64
			var parallel, critical, required bool
			if err := rows.Scan(&id, &processID, &code, &name, &sequence, &unitPrice, &ratio, &department, &seconds, &parallel, &critical, &required, &remark); err != nil {
				return err
			}
			price, _ := decimal.NewFromString(unitPrice)
			rate, _ := decimal.NewFromString(ratio)
			total = total.Add(price.Mul(rate))
			items = append(items, map[string]any{"id": id, "process_id": processID, "process_code": code, "name": name, "sequence_no": sequence, "unit_price": unitPrice, "report_ratio": ratio, "department_id": department, "standard_work_seconds": seconds, "is_parallel": parallel, "is_critical": critical, "is_required": required, "remark": remark})
		}
		var configurerName *string
		var configuredAt *int64
		if err := api.DB.QueryRow(r.Context(), `SELECT COALESCE(e.name,u.login_name), pp.created_at FROM product_process pp JOIN user_account u ON u.tenant_id=pp.tenant_id AND u.id=pp.created_by LEFT JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id WHERE pp.tenant_id=$1 AND pp.product_id=$2 AND pp.product_version=(SELECT version FROM product WHERE tenant_id=$1 AND id=$2) ORDER BY pp.sequence_no LIMIT 1`, p.TenantID, r.PathValue("id")).Scan(&configurerName, &configuredAt); err != nil {
			configurerName, configuredAt = nil, nil
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items, "theoretical_price": total.StringFixed(6), "configurer_name": configurerName, "configured_at": configuredAt})
	})
	registerProcessDefinitions(api)
}

type routeItem struct {
	ProcessID           string  `json:"process_id"`
	SequenceNo          int     `json:"sequence_no"`
	UnitPrice           string  `json:"unit_price"`
	ReportRatio         string  `json:"report_ratio"`
	DepartmentID        *string `json:"department_id"`
	StandardWorkSeconds int64   `json:"standard_work_seconds"`
	IsParallel          bool    `json:"is_parallel"`
	IsCritical          bool    `json:"is_critical"`
	IsRequired          bool    `json:"is_required"`
	Remark              *string `json:"remark"`
}

func saveProductProcesses(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	api := currentAPI
	var in struct {
		Version int64       `json:"version"`
		Items   []routeItem `json:"items"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	sequences := map[int]bool{}
	processes := map[string]bool{}
	for _, item := range in.Items {
		price, err1 := decimal.NewFromString(item.UnitPrice)
		ratio, err2 := decimal.NewFromString(item.ReportRatio)
		if item.ProcessID == "" || item.SequenceNo < 1 || sequences[item.SequenceNo] || processes[item.ProcessID] || err1 != nil || err2 != nil || price.IsNegative() || !ratio.IsPositive() {
			return httpapi.Error(400, "INVALID_ROUTE", "工序、序号、工价或配比不合法")
		}
		sequences[item.SequenceNo] = true
		processes[item.ProcessID] = true
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var current int64
	err = tx.QueryRow(r.Context(), "SELECT version FROM product WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&current)
	if err == pgx.ErrNoRows {
		return httpapi.Error(404, "NOT_FOUND", "产品不存在")
	}
	if err != nil {
		return err
	}
	if current != in.Version {
		return httpapi.Error(409, "VERSION_CONFLICT", "产品已变更")
	}
	newVersion := current + 1
	for _, item := range in.Items {
		if _, err = tx.Exec(r.Context(), `INSERT INTO product_process(tenant_id,id,product_id,product_version,process_id,sequence_no,unit_price,report_ratio,department_id,standard_work_seconds,is_parallel,is_critical,is_required,remark,created_by)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, p.TenantID, platform.ID(), r.PathValue("id"), newVersion, item.ProcessID, item.SequenceNo, item.UnitPrice, item.ReportRatio, item.DepartmentID, item.StandardWorkSeconds, item.IsParallel, item.IsCritical, item.IsRequired, item.Remark, p.UserID); err != nil {
			return httpapi.Error(400, "INVALID_ROUTE", "工序引用无效")
		}
	}
	status := "active"
	if len(in.Items) == 0 {
		status = "draft"
	}
	if _, err = tx.Exec(r.Context(), `UPDATE product SET version=$1,status=$2,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$6 AND id=$7`, newVersion, status, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
		return err
	}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "save_processes", "product", r.PathValue("id"), map[string]any{"version": current}, in, ""); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 200, map[string]any{"product_id": r.PathValue("id"), "product_version": newVersion, "items": in.Items, "status": status})
}

var currentAPI *httpapi.API

func registerProcessDefinitions(api *httpapi.API) {
	currentAPI = api
	api.Handle("POST /api/v1/process-definitions", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ProcessCode         string  `json:"process_code"`
			Name                string  `json:"name"`
			DefaultUnitPrice    string  `json:"default_unit_price"`
			DefaultReportRatio  string  `json:"default_report_ratio"`
			DefaultDepartmentID *string `json:"default_department_id"`
			StandardWorkSeconds int64   `json:"standard_work_seconds"`
			Remark              *string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.DefaultUnitPrice == "" {
			in.DefaultUnitPrice = "0"
		}
		if in.DefaultReportRatio == "" {
			in.DefaultReportRatio = "1"
		}
		price, e1 := decimal.NewFromString(in.DefaultUnitPrice)
		ratio, e2 := decimal.NewFromString(in.DefaultReportRatio)
		if in.ProcessCode == "" || in.Name == "" || e1 != nil || e2 != nil || price.IsNegative() || !ratio.IsPositive() {
			return httpapi.Error(400, "VALIDATION_ERROR", "工序编码、名称、工价或配比不合法")
		}
		id, now := platform.ID(), platform.Now()
		_, err := api.DB.Exec(r.Context(), `INSERT INTO process_definition(tenant_id,id,process_code,name,default_unit_price,default_report_ratio,default_department_id,standard_work_seconds,remark,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$10,$11,$12)`, p.TenantID, id, in.ProcessCode, in.Name, in.DefaultUnitPrice, in.DefaultReportRatio, in.DefaultDepartmentID, in.StandardWorkSeconds, in.Remark, now, p.UserID, api.Config.NodeID)
		if err != nil {
			return httpapi.Error(409, "DUPLICATE_PROCESS", "工序编码已存在")
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "version": 1, "status": "active"})
	})
	api.Handle("GET /api/v1/process-definitions", "process:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,process_code,name,default_unit_price::text,default_report_ratio::text,default_department_id,standard_work_seconds,status,version FROM process_definition WHERE tenant_id=$1 ORDER BY process_code`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, price, ratio, status string
			var department *string
			var seconds, version int64
			if err := rows.Scan(&id, &code, &name, &price, &ratio, &department, &seconds, &status, &version); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "process_code": code, "name": name, "default_unit_price": price, "default_report_ratio": ratio, "default_department_id": department, "standard_work_seconds": seconds, "status": status, "version": version})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("PATCH /api/v1/process-definitions/{id}", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ID                  string  `json:"id"`
			Version             int64   `json:"version"`
			ProcessCode         string  `json:"process_code"`
			Name                string  `json:"name"`
			DefaultUnitPrice    string  `json:"default_unit_price"`
			DefaultReportRatio  string  `json:"default_report_ratio"`
			StandardWorkSeconds int64   `json:"standard_work_seconds"`
			Remark              *string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.ID != "" && in.ID != r.PathValue("id") {
			return httpapi.Error(400, "INVALID_ID", "请求体记录ID与路径ID不一致")
		}
		result, err := api.DB.Exec(r.Context(), `UPDATE process_definition SET process_code=$1,name=$2,default_unit_price=$3,default_report_ratio=$4,standard_work_seconds=$5,remark=$6,version=version+1,updated_at=$7,updated_by=$8,updated_node=$9 WHERE tenant_id=$10 AND id=$11 AND version=$12`, in.ProcessCode, in.Name, in.DefaultUnitPrice, in.DefaultReportRatio, in.StandardWorkSeconds, in.Remark, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), in.Version)
		if isUniqueViolation(err) {
			return httpapi.Error(409, "DUPLICATE_PROCESS", "工序编码已存在")
		}
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(409, "VERSION_CONFLICT", "工序已变更")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "version": in.Version + 1})
	})
	api.Handle("DELETE /api/v1/process-definitions/{id}", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var used int
		if err := tx.QueryRow(r.Context(), `SELECT (SELECT COUNT(*) FROM production_task t JOIN product_process pp ON pp.tenant_id=t.tenant_id AND pp.id=t.product_process_id WHERE t.tenant_id=$1 AND pp.process_id=$2)+(SELECT COUNT(*) FROM work_order_process_snapshot s JOIN product_process pp ON pp.tenant_id=s.tenant_id AND pp.id=s.product_process_id WHERE s.tenant_id=$1 AND pp.process_id=$2)`, p.TenantID, r.PathValue("id")).Scan(&used); err != nil {
			return err
		}
		if used > 0 {
			return httpapi.Error(409, "PROCESS_IN_USE", "该工序已用于生产记录，不能删除")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM employee_skill WHERE tenant_id=$1 AND process_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM product_process WHERE tenant_id=$1 AND process_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		result, err := tx.Exec(r.Context(), "DELETE FROM process_definition WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(404, "NOT_FOUND", "工序不存在")
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete", "process_definition", r.PathValue("id"), nil, nil, "无生产引用，删除工序"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})
}

var imageViewTypes = map[string]bool{"standard": true, "front": true, "side": true, "back": true, "top": true, "bottom": true, "interior": true, "other": true}

func copyProductRoutes(ctx context.Context, tx pgx.Tx, tenantID, productID string, oldVersion, newVersion int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO product_process(tenant_id,id,product_id,product_version,process_id,sequence_no,unit_price,report_ratio,department_id,standard_work_seconds,is_parallel,is_critical,is_required,defect_config,remark)
		SELECT tenant_id,md5(id||random()::text),product_id,$2::bigint,process_id,sequence_no,unit_price,report_ratio,department_id,standard_work_seconds,is_parallel,is_critical,is_required,defect_config,remark FROM product_process WHERE tenant_id=$1 AND product_id=$3 AND product_version=$4`, tenantID, newVersion, productID, oldVersion)
	return err
}

var _ = strings.TrimSpace
