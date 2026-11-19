package scenario

import (
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
)

/* 场景向导（设计 6.12.6）：第一步选场景，按 A/B/C 分步创建项目与受托/自有工单。 */

func registerIntake(api *httpapi.API) {
	/* 向导元数据：场景、子类型、各场景步骤、允许无记录字段 */
	api.Handle("GET /api/v1/intake/steps", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return httpapi.JSON(w, http.StatusOK, map[string]any{"scenes": intakeSteps()})
	})

	/* 按场景新建项目：A 全链 / B 受托单 / C 受托工序（无产品信息） */
	api.Handle("POST /api/v1/intake/projects", "intake:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			SceneType            string `json:"scene_type"`
			SceneSub             string `json:"scene_sub"`
			ProjectCode          string `json:"project_code"`
			CustomerID           string `json:"customer_id"`
			ProductID            string `json:"product_id"`
			OrderQty             int64  `json:"order_qty"`
			SourceOrderRef       string `json:"source_order_ref"`
			RequiredDeliveryDate string `json:"required_delivery_date"`
			Remark               string `json:"remark"`
			CreateWorkOrder      bool   `json:"create_work_order"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if !validScene(in.SceneType, in.SceneSub) {
			return httpapi.Error(http.StatusBadRequest, "SCENE_INVALID", "业务场景或子类型无效")
		}
		if strings.TrimSpace(in.ProjectCode) == "" || strings.TrimSpace(in.CustomerID) == "" || in.OrderQty <= 0 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "项目编号、客户/委托方与数量必填")
		}
		/* C 接外包工序允许无产品信息（6.12.7 允许无记录） */
		if in.SceneType != "C" && strings.TrimSpace(in.ProductID) == "" {
			return httpapi.Error(http.StatusBadRequest, "PRODUCT_REQUIRED", "该场景必须选择产品")
		}
		if isOutsourceScene(in.SceneType) && strings.TrimSpace(in.SourceOrderRef) == "" {
			return httpapi.Error(http.StatusBadRequest, "SOURCE_ORDER_REF_REQUIRED", "受托场景必须填写对方单号")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		now := platformNow()
		projectID, workOrderID := platformNewID(), ""
		productID := any(nil)
		if strings.TrimSpace(in.ProductID) != "" {
			productID = in.ProductID
		}
		/* 商务单价为 4.15 敏感列：本向导不采集、不写入，交由列默认值（0）处理；
		 * 后续由订单商务模块经 RowCrypto.ForSecure 维护，避免新增明文写入路径。 */
		if _, err := tx.Exec(ctx, `INSERT INTO project(tenant_id,id,project_code,customer_id,product_id,received_date,order_qty,
			required_delivery_date,status,scene_type,scene_sub,
			created_at,created_by,created_node,updated_at,updated_by,updated_node)
			VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),'confirmed',$9,$10,$6,$11,$12,$6,$11,$12)`,
			p.TenantID, projectID, in.ProjectCode, in.CustomerID, productID, now, in.OrderQty,
			in.RequiredDeliveryDate, in.SceneType, in.SceneSub, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if in.CreateWorkOrder {
			workOrderID = platformNewID()
			/* C 场景 product_id 可空、product_version 用 0 占位；工序快照仅受托工序由后续流程生成 */
			if _, err := tx.Exec(ctx, `INSERT INTO work_order(tenant_id,id,work_order_code,project_id,product_id,product_version,
				planned_product_qty,status,priority,completion_policy,scene_type,scene_sub,order_source,source_order_ref,
				created_at,created_by,created_node,updated_at,updated_by,updated_node)
				VALUES($1,$2,$3,$4,$5,$6,$7,'draft','normal','all_required',$8,$9,$10,NULLIF($11,''),$12,$13,$14,$12,$13,$14)`,
				p.TenantID, workOrderID, in.ProjectCode+"-1", projectID, productID, 0, in.OrderQty,
				in.SceneType, in.SceneSub, sceneOrderSource(in.SceneType), in.SourceOrderRef,
				now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "intake_create_project", "project", projectID, nil,
			map[string]any{"scene_type": in.SceneType, "scene_sub": in.SceneSub, "project_code": in.ProjectCode,
				"order_qty": in.OrderQty, "source_order_ref": in.SourceOrderRef}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{
			"id": projectID, "work_order_id": workOrderID, "scene_type": in.SceneType, "scene_sub": in.SceneSub,
			"order_source": sceneOrderSource(in.SceneType), "outsource": isOutsourceScene(in.SceneType),
			"allow_empty": allowEmptyFields(in.SceneSub),
		})
	})

	/* 向导下拉数据：客户/委托方、产品、工单 */
	api.Handle("GET /api/v1/intake/options", "intake:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		customers, products, workOrders := []map[string]any{}, []map[string]any{}, []map[string]any{}
		rows, err := api.DB.Query(r.Context(), `SELECT id,name,code FROM customer WHERE tenant_id=$1 AND status='active' ORDER BY name LIMIT 200`, p.TenantID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, name, code string
			if err := rows.Scan(&id, &name, &code); err != nil {
				rows.Close()
				return err
			}
			customers = append(customers, map[string]any{"id": id, "name": name, "code": code})
		}
		rows.Close()
		rows, err = api.DB.Query(r.Context(), `SELECT id,name,product_code FROM product WHERE tenant_id=$1 ORDER BY name LIMIT 200`, p.TenantID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, name, code string
			if err := rows.Scan(&id, &name, &code); err != nil {
				rows.Close()
				return err
			}
			products = append(products, map[string]any{"id": id, "name": name, "code": code})
		}
		rows.Close()
		rows, err = api.DB.Query(r.Context(), `SELECT id,work_order_code,scene_type,scene_sub FROM work_order WHERE tenant_id=$1 ORDER BY id DESC LIMIT 200`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, code, st, sub string
			if err := rows.Scan(&id, &code, &st, &sub); err != nil {
				return err
			}
			workOrders = append(workOrders, map[string]any{"id": id, "work_order_code": code, "scene_type": st, "scene_sub": sub})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"customers": customers, "products": products, "work_orders": workOrders,
		})
	})
}

func allowEmptyFields(sub string) []string {
	for _, rule := range outsourceDataRules() {
		if rule["scene_sub"] == sub {
			if v, ok := rule["allow_empty"].([]string); ok {
				return v
			}
		}
	}
	return []string{}
}

func intakeSteps() []map[string]any {
	return []map[string]any{
		{"scene_type": "A", "label": "接服贸客商的单", "steps": []string{
			"客商信息", "物料入库（分级数据必填，唛架选填）", "裁床单两级录入", "产品工序拓扑",
			"生成项目工单与资源分配（本厂+外包）", "发布并派工"}},
		{"scene_type": "B", "label": "接外包单", "steps": []string{
			"委托方与外包单号", "客供料代管入库", "裁床（对方已裁床则跳过并只读接收）",
			"工序确认", "生成受托工单与资源分配（含再外发）", "发布并派工"}},
		{"scene_type": "C", "label": "接外包工序", "steps": []string{
			"委托方与受托工序段", "半成品代管入库（接收上游扎号/菲票）", "跳过裁床（允许无记录）",
			"自动生成受托工序快照", "生成简化工单", "发布并派工"}},
	}
}
