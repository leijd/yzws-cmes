package outsource

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
)

/* registerPartners 外协厂商档案（设计 4.25.2）：
 * partner_tenant_id 为空 = 非系统租户的外部厂商；两种形态同一套档案与委外流程。 */
func registerPartners(api *httpapi.API) {
	api.Handle("GET /api/v1/outsource/partners", "outsource:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "pt.tenant_id=$1"
		if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
			args = append(args, status)
			where += fmt.Sprintf(" AND pt.status=$%d", len(args))
		}
		if kw := strings.TrimSpace(r.URL.Query().Get("keyword")); kw != "" {
			args = append(args, "%"+kw+"%")
			where += fmt.Sprintf(" AND (pt.code ILIKE $%d OR pt.name ILIKE $%d)", len(args), len(args))
		}
		rows, err := api.DB.Query(r.Context(), `SELECT pt.id,pt.code,pt.name,COALESCE(pt.partner_tenant_id,''),COALESCE(t.name,''),
			COALESCE(pt.contact_name,''),COALESCE(pt.contact_phone,''),COALESCE(pt.address,''),COALESCE(pt.qualification_note,''),
			pt.quality_rating,pt.status,COALESCE(pt.remark,''),pt.version,
			(SELECT COUNT(*) FROM outsourcing_order o WHERE o.tenant_id=pt.tenant_id AND o.partner_id=pt.id AND o.status NOT IN ('cancelled','settled'))
			FROM outsourcing_partner pt LEFT JOIN tenant t ON t.id=pt.partner_tenant_id
			WHERE `+where+` ORDER BY pt.created_at DESC LIMIT 200`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, partnerTenantID, partnerTenantName, contactName, contactPhone, address, qual, status, remark string
			var rating *float64
			var version, activeOrders int64
			if err := rows.Scan(&id, &code, &name, &partnerTenantID, &partnerTenantName, &contactName, &contactPhone, &address,
				&qual, &rating, &status, &remark, &version, &activeOrders); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "partner_tenant_id": partnerTenantID,
				"partner_tenant_name": partnerTenantName, "contact_name": contactName, "contact_phone": contactPhone,
				"address": address, "qualification_note": qual, "quality_rating": rating, "status": status,
				"remark": remark, "version": version, "active_orders": activeOrders})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/outsource/partners", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Code              string  `json:"code"`
			Name             string  `json:"name"`
			PartnerTenantCode string `json:"partner_tenant_code"`
			ContactName      string  `json:"contact_name"`
			ContactPhone     string  `json:"contact_phone"`
			Address          string  `json:"address"`
			Qualification    string  `json:"qualification_note"`
			Remark           string  `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			return httpapi.Error(http.StatusBadRequest, "NAME_REQUIRED", "外协厂商名称不能为空")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		/* 对方为本系统租户时按租户编码解析 */
		var partnerTenantID *string
		if code := strings.TrimSpace(in.PartnerTenantCode); code != "" {
			var tid, tstatus string
			err := tx.QueryRow(ctx, `SELECT id,status FROM tenant WHERE code=$1 OR short_code=$1`, code).Scan(&tid, &tstatus)
			if err != nil {
				return httpapi.Error(http.StatusBadRequest, "TENANT_NOT_FOUND", "未找到该系统租户，请核对租户编码")
			}
			if tstatus != "active" {
				return httpapi.Error(http.StatusBadRequest, "TENANT_NOT_ACTIVE", "该租户当前不可用")
			}
			if tid == p.TenantID {
				return httpapi.Error(http.StatusBadRequest, "TENANT_SELF", "不能将本厂登记为自己的外包方")
			}
			partnerTenantID = &tid
		}
		code := strings.TrimSpace(in.Code)
		if code == "" {
			var n int
			if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM outsourcing_partner WHERE tenant_id=$1`, p.TenantID).Scan(&n); err != nil {
				return err
			}
			code = fmt.Sprintf("WF-%05d", n+1)
		}
		id := platform.ID()
		now := platform.Now()
		_, err = tx.Exec(ctx, `INSERT INTO outsourcing_partner(tenant_id,id,code,name,partner_tenant_id,contact_name,contact_phone,address,
			qualification_note,status,remark,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),'active',NULLIF($10,''),$11,$12,$13,$11,$12,$13,1)`,
			p.TenantID, id, code, in.Name, partnerTenantID, in.ContactName, in.ContactPhone, in.Address, in.Qualification, in.Remark,
			now, p.UserID, api.Config.NodeID)
		if err != nil {
			if strings.Contains(err.Error(), "outsourcing_partner_tenant_id_code_key") || strings.Contains(err.Error(), "unique") {
				return httpapi.Error(http.StatusConflict, "DUPLICATE_PARTNER", "厂商编码重复或该系统租户已登记")
			}
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "create_outsource_partner", "outsourcing_partner", id, nil,
			map[string]any{"code": code, "name": in.Name}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "code": code})
	})

	api.Handle("PATCH /api/v1/outsource/partners/{id}", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name           string  `json:"name"`
			ContactName    string  `json:"contact_name"`
			ContactPhone   string  `json:"contact_phone"`
			Address        string  `json:"address"`
			Qualification  string  `json:"qualification_note"`
			Remark         string  `json:"remark"`
			Version        int64   `json:"version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Version == 0 {
			return httpapi.Error(http.StatusBadRequest, "VERSION_REQUIRED", "缺少版本号")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		now := platform.Now()
		result, err := tx.Exec(ctx, `UPDATE outsourcing_partner SET name=COALESCE(NULLIF($3,''),name), contact_name=NULLIF($4,''),
			contact_phone=NULLIF($5,''), address=NULLIF($6,''), qualification_note=NULLIF($7,''),
			remark=NULLIF($8,''), version=version+1, updated_at=$9, updated_by=$10, updated_node=$11
			WHERE tenant_id=$1 AND id=$2 AND version=$12`,
			p.TenantID, r.PathValue("id"), in.Name, in.ContactName, in.ContactPhone, in.Address, in.Qualification, in.Remark,
			now, p.UserID, api.Config.NodeID, in.Version)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "记录不存在或已变更，请刷新后重试")
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "update_outsource_partner", "outsourcing_partner", r.PathValue("id"), nil,
			map[string]any{"name": in.Name}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	api.Handle("POST /api/v1/outsource/partners/{id}/disable", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var activeOrders int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM outsourcing_order WHERE tenant_id=$1 AND partner_id=$2
			AND status NOT IN ('cancelled','settled','completed')`, p.TenantID, r.PathValue("id")).Scan(&activeOrders); err != nil {
			return err
		}
		if activeOrders > 0 {
			return httpapi.Error(http.StatusConflict, "PARTNER_HAS_ACTIVE_ORDERS", "该厂商存在进行中的委外单，不能停用")
		}
		now := platform.Now()
		result, err := tx.Exec(ctx, `UPDATE outsourcing_partner SET status='disabled', version=version+1, updated_at=$3, updated_by=$4, updated_node=$5
			WHERE tenant_id=$1 AND id=$2 AND status='active'`, p.TenantID, r.PathValue("id"), now, p.UserID, api.Config.NodeID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "记录不存在或已停用")
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "disable_outsource_partner", "outsourcing_partner", r.PathValue("id"), nil, nil, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

/* findActiveCollaboration 查本厂（委托方）与受托租户之间 active 的协作关系。 */
func findActiveCollaboration(ctx context.Context, q querier, consignorTenantID, contractorTenantID string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT id FROM tenant_collaboration
		WHERE consignor_tenant_id=$1 AND contractor_tenant_id=$2 AND status='active'`,
		consignorTenantID, contractorTenantID).Scan(&id)
	return id, err
}
