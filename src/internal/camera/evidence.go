package camera

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* registerEvidence 证据存证（设计 4.26.4）：手机拍摄上传、绑定封存、一次性导出。
 * 封存后只读（EVIDENCE_LOCKED）；导出复核 SHA-256，不一致即拒绝并告警。 */
func registerEvidence(api *httpapi.API) {
	api.Handle("GET /api/v1/evidence", "video:view,evidence:seal,evidence:export", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "e.tenant_id=$1"
		if v := r.URL.Query().Get("biz_type"); v != "" {
			args = append(args, v)
			where += fmt.Sprintf(" AND e.biz_type=$%d", len(args))
		}
		if v := r.URL.Query().Get("biz_id"); v != "" {
			args = append(args, v)
			where += fmt.Sprintf(" AND e.biz_id=$%d", len(args))
		}
		if v := r.URL.Query().Get("status"); v != "" {
			args = append(args, v)
			where += fmt.Sprintf(" AND e.status=$%d", len(args))
		}
		rows, err := api.DB.Query(r.Context(), `SELECT e.id,COALESCE(e.seal_no,''),e.source_type,COALESCE(e.camera_id,''),COALESCE(c.name,''),
			COALESCE(e.captured_by,''),COALESCE(emp.name,''),COALESCE(e.capture_at,0),e.biz_type,COALESCE(e.biz_id,''),
			COALESCE(e.start_at,0),COALESCE(e.end_at,0),e.status,COALESCE(e.sha256,''),COALESCE(e.remark,''),e.created_at,
			e.attachment_id IS NOT NULL AND e.attachment_id<>''
			FROM evidence_clip e
			LEFT JOIN camera_device c ON c.tenant_id=e.tenant_id AND c.id=e.camera_id
			LEFT JOIN employee emp ON emp.tenant_id=e.tenant_id AND emp.id=e.captured_by
			WHERE `+where+` ORDER BY e.created_at DESC LIMIT 200`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, sealNo, sourceType, cameraID, cameraName, capturedBy, capturerName, bizType, bizID, status, hash, remark string
			var captureAt, startAt, endAt, createdAt int64
			var hasFile bool
			if err := rows.Scan(&id, &sealNo, &sourceType, &cameraID, &cameraName, &capturedBy, &capturerName, &captureAt,
				&bizType, &bizID, &startAt, &endAt, &status, &hash, &remark, &createdAt, &hasFile); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "seal_no": sealNo, "source_type": sourceType, "camera_id": cameraID,
				"camera_name": cameraName, "captured_by": capturedBy, "capturer_name": capturerName, "capture_at": captureAt,
				"biz_type": bizType, "biz_id": bizID, "start_at": startAt, "end_at": endAt, "status": status,
				"sha256": hash, "remark": remark, "created_at": createdAt, "has_file": hasFile})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	/* 手机拍摄存证：附件直传完成后登记元数据，服务端即锁哈希（设计 4.26.2 F9） */
	api.Handle("POST /api/v1/evidence/mobile-capture", "evidence:seal", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			AttachmentID string `json:"attachment_id"`
			BizType      string `json:"biz_type"`
			BizID        string `json:"biz_id"`
			DeviceInfo   string `json:"device_info"`
			GpsLocation  string `json:"gps_location"`
			Remark       string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.AttachmentID == "" || in.BizType == "" {
			return httpapi.Error(http.StatusBadRequest, "INPUT_INVALID", "附件与业务类型为必填项")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var sha, storageKey string
		var sizeBytes int64
		err = tx.QueryRow(ctx, `SELECT sha256,storage_key,size_bytes FROM attachment
			WHERE tenant_id=$1 AND id=$2 AND process_status='ready'`, p.TenantID, in.AttachmentID).Scan(&sha, &storageKey, &sizeBytes)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusBadRequest, "ATTACHMENT_NOT_READY", "附件不存在或尚未上传完成")
		}
		if err != nil {
			return err
		}
		capturedBy := p.EmployeeID
		if capturedBy == "" {
			capturedBy = p.UserID
		}
		id := platform.ID()
		now := platform.Now()
		if _, err := tx.Exec(ctx, `INSERT INTO evidence_clip(tenant_id,id,source_type,captured_by,capture_at,device_info,gps_location,
			biz_type,biz_id,capture_mode,storage_key,attachment_id,file_size,sha256,status,remark,
			created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,'mobile',$3,$4,NULLIF($5,''),NULLIF($6,''),$7,NULLIF($8,''),'manual',$9,$10,$11,$12,'capturing',NULLIF($13,''),
			$4,$14,$15,$4,$14,$15,1)`,
			p.TenantID, id, capturedBy, now, in.DeviceInfo, in.GpsLocation, in.BizType, in.BizID,
			storageKey, in.AttachmentID, sizeBytes, sha, in.Remark, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "mobile_capture_evidence", "evidence_clip", id, nil,
			map[string]any{"biz_type": in.BizType, "biz_id": in.BizID}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id})
	})

	/* 绑定业务单据并封存（手机证据与摄像头证据共用） */
	api.Handle("POST /api/v1/evidence/{id}/bind", "evidence:seal", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			BizType string `json:"biz_type"`
			BizID   string `json:"biz_id"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.BizType == "" || in.BizID == "" {
			return httpapi.Error(http.StatusBadRequest, "INPUT_INVALID", "业务类型与单据为必填项")
		}
		return sealEvidence(w, r, api, p, in.BizType, in.BizID)
	})

	/* 摄像头录像片段封存登记：片段截取由流媒体网关完成，本接口登记元数据并立即封存 */
	api.Handle("POST /api/v1/evidence/seal", "evidence:seal", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			CameraID string `json:"camera_id"`
			StartAt  int64  `json:"start_at"`
			EndAt    int64  `json:"end_at"`
			BizType  string `json:"biz_type"`
			BizID    string `json:"biz_id"`
			Remark   string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.CameraID == "" || in.StartAt <= 0 || in.EndAt <= in.StartAt || in.BizType == "" {
			return httpapi.Error(http.StatusBadRequest, "INPUT_INVALID", "摄像头、起止时间与业务类型为必填项")
		}
		ctx := r.Context()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var status string
		var noRecZone bool
		err = tx.QueryRow(ctx, `SELECT status,no_recording_zone FROM camera_device WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, in.CameraID).Scan(&status, &noRecZone)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "设备不存在")
		}
		if err != nil {
			return err
		}
		if noRecZone {
			return httpapi.Error(http.StatusForbidden, "CAMERA_FORBIDDEN", "禁拍区域设备不允许查看或录像")
		}
		if status != "active" {
			return httpapi.Error(http.StatusServiceUnavailable, "CAMERA_UNAVAILABLE", "摄像头离线或未接入流媒体服务")
		}
		sealNo, err := nextSealNo(ctx, tx, p.TenantID)
		if err != nil {
			return err
		}
		id := platform.ID()
		now := platform.Now()
		watermark := fmt.Sprintf("%s|%s|%s", p.TenantID, time.UnixMicro(now).Format("2006-01-02 15:04:05"), p.LoginName)
		if _, err := tx.Exec(ctx, `INSERT INTO evidence_clip(tenant_id,id,seal_no,source_type,camera_id,biz_type,biz_id,start_at,end_at,
			capture_mode,watermark_text,status,sealed_by,sealed_at,remark,
			created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,'camera',$4,$5,NULLIF($6,''),$7,$8,'manual',$9,'sealed',$10,$11,NULLIF($12,''),$11,$10,$13,$11,$10,$13,1)`,
			p.TenantID, id, sealNo, in.CameraID, in.BizType, in.BizID, in.StartAt, in.EndAt, watermark, p.UserID, now,
			in.Remark, api.Config.NodeID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "seal_camera_evidence", "evidence_clip", id, nil,
			map[string]any{"seal_no": sealNo, "biz_type": in.BizType}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "seal_no": sealNo})
	})

	/* 导出：复核哈希 → 签发一次性限时下载票据（设计 3.9 口径） */
	api.Handle("POST /api/v1/evidence/{id}/export", "evidence:export", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		var status, storageKey, expectHash string
		err := api.DB.QueryRow(ctx, `SELECT status,storage_key,COALESCE(sha256,'') FROM evidence_clip WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, r.PathValue("id")).Scan(&status, &storageKey, &expectHash)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "证据不存在")
		}
		if err != nil {
			return err
		}
		if status != "sealed" && status != "archived" {
			return httpapi.Error(http.StatusConflict, "EVIDENCE_NOT_SEALED", "证据未封存，不能导出")
		}
		if storageKey == "" {
			return httpapi.Error(http.StatusConflict, "EVIDENCE_NO_FILE", "摄像头片段文件待流媒体网关接入，暂不能导出")
		}
		filePath := filepath.Join(api.Config.AttachmentDir, storageKey, "content")
		actualHash, err := fileSHA256(filePath)
		if err != nil {
			return httpapi.Error(http.StatusInternalServerError, "EVIDENCE_FILE_MISSING", "证据文件读取失败")
		}
		if expectHash != "" && actualHash != expectHash {
			return httpapi.Error(http.StatusConflict, "EVIDENCE_HASH_MISMATCH", "证据文件哈希与封存值不一致，已拒绝导出")
		}
		ticket := platform.Token(32)
		now := platform.Now()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `INSERT INTO evidence_export_log(tenant_id,id,evidence_id,exported_by,ticket_hash,expires_at,created_at,created_node)
			VALUES($1,$2,$3,$4,$5,$6,$6,$7)`,
			p.TenantID, platform.ID(), r.PathValue("id"), p.UserID, hashTicket(ticket), now+int64(5*time.Minute), api.Config.NodeID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "export_evidence", "evidence_clip", r.PathValue("id"), nil,
			map[string]any{"sha256_verified": true}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"ticket": ticket, "expires_in": 300})
	})

	api.Handle("GET /api/v1/evidence/{id}/download", "evidence:export", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ticket := r.URL.Query().Get("ticket")
		if ticket == "" {
			return httpapi.Error(http.StatusBadRequest, "TICKET_REQUIRED", "缺少下载票据")
		}
		ctx := r.Context()
		now := platform.Now()
		var logID, storageKey string
		err := api.DB.QueryRow(ctx, `SELECT l.id,e.storage_key FROM evidence_export_log l
			JOIN evidence_clip e ON e.tenant_id=l.tenant_id AND e.id=l.evidence_id
			WHERE l.tenant_id=$1 AND l.evidence_id=$2 AND l.ticket_hash=$3 AND l.consumed_at IS NULL AND l.expires_at>$4`,
			p.TenantID, r.PathValue("id"), hashTicket(ticket), now).Scan(&logID, &storageKey)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusForbidden, "TICKET_INVALID", "下载票据无效或已过期")
		}
		if err != nil {
			return err
		}
		if _, err := api.DB.Exec(ctx, `UPDATE evidence_export_log SET consumed_at=$3 WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, logID, now); err != nil {
			return err
		}
		http.ServeFile(w, r, filepath.Join(api.Config.AttachmentDir, storageKey, "content"))
		return nil
	})
}

