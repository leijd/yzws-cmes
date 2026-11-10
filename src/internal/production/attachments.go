package production

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerAttachments(api *httpapi.API) {
	api.Handle("POST /api/v1/attachments/init", "attachment:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			SHA256     string `json:"sha256"`
			SizeBytes  int64  `json:"size_bytes"`
			MimeType   string `json:"mime_type"`
			DurationMS *int64 `json:"duration_ms"`
			OwnerType  string `json:"owner_type"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if len(in.SHA256) != 64 || in.SizeBytes <= 0 || in.SizeBytes > 200<<20 || in.MimeType == "" {
			return httpapi.Error(400, "INVALID_ATTACHMENT", "附件哈希、类型或大小不合法")
		}
		var existing string
		err := api.DB.QueryRow(r.Context(), "SELECT id FROM attachment WHERE tenant_id=$1 AND sha256=$2 AND size_bytes=$3", p.TenantID, in.SHA256, in.SizeBytes).Scan(&existing)
		if err == nil {
			return httpapi.JSON(w, 200, map[string]any{"attachment_id": existing, "instant": true, "received_chunks": []int{}})
		}
		if err != pgx.ErrNoRows {
			return err
		}
		id := platform.ID()
		_, err = api.DB.Exec(r.Context(), `INSERT INTO attachment(tenant_id,id,sha256,mime_type,size_bytes,duration_ms,storage_key,created_by,created_node,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, p.TenantID, id, in.SHA256, in.MimeType, in.SizeBytes, in.DurationMS, id, p.UserID, api.Config.NodeID, platform.Now())
		if err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"attachment_id": id, "instant": false, "received_chunks": []int{}, "chunk_size": 4 << 20})
	})
	api.Handle("PUT /api/v1/attachments/{id}/chunks/{index}", "attachment:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		index, err := strconv.Atoi(r.PathValue("index"))
		if err != nil || index < 0 {
			return httpapi.Error(400, "INVALID_CHUNK", "分片序号不合法")
		}
		var storageKey string
		if err = api.DB.QueryRow(r.Context(), "SELECT storage_key FROM attachment WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&storageKey); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "附件不存在")
		}
		dir := filepath.Join(api.Config.AttachmentDir, storageKey)
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		file, err := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%d.part", index)), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(file, io.LimitReader(r.Body, 4<<20+1))
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written > 4<<20 {
			return httpapi.Error(400, "CHUNK_TOO_LARGE", "分片超过大小限制")
		}
		return httpapi.JSON(w, 200, map[string]any{"received_chunk": index, "next_chunk": index + 1})
	})
	api.Handle("POST /api/v1/attachments/{id}/complete", "attachment:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			TotalChunks int `json:"total_chunks"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		var storageKey, expectedHash, mime string
		var expectedSize int64
		if err := api.DB.QueryRow(r.Context(), "SELECT storage_key,sha256,mime_type,size_bytes FROM attachment WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&storageKey, &expectedHash, &mime, &expectedSize); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "附件不存在")
		}
		dir := filepath.Join(api.Config.AttachmentDir, storageKey)
		contentPath := filepath.Join(dir, "content")
		out, err := os.OpenFile(contentPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		hash := sha256.New()
		writer := io.MultiWriter(out, hash)
		for i := 0; i < in.TotalChunks; i++ {
			part, openErr := os.Open(filepath.Join(dir, fmt.Sprintf("%d.part", i)))
			if openErr != nil {
				out.Close()
				return httpapi.Error(400, "MISSING_CHUNK", fmt.Sprintf("缺少分片 %d", i))
			}
			_, copyErr := io.Copy(writer, part)
			part.Close()
			if copyErr != nil {
				out.Close()
				return copyErr
			}
		}
		if err = out.Close(); err != nil {
			return err
		}
		if hex.EncodeToString(hash.Sum(nil)) != expectedHash {
			os.Remove(contentPath)
			return httpapi.Error(400, "ATTACHMENT_HASH_MISMATCH", "附件哈希不一致")
		}
		info, err := os.Stat(contentPath)
		if err != nil {
			return err
		}
		if info.Size() != expectedSize {
			return httpapi.Error(400, "ATTACHMENT_SIZE_MISMATCH", "附件大小不一致")
		}
		if _, err = api.DB.Exec(r.Context(), "UPDATE attachment SET process_status='ready' WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		for i := 0; i < in.TotalChunks; i++ {
			_ = os.Remove(filepath.Join(dir, fmt.Sprintf("%d.part", i)))
		}
		return httpapi.JSON(w, 200, map[string]any{"attachment_id": r.PathValue("id"), "process_status": "ready", "sync_status": "local"})
	})
	api.Handle("GET /api/v1/attachments/{id}", "attachment:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var item map[string]any
		err := api.DB.QueryRow(r.Context(), "SELECT row_to_json(t) FROM (SELECT * FROM attachment WHERE tenant_id=$1 AND id=$2) t", p.TenantID, r.PathValue("id")).Scan(&item)
		if err != nil {
			return httpapi.Error(404, "NOT_FOUND", "附件不存在")
		}
		return httpapi.JSON(w, 200, item)
	})
	api.Handle("GET /api/v1/attachments/{id}/content", "attachment:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var key, mime, status string
		if err := api.DB.QueryRow(r.Context(), "SELECT storage_key,mime_type,process_status FROM attachment WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&key, &mime, &status); err != nil || status != "ready" {
			return httpapi.Error(404, "ATTACHMENT_PENDING", "附件不存在或尚未就绪")
		}
		w.Header().Set("Content-Type", mime)
		http.ServeFile(w, r, filepath.Join(api.Config.AttachmentDir, key, "content"))
		return nil
	})
}
