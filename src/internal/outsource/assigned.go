package outsource

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* registerAssigned 受托侧（设计 4.25.9）：本厂作为受托方。
 * 委托方为系统租户：经 active 协作看到对方委外单白名单字段，接受后生成本厂受托工单；
 * 委托方为非租户：/outsource/incoming 手工登记，复用 customer + project + work_order。 */
func registerAssigned(api *httpapi.API) {
	api.Handle("GET /api/v1/outsource/assigned", "outsource:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		/* 白名单字段：不含单价/成本/对方人员（设计 7.4） */
		rows, err := api.DB.Query(r.Context(), `SELECT o.id,o.order_no,t.name,o.scope_type,o.total_qty,
			COALESCE(o.agreed_due_date,''),o.status,COALESCE(o.issued_at,0)
			FROM outsourcing_order o
			JOIN tenant_collaboration c ON c.id=o.collaboration_id AND c.contractor_tenant_id=$1 AND c.status='active'
			JOIN tenant t ON t.id=o.tenant_id
			WHERE o.status IN ('issued','in_transit','processing','partially_returned')
			ORDER BY o.issued_at DESC LIMIT 100`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, orderNo, consignorName, scopeType, status, dueDate string
			var totalQty float64
			var issuedAt int64
			if err := rows.Scan(&id, &orderNo, &consignorName, &scopeType, &totalQty, &dueDate, &status, &issuedAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "order_no": orderNo, "consignor_name": consignorName,
				"scope_type": scopeType, "total_qty": totalQty, "agreed_due_date": dueDate, "status": status, "issued_at": issuedAt})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/outsource/assigned/{id}/accept", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ProductID string  `json:"product_id"`
			Qty       float64 `json:"qty"`
			ScopeNote string  `json:"scope_note"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		/* 校验：委外单存在且经 active 协作指向本厂 */
		var orderNo, consignorTenantID, collabID string
		var totalQty float64
		err = tx.QueryRow(ctx, `SELECT o.order_no,o.tenant_id,o.collaboration_id,o.total_qty
			FROM outsourcing_order o JOIN tenant_collaboration c ON c.id=o.collaboration_id
			WHERE o.id=$1 AND c.contractor_tenant_id=$2 AND c.status='active' AND o.status='issued'`,
			r.PathValue("id"), p.TenantID).Scan(&orderNo, &consignorTenantID, &collabID, &totalQty)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "委外单不存在、未生效或状态不允许接受")
		}
		if err != nil {
			return err
		}
		qty := in.Qty
		if qty <= 0 {
			qty = totalQty
		}
		workOrderID, err := createContractedWorkOrder(ctx, tx, api, p, consignorTenantID, orderNo, collabID, in.ProductID, qty, in.ScopeNote)
		if err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "accept_outsource_assigned", "work_order", workOrderID, nil,
			map[string]any{"source_order_ref": orderNo, "consignor": consignorTenantID}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"work_order_id": workOrderID})
	})

	api.Handle("POST /api/v1/outsource/assigned/{id}/decline", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "decline_outsource_assigned", "outsourcing_order", r.PathValue("id"), nil,
			map[string]any{"reason": in.Reason}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	/* 非租户委托方：受托订单登记（设计 11.4/11.5） */
	api.Handle("GET /api/v1/outsource/incoming", "outsource:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT w.id,w.work_order_code,COALESCE(w.source_order_ref,''),c.name,pr.name,
			w.planned_product_qty,w.status,w.created_at,
			(SELECT COALESCE(SUM(t.approved_good_qty),0) FROM production_task t WHERE t.tenant_id=w.tenant_id AND t.work_order_id=w.id AND t.status<>'cancelled')
			FROM work_order w
			JOIN project pj ON pj.tenant_id=w.tenant_id AND pj.id=w.project_id
			JOIN customer c ON c.tenant_id=pj.tenant_id AND c.id=pj.customer_id
			JOIN product pr ON pr.tenant_id=w.tenant_id AND pr.id=w.product_id
			WHERE w.tenant_id=$1 AND w.order_source='contracted' ORDER BY w.created_at DESC LIMIT 200`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, sourceRef, customerName, productName, status string
			var qty, approved float64
			var createdAt int64
			if err := rows.Scan(&id, &code, &sourceRef, &customerName, &productName, &qty, &status, &createdAt, &approved); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "work_order_code": code, "source_order_ref": sourceRef,
				"customer_name": customerName, "product_name": productName, "planned_qty": qty, "approved_qty": approved,
				"status": status, "created_at": createdAt})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/outsource/incoming", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			CustomerID     string  `json:"customer_id"`
			ProductID      string  `json:"product_id"`
			Qty            float64 `json:"qty"`
			SourceOrderRef string  `json:"source_order_ref"`
			ScopeNote      string  `json:"scope_note"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.CustomerID == "" || in.ProductID == "" || in.Qty <= 0 {
			return httpapi.Error(http.StatusBadRequest, "INPUT_INVALID", "客户、产品与数量为必填项")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		workOrderID, err := createContractedWorkOrderForCustomer(ctx, tx, api, p, in.CustomerID, in.ProductID, in.Qty,
			strings.TrimSpace(in.SourceOrderRef), in.ScopeNote)
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"work_order_id": workOrderID})
	})

	/* 对非租户委托方导出受托工单进度（线下回报用，设计 11.4 第 5 步） */
	api.Handle("GET /api/v1/outsource/incoming/{id}/progress-export", "outsource:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		var code, sourceRef, productName, customerName, status string
		var planned float64
		err := api.DB.QueryRow(ctx, `SELECT w.work_order_code,COALESCE(w.source_order_ref,''),pr.name,c.name,w.status,w.planned_product_qty
			FROM work_order w JOIN project pj ON pj.tenant_id=w.tenant_id AND pj.id=w.project_id
			JOIN product pr ON pr.tenant_id=w.tenant_id AND pr.id=w.product_id
			JOIN customer c ON c.tenant_id=pj.tenant_id AND c.id=pj.customer_id
			WHERE w.tenant_id=$1 AND w.id=$2 AND w.order_source='contracted'`, p.TenantID, r.PathValue("id")).
			Scan(&code, &sourceRef, &productName, &customerName, &status, &planned)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "受托工单不存在")
		}
		if err != nil {
			return err
		}
		var reported, approved float64
		if err := api.DB.QueryRow(ctx, `SELECT COALESCE(SUM(reported_qty),0),COALESCE(SUM(approved_good_qty),0)
			FROM production_task WHERE tenant_id=$1 AND work_order_id=$2 AND status<>'cancelled'`,
			p.TenantID, r.PathValue("id")).Scan(&reported, &approved); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"work_order_code": code, "source_order_ref": sourceRef,
			"product_name": productName, "customer_name": customerName, "status": status, "planned_qty": planned,
			"reported_qty": reported, "approved_qty": approved, "exported_at": platform.Now()})
	})
}

/* createContractedWorkOrder 接受系统租户委外单后生成本厂受托工单：
 * 自动补建"协作客户"档案 → 受托项目 → work_order(contracted)。 */
func createContractedWorkOrder(ctx context.Context, tx pgx.Tx, api *httpapi.API, p auth.Principal,
	consignorTenantID, sourceOrderRef, collabID, productID string, qty float64, scopeNote string) (string, error) {
	customerID, err := ensureCollabCustomer(ctx, tx, api, p, consignorTenantID)
	if err != nil {
		return "", err
	}
	return createContractedWorkOrderForCustomer(ctx, tx, api, p, customerID, productID, qty, sourceOrderRef, scopeNote, collabID)
}

/* ensureCollabCustomer 为系统租户委托方在本厂建立/复用客户档案（remark 标记来源租户）。 */
func ensureCollabCustomer(ctx context.Context, tx pgx.Tx, api *httpapi.API, p auth.Principal, consignorTenantID string) (string, error) {
	marker := "协作客户:" + consignorTenantID
	var id string
	err := tx.QueryRow(ctx, `SELECT id FROM customer WHERE tenant_id=$1 AND remark=$2`, p.TenantID, marker).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	var tenantName string
	if err := tx.QueryRow(ctx, `SELECT name FROM tenant WHERE id=$1`, consignorTenantID).Scan(&tenantName); err != nil {
		return "", err
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM customer WHERE tenant_id=$1 AND code LIKE 'XZ%'`, p.TenantID).Scan(&n); err != nil {
		return "", err
	}
	id = platform.ID()
	now := platform.Now()
	code := fmt.Sprintf("XZ%05d", n+1)
	if _, err := tx.Exec(ctx, `INSERT INTO customer(tenant_id,id,code,name,remark,status,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
		VALUES($1,$2,$3,$4,$5,'active',$6,$7,$8,$6,$7,$8,1)`,
		p.TenantID, id, code, tenantName+"(协作)", marker, now, p.UserID, api.Config.NodeID); err != nil {
		return "", err
	}
	return id, nil
}

