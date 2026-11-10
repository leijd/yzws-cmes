package production

import (
	"net/http"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"github.com/shopspring/decimal"
)

// registerInventory 提供物料库存汇总查询。库存流水只追加，汇总按物料+仓库实时计算。
func registerInventory(api *httpapi.API) {
	api.Handle("GET /api/v1/inventory/summary", "masterdata:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		inventoryType := r.URL.Query().Get("inventory_type")
		if inventoryType == "" {
			inventoryType = "material"
		}
		if inventoryType != "material" {
			return httpapi.Error(http.StatusBadRequest, "UNSUPPORTED_INVENTORY_TYPE", "当前只支持物料库存汇总")
		}
		rows, err := api.DB.Query(r.Context(), `SELECT il.material_id,m.code,m.name,COALESCE(m.specification,''),m.unit,m.safety_stock::text,
			il.warehouse_id,w.code,w.name,SUM(il.qty_delta)::text
			FROM inventory_ledger il
			JOIN material m ON m.tenant_id=il.tenant_id AND m.id=il.material_id
			JOIN warehouse w ON w.tenant_id=il.tenant_id AND w.id=il.warehouse_id
			WHERE il.tenant_id=$1 AND il.inventory_type='material'
			GROUP BY il.material_id,m.code,m.name,m.specification,m.unit,m.safety_stock,il.warehouse_id,w.code,w.name
			ORDER BY m.code,w.code`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		alertCount := 0
		for rows.Next() {
			var materialID, materialCode, materialName, specification, unit, safetyText, warehouseID, warehouseCode, warehouseName, qty string
			if err := rows.Scan(&materialID, &materialCode, &materialName, &specification, &unit, &safetyText, &warehouseID, &warehouseCode, &warehouseName, &qty); err != nil {
				return err
			}
			stock, _ := decimal.NewFromString(qty)
			safety, _ := decimal.NewFromString(safetyText)
			alertStatus := "normal"
			shortage := "0"
			if stock.LessThanOrEqual(decimal.Zero) {
				alertStatus = "out_of_stock"
				shortage = safety.Sub(stock).StringFixed(6)
				alertCount++
			} else if safety.GreaterThan(decimal.Zero) && stock.LessThanOrEqual(safety) {
				alertStatus = "low"
				shortage = safety.Sub(stock).StringFixed(6)
				alertCount++
			}
			items = append(items, map[string]any{
				"material_id": materialID, "material_code": materialCode, "material_name": materialName,
				"specification": specification, "unit": unit, "safety_stock": safetyText,
				"warehouse_id": warehouseID, "warehouse_code": warehouseCode, "warehouse_name": warehouseName,
				"qty": qty, "shortage": shortage, "alert_status": alertStatus,
			})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "alert_count": alertCount})
	})
}