/* sealEvidence 绑定单据并封存：生成 ZJ 编号、锁定只读。 */
func sealEvidence(w http.ResponseWriter, r *http.Request, api *httpapi.API, p auth.Principal, bizType, bizID string) error {
	ctx := r.Context()
	tx, err := api.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM evidence_clip WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,
		p.TenantID, r.PathValue("id")).Scan(&status); err != nil {
		return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "证据不存在")
	}
	if status == "sealed" || status == "archived" {
		return httpapi.Error(http.StatusConflict, "EVIDENCE_LOCKED", "证据已封存，禁止修改")
	}
	sealNo, err := nextSealNo(ctx, tx, p.TenantID)
	if err != nil {
		return err
	}
	now := platform.Now()
	if _, err := tx.Exec(ctx, `UPDATE evidence_clip SET seal_no=$3,biz_type=$4,biz_id=$5,status='sealed',sealed_by=$6,sealed_at=$7,
		updated_at=$7,updated_by=$6,updated_node=$8,version=version+1
		WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id"), sealNo, bizType, bizID, p.UserID, now, api.Config.NodeID); err != nil {
		return err
	}
	if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "seal_evidence", "evidence_clip", r.PathValue("id"), nil,
		map[string]any{"seal_no": sealNo, "biz_type": bizType, "biz_id": bizID}, ""); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"seal_no": sealNo})
}

func nextSealNo(ctx context.Context, tx pgx.Tx, tenantID string) (string, error) {
	ym := time.Now().Format("200601")
	var n int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM evidence_clip WHERE tenant_id=$1 AND seal_no LIKE $2`,
		tenantID, "ZJ"+ym+"%").Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("ZJ%s%03d", ym, n+1), nil
}

func hashTicket(ticket string) string {
	sum := sha256.Sum256([]byte(ticket))
	return hex.EncodeToString(sum[:])
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