func createContractedWorkOrderForCustomer(ctx context.Context, tx pgx.Tx, api *httpapi.API, p auth.Principal,
	customerID, productID string, qty float64, sourceOrderRef, scopeNote string, collabID ...string) (string, error) {
	var productVersion int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(version,1) FROM product WHERE tenant_id=$1 AND id=$2`,
		p.TenantID, productID).Scan(&productVersion); err != nil {
		return "", httpapi.Error(http.StatusBadRequest, "PRODUCT_NOT_FOUND", "产品不存在")
	}
	now := platform.Now()
	projectCode, err := nextSeqNo(ctx, tx, p.TenantID, "project", "project_code", "ST")
	if err != nil {
		return "", err
	}
	projectID := platform.ID()
	if _, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,project_code,customer_id,product_id,received_date,order_qty,
		status,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
		VALUES($1,$2,$3,$4,$5,$6,$7,'draft',$6,$8,$9,$6,$8,$9,1)`,
		p.TenantID, projectID, projectCode, customerID, productID, now, qty, p.UserID, api.Config.NodeID); err != nil {
		return "", err
	}
	workOrderCode, err := nextSeqNo(ctx, tx, p.TenantID, "work_order", "work_order_code", "SG")
	if err != nil {
		return "", err
	}
	workOrderID := platform.ID()
	var collab any
	if len(collabID) > 0 && collabID[0] != "" {
		collab = collabID[0]
	}
	if _, err := tx.Exec(ctx, `INSERT INTO work_order(tenant_id,id,work_order_code,project_id,product_id,product_version,
		planned_product_qty,status,order_source,source_collaboration_id,source_order_ref,special_requirement,
		created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
		VALUES($1,$2,$3,$4,$5,$6,$7,'draft','contracted',$8,NULLIF($9,''),NULLIF($10,''),$11,$12,$13,$11,$12,$13,1)`,
		p.TenantID, workOrderID, workOrderCode, projectID, productID, productVersion, qty,
		collab, sourceOrderRef, scopeNote, now, p.UserID, api.Config.NodeID); err != nil {
		return "", err
	}
	if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "create_contracted_work_order", "work_order", workOrderID, nil,
		map[string]any{"work_order_code": workOrderCode, "source_order_ref": sourceOrderRef, "qty": qty}, ""); err != nil {
		return "", err
	}
	return workOrderID, nil
}
