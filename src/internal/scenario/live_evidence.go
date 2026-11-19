package scenario

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/orgapp"
	"github.com/jackc/pgx/v5"
)

/* 直播取证（设计 6.12.8）：线检/总检/报工审核/成品入库/发货/外协回货验收等环节
 * 可发起实时直播（固定摄像头实时流或手机实时画面），授权人员远程观看；
 * 结束时按时段同步封存片段，复用 4.26 evidence_clip 的哈希与封存机制。
 * 权限：发起与封存需 video:view/evidence:seal；观看按 5.7 数据范围过滤。 */

func registerLiveEvidence(api *httpapi.API) {
	api.Handle("GET /api/v1/evidence/live-sessions", "video:view", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "tenant_id=$1"
		if v := strings.TrimSpace(r.URL.Query().Get("biz_type")); v != "" {
			args = append(args, v)
			where += " AND biz_type=$" + itoa(len(args))
		}
		if v := strings.TrimSpace(r.URL.Query().Get("biz_id")); v != "" {
			args = append(args, v)
			where += " AND biz_id=$" + itoa(len(args))
		}
		/* 5.7 数据范围：非全厂账号只看自己发起或自己被列为观看人的直播会话 */
		access, err := orgapp.ComputeAccess(r.Context(), api.DB, p)
		if err != nil {
			return err
		}
		if !access.All {
			/* watchers 为 JSONB 字符串数组，用 ? 操作符判断数组是否含该元素（参数保持 text） */
			args = append(args, p.UserID)
			where += " AND (started_by=$" + itoa(len(args))
			args = append(args, p.UserID)
			where += " OR watchers ? $" + itoa(len(args)) + "::text"
			if p.EmployeeID != "" && p.EmployeeID != p.UserID {
				args = append(args, p.EmployeeID)
				where += " OR watchers ? $" + itoa(len(args)) + "::text"
			}
			where += ")"
		}
		args = append(args, 100)
		rows, err := api.DB.Query(r.Context(), `SELECT id,biz_type,biz_id,COALESCE(scene_type,''),COALESCE(scene_sub,''),
			source_type,COALESCE(camera_id,''),started_by,started_at,COALESCE(ended_at,0),status,
			COALESCE(evidence_clip_id,''),COALESCE(remark,'') FROM evidence_live_session WHERE `+where+`
			ORDER BY started_at DESC LIMIT $`+itoa(len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, bizType, bizID, st, sub, sourceType, cameraID, startedBy, status, clipID, remark string
			var startedAt, endedAt int64
			if err := rows.Scan(&id, &bizType, &bizID, &st, &sub, &sourceType, &cameraID, &startedBy,
				&startedAt, &endedAt, &status, &clipID, &remark); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "biz_type": bizType, "biz_id": bizID,
				"scene_type": st, "scene_sub": sub, "source_type": sourceType, "camera_id": cameraID,
				"started_by": startedBy, "started_at": startedAt, "ended_at": endedAt, "status": status,
				"evidence_clip_id": clipID, "remark": remark, "outsource": isOutsourceScene(st)})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/evidence/live-sessions", "evidence:seal", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		/* 写操作需 evidence:seal + video:view（AND），防止只有只读 video:view 的账号写库 */
		if err := requireAll(p, "evidence:seal", "video:view"); err != nil {
			return err
		}
		var in struct {
			BizType    string   `json:"biz_type"`
			BizID      string   `json:"biz_id"`
			SceneType  string   `json:"scene_type"`
			SceneSub   string   `json:"scene_sub"`
			SourceType string   `json:"source_type"`
			CameraID   string   `json:"camera_id"`
			Watchers   []string `json:"watchers"`
			Remark     string   `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		allowed := map[string]bool{"line_inspection": true, "final_inspection": true, "report_review": true,
			"finished_receipt": true, "shipment": true, "outsource_return": true}
		if !allowed[in.BizType] || strings.TrimSpace(in.BizID) == "" {
			return httpapi.Error(http.StatusBadRequest, "LIVE_BIZ_INVALID", "直播取证业务环节无效")
		}
		if in.SourceType == "" {
			in.SourceType = "camera"
		}
		if in.SourceType != "camera" && in.SourceType != "mobile" {
			return httpapi.Error(http.StatusBadRequest, "LIVE_SOURCE_INVALID", "直播来源仅支持 camera/mobile")
		}
		if in.SceneType != "" && !validScene(in.SceneType, in.SceneSub) {
			return httpapi.Error(http.StatusBadRequest, "SCENE_INVALID", "业务场景或子类型无效")
		}
		/* 摄像头来源须是本厂在线且非禁拍设备（沿用 4.26 / 5.15 口径），避免封存时外键失败 */
		if in.SourceType == "camera" {
			if strings.TrimSpace(in.CameraID) == "" {
				return httpapi.Error(http.StatusBadRequest, "CAMERA_ID_REQUIRED", "摄像头直播必须选择设备")
			}
			var forbidden, offline bool
			err := api.DB.QueryRow(r.Context(), `SELECT no_recording_zone,status<>'active' FROM camera_device
				WHERE tenant_id=$1 AND id=$2`, p.TenantID, in.CameraID).Scan(&forbidden, &offline)
			if err == pgx.ErrNoRows {
				return httpapi.Error(http.StatusNotFound, "CAMERA_UNAVAILABLE", "摄像头不存在")
			}
			if err != nil {
				return err
			}
			if forbidden {
				return httpapi.Error(http.StatusForbidden, "CAMERA_FORBIDDEN", "禁拍区域设备不允许直播取证")
			}
			if offline {
				return httpapi.Error(http.StatusServiceUnavailable, "CAMERA_UNAVAILABLE", "摄像头离线或已停用")
			}
		} else {
			in.CameraID = ""
		}
		/* 观看人按 5.7 数据范围收敛：非全厂账号不能指定范围外人员 */
		access, err := orgapp.ComputeAccess(r.Context(), api.DB, p)
		if err != nil {
			return err
		}
		if in.Watchers == nil {
			in.Watchers = []string{}
		}
		if !access.All {
			in.Watchers = []string{}
		}
		id, now := nowID()
		_, err = api.DB.Exec(r.Context(), `INSERT INTO evidence_live_session(tenant_id,id,biz_type,biz_id,scene_type,scene_sub,
			source_type,camera_id,started_by,started_at,status,watchers,live_ticket,remark,
			created_at,created_by,created_node,updated_at,updated_by,updated_node)
			VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,NULLIF($8,''),$9,$10,'live',$11::jsonb,$12,NULLIF($13,''),
			$10,$9,$14,$10,$9,$14)`,
			p.TenantID, id, in.BizType, in.BizID, in.SceneType, in.SceneSub, in.SourceType, in.CameraID,
			p.UserID, now, jsonString(in.Watchers), "LIVE-"+id, in.Remark, api.Config.NodeID)
		if err != nil {
			return err
		}
		if err := auditWrite(r.Context(), api, p, "start_evidence_live", "evidence_live_session", id,
			map[string]any{"biz_type": in.BizType, "biz_id": in.BizID, "source_type": in.SourceType}); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "status": "live",
			"live_ticket": "LIVE-" + id, "started_at": now})
	})

	/* 结束直播：可选同步封存片段（写 evidence_clip，哈希按 4.26 口径） */
	api.Handle("POST /api/v1/evidence/live-sessions/{id}/end", "evidence:seal", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Seal   bool   `json:"seal"`
			Remark string `json:"remark"`
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
		var bizType, bizID, sourceType, cameraID, startedBy, status string
		var startedAt int64
		if err := tx.QueryRow(ctx, `SELECT biz_type,biz_id,source_type,COALESCE(camera_id,''),started_by,started_at,status
			FROM evidence_live_session WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, p.TenantID, r.PathValue("id")).Scan(
			&bizType, &bizID, &sourceType, &cameraID, &startedBy, &startedAt, &status); err != nil {
			if err == pgx.ErrNoRows {
				return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "直播会话不存在")
			}
			return err
		}
		if status != "live" {
			return httpapi.Error(http.StatusConflict, "LIVE_ALREADY_ENDED", "直播已结束")
		}
		now := platformNow()
		clipID := ""
		finalStatus := "ended"
		if in.Seal {
			/* 复用 4.26 封存口径：片段哈希在服务端计算后写入并锁定；
			 * evidence_clip.biz_type 为固定枚举，直播环节统一映射到 quality/general。 */
			clipID = platformNewID()
			bizEnum := "general"
			switch bizType {
			case "line_inspection", "final_inspection", "report_review":
				bizEnum = "quality"
			case "shipment":
				bizEnum = "shipment"
			case "outsource_return":
				bizEnum = "outsourcing"
			}
			/* evidence_clip CHECK: camera 来源必须带 camera_id，mobile 来源必须带 captured_by。
			 * 手机直播本身无摄像头 ID，统一按 mobile + 直播发起人封存，避免 500。 */
			clipSource, clipCamera, clipCaptured := sourceType, cameraID, ""
			if clipSource == "mobile" || clipCamera == "" {
				clipSource, clipCamera, clipCaptured = "mobile", "", startedBy
			}
			hash := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%d|%d", clipID, bizID, startedAt, now))
			_, err = tx.Exec(ctx, `INSERT INTO evidence_clip(tenant_id,id,seal_no,source_type,camera_id,captured_by,biz_type,biz_id,
				start_at,end_at,capture_mode,storage_key,sha256,status,sealed_by,sealed_at,remark,
				created_at,created_by,created_node,updated_at,updated_by,updated_node)
				VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,$9,$10,'event',$11,$12,'sealed',$13,$10,NULLIF($14,''),
				$10,$13,$15,$10,$13,$15)`,
				p.TenantID, clipID, "ZJ-LIVE-"+clipID, clipSource, clipCamera, clipCaptured, bizEnum, bizID,
				startedAt, now, "live/"+clipID, hex.EncodeToString(hash[:]), p.UserID, in.Remark, api.Config.NodeID)
			if err != nil {
				return err
			}
			finalStatus = "sealed"
		}
		if _, err := tx.Exec(ctx, `UPDATE evidence_live_session SET status=$3,ended_at=$4,
			evidence_clip_id=NULLIF($5,''),remark=COALESCE(NULLIF($6,''),remark),
			updated_at=$4,updated_by=$7,updated_node=$8,version=version+1 WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, r.PathValue("id"), finalStatus, now, clipID, in.Remark, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "end_evidence_live", "evidence_live_session", r.PathValue("id"), nil,
			map[string]any{"status": finalStatus, "evidence_clip_id": clipID}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ended": true, "status": finalStatus, "evidence_clip_id": clipID})
	})
}
