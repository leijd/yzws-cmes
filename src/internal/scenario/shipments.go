package scenario

import (
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
)

/* 发货（设计 6.12.2 / 6.12.7）：复用既有 shipment 表与 /shipments 接口，
 * 本接口补充场景徽标、外协标识与委托方/客商信息，供发货页三态视图直接展示。
 * A 场景为出货给客商；B/C 场景为回交委托方（标题带[外协]前缀）。 */

func registerShipments(api *httpapi.API) {
	api.Handle("GET /api/v1/scene/shipments", "shipment:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT s.id,s.shipment_no,s.project_id,s.status,s.version,s.shipment_date,
			COALESCE(pr.project_code,''),COALESCE(c.name,''),COALESCE(pr.scene_type,'A'),COALESCE(pr.scene_sub,'A1'),
			COALESCE(w.source_order_ref,''),COALESCE(SUM(l.qty),0)
			FROM shipment s
			LEFT JOIN project pr ON pr.tenant_id=s.tenant_id AND pr.id=s.project_id
			LEFT JOIN customer c ON c.tenant_id=s.tenant_id AND c.id=s.customer_id
			LEFT JOIN LATERAL (SELECT wo.source_order_ref FROM work_order wo WHERE wo.tenant_id=s.tenant_id AND wo.project_id=pr.id ORDER BY wo.id LIMIT 1) w ON TRUE
			LEFT JOIN shipment_line l ON l.tenant_id=s.tenant_id AND l.shipment_id=s.id
			WHERE s.tenant_id=$1 GROUP BY s.tenant_id,s.id,pr.project_code,c.name,pr.scene_type,pr.scene_sub,w.source_order_ref
			ORDER BY s.created_at DESC LIMIT 200`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, projectID, status, projectCode, customer, st, sub, sourceRef string
			var version, shipmentDate, qty int64
			if err := rows.Scan(&id, &no, &projectID, &status, &version, &shipmentDate, &projectCode, &customer,
				&st, &sub, &sourceRef, &qty); err != nil {
				return err
			}
			deliveryType := "出货"
			if isOutsourceScene(st) {
				deliveryType = "回交"
			}
			items = append(items, map[string]any{"id": id, "shipment_no": no, "project_id": projectID,
				"status": status, "version": version, "shipment_date": shipmentDate, "project_code": projectCode,
				"counterparty": customer, "scene_type": st, "scene_sub": sub, "source_order_ref": sourceRef,
				"qty": qty, "outsource": isOutsourceScene(st), "party": partyOf(st, "own"),
				"delivery_type": deliveryType, "title_prefix": map[bool]string{true: "[外协]", false: ""}[isOutsourceScene(st)]})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
}
