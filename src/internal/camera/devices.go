package camera

import (
	"net/http"
	"strconv"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* registerDevices 摄像设备管理（设计 4.26.3）：建档、绑定车间/产线、禁拍区域、停用。
 * stream_ref 永不回显（含接入凭据引用，设计 7.5）。 */
func registerDevices(api *httpapi.API) {
	api.Handle("GET /api/v1/cameras", "video:view", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT c.id,c.code,c.name,c.protocol,COALESCE(c.channel_no,''),
			COALESCE(c.bind_org_id,''),COALESCE(o.name,''),COALESCE(c.location_desc,''),c.no_recording_zone,
			c.retention_days,c.status,COALESCE(c.last_heartbeat_at,0),COALESCE(c.remark,''),c.version,
			(SELECT COUNT(*) FROM evidence_clip e WHERE e.tenant_id=c.tenant_id AND e.camera_id=c.id)
			FROM camera_device c LEFT JOIN org_unit o ON o.tenant_id=c.tenant_id AND o.id=c.bind_org_id
			WHERE c.tenant_id=$1 ORDER BY c.created_at DESC LIMIT 200`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, protocol, channelNo, bindOrgID, bindOrgName, location, status, remark string
			var noRecZone bool
			var retentionDays int
			var heartbeat, version, evidenceCount int64
			if err := rows.Scan(&id, &code, &name, &protocol, &channelNo, &bindOrgID, &bindOrgName, &location,
				&noRecZone, &retentionDays, &status, &heartbeat, &remark, &version, &evidenceCount); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "protocol": protocol, "channel_no": channelNo,
				"bind_org_id": bindOrgID, "bind_org_name": bindOrgName, "location_desc": location, "no_recording_zone": noRecZone,
				"retention_days": retentionDays, "status": status, "last_heartbeat_at": heartbeat, "remark": remark,
				"version": version, "evidence_count": evidenceCount})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/cameras", "video:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Code           string `json:"code"`
			Name           string `json:"name"`
			Protocol       string `json:"protocol"`
			ChannelNo      string `json:"channel_no"`
			StreamRef      string `json:"stream_ref"`
			BindOrgID      string `json:"bind_org_id"`
			LocationDesc   string `json:"location_desc"`
			NoRecordingZone bool  `json:"no_recording_zone"`
			RetentionDays  int    `json:"retention_days"`
			Remark         string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.Code) == "" || strings.TrimSpace(in.Name) == "" {
			return httpapi.Error(http.StatusBadRequest, "INPUT_INVALID", "设备编码与名称为必填项")
		}
		if in.Protocol == "" {
			in.Protocol = "rtsp"
		}
		if in.RetentionDays <= 0 {
			in.RetentionDays = 30
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if in.BindOrgID != "" {
			var exists int
			if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM org_unit WHERE tenant_id=$1 AND id=$2 AND status='active'`,
				p.TenantID, in.BindOrgID).Scan(&exists); err != nil || exists == 0 {
				return httpapi.Error(http.StatusBadRequest, "ORG_NOT_FOUND", "绑定车间/产线不存在")
			}
		}
		id := platform.ID()
		now := platform.Now()
		if _, err := tx.Exec(ctx, `INSERT INTO camera_device(tenant_id,id,code,name,protocol,channel_no,stream_ref,bind_org_id,location_desc,
			no_recording_zone,retention_days,status,remark,created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),$10,$11,'active',NULLIF($12,''),$13,$14,$15,$13,$14,$15,1)`,
			p.TenantID, id, in.Code, in.Name, in.Protocol, in.ChannelNo, in.StreamRef, in.BindOrgID, in.LocationDesc,
			in.NoRecordingZone, in.RetentionDays, in.Remark, now, p.UserID, api.Config.NodeID); err != nil {
			if strings.Contains(err.Error(), "unique") {
				return httpapi.Error(http.StatusConflict, "DUPLICATE_CAMERA_CODE", "设备编码已存在，请更换")
			}
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "create_camera", "camera_device", id, nil,
			map[string]any{"code": in.Code, "name": in.Name}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id})
	})

	api.Handle("PATCH /api/v1/cameras/{id}", "video:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name            string `json:"name"`
			ChannelNo       string `json:"channel_no"`
			StreamRef       string `json:"stream_ref"`
			BindOrgID       string `json:"bind_org_id"`
			LocationDesc    string `json:"location_desc"`
			NoRecordingZone *bool  `json:"no_recording_zone"`
			RetentionDays   int    `json:"retention_days"`
			Remark          string `json:"remark"`
			Version         int64  `json:"version"`
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
		query := `UPDATE camera_device SET name=COALESCE(NULLIF($3,''),name), channel_no=NULLIF($4,''),
			stream_ref=CASE WHEN $5<>'' THEN $5 ELSE stream_ref END, location_desc=NULLIF($6,''), remark=NULLIF($7,''),
			version=version+1, updated_at=$8, updated_by=$9, updated_node=$10`
		args := []any{p.TenantID, r.PathValue("id"), in.Name, in.ChannelNo, in.StreamRef, in.LocationDesc, in.Remark, now, p.UserID, api.Config.NodeID}
		if in.NoRecordingZone != nil {
			args = append(args, *in.NoRecordingZone)
			query += ", no_recording_zone=$" + itoa(len(args))
		}
		if in.RetentionDays > 0 {
			args = append(args, in.RetentionDays)
			query += ", retention_days=$" + itoa(len(args))
		}
		args = append(args, in.Version)
		query += " WHERE tenant_id=$1 AND id=$2 AND version=$" + itoa(len(args))
		result, err := tx.Exec(ctx, query, args...)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "记录不存在或已变更，请刷新后重试")
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "update_camera", "camera_device", r.PathValue("id"), nil, nil, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	api.Handle("POST /api/v1/cameras/{id}/disable", "video:manage", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		now := platform.Now()
		result, err := tx.Exec(ctx, `UPDATE camera_device SET status='disabled',version=version+1,updated_at=$3,updated_by=$4,updated_node=$5
			WHERE tenant_id=$1 AND id=$2 AND status<>'disabled'`, p.TenantID, r.PathValue("id"), now, p.UserID, api.Config.NodeID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "记录不存在或已停用")
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "disable_camera", "camera_device", r.PathValue("id"), nil, nil, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	/* 实时播放票据：流媒体网关接入前仅校验设备状态并签发短时票据（设计 5.15）。
	 * no_recording_zone 一律拒绝；离线设备返回 CAMERA_UNAVAILABLE。 */
	api.Handle("GET /api/v1/cameras/{id}/live-ticket", "video:view", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var status, streamRef string
		var noRecZone bool
		err := api.DB.QueryRow(r.Context(), `SELECT status,COALESCE(stream_ref,''),no_recording_zone FROM camera_device
			WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id")).Scan(&status, &streamRef, &noRecZone)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "设备不存在")
		}
		if err != nil {
			return err
		}
		if noRecZone {
			return httpapi.Error(http.StatusForbidden, "CAMERA_FORBIDDEN", "禁拍区域设备不允许查看或录像")
		}
		if status != "active" || streamRef == "" {
			return httpapi.Error(http.StatusServiceUnavailable, "CAMERA_UNAVAILABLE", "摄像头离线或未接入流媒体服务")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ticket": platform.Token(24), "expires_in": 120,
			"note": "流媒体网关未接入，当前仅签发播放票据"})
	})

	/* 录像时间轴：返回时段内证据标记（业务事件联动标记见 4.26.2 F3，网关接入后由网关提供录像分段） */
	api.Handle("GET /api/v1/cameras/timeline", "video:view", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		cameraID := r.URL.Query().Get("camera_id")
		rows, err := api.DB.Query(r.Context(), `SELECT id,seal_no,biz_type,biz_id,start_at,end_at,status
			FROM evidence_clip WHERE tenant_id=$1 AND camera_id=$2 ORDER BY start_at DESC LIMIT 100`, p.TenantID, cameraID)
		if err != nil {
			return err
		}
		defer rows.Close()
		marks := []map[string]any{}
		for rows.Next() {
			var id, bizType, status string
			var sealNo, bizID *string
			var startAt, endAt *int64
			if err := rows.Scan(&id, &sealNo, &bizType, &bizID, &startAt, &endAt, &status); err != nil {
				return err
			}
			marks = append(marks, map[string]any{"id": id, "seal_no": sealNo, "biz_type": bizType, "biz_id": bizID,
				"start_at": startAt, "end_at": endAt, "status": status})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"marks": marks, "segments": []any{}})
	})
}

func itoa(n int) string { return strconv.Itoa(n) }
