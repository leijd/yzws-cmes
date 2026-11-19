package outsource

import (
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* registerCollaborations 跨租户协作授权（设计 4.25.3）：
 * 任一方发起 pending → 对方确认 active；active⇄suspended；terminated 不可逆。
 * 终止/暂停即切断跨租户投影可见性，历史数据保留。 */
func registerCollaborations(api *httpapi.API) {
	api.Handle("GET /api/v1/outsource/collaborations", "outsource:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT c.id,c.consignor_tenant_id,ct.name,c.contractor_tenant_id,rt.name,
			c.status,c.scope_note,c.proposed_tenant_id,COALESCE(c.confirmed_at,0),COALESCE(c.effective_at,0),
			COALESCE(c.terminated_reason,''),c.version,c.created_at
			FROM tenant_collaboration c
			JOIN tenant ct ON ct.id=c.consignor_tenant_id JOIN tenant rt ON rt.id=c.contractor_tenant_id
			WHERE c.consignor_tenant_id=$1 OR c.contractor_tenant_id=$1
			ORDER BY c.created_at DESC LIMIT 100`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, consignorID, consignorName, contractorID, contractorName, status, scopeNote, proposedBy, termReason string
			var confirmedAt, effectiveAt, version, createdAt int64
			if err := rows.Scan(&id, &consignorID, &consignorName, &contractorID, &contractorName, &status, &scopeNote,
				&proposedBy, &confirmedAt, &effectiveAt, &termReason, &version, &createdAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "consignor_tenant_id": consignorID, "consignor_name": consignorName,
				"contractor_tenant_id": contractorID, "contractor_name": contractorName, "status": status,
				"scope_note": scopeNote, "proposed_by_me": proposedBy == p.TenantID, "confirmed_at": confirmedAt,
				"effective_at": effectiveAt, "terminated_reason": termReason, "version": version, "created_at": createdAt,
				"my_role": map[bool]string{true: "consignor", false: "contractor"}[consignorID == p.TenantID]})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/outsource/collaborations", "outsource:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			PeerTenantCode string `json:"peer_tenant_code"`
			MyRole         string `json:"my_role"` // consignor 我委托 / contractor 我受托
			ScopeNote      string `json:"scope_note"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.MyRole != "consignor" && in.MyRole != "contractor" {
			return httpapi.Error(http.StatusBadRequest, "ROLE_INVALID", "my_role 仅支持 consignor/contractor")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var peerID, peerStatus string
		if err := tx.QueryRow(ctx, `SELECT id,status FROM tenant WHERE code=$1 OR short_code=$1`,
			strings.TrimSpace(in.PeerTenantCode)).Scan(&peerID, &peerStatus); err != nil {
			return httpapi.Error(http.StatusBadRequest, "TENANT_NOT_FOUND", "未找到该系统租户，请核对租户编码")
		}
		if peerStatus != "active" {
			return httpapi.Error(http.StatusBadRequest, "TENANT_NOT_ACTIVE", "对方租户当前不可用")
		}
		if peerID == p.TenantID {
			return httpapi.Error(http.StatusBadRequest, "TENANT_SELF", "不能与本厂建立协作")
		}
		consignorID, contractorID := p.TenantID, peerID
		if in.MyRole == "contractor" {
			consignorID, contractorID = peerID, p.TenantID
		}
		id := platform.ID()
		now := platform.Now()
		if _, err := tx.Exec(ctx, `INSERT INTO tenant_collaboration(id,consignor_tenant_id,contractor_tenant_id,status,
			proposed_tenant_id,proposed_by,proposed_at,scope_note,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,'pending',$4,$5,$6,NULLIF($7,''),$6,$5,$8,$6,$5,$8,1)`,
			id, consignorID, contractorID, p.TenantID, p.UserID, now, in.ScopeNote, api.Config.NodeID); err != nil {
			if strings.Contains(err.Error(), "uq_collab_pair_active") {
				return httpapi.Error(http.StatusConflict, "DUPLICATE_COLLABORATION", "双方已存在进行中的协作关系")
			}
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "create_collaboration", "tenant_collaboration", id, nil,
			map[string]any{"consignor": consignorID, "contractor": contractorID}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id})
	})

	/* 状态流转统一入口：confirm/suspend/terminate */
	transition := func(action string) func(http.ResponseWriter, *http.Request, auth.Principal) error {
		return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
			var in struct {
				Reason string `json:"reason"`
			}
			if err := httpapi.Decode(r, &in); err != nil && action == "terminate" {
				return err
			}
			ctx := r.Context()
			tx, err := api.DB.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			var consignorID, contractorID, proposedBy, status string
			err = tx.QueryRow(ctx, `SELECT consignor_tenant_id,contractor_tenant_id,proposed_tenant_id,status
				FROM tenant_collaboration WHERE id=$1 FOR UPDATE`, r.PathValue("id")).
				Scan(&consignorID, &contractorID, &proposedBy, &status)
			if err == pgx.ErrNoRows {
				return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "协作关系不存在")
			}
			if err != nil {
				return err
			}
			/* 非协作方统一 404，不泄露他厂协作关系 */
			if consignorID != p.TenantID && contractorID != p.TenantID {
				api.RecordDenied(ctx, p, "tenant_collaboration", r.PathValue("id"), "非协作方尝试操作协作关系")
				return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "协作关系不存在")
			}
			now := platform.Now()
			var resultErr error
			switch action {
			case "confirm":
				if proposedBy == p.TenantID {
					return httpapi.Error(http.StatusForbidden, "CONFIRM_SELF", "协作须由对方确认，发起方不能自行确认")
				}
				if status != "pending" {
					return httpapi.Error(http.StatusConflict, "COLLAB_NOT_ACTIVE", "协作关系当前状态不允许确认")
				}
				_, resultErr = tx.Exec(ctx, `UPDATE tenant_collaboration SET status='active',confirmed_by=$2,confirmed_at=$3,effective_at=$3,
					updated_at=$3,updated_by=$2,updated_node=$4,version=version+1 WHERE id=$1`, r.PathValue("id"), p.UserID, now, api.Config.NodeID)
			case "suspend":
				if status != "active" {
					return httpapi.Error(http.StatusConflict, "COLLAB_NOT_ACTIVE", "仅生效中的协作可暂停")
				}
				_, resultErr = tx.Exec(ctx, `UPDATE tenant_collaboration SET status='suspended',updated_at=$2,updated_by=$3,updated_node=$4,version=version+1
					WHERE id=$1`, r.PathValue("id"), now, p.UserID, api.Config.NodeID)
			case "terminate":
				if strings.TrimSpace(in.Reason) == "" {
					return httpapi.Error(http.StatusBadRequest, "REASON_REQUIRED", "终止协作必须填写原因")
				}
				if status == "terminated" {
					return httpapi.Error(http.StatusConflict, "COLLAB_NOT_ACTIVE", "协作已终止")
				}
				_, resultErr = tx.Exec(ctx, `UPDATE tenant_collaboration SET status='terminated',terminated_reason=$2,updated_at=$3,updated_by=$4,updated_node=$5,version=version+1
					WHERE id=$1`, r.PathValue("id"), in.Reason, now, p.UserID, api.Config.NodeID)
			}
			if resultErr != nil {
				return resultErr
			}
			if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, action+"_collaboration", "tenant_collaboration", r.PathValue("id"),
				map[string]any{"status": status}, map[string]any{"action": action}, in.Reason); err != nil {
				return err
			}
			if err := tx.Commit(ctx); err != nil {
				return err
			}
			return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
		}
	}
	api.Handle("POST /api/v1/outsource/collaborations/{id}/confirm", "outsource:write", transition("confirm"))
	api.Handle("POST /api/v1/outsource/collaborations/{id}/suspend", "outsource:write", transition("suspend"))
	api.Handle("POST /api/v1/outsource/collaborations/{id}/terminate", "outsource:write", transition("terminate"))
}
