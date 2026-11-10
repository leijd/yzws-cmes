package production

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

type partTemplateItem struct {
	PartCode string `json:"part_code"`
	PartName string `json:"part_name"`
	SortNo   int    `json:"sort_no"`
}

func registerBundleParts(api *httpapi.API) {
	api.Handle("GET /api/v1/bundles/{id}/parts", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var exists bool
		if err := api.DB.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM bundle WHERE tenant_id=$1 AND id=$2)", p.TenantID, r.PathValue("id")).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return httpapi.Error(404, "NOT_FOUND", "扎不存在")
		}
		rows, err := api.DB.Query(r.Context(), `SELECT bp.id,bp.part_code,bp.part_name,bp.sort_no,bp.qty,bp.status,t.id,COALESCE(t.public_code,''),COALESCE(t.print_status,'unprinted'),COALESCE(t.print_count,0),COALESCE(t.print_revision,1) FROM bundle_part bp LEFT JOIN ticket t ON t.tenant_id=bp.tenant_id AND t.bundle_part_id=bp.id WHERE bp.tenant_id=$1 AND bp.bundle_id=$2 ORDER BY bp.sort_no`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, status, publicCode, printStatus string
			var sortNo int
			var qty, printCount, revision int64
			var ticketID *string
			if err := rows.Scan(&id, &code, &name, &sortNo, &qty, &status, &ticketID, &publicCode, &printStatus, &printCount, &revision); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "part_code": code, "part_name": name, "sort_no": sortNo, "qty": qty, "status": status, "ticket_id": ticketID, "public_code": publicCode, "print_status": printStatus, "print_count": printCount, "print_revision": revision})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})
}

func loadPartTemplate(ctx context.Context, tx pgx.Tx, tenantID, workOrderID string) ([]partTemplateItem, error) {
	var raw string
	if err := tx.QueryRow(ctx, `SELECT pr.part_template::text FROM work_order w JOIN product pr ON pr.tenant_id=w.tenant_id AND pr.id=w.product_id WHERE w.tenant_id=$1 AND w.id=$2`, tenantID, workOrderID).Scan(&raw); err != nil {
		return nil, err
	}
	var items []partTemplateItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, httpapi.Error(400, "INVALID_PART_TEMPLATE", "款式部位模板格式不正确")
	}
	if len(items) == 0 {
		return []partTemplateItem{{PartCode: "whole", PartName: "整件", SortNo: 1}}, nil
	}
	codes := map[string]bool{}
	sortNos := map[int]bool{}
	for i := range items {
		items[i].PartCode = strings.TrimSpace(items[i].PartCode)
		items[i].PartName = strings.TrimSpace(items[i].PartName)
		if items[i].SortNo == 0 {
			items[i].SortNo = i + 1
		}
		if items[i].PartCode == "" || items[i].PartName == "" || items[i].SortNo < 0 || codes[items[i].PartCode] || sortNos[items[i].SortNo] {
			return nil, httpapi.Error(400, "INVALID_PART_TEMPLATE", "款式部位模板的编码、名称和顺序必须有效且不重复")
		}
		codes[items[i].PartCode] = true
		sortNos[items[i].SortNo] = true
	}
	sort.Slice(items, func(a, b int) bool { return items[a].SortNo < items[b].SortNo })
	return items, nil
}

func insertBundleParts(ctx context.Context, tx pgx.Tx, p auth.Principal, node, bundleID string, qty int64, parts []partTemplateItem) error {
	now := platform.Now()
	for _, part := range parts {
		partID := platform.ID()
		if _, err := tx.Exec(ctx, `INSERT INTO bundle_part(tenant_id,id,bundle_id,part_code,part_name,sort_no,qty,status,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,'active',$8,$9,$10,$8,$9,$10)`, p.TenantID, partID, bundleID, part.PartCode, part.PartName, part.SortNo, qty, now, p.UserID, node); err != nil {
			return err
		}
		publicCode := platform.Token(6)
		if _, err := tx.Exec(ctx, `INSERT INTO ticket(tenant_id,id,bundle_id,bundle_part_id,part_code,part_name,public_code,signature_key_id,payload_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,'local-key',$8,$9)`, p.TenantID, platform.ID(), bundleID, partID, part.PartCode, part.PartName, publicCode, platform.Hash([]byte(partID+publicCode)), now); err != nil {
			return err
		}
	}
	return nil
}
