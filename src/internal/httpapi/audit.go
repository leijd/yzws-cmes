package httpapi

import (
	"context"
	"encoding/json"
	"log"

	"clothp/internal/auth"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* RecordDenied 越权尝试审计（设计 4.9）：统一写 audit_log(action='access_denied')。
 * 审计本身失败不阻断业务响应，只记录服务日志，避免审计不可用导致正常请求失败。 */
func (a *API) RecordDenied(ctx context.Context, p auth.Principal, ownerType, ownerID, reason string) {
	if p.TenantID == "" {
		return
	}
	_, err := a.DB.Exec(ctx, `INSERT INTO audit_log(tenant_id,id,request_id,actor_type,actor_id,action,owner_type,owner_id,reason,created_node,created_at)
		VALUES($1,$2,$3,'user',$4,'access_denied',$5,$6,$7,$8,$9)`,
		p.TenantID, "deny-"+platform.ID(), RequestID(ctx), p.UserID, ownerType, ownerID, reason, a.Config.NodeID, platform.Now())
	if err != nil {
		log.Printf("audit access_denied failed: tenant=%s owner=%s/%s err=%v", p.TenantID, ownerType, ownerID, err)
	}
}

func Audit(ctx context.Context, tx pgx.Tx, p auth.Principal, nodeID, action, ownerType, ownerID string, before, after any, reason string) error {
	var beforeJSON, afterJSON []byte
	if before != nil {
		beforeJSON, _ = json.Marshal(before)
	}
	if after != nil {
		afterJSON, _ = json.Marshal(after)
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_log(tenant_id,id,request_id,actor_type,actor_id,action,owner_type,owner_id,
		before_values,after_values,reason,created_node,created_at) VALUES($1,$2,$3,'user',$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		p.TenantID, platform.ID(), RequestID(ctx), p.UserID, action, ownerType, ownerID, beforeJSON, afterJSON, reason, nodeID, platform.Now())
	return err
}

func Outbox(ctx context.Context, tx pgx.Tx, tenantID, nodeID, aggregateType, aggregateID, eventType string, payload any) error {
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(origin_seq),0)+1 FROM outbox_event WHERE tenant_id=$1 AND origin_node_id=$2`, tenantID, nodeID).Scan(&sequence); err != nil {
		return err
	}
	body, _ := json.Marshal(payload)
	_, err := tx.Exec(ctx, `INSERT INTO outbox_event(tenant_id,event_id,origin_node_id,origin_seq,aggregate_type,aggregate_id,
		event_type,occurred_at,payload,payload_sha256,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		tenantID, platform.ID(), nodeID, sequence, aggregateType, aggregateID, eventType, platform.Now(), string(body), platform.Hash(body), platform.Now())
	return err
}
