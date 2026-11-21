package production

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"clothp/internal/auth"
	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// projectCreateMu 串行化新增订单项目：取号（双库校验）+ 插入处于同一临界区，
// 防止并发创建取到相同流水号造成项目编号重复。
var projectCreateMu sync.Mutex

// looksLikeUUID 判断文本是否为字典 ID（36 位 8-4-4-4-12 十六进制），
// 用于阻止把编号误当成客户/产品名称写入。
func looksLikeUUID(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

// readProjectMaxSeq 读取指定库内符合 QDyyyyMMdd-NNN 规则的项目编号最大流水。
// 历史遗留的非该格式编号（如纯微秒编号）不参与流水计算。
func readProjectMaxSeq(ctx context.Context, pool *pgxpool.Pool, tenantID string) (int64, error) {
	if pool == nil {
		return 0, nil
	}
	var maxSeq int64
	err := pool.QueryRow(ctx, `SELECT COALESCE(MAX(SUBSTRING(project_code FROM '-([0-9]+)$')::bigint),0)
		FROM project WHERE tenant_id=$1 AND project_code ~ '^QD[0-9]{8}-[0-9]+$'`, tenantID).Scan(&maxSeq)
	return maxSeq, err
}

// nextProjectCode 双库校验取最大号 +1：分别读取明文库与密文库的项目编号流水，
// 取两库最大值加一生成新编号，保证编号在两库范围内都不重复。
// 双库流水不一致属于原则性数据分叉：记录 CRITICAL 日志明确暴露，绝不静默绕过。
func nextProjectCode(ctx context.Context, api *httpapi.API, tenantID string) (string, error) {
	var plainPool, securePool *pgxpool.Pool
	if api.Store != nil {
		plainPool, securePool = api.Store.Plain, api.Store.Secure
	}
	if plainPool == nil && securePool == nil {
		plainPool = api.DB
	}
	plainSeq, err := readProjectMaxSeq(ctx, plainPool, tenantID)
	if err != nil {
		return "", fmt.Errorf("读取明文库项目最大号失败: %w", err)
	}
	secureSeq, err := readProjectMaxSeq(ctx, securePool, tenantID)
	if err != nil {
		return "", fmt.Errorf("读取密文库项目最大号失败: %w", err)
	}
	if plainSeq != secureSeq {
		log.Printf("CRITICAL: 项目编号双库数据不一致（原则性问题，需立即对账）: tenant=%s plain_max_seq=%d secure_max_seq=%d", tenantID, plainSeq, secureSeq)
	}
	maxSeq := plainSeq
	if secureSeq > maxSeq {
		maxSeq = secureSeq
	}
	return fmt.Sprintf("QD%s-%03d", time.Now().Format("20060102"), maxSeq+1), nil
}

func registerOrders(api *httpapi.API) {
	// 按名称解析客户/产品：存在则直接返回，不存在则自动创建，供订单项目手填使用。
	api.Handle("POST /api/v1/projects/resolve-customer", "project:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name string `json:"name"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "客户名称不能为空")
		}
		var id string
		if err := api.DB.QueryRow(r.Context(), `SELECT id FROM customer WHERE tenant_id=$1 AND name=$2 LIMIT 1`, p.TenantID, name).Scan(&id); err == nil {
			return httpapi.JSON(w, 200, map[string]any{"id": id, "created": false})
		} else if err != pgx.ErrNoRows {
			return err
		}
		id, now := platform.ID(), platform.Now()
		code := "CUS" + fmt.Sprintf("%d", now)
		_, err := api.DB.Exec(r.Context(), `INSERT INTO customer(tenant_id,id,code,name,status,created_at,created_by,created_node,updated_at,updated_by,updated_node)
			VALUES($1,$2,$3,$4,'active',$5,$6,$7,$5,$6,$7)`, p.TenantID, id, code, name, now, p.UserID, api.Config.NodeID)
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "created": true})
	})

	api.Handle("POST /api/v1/projects/resolve-product", "project:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name       string `json:"name"`
			CustomerID string `json:"customer_id"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "产品名称不能为空")
		}
		var id string
		if err := api.DB.QueryRow(r.Context(), `SELECT id FROM product WHERE tenant_id=$1 AND name=$2 LIMIT 1`, p.TenantID, name).Scan(&id); err == nil {
			return httpapi.JSON(w, 200, map[string]any{"id": id, "created": false})
		} else if err != pgx.ErrNoRows {
			return err
		}
		id, now := platform.ID(), platform.Now()
		code := "PRD" + fmt.Sprintf("%d", now)
		_, err := api.DB.Exec(r.Context(), `INSERT INTO product(tenant_id,id,product_code,name,unit,status,created_at,created_by,created_node,updated_at,updated_by,updated_node)
			VALUES($1,$2,$3,$4,'件','draft',$5,$6,$7,$5,$6,$7)`, p.TenantID, id, code, name, now, p.UserID, api.Config.NodeID)
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "created": true})
	})

	api.Handle("POST /api/v1/projects", "project:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ProjectCode          string  `json:"project_code"`
			CustomerID           string  `json:"customer_id"`
			ProductID            string  `json:"product_id"`
			CustomerName         string  `json:"customer_name"`
			ProductName          string  `json:"product_name"`
			StyleNo              string  `json:"style_no"`
			StyleName            string  `json:"style_name"`
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
		if in.OrderQty <= 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "订单数必须大于 0")
		}
		// 客户/产品按自由文本落库，必须是名称，不接受把字典 ID 当名称写入
		if looksLikeUUID(in.CustomerName) {
			return httpapi.Error(400, "VALIDATION_ERROR", "客户应填写名称，不能是编号")
		}
		if looksLikeUUID(in.ProductName) {
			return httpapi.Error(400, "VALIDATION_ERROR", "产品应填写名称，不能是编号")
		}
		if in.CustomerUnitPrice == "" {
			in.CustomerUnitPrice = "0"
		}
		if in.ExternalUnitPrice == "" {
			in.ExternalUnitPrice = "0"
		}
		// 新增项目前锁住取号 + 写入，串行化避免并发取到相同流水号。
		projectCreateMu.Lock()
		defer projectCreateMu.Unlock()

		id, now := platform.ID(), platform.Now()
		if in.ProjectCode == "" {
			code, err := nextProjectCode(r.Context(), api, p.TenantID)
			if err != nil {
				return httpapi.Error(500, "PROJECT_CODE_ERROR", "生成项目编号失败: "+err.Error())
			}
			in.ProjectCode = code
		}
		insertSQL := `INSERT INTO project(tenant_id,id,project_code,customer_id,product_id,customer_name,product_name,style_no,style_name,received_date,order_qty,customer_unit_price,external_unit_price,material_owner,owner_employee_id,qc_employee_id,required_delivery_date,status,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,'confirmed',$18,$19,$20,$18,$19,$20)`
		args := []any{p.TenantID, id, in.ProjectCode, nullableText(in.CustomerID), nullableText(in.ProductID),
			nullableText(in.CustomerName), nullableText(in.ProductName), nullableText(in.StyleNo), nullableText(in.StyleName),
			in.ReceivedDate, in.OrderQty, in.CustomerUnitPrice, in.ExternalUnitPrice, in.MaterialOwner,
			in.OwnerEmployeeID, in.QCEmployeeID, in.RequiredDeliveryDate, now, p.UserID, api.Config.NodeID}
		response := map[string]any{"id": id, "version": 1, "status": "confirmed"}

		/* 单库降级：Writer/双库未配置时保持旧的单库直写行为 */
		if api.Writer == nil || api.Store == nil || api.Store.Plain == nil || api.Store.Secure == nil {
			if _, err := api.DB.Exec(r.Context(), insertSQL, args...); err != nil {
				log.Printf("create project insert failed: code=%s customer=%s product=%s err=%v", in.ProjectCode, in.CustomerName, in.ProductName, err)
				return httpapi.Error(409, "INVALID_PROJECT", "项目编号重复或数据校验失败: "+err.Error())
			}
			return httpapi.JSON(w, 201, response)
		}

		/* 双写 2PC：project 无敏感列，两库写同一 SQL + 同一 args。
		 * 唯一冲突直接映射 409；PREPARE 后不可再执行 SQL，因此不在 Commit 前换号重试。 */
		tx, err := api.Writer.Begin(r.Context(), p.TenantID)
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		tx.MarkAggregate("project", id)
		if err := dualwrite.ExecBoth(r.Context(), tx, insertSQL, args...); err != nil {
			log.Printf("create project dual insert failed: code=%s customer=%s product=%s err=%v", in.ProjectCode, in.CustomerName, in.ProductName, err)
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return httpapi.Error(409, "INVALID_PROJECT", "项目编号重复或数据校验失败: "+err.Error())
			}
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, response)
	})
	api.Handle("GET /api/v1/projects", "project:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT p.id,p.project_code,
			COALESCE(NULLIF(p.customer_name,''),NULLIF(c.name,''),''),
			COALESCE(NULLIF(p.product_name,''),NULLIF(p2.name,''),''),
			COALESCE(p.style_no,''),COALESCE(p.style_name,''),p.order_qty,p.customer_unit_price::text,p.external_unit_price::text,p.required_delivery_date,p.status,p.version,COALESCE((SELECT SUM(sl.qty) FROM shipment_line sl JOIN shipment s ON s.tenant_id=sl.tenant_id AND s.id=sl.shipment_id WHERE sl.tenant_id=p.tenant_id AND s.project_id=p.id AND s.status='confirmed'),0)
			- COALESCE((SELECT SUM(sr.qty) FROM shipment_return sr WHERE sr.tenant_id=p.tenant_id AND sr.project_id=p.id AND sr.status='posted'),0),
			COALESCE(p2.image_attachment_id,(SELECT l.attachment_id FROM attachment_link l WHERE l.tenant_id=p2.tenant_id AND l.owner_type='product' AND l.owner_id=p2.id AND l.is_deleted=FALSE ORDER BY CASE WHEN l.usage_type='standard' THEN 0 ELSE 1 END,l.sort_no,l.id LIMIT 1)) AS image_attachment_id,
			p.product_id
			FROM project p LEFT JOIN customer c ON c.tenant_id=p.tenant_id AND c.id=p.customer_id
			LEFT JOIN product p2 ON p2.tenant_id=p.tenant_id AND p2.id=p.product_id WHERE p.tenant_id=$1 ORDER BY p.id DESC`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, customer, product, styleNo, styleName, price, external, status string
			var qty, version, shipped int64
			var delivery *string
			var imageID, productID *string
			if err := rows.Scan(&id, &code, &customer, &product, &styleNo, &styleName, &qty, &price, &external, &delivery, &status, &version, &shipped, &imageID, &productID); err != nil {
				return err
			}
			unshipped := qty - shipped
			if unshipped < 0 {
				unshipped = 0
			}
			items = append(items, map[string]any{"id": id, "project_code": code, "customer_name": customer, "product_name": product, "product_id": productID, "style_no": styleNo, "style_name": styleName, "order_qty": qty, "shipped_qty": shipped, "customer_unit_price": price, "external_unit_price": external, "required_delivery_date": delivery, "status": status, "version": version, "unshipped_qty": unshipped, "image_attachment_id": imageID})
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
			CustomerName         *string `json:"customer_name"`
			ProductName          *string `json:"product_name"`
			StyleNo              *string `json:"style_no"`
			StyleName            *string `json:"style_name"`
			Version              int64   `json:"version"`
			Reason               string  `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		// 客户/产品必须是名称，不接受字典 ID
		if in.CustomerName != nil && looksLikeUUID(*in.CustomerName) {
			return httpapi.Error(400, "VALIDATION_ERROR", "客户应填写名称，不能是编号")
		}
		if in.ProductName != nil && looksLikeUUID(*in.ProductName) {
			return httpapi.Error(400, "VALIDATION_ERROR", "产品应填写名称，不能是编号")
		}
		if in.Reason == "" {
			in.Reason = "编辑订单项目"
		}
		projectID := r.PathValue("id")
		checkSQL := `SELECT p.order_qty,
			COALESCE((SELECT SUM(sl.qty) FROM shipment_line sl JOIN shipment s ON s.tenant_id=sl.tenant_id AND s.id=sl.shipment_id WHERE sl.tenant_id=p.tenant_id AND s.project_id=p.id AND s.status='confirmed'),0)
			- COALESCE((SELECT SUM(sr.qty) FROM shipment_return sr WHERE sr.tenant_id=p.tenant_id AND sr.project_id=p.id AND sr.status='posted'),0),
			COALESCE((SELECT SUM(w.planned_product_qty) FROM work_order w WHERE w.tenant_id=p.tenant_id AND w.project_id=p.id AND w.status<>'cancelled'),0),
			p.version FROM project p WHERE p.tenant_id=$1 AND p.id=$2`
		updateSQL := `UPDATE project SET order_qty=$1,required_delivery_date=COALESCE($2,required_delivery_date),customer_unit_price=COALESCE($3,customer_unit_price),external_unit_price=COALESCE($4,external_unit_price),customer_name=COALESCE($5,customer_name),product_name=COALESCE($6,product_name),style_no=COALESCE($7,style_no),style_name=COALESCE($8,style_name),version=version+1,updated_at=$9,updated_by=$10,updated_node=$11 WHERE tenant_id=$12 AND id=$13 AND version=$14`
		now := platform.Now()
		dual := api.Writer != nil && api.Store != nil && api.Store.Plain != nil && api.Store.Secure != nil

		// 读取校验行（version/订单数/已出货/已排产）统一从读池获取，双写时加行锁由 writeMode 单侧事务负责
		var currentQty, shipped, planned, version int64
		if err := api.DB.QueryRow(r.Context(), checkSQL, p.TenantID, projectID).Scan(&currentQty, &shipped, &planned, &version); err != nil {
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
		args := []any{newQty, in.RequiredDeliveryDate, in.CustomerUnitPrice, in.ExternalUnitPrice,
			in.CustomerName, in.ProductName, in.StyleNo, in.StyleName, now, p.UserID, api.Config.NodeID, p.TenantID, projectID, in.Version}
		response := map[string]any{"id": projectID, "order_qty": newQty, "version": version + 1}

		if !dual {
			/* 单库降级 */
			tx, err := api.DB.Begin(r.Context())
			if err != nil {
				return err
			}
			defer tx.Rollback(r.Context())
			res, err := tx.Exec(r.Context(), updateSQL, args...)
			if err != nil {
				return err
			}
			if res.RowsAffected() == 0 {
				return httpapi.Error(409, "VERSION_CONFLICT", "项目已变更，请刷新后重试")
			}
			if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "update", "project", projectID, map[string]any{"order_qty": currentQty}, response, in.Reason); err != nil {
				return err
			}
			if err := tx.Commit(r.Context()); err != nil {
				return err
			}
			return httpapi.JSON(w, 200, response)
		}

		/* 双写 2PC：project 无敏感列，两库执行同一条 UPDATE（带 version CAS） */
		tx, err := api.Writer.Begin(r.Context(), p.TenantID)
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		tx.MarkAggregate("project", projectID)
		if err := dualwrite.ExecBoth(r.Context(), tx, updateSQL, args...); err != nil {
			return err
		}
		// 审计只写审计侧一次（audit_log 不参与双库补偿）
		auditTx := tx.Secure()
		if auditTx == nil {
			auditTx = tx.Plain()
		}
		if err := httpapi.Audit(r.Context(), auditTx, p, api.Config.NodeID, "update", "project", projectID, map[string]any{"order_qty": currentQty}, response, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
	})
	api.Handle("POST /api/v1/projects/{id}/confirm", "project:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version int64  `json:"version"`
			Reason  string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Reason == "" {
			in.Reason = "项目订单转正式"
		}
		projectID := r.PathValue("id")
		now := platform.Now()
		dual := api.Writer != nil && api.Store != nil && api.Store.Plain != nil && api.Store.Secure != nil
		updateSQL := `UPDATE project SET status='confirmed',version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5 AND version=$6 AND status='draft'`
		args := []any{now, p.UserID, api.Config.NodeID, p.TenantID, projectID, in.Version}
		response := map[string]any{"id": projectID, "status": "confirmed", "version": in.Version + 1}

		if !dual {
			tx, err := api.DB.Begin(r.Context())
			if err != nil {
				return err
			}
			defer tx.Rollback(r.Context())
			res, err := tx.Exec(r.Context(), updateSQL, args...)
			if err != nil {
				return err
			}
			if res.RowsAffected() == 0 {
				return httpapi.Error(409, "INVALID_STATE", "项目已变更或已转正式，请刷新后重试")
			}
			if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "confirm_project", "project", projectID, map[string]any{"status": "draft"}, response, in.Reason); err != nil {
				return err
			}
			if err := tx.Commit(r.Context()); err != nil {
				return err
			}
			return httpapi.JSON(w, 200, response)
		}
		tx, err := api.Writer.Begin(r.Context(), p.TenantID)
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		tx.MarkAggregate("project", projectID)
		if err := dualwrite.ExecBoth(r.Context(), tx, updateSQL, args...); err != nil {
			return err
		}
		auditTx := tx.Secure()
		if auditTx == nil {
			auditTx = tx.Plain()
		}
		if err := httpapi.Audit(r.Context(), auditTx, p, api.Config.NodeID, "confirm_project", "project", projectID, map[string]any{"status": "draft"}, response, in.Reason); err != nil {
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
		projectID := r.PathValue("id")
		// 只要后续任何环节已有数据就不允许删除：逐表按 (tenant_id, project_id) 检查
		downstream := []struct{ table, label string }{
			{"material_receipt", "物料入库"},
			{"cutting_sheet", "裁床"},
			{"cutting_bed", "床次"},
			{"marker", "唛架"},
			{"work_order", "生产工单"},
			{"material_issue", "生产领料"},
			{"process_inspection", "工序巡检"},
			{"work_report", "工序汇报"},
			{"style_progress_projection", "进度投影"},
			{"shipment", "出货"},
			{"shipment_return", "出货退回"},
			{"outsourcing_order", "委外单"},
		}
		blockers := []string{}
		for _, d := range downstream {
			var exists bool
			if err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`, d.table).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				continue
			}
			var n int
			if err := api.DB.QueryRow(r.Context(), fmt.Sprintf(`SELECT COUNT(*) FROM "%s" WHERE tenant_id=$1 AND project_id=$2`, d.table), p.TenantID, projectID).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				blockers = append(blockers, d.label)
			}
		}
		if len(blockers) > 0 {
			appErr := httpapi.Error(409, "PROJECT_IN_USE", "项目已产生后续业务数据（"+strings.Join(blockers, "、")+"），不能删除。请先撤销相关单据后再试。")
			appErr.Details = map[string]any{"blocking_stages": blockers}
			return appErr
		}

		deleteSQL := `DELETE FROM project WHERE tenant_id=$1 AND id=$2`
		args := []any{p.TenantID, projectID}
		dual := api.Writer != nil && api.Store != nil && api.Store.Plain != nil && api.Store.Secure != nil

		if !dual {
			tx, err := api.DB.Begin(r.Context())
			if err != nil {
				return err
			}
			defer tx.Rollback(r.Context())
			res, err := tx.Exec(r.Context(), deleteSQL, args...)
			if err != nil {
				return err
			}
			if res.RowsAffected() == 0 {
				return httpapi.Error(404, "NOT_FOUND", "项目不存在")
			}
			if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete", "project", projectID, map[string]any{"id": projectID}, nil, "删除项目"); err != nil {
				return err
			}
			if err := tx.Commit(r.Context()); err != nil {
				return err
			}
			return httpapi.JSON(w, 200, map[string]any{"deleted": true})
		}

		tx, err := api.Writer.Begin(r.Context(), p.TenantID)
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		tx.MarkAggregate("project", projectID)
		if err := dualwrite.ExecBoth(r.Context(), tx, deleteSQL, args...); err != nil {
			return err
		}
		auditTx := tx.Secure()
		if auditTx == nil {
			auditTx = tx.Plain()
		}
		if err := httpapi.Audit(r.Context(), auditTx, p, api.Config.NodeID, "delete", "project", projectID, map[string]any{"id": projectID}, nil, "删除项目"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})
	/* 编辑工单：计划数受项目订单总量约束（排除本工单自身），车间/产线联动校验；已结束工单不可改 */
	api.Handle("PATCH /api/v1/work-orders/{id}", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version           int64   `json:"version"`
			PlannedProductQty int64   `json:"planned_product_qty"`
			Priority          string  `json:"priority"`
			WorkshopOrgUnitID *string `json:"workshop_org_unit_id"`
			LineOrgUnitID     *string `json:"line_org_unit_id"`
			PlannedStartAt    *int64  `json:"planned_start_at"`
			PlannedEndAt      *int64  `json:"planned_end_at"`
			Reason            string  `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.PlannedProductQty <= 0 {
			return httpapi.Error(400, "VALIDATION_ERROR", "计划产品数必须大于零")
		}
		if in.Priority != "normal" && in.Priority != "urgent" {
			return httpapi.Error(400, "VALIDATION_ERROR", "紧急程度只能是 normal 或 urgent")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var status, projectID string
		var version int64
		if err = tx.QueryRow(r.Context(), "SELECT status,project_id,version FROM work_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, r.PathValue("id")).Scan(&status, &projectID, &version); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "工单不存在")
			}
			return err
		}
		if in.Version != version {
			return httpapi.Error(409, "VERSION_CONFLICT", "工单已变更，请刷新后重试")
		}
		if status == "completed" || status == "cancelled" {
			return httpapi.Error(409, "INVALID_STATE", "已完成或已取消的工单不能编辑")
		}
		var orderQty, planned int64
		if err = tx.QueryRow(r.Context(), `SELECT p.order_qty,COALESCE((SELECT SUM(w.planned_product_qty) FROM work_order w WHERE w.tenant_id=p.tenant_id AND w.project_id=p.id AND w.status<>'cancelled' AND w.id<>$3),0) FROM project p WHERE p.tenant_id=$1 AND p.id=$2`, p.TenantID, projectID, r.PathValue("id")).Scan(&orderQty, &planned); err != nil {
			return err
		}
		if planned+in.PlannedProductQty > orderQty {
			appErr := httpapi.Error(409, "QUANTITY_EXCEEDED", "同项目工单数量合计超过项目订单数")
			appErr.Details = map[string]any{"order_qty": orderQty, "planned": planned, "requested": in.PlannedProductQty, "remaining": orderQty - planned}
			return appErr
		}
		if err = validateWorkshopLine(r.Context(), tx, p.TenantID, in.WorkshopOrgUnitID, in.LineOrgUnitID); err != nil {
			return err
		}
		if _, err = tx.Exec(r.Context(), `UPDATE work_order SET planned_product_qty=$1,priority=$2,workshop_org_unit_id=$3,line_org_unit_id=$4,planned_start_at=$5,planned_end_at=$6,version=version+1,updated_at=$7,updated_by=$8,updated_node=$9 WHERE tenant_id=$10 AND id=$11`, in.PlannedProductQty, in.Priority, in.WorkshopOrgUnitID, in.LineOrgUnitID, in.PlannedStartAt, in.PlannedEndAt, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		response := map[string]any{"id": r.PathValue("id"), "version": version + 1}
		if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "update", "work_order", r.PathValue("id"), nil, response, in.Reason); err != nil {
			return err
		}
		if err = tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
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
	rows, err := api.DB.Query(r.Context(), fmt.Sprintf(`SELECT w.id,w.work_order_code,COALESCE(p.name,'—'),w.planned_product_qty,w.status,w.priority,w.product_version,w.version,COALESCE(w.review_status,''),
		w.scene_type,w.scene_sub,w.order_source,COALESCE(w.source_order_ref,''),COALESCE(pj.project_code,''),COALESCE(pj.id,''),
		COALESCE((SELECT SUM(t.planned_qty) FROM production_task t WHERE t.tenant_id=w.tenant_id AND t.work_order_id=w.id AND t.parent_task_id IS NULL),0),
		/* 工单完成件数统一使用「已审核成品入库数」口径，与项目/产品进度一致。
		 * 旧实现按生产报工统计，未派工/报工但已入库的工单会被算成零。 */
		COALESCE((SELECT SUM(rl.qty) FROM finished_receipt rc JOIN finished_receipt_line rl ON rl.tenant_id=rc.tenant_id AND rl.receipt_id=rc.id WHERE rc.tenant_id=w.tenant_id AND rc.work_order_id=w.id AND rc.status='approved'),0),
		COALESCE(p.image_attachment_id,(SELECT l.attachment_id FROM attachment_link l WHERE l.tenant_id=p.tenant_id AND l.owner_type='product' AND l.owner_id=p.id AND l.is_deleted=FALSE ORDER BY CASE WHEN l.usage_type='standard' THEN 0 ELSE 1 END,l.sort_no,l.id LIMIT 1)) AS image_attachment_id,
		COALESCE((SELECT o.name FROM org_unit o WHERE o.tenant_id=w.tenant_id AND o.id=w.line_org_unit_id),'') AS line_name,
		(SELECT COUNT(*) FROM work_order_group g WHERE g.tenant_id=w.tenant_id AND g.work_order_id=w.id),
		(SELECT COUNT(*) FROM work_order_cutting_sheet s WHERE s.tenant_id=w.tenant_id AND s.work_order_id=w.id)
		FROM work_order w LEFT JOIN product p ON p.tenant_id=w.tenant_id AND p.id=w.product_id
		LEFT JOIN project pj ON pj.tenant_id=w.tenant_id AND pj.id=w.project_id WHERE %s ORDER BY w.id LIMIT $%d`, where, len(args)), args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, code, product, status, priority, reviewStatus, sceneType, sceneSub, orderSource, sourceRef, projectCode, projectID string
		var qty, productVersion, version, planned, finished, groupCount, sheetCount int64
		var imageID *string
		var lineName string
		if err := rows.Scan(&id, &code, &product, &qty, &status, &priority, &productVersion, &version, &reviewStatus,
			&sceneType, &sceneSub, &orderSource, &sourceRef, &projectCode, &projectID, &planned, &finished, &imageID, &lineName, &groupCount, &sheetCount); err != nil {
			return err
		}
		ratio := 0.0
		if qty > 0 {
			ratio = float64(finished) / float64(qty)
		}
		items = append(items, map[string]any{"id": id, "work_order_code": code, "product_name": product, "planned_product_qty": qty, "status": status, "priority": priority, "product_version": productVersion, "version": version, "review_status": reviewStatus, "planned_qty": planned, "approved_good_qty": finished, "progress_ratio": ratio, "image_attachment_id": imageID,
			"scene_type": sceneType, "scene_sub": sceneSub, "order_source": orderSource, "source_order_ref": sourceRef,
			"project_code": projectCode, "project_id": projectID, "group_count": groupCount,
			"line_name": lineName, "sheet_count": sheetCount})
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
		prevIDs                      []string
	}
	routes := []route{}
	for rows.Next() {
		var v route
		if err := rows.Scan(&v.id, &v.code, &v.name, &v.sequence, &v.price, &v.ratio, &v.department, &v.seconds, &v.parallel, &v.critical, &v.required, &v.defects); err != nil {
			return err
		}
		v.prevIDs = []string{}
		routes = append(routes, v)
	}
	if len(routes) == 0 {
		return httpapi.Error(409, "PRODUCT_PROCESS_EMPTY", "产品工序为空")
	}
	/* 读取工序 DAG 边，构建每个 product_process.id 的直接前驱列表，随工单快照固化 */
	edgeRows, err := tx.Query(r.Context(), `SELECT from_id,to_id FROM product_process_edge WHERE tenant_id=$1 AND product_id=$2 AND product_version=$3`, p.TenantID, productID, productVersion)
	if err != nil {
		return err
	}
	prevByID := map[string][]string{}
	for edgeRows.Next() {
		var from, to string
		if err := edgeRows.Scan(&from, &to); err != nil {
			edgeRows.Close()
			return err
		}
		prevByID[to] = append(prevByID[to], from)
	}
	edgeRows.Close()
	for i := range routes {
		if p := prevByID[routes[i].id]; len(p) > 0 {
			routes[i].prevIDs = p
		}
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
		prevJSON, _ := json.Marshal(v.prevIDs)
		if _, err = tx.Exec(r.Context(), `INSERT INTO work_order_process_snapshot(tenant_id,id,work_order_id,product_process_id,sequence_no,process_code,process_name,planned_qty,unit_price,report_ratio,department_id,standard_work_seconds,route_flags,defect_config,prev_ids,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, p.TenantID, snapshotID, r.PathValue("id"), v.id, v.sequence, v.code, v.name, qty, v.price, v.ratio, v.department, v.seconds, flags, v.defects, string(prevJSON), platform.Now()); err != nil {
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
