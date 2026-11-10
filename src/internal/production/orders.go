package production

import (
	"errors"
	"fmt"
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func registerOrders(api *httpapi.API) {
	api.Handle("POST /api/v1/projects", "project:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ProjectCode          string  `json:"project_code"`
			CustomerID           string  `json:"customer_id"`
			ProductID            string  `json:"product_id"`
			ReceivedDate         *int64  `json:"received_date"`
			OrderQty             int64   `json:"order_qty"`
			CustomerUnitPrice    string  `json:"customer_unit_price"`
			ExternalUnitPrice    string  `json:"external_unit_price"`
			MaterialOwner        *string `json:"material_owner"`
			OwnerEmployeeID      *string `json:"owner_employee_id"`
			QCEmployeeID         *string `json:"qc_employee_id"`
			RequiredDeliveryDate *string `json:"required_delivery_date"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.ProjectCode == "" || in.CustomerID == "" || in.ProductID == "" || in.OrderQty <= 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "项目编号、客户、产品和订单数必填")
		}
		if in.CustomerUnitPrice == "" {
			in.CustomerUnitPrice = "0"
		}
		if in.ExternalUnitPrice == "" {
			in.ExternalUnitPrice = "0"
		}
		id, now := platform.ID(), platform.Now()
		_, err := api.DB.Exec(r.Context(), `INSERT INTO project(tenant_id,id,project_code,customer_id,product_id,received_date,order_qty,customer_unit_price,external_unit_price,material_owner,owner_employee_id,qc_employee_id,required_delivery_date,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$14,$15,$16)`, p.TenantID, id, in.ProjectCode, in.CustomerID, in.ProductID, in.ReceivedDate, in.OrderQty, in.CustomerUnitPrice, in.ExternalUnitPrice, in.MaterialOwner, in.OwnerEmployeeID, in.QCEmployeeID, in.RequiredDeliveryDate, now, p.UserID, api.Config.NodeID)
		if err != nil {
			return httpapi.Error(409, "INVALID_PROJECT", "项目编号重复或客户/产品引用无效")
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "version": 1, "status": "draft"})
	})
	api.Handle("GET /api/v1/projects", "project:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT p.id,p.project_code,c.name,p2.name,p.order_qty,p.customer_unit_price::text,p.external_unit_price::text,p.required_delivery_date,p.status,p.version,COALESCE((SELECT SUM(sl.qty) FROM shipment_line sl JOIN shipment s ON s.tenant_id=sl.tenant_id AND s.id=sl.shipment_id WHERE sl.tenant_id=p.tenant_id AND s.project_id=p.id AND s.status='confirmed'),0)
			- COALESCE((SELECT SUM(sr.qty) FROM shipment_return sr WHERE sr.tenant_id=p.tenant_id AND sr.project_id=p.id AND sr.status='posted'),0),
			COALESCE(p2.image_attachment_id,(SELECT l.attachment_id FROM attachment_link l WHERE l.tenant_id=p2.tenant_id AND l.owner_type='product' AND l.owner_id=p2.id AND l.is_deleted=FALSE ORDER BY CASE WHEN l.usage_type='standard' THEN 0 ELSE 1 END,l.sort_no,l.id LIMIT 1)) AS image_attachment_id
			FROM project p JOIN customer c ON c.tenant_id=p.tenant_id AND c.id=p.customer_id JOIN product p2 ON p2.tenant_id=p.tenant_id AND p2.id=p.product_id WHERE p.tenant_id=$1 ORDER BY p.id DESC`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, customer, product, price, external, status string
			var qty, version, shipped int64
			var delivery *string
			var imageID *string
			if err := rows.Scan(&id, &code, &customer, &product, &qty, &price, &external, &delivery, &status, &version, &shipped, &imageID); err != nil {
				return err
			}
			unshipped := qty - shipped
			if unshipped < 0 {
				unshipped = 0
			}
			items = append(items, map[string]any{"id": id, "project_code": code, "customer_name": customer, "product_name": product, "order_qty": qty, "shipped_qty": shipped, "customer_unit_price": price, "external_unit_price": external, "required_delivery_date": delivery, "status": status, "version": version, "unshipped_qty": unshipped, "image_attachment_id": imageID})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("GET /api/v1/projects/{id}/summary", "project:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var qty, shipped int64
		var price, external string
		err := api.DB.QueryRow(r.Context(), `SELECT p.order_qty,p.customer_unit_price::text,p.external_unit_price::text,COALESCE((SELECT SUM(sl.qty) FROM shipment_line sl JOIN shipment s ON s.tenant_id=sl.tenant_id AND s.id=sl.shipment_id WHERE sl.tenant_id=p.tenant_id AND s.project_id=p.id AND s.status='confirmed'),0)
			- COALESCE((SELECT SUM(sr.qty) FROM shipment_return sr WHERE sr.tenant_id=p.tenant_id AND sr.project_id=p.id AND sr.status='posted'),0) FROM project p WHERE p.tenant_id=$1 AND p.id=$2`, p.TenantID, r.PathValue("id")).Scan(&qty, &price, &external, &shipped)
		if err == pgx.ErrNoRows {
			return httpapi.Error(404, "NOT_FOUND", "项目不存在")
		}
		if err != nil {
			return err
		}
		customerPrice, _ := decimal.NewFromString(price)
		externalPrice, _ := decimal.NewFromString(external)
		unshipped := qty - shipped
		if unshipped < 0 {
			unshipped = 0
		}
		return httpapi.JSON(w, 200, map[string]any{"order_qty": qty, "shipped_qty": shipped, "unshipped_qty": unshipped, "customer_amount": customerPrice.Mul(decimal.NewFromInt(shipped)).StringFixed(6), "factory_amount": externalPrice.Mul(decimal.NewFromInt(shipped)).StringFixed(6), "customer_unit_price": price, "external_unit_price": external})
	})
	api.Handle("PATCH /api/v1/projects/{id}", "project:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			OrderQty             *int64  `json:"order_qty"`
			RequiredDeliveryDate *string `json:"required_delivery_date"`
			CustomerUnitPrice    *string `json:"customer_unit_price"`
			ExternalUnitPrice    *string `json:"external_unit_price"`
			Version              int64   `json:"version"`
			Reason               string  `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var currentQty, shipped, planned, version int64
		if err := tx.QueryRow(r.Context(), `SELECT p.order_qty,
			COALESCE((SELECT SUM(sl.qty) FROM shipment_line sl JOIN shipment s ON s.tenant_id=sl.tenant_id AND s.id=sl.shipment_id WHERE sl.tenant_id=p.tenant_id AND s.project_id=p.id AND s.status='confirmed'),0)
			- COALESCE((SELECT SUM(sr.qty) FROM shipment_return sr WHERE sr.tenant_id=p.tenant_id AND sr.project_id=p.id AND sr.status='posted'),0),
			COALESCE((SELECT SUM(w.planned_product_qty) FROM work_order w WHERE w.tenant_id=p.tenant_id AND w.project_id=p.id AND w.status<>'cancelled'),0),
			p.version FROM project p WHERE p.tenant_id=$1 AND p.id=$2 FOR UPDATE`, p.TenantID, r.PathValue("id")).Scan(&currentQty, &shipped, &planned, &version); err != nil {
			if err == pgx.ErrNoRows {
				return httpapi.Error(404, "NOT_FOUND", "项目不存在")
			}
			return err
		}
		if version != in.Version {
			return httpapi.Error(409, "VERSION_CONFLICT", "项目已变更，请刷新后重试")
		}
		newQty := currentQty
		if in.OrderQty != nil {
			newQty = *in.OrderQty
		}
		if newQty < shipped || newQty < planned {
			appErr := httpapi.Error(400, "PROJECT_QTY_TOO_LOW", "调整后订单数不能小于已出货数或已排产数")
			appErr.Details = map[string]any{"shipped_qty": shipped, "planned_qty": planned, "requested": newQty}
			return appErr
		}
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), `UPDATE project SET order_qty=$1,required_delivery_date=COALESCE($2,required_delivery_date),customer_unit_price=COALESCE($3,customer_unit_price),external_unit_price=COALESCE($4,external_unit_price),version=version+1,updated_at=$5,updated_by=$6,updated_node=$7 WHERE tenant_id=$8 AND id=$9`, newQty, in.RequiredDeliveryDate, in.CustomerUnitPrice, in.ExternalUnitPrice, now, p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		response := map[string]any{"id": r.PathValue("id"), "order_qty": newQty, "version": version + 1}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "update", "project", r.PathValue("id"), map[string]any{"order_qty": currentQty}, response, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
	})
	api.Handle("POST /api/v1/work-orders", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			WorkOrderCode        string  `json:"work_order_code"`
			ProjectID            string  `json:"project_id"`
			ProductVersion       int64   `json:"product_version"`
			PlannedProductQty    int64   `json:"planned_product_qty"`
			Priority             string  `json:"priority"`
			PlannedStartAt       *int64  `json:"planned_start_at"`
			PlannedEndAt         *int64  `json:"planned_end_at"`
			PackagingRequirement *string `json:"packaging_requirement"`
			SpecialRequirement   *string `json:"special_requirement"`
			IssueDate            *string `json:"issue_date"`
			Color                *string `json:"color"`
			WorkshopOrgUnitID    *string `json:"workshop_org_unit_id"`
			LineOrgUnitID        *string `json:"line_org_unit_id"`
			ReportMode           string  `json:"report_mode"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.WorkOrderCode == "" || in.ProjectID == "" || in.PlannedProductQty <= 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "工单编号、项目和计划数必填")
		}
		if in.Priority == "" {
			in.Priority = "normal"
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var productID string
		var currentVersion, orderQty, planned int64
		err = tx.QueryRow(r.Context(), `SELECT p.product_id,p.order_qty,COALESCE((SELECT SUM(w.planned_product_qty) FROM work_order w WHERE w.tenant_id=p.tenant_id AND w.project_id=p.id AND w.status<>'cancelled'),0) FROM project p WHERE p.tenant_id=$1 AND p.id=$2`, p.TenantID, in.ProjectID).Scan(&productID, &orderQty, &planned)
		if err != nil {
			return httpapi.Error(400, "INVALID_PROJECT", "项目不存在")
		}
		if planned+in.PlannedProductQty > orderQty {
			appErr := httpapi.Error(409, "QUANTITY_EXCEEDED", "同项目工单数量合计超过项目来料数")
			appErr.Details = map[string]any{"order_qty": orderQty, "planned": planned, "requested": in.PlannedProductQty, "remaining": orderQty - planned}
			return appErr
		}
		if err = validateWorkshopLine(r.Context(), tx, p.TenantID, in.WorkshopOrgUnitID, in.LineOrgUnitID); err != nil {
			return err
		}
		if in.ReportMode == "" {
			in.ReportMode = "B"
		}
		if in.ReportMode != "A" && in.ReportMode != "B" {
			return httpapi.Error(400, "VALIDATION_ERROR", "报工模式只能是 A 或 B")
		}
		if err = tx.QueryRow(r.Context(), "SELECT version FROM product WHERE tenant_id=$1 AND id=$2", p.TenantID, productID).Scan(&currentVersion); err != nil {
			return err
		}
		if in.ProductVersion == 0 {
			in.ProductVersion = currentVersion
		}
		var routes int
		if err = tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM product_process WHERE tenant_id=$1 AND product_id=$2 AND product_version=$3", p.TenantID, productID, in.ProductVersion).Scan(&routes); err != nil {
			return err
		}
		if routes == 0 {
			return httpapi.Error(400, "PRODUCT_PROCESS_EMPTY", "产品尚未配置工序")
		}
		id, now := platform.ID(), platform.Now()
		_, err = tx.Exec(r.Context(), `INSERT INTO work_order(tenant_id,id,work_order_code,project_id,product_id,product_version,planned_product_qty,status,priority,planned_start_at,planned_end_at,packaging_requirement,special_requirement,issue_date,color,workshop_org_unit_id,line_org_unit_id,report_mode,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,'pending_release',$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$18,$19,$20)`, p.TenantID, id, in.WorkOrderCode, in.ProjectID, productID, in.ProductVersion, in.PlannedProductQty, in.Priority, in.PlannedStartAt, in.PlannedEndAt, in.PackagingRequirement, in.SpecialRequirement, in.IssueDate, in.Color, in.WorkshopOrgUnitID, in.LineOrgUnitID, in.ReportMode, now, p.UserID, api.Config.NodeID)
		if err != nil {
			return httpapi.Error(409, "DUPLICATE_WORK_ORDER", "工单编号已存在")
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "version": 1, "status": "pending_release", "product_version": in.ProductVersion})
	})
	api.Handle("GET /api/v1/work-orders", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return listWorkOrders(api, w, r, p)
	})
	api.Handle("GET /api/v1/work-orders/{id}", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var item map[string]any
		err := api.DB.QueryRow(r.Context(), "SELECT row_to_json(t) FROM (SELECT * FROM work_order WHERE tenant_id=$1 AND id=$2) t", p.TenantID, r.PathValue("id")).Scan(&item)
		if err == pgx.ErrNoRows {
			return httpapi.Error(404, "NOT_FOUND", "工单不存在")
		}
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 200, item)
	})
	api.Handle("POST /api/v1/cutting-orders", "cutting:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return createCutting(api, w, r, p)
	})
	api.Handle("GET /api/v1/cutting-orders", "cutting:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,cutting_no,work_order_id,cut_qty,status,version FROM cutting_order WHERE tenant_id=$1 ORDER BY id DESC`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, wo, status string
			var qty, version int64
			if err := rows.Scan(&id, &no, &wo, &qty, &status, &version); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "cutting_no": no, "work_order_id": wo, "cut_qty": qty, "status": status, "version": version})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
	api.Handle("POST /api/v1/cutting-orders/{id}/confirm", "cutting:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return confirmCutting(api, w, r, p)
	})
	api.Handle("POST /api/v1/work-orders/{id}/release", "work_order:release", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return releaseWorkOrder(api, w, r, p)
	})
	for action, status := range map[string]string{"pause": "paused", "resume": "in_production", "complete": "completed", "cancel": "cancelled"} {
		action, status := action, status
		api.Handle("POST /api/v1/work-orders/{id}/"+action, "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
			var in struct {
				Version int64  `json:"version"`
				Reason  string `json:"reason"`
			}
			if err := httpapi.Decode(r, &in); err != nil {
				return err
			}
			result, err := api.DB.Exec(r.Context(), fmt.Sprintf(`UPDATE work_order SET status=$1,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6 AND version=$7 AND status NOT IN ('completed','cancelled')`), status, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), in.Version)
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return httpapi.Error(409, "INVALID_STATE", "工单状态或版本不允许此操作")
			}
			return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": status, "version": in.Version + 1})
		})
	}
	api.Handle("DELETE /api/v1/projects/{id}", "project:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var orderCount, shipmentCount int
		if err := api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM work_order WHERE tenant_id=$1 AND project_id=$2", p.TenantID, r.PathValue("id")).Scan(&orderCount); err != nil {
			return err
		}
		if err := api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM shipment WHERE tenant_id=$1 AND project_id=$2", p.TenantID, r.PathValue("id")).Scan(&shipmentCount); err != nil {
			return err
		}
		if orderCount > 0 || shipmentCount > 0 {
			return httpapi.Error(409, "PROJECT_IN_USE", "项目已流转到工单或出货环节，不能删除")
		}
		result, err := api.DB.Exec(r.Context(), "DELETE FROM project WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(404, "NOT_FOUND", "项目不存在")
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})
	api.Handle("DELETE /api/v1/work-orders/{id}", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var status string
		if err := tx.QueryRow(r.Context(), "SELECT status FROM work_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "工单不存在")
			}
			return err
		}
		if status != "pending_release" {
			return httpapi.Error(409, "WORK_ORDER_STARTED", "工单已下发到生产环节，不能删除，可改为取消")
		}
		var cuttingCount, taskCount int
		if err := tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM cutting_order WHERE tenant_id=$1 AND work_order_id=$2", p.TenantID, r.PathValue("id")).Scan(&cuttingCount); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM production_task WHERE tenant_id=$1 AND work_order_id=$2", p.TenantID, r.PathValue("id")).Scan(&taskCount); err != nil {
			return err
		}
		if cuttingCount > 0 || taskCount > 0 {
			return httpapi.Error(409, "WORK_ORDER_IN_USE", "工单已流转到裁床或任务环节，不能删除")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM work_order WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete", "work_order", r.PathValue("id"), nil, nil, "未下发的工单删除"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})
	api.Handle("DELETE /api/v1/cutting-orders/{id}", "cutting:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var status string
		if err := tx.QueryRow(r.Context(), "SELECT status FROM cutting_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "裁床单不存在")
			}
			return err
		}
		if status != "draft" {
			return httpapi.Error(409, "CUTTING_CONFIRMED", "裁床单已确认生效，不能删除")
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM cutting_detail WHERE tenant_id=$1 AND cutting_order_id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM cutting_order WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete", "cutting_order", r.PathValue("id"), nil, nil, "草稿裁床单删除"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})
}

func listWorkOrders(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	limit, cursor := httpapi.Page(r)
	args := []any{p.TenantID}
	where := "w.tenant_id=$1"
	if cursor != "" {
		args = append(args, cursor)
		where += fmt.Sprintf(" AND w.id>$%d", len(args))
	}
	if status := r.URL.Query().Get("status"); status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND w.status=$%d", len(args))
	}
	args = append(args, limit+1)
	rows, err := api.DB.Query(r.Context(), fmt.Sprintf(`SELECT w.id,w.work_order_code,p.name,w.planned_product_qty,w.status,w.priority,w.product_version,w.version,COALESCE(w.review_status,''),
		COALESCE((SELECT SUM(t.planned_qty) FROM production_task t WHERE t.tenant_id=w.tenant_id AND t.work_order_id=w.id AND t.parent_task_id IS NULL),0),
		COALESCE((SELECT MIN(t.approved_good_qty) FROM production_task t WHERE t.tenant_id=w.tenant_id AND t.work_order_id=w.id AND t.parent_task_id IS NOT NULL),0),
		COALESCE(p.image_attachment_id,(SELECT l.attachment_id FROM attachment_link l WHERE l.tenant_id=p.tenant_id AND l.owner_type='product' AND l.owner_id=p.id AND l.is_deleted=FALSE ORDER BY CASE WHEN l.usage_type='standard' THEN 0 ELSE 1 END,l.sort_no,l.id LIMIT 1)) AS image_attachment_id
		FROM work_order w JOIN product p ON p.tenant_id=w.tenant_id AND p.id=w.product_id WHERE %s ORDER BY w.id LIMIT $%d`, where, len(args)), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, code, product, status, priority, reviewStatus string
		var qty, productVersion, version, planned, finished int64
		var imageID *string
		if err := rows.Scan(&id, &code, &product, &qty, &status, &priority, &productVersion, &version, &reviewStatus, &planned, &finished, &imageID); err != nil {
			return err
		}
		ratio := 0.0
		if qty > 0 {
			ratio = float64(finished) / float64(qty)
		}
		items = append(items, map[string]any{"id": id, "work_order_code": code, "product_name": product, "planned_product_qty": qty, "status": status, "priority": priority, "product_version": productVersion, "version": version, "review_status": reviewStatus, "planned_qty": planned, "approved_good_qty": finished, "progress_ratio": ratio, "image_attachment_id": imageID})
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
}

type cuttingDetailInput struct {
	Color       string `json:"color"`
	Size        string `json:"size"`
	BundleCount int64  `json:"bundle_count"`
	PieceQty    int64  `json:"piece_qty"`
	SortNo      int    `json:"sort_no"`
}

func createCutting(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		CuttingNo    string               `json:"cutting_no"`
		WorkOrderID  string               `json:"work_order_id"`
		CuttingDate  int64                `json:"cutting_date"`
		BedNo        *string              `json:"bed_no"`
		MarkerNo     *string              `json:"marker_no"`
		MarkerID     *string              `json:"marker_id"`
		FabricLayers int64                `json:"fabric_layers"`
		CutQty       int64                `json:"cut_qty"`
		CutterID     *string              `json:"cutter_id"`
		ReviewerID   *string              `json:"reviewer_id"`
		Details      []cuttingDetailInput `json:"details"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.CuttingNo == "" || in.WorkOrderID == "" || in.CutQty <= 0 || len(in.Details) == 0 {
		return httpapi.Error(400, "VALIDATION_ERROR", "裁床单号、工单、裁片数和明细必填")
	}
	var sum int64
	for _, d := range in.Details {
		if d.BundleCount < 1 {
			d.BundleCount = 1
		}
		sum += d.PieceQty
	}
	if sum != in.CutQty {
		return httpapi.Error(400, "CUTTING_QUANTITY_MISMATCH", "裁片数与配码明细合计不一致")
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	id, now := platform.ID(), platform.Now()
	if in.MarkerID != nil && *in.MarkerID == "" {
		in.MarkerID = nil
	}
	if in.MarkerID != nil {
		var markerWorkOrder, markerStatus, markerNo string
		err = tx.QueryRow(r.Context(), "SELECT work_order_id,review_status,marker_no FROM marker WHERE tenant_id=$1 AND id=$2", p.TenantID, *in.MarkerID).Scan(&markerWorkOrder, &markerStatus, &markerNo)
		if err != nil {
			return httpapi.Error(400, "INVALID_MARKER", "唛架不存在")
		}
		if markerWorkOrder != in.WorkOrderID {
			return httpapi.Error(400, "MARKER_WORK_ORDER_MISMATCH", "唛架不属于所选工单")
		}
		if markerStatus != "approved" {
			return httpapi.Error(409, "MARKER_NOT_APPROVED", "唛架未审核通过，不能被裁床单引用")
		}
		in.MarkerNo = &markerNo
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO cutting_order(tenant_id,id,cutting_no,work_order_id,cutting_date,bed_no,marker_no,marker_id,fabric_layers,cut_qty,cutter_id,reviewer_id,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$13,$14,$15)`, p.TenantID, id, in.CuttingNo, in.WorkOrderID, in.CuttingDate, in.BedNo, in.MarkerNo, in.MarkerID, in.FabricLayers, in.CutQty, in.CutterID, in.ReviewerID, now, p.UserID, api.Config.NodeID)
	if err != nil {
		return httpapi.Error(409, "INVALID_CUTTING", "裁床编号重复或工单已有裁床单")
	}
	for i, d := range in.Details {
		if d.BundleCount < 1 {
			d.BundleCount = 1
		}
		if _, err = tx.Exec(r.Context(), `INSERT INTO cutting_detail(tenant_id,id,cutting_order_id,color,size,bundle_count,piece_qty,sort_no) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, p.TenantID, platform.ID(), id, d.Color, d.Size, d.BundleCount, d.PieceQty, i+1); err != nil {
			return err
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 201, map[string]any{"id": id, "version": 1, "status": "draft"})
}
func confirmCutting(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	var in struct {
		Version int64 `json:"version"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var status, workOrderID, bedNo string
	var cutQty int64
	err = tx.QueryRow(r.Context(), "SELECT status,work_order_id,COALESCE(bed_no,''),cut_qty FROM cutting_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &workOrderID, &bedNo, &cutQty)
	if err != nil {
		return httpapi.Error(404, "NOT_FOUND", "裁床单不存在")
	}
	if status != "draft" {
		return httpapi.Error(409, "INVALID_STATE", "裁床单已确认")
	}
	var hasMarker bool
	var markerStatus string
	if err = tx.QueryRow(r.Context(), "SELECT c.marker_id IS NOT NULL,COALESCE(m.review_status,'') FROM cutting_order c LEFT JOIN marker m ON m.tenant_id=c.tenant_id AND m.id=c.marker_id WHERE c.tenant_id=$1 AND c.id=$2", p.TenantID, r.PathValue("id")).Scan(&hasMarker, &markerStatus); err != nil {
		return err
	}
	if hasMarker && markerStatus != "approved" {
		return httpapi.Error(409, "MARKER_NOT_APPROVED", "唛架未审核通过，不能确认裁床")
	}
	var sum int64
	if err = tx.QueryRow(r.Context(), "SELECT COALESCE(SUM(piece_qty),0) FROM cutting_detail WHERE tenant_id=$1 AND cutting_order_id=$2", p.TenantID, r.PathValue("id")).Scan(&sum); err != nil {
		return err
	}
	if sum != cutQty {
		return httpapi.Error(400, "CUTTING_QUANTITY_MISMATCH", "裁床数量不守恒")
	}
	rows, err := tx.Query(r.Context(), "SELECT id,color,size,bundle_count,piece_qty FROM cutting_detail WHERE tenant_id=$1 AND cutting_order_id=$2 ORDER BY sort_no,id", p.TenantID, r.PathValue("id"))
	if err != nil {
		return err
	}
	type detail struct {
		id, color, size string
		count, qty      int64
	}
	details := []detail{}
	for rows.Next() {
		var d detail
		if err := rows.Scan(&d.id, &d.color, &d.size, &d.count, &d.qty); err != nil {
			rows.Close()
			return err
		}
		details = append(details, d)
	}
	rows.Close()
	parts, err := loadPartTemplate(r.Context(), tx, p.TenantID, workOrderID)
	if err != nil {
		return err
	}
	bundleNo := 1
	for _, d := range details {
		base := d.qty / d.count
		remainder := d.qty % d.count
		for i := int64(0); i < d.count; i++ {
			qty := base
			if i < remainder {
				qty++
			}
			if qty <= 0 {
				return httpapi.Error(400, "INVALID_BUNDLE", "扎件数必须大于零")
			}
			bundleID := platform.ID()
			if _, err = tx.Exec(r.Context(), `INSERT INTO bundle(tenant_id,id,work_order_id,cutting_detail_id,bundle_no,bed_no,color,size,original_qty,current_qty,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9,$10)`, p.TenantID, bundleID, workOrderID, d.id, fmt.Sprint(bundleNo), bedNo, d.color, d.size, qty, platform.Now()); err != nil {
				return err
			}
			if err = insertBundleParts(r.Context(), tx, p, api.Config.NodeID, bundleID, qty, parts); err != nil {
				return err
			}
			bundleNo++
		}
	}
	result, err := tx.Exec(r.Context(), "UPDATE cutting_order SET status='confirmed',version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5 AND version=$6", platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), in.Version)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return httpapi.Error(409, "VERSION_CONFLICT", "裁床单已变更")
	}
	if err = refreshWorkOrderProgress(r.Context(), tx, p.TenantID, workOrderID); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "confirmed", "bundle_count": bundleNo - 1, "version": in.Version + 1})
}
func releaseWorkOrder(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	if _, err := httpapi.IdempotencyKey(r); err != nil {
		return err
	}
	var in struct {
		Version int64  `json:"version"`
		Remark  string `json:"remark"`
	}
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var status, productID, reviewStatus string
	var productVersion, planned, currentVersion int64
	err = tx.QueryRow(r.Context(), "SELECT status,product_id,product_version,planned_product_qty,version,review_status FROM work_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &productID, &productVersion, &planned, &currentVersion, &reviewStatus)
	if err != nil {
		return httpapi.Error(404, "NOT_FOUND", "工单不存在")
	}
	if currentVersion != in.Version {
		return httpapi.Error(409, "VERSION_CONFLICT", "工单已变更")
	}
	if status != "pending_release" {
		return httpapi.Error(409, "WORK_ORDER_ALREADY_RELEASED", "当前工单不可下发")
	}
	if reviewStatus != "approved" {
		return httpapi.Error(409, "WORK_ORDER_NOT_APPROVED", "工单尚未审核通过，不能下发")
	}
	var cuttingStatus string
	if err = tx.QueryRow(r.Context(), "SELECT status FROM cutting_order WHERE tenant_id=$1 AND work_order_id=$2", p.TenantID, r.PathValue("id")).Scan(&cuttingStatus); err != nil || cuttingStatus != "confirmed" {
		return httpapi.Error(409, "CUTTING_NOT_CONFIRMED", "裁床尚未确认")
	}
	rows, err := tx.Query(r.Context(), `SELECT pp.id,pd.process_code,pd.name,pp.sequence_no,pp.unit_price::text,pp.report_ratio::text,pp.department_id,pp.standard_work_seconds,pp.is_parallel,pp.is_critical,pp.is_required,pp.defect_config FROM product_process pp JOIN process_definition pd ON pd.tenant_id=pp.tenant_id AND pd.id=pp.process_id WHERE pp.tenant_id=$1 AND pp.product_id=$2 AND pp.product_version=$3 ORDER BY pp.sequence_no`, p.TenantID, productID, productVersion)
	if err != nil {
		return err
	}
	defer rows.Close()
	type route struct {
		id, code, name, price, ratio string
		sequence                     int
		department                   *string
		seconds                      int64
		parallel, critical, required bool
		defects                      []byte
	}
	routes := []route{}
	for rows.Next() {
		var v route
		if err := rows.Scan(&v.id, &v.code, &v.name, &v.sequence, &v.price, &v.ratio, &v.department, &v.seconds, &v.parallel, &v.critical, &v.required, &v.defects); err != nil {
			return err
		}
		routes = append(routes, v)
	}
	if len(routes) == 0 {
		return httpapi.Error(409, "PRODUCT_PROCESS_EMPTY", "产品工序为空")
	}
	tasks := []map[string]any{}
	for _, v := range routes {
		ratio, _ := decimal.NewFromString(v.ratio)
		qty := ratio.Mul(decimal.NewFromInt(planned)).Round(0).IntPart()
		if qty <= 0 {
			return httpapi.Error(400, "INVALID_PLANNED_QTY", "工序计划量无效")
		}
		snapshotID, taskID := platform.ID(), platform.ID()
		flags := fmt.Sprintf(`{"parallel":%t,"critical":%t,"required":%t}`, v.parallel, v.critical, v.required)
		if _, err = tx.Exec(r.Context(), `INSERT INTO work_order_process_snapshot(tenant_id,id,work_order_id,product_process_id,sequence_no,process_code,process_name,planned_qty,unit_price,report_ratio,department_id,standard_work_seconds,route_flags,defect_config,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, p.TenantID, snapshotID, r.PathValue("id"), v.id, v.sequence, v.code, v.name, qty, v.price, v.ratio, v.department, v.seconds, flags, v.defects, platform.Now()); err != nil {
			return err
		}
		now := platform.Now()
		if _, err = tx.Exec(r.Context(), `INSERT INTO production_task(tenant_id,id,work_order_id,product_process_id,department_id,planned_qty,unit_price,report_ratio,standard_work_seconds,status,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'unassigned',$10,$11,$12,$10,$11,$12)`, p.TenantID, taskID, r.PathValue("id"), v.id, v.department, qty, v.price, v.ratio, v.seconds, now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		tasks = append(tasks, map[string]any{"task_id": taskID, "process_id": v.id, "planned_qty": qty, "unit_price": v.price, "report_ratio": v.ratio})
	}
	now := platform.Now()
	if _, err = tx.Exec(r.Context(), `UPDATE work_order SET status='in_production',version=version+1,released_by=$1,released_at=$2,actual_start_at=$2,updated_at=$2,updated_by=$1,updated_node=$3 WHERE tenant_id=$4 AND id=$5`, p.UserID, now, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
		return err
	}
	response := map[string]any{"work_order_id": r.PathValue("id"), "status": "in_production", "product_version": productVersion, "tasks": tasks, "sync_status": "pending"}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "release", "work_order", r.PathValue("id"), map[string]any{"status": status}, response, in.Remark); err != nil {
		return err
	}
	if err = httpapi.Outbox(r.Context(), tx, p.TenantID, api.Config.NodeID, "work_order", r.PathValue("id"), "work_order.released", response); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 200, response)
}
