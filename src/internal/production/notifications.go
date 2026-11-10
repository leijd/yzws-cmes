package production

import (
	"net/http"
	"strconv"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
)

func registerNotifications(api *httpapi.API) {
	api.Handle("GET /api/v1/notifications", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		limit, cursor := httpapi.Page(r)
		args := []any{p.TenantID, p.UserID}
		where := "tenant_id=$1 AND user_id=$2"
		if kind := strings.TrimSpace(r.URL.Query().Get("type")); kind != "" {
			args = append(args, kind)
			where += " AND notification_type=$" + strconv.Itoa(len(args))
		}
		switch strings.TrimSpace(r.URL.Query().Get("read")) {
		case "true":
			where += " AND read_at IS NOT NULL"
		case "false":
			where += " AND read_at IS NULL"
		}
		if cursor != "" {
			args = append(args, cursor)
			where += " AND id<$" + strconv.Itoa(len(args))
		}
		args = append(args, limit+1)
		rows, err := api.DB.Query(r.Context(), `SELECT id,notification_type,owner_type,owner_id,title,body,COALESCE(read_at,0),created_at FROM notification WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT $`+strconv.Itoa(len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, kind, ownerType, ownerID, title, body string
			var readAt, createdAt int64
			if err := rows.Scan(&id, &kind, &ownerType, &ownerID, &title, &body, &readAt, &createdAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "type": kind, "owner_type": ownerType, "owner_id": ownerID, "title": title, "body": body, "read_at": readAt, "read": readAt > 0, "created_at": createdAt})
		}
		hasMore := len(items) > limit
		if hasMore {
			items = items[:limit]
		}
		var unread int64
		if err := api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM notification WHERE tenant_id=$1 AND user_id=$2 AND read_at IS NULL", p.TenantID, p.UserID).Scan(&unread); err != nil {
			return err
		}
		var next any
		if hasMore && len(items) > 0 {
			next = items[len(items)-1]["id"]
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items, "next_cursor": next, "has_more": hasMore, "unread_count": unread})
	})

	api.Handle("POST /api/v1/notifications/read", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			IDs       []string `json:"ids"`
			AllBefore *int64   `json:"all_before"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		now := platform.Now()
		updated := int64(0)
		if in.AllBefore != nil {
			result, err := api.DB.Exec(r.Context(), "UPDATE notification SET read_at=$1,updated_at=$1 WHERE tenant_id=$2 AND user_id=$3 AND read_at IS NULL AND created_at<=$4", now, p.TenantID, p.UserID, *in.AllBefore)
			if err != nil {
				return err
			}
			updated += result.RowsAffected()
		}
		for _, id := range in.IDs {
			result, err := api.DB.Exec(r.Context(), "UPDATE notification SET read_at=$1,updated_at=$1 WHERE tenant_id=$2 AND user_id=$3 AND id=$4 AND read_at IS NULL", now, p.TenantID, p.UserID, id)
			if err != nil {
				return err
			}
			updated += result.RowsAffected()
		}
		return httpapi.JSON(w, 200, map[string]any{"updated": updated})
	})

	api.Handle("GET /api/v1/sessions", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,node_id,device_info::text,created_at,expires_at,offline_expires_at,COALESCE(revoked_at,0),COALESCE(reason,'') FROM auth_session WHERE tenant_id=$1 AND user_id=$2 ORDER BY created_at DESC`, p.TenantID, p.UserID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, nodeID, deviceInfo, reason string
			var createdAt, expiresAt, offlineExpiresAt, revokedAt int64
			if err := rows.Scan(&id, &nodeID, &deviceInfo, &createdAt, &expiresAt, &offlineExpiresAt, &revokedAt, &reason); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "node_id": nodeID, "device_info": deviceInfo, "created_at": createdAt, "expires_at": expiresAt, "offline_expires_at": offlineExpiresAt, "revoked_at": revokedAt, "reason": reason, "current": id == p.SessionID})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("DELETE /api/v1/sessions/{id}", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		result, err := api.DB.Exec(r.Context(), "UPDATE auth_session SET revoked_at=$1,reason='self_revoke' WHERE tenant_id=$2 AND user_id=$3 AND id=$4 AND revoked_at IS NULL", platform.Now(), p.TenantID, p.UserID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(404, "NOT_FOUND", "会话不存在或已撤销")
		}
		return httpapi.JSON(w, 200, map[string]any{"revoked": true, "id": r.PathValue("id")})
	})
}
