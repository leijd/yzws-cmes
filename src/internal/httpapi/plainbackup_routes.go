package httpapi

import (
	"archive/zip"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/plainbackup"
)

/* RegisterPlainBackupRoutes 密文库解密明文离线备份（设计 3.9 受控例外）。
 * 全部端点要求 backup:export；下载凭证一次性且短期有效，取包即作废；
 * zip 在响应流内打包，不在公开目录落临时文件。 */
func (a *API) RegisterPlainBackupRoutes() {
	backupDir := filepath.Join(filepath.Dir(a.Config.AttachmentDir), "backups")

	/* 发起明文导出（同步执行；本厂单租户范围） */
	a.Handle("POST /api/v1/tenant/plain-backups", "backup:export", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Reason string `json:"reason"`
		}
		_ = Decode(r, &in)
		result, err := plainbackup.Export(r.Context(), a.Store, a.Crypto, backupDir, p.TenantID, p.UserID, strings.TrimSpace(in.Reason))
		if err != nil {
			return Error(http.StatusInternalServerError, "EXPORT_FAILED", "明文导出失败："+err.Error())
		}
		return JSON(w, http.StatusOK, result)
	})

	/* 本厂导出任务列表（不含敏感内容） */
	a.Handle("GET /api/v1/tenant/plain-backups", "backup:export", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := a.DB.Query(r.Context(), `SELECT id,status,manifest_sha256,total_rows,table_count,snapshot_at,started_at,finished_at,error,
			(downloaded_at IS NOT NULL) AS downloaded
			FROM backup_job WHERE tenant_id=$1 AND backup_type='plaintext_export' ORDER BY started_at DESC LIMIT 50`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, status string
			var sha *string
			var finished, snapshot, started *int64
			var totalRows int64
			var tableCount int
			var errText *string
			var downloaded bool
			if err := rows.Scan(&id, &status, &sha, &totalRows, &tableCount, &snapshot, &started, &finished, &errText, &downloaded); err != nil {
				return err
			}
			items = append(items, map[string]any{
				"id": id, "status": status, "manifest_sha256": sha, "total_rows": totalRows, "table_count": tableCount,
				"snapshot_at": snapshot, "started_at": started, "finished_at": finished,
				"error": errText, "downloaded": downloaded,
			})
		}
		return JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	/* 校验备份包：结构/行数/SHA-256，不导入 */
	a.Handle("POST /api/v1/tenant/plain-backups/{id}/verify", "backup:export", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		result, err := plainbackup.Verify(r.Context(), a.Store, p.TenantID, r.PathValue("id"))
		if err != nil {
			return Error(http.StatusBadRequest, "VERIFY_FAILED", err.Error())
		}
		return JSON(w, http.StatusOK, result)
	})

	/* 签发一次性下载凭证（10 分钟有效） */
	a.Handle("POST /api/v1/tenant/plain-backups/{id}/download-ticket", "backup:export", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		token, err := plainbackup.IssueDownloadToken(r.Context(), a.Store, p.TenantID, r.PathValue("id"))
		if err != nil {
			return Error(http.StatusConflict, "TICKET_DENIED", err.Error())
		}
		return JSON(w, http.StatusOK, map[string]any{"token": token, "expires_in_seconds": 600})
	})

	/* 一次性下载：zip 流式打包；凭证消费后不可重复取包 */
	a.Handle("GET /api/v1/tenant/plain-backups/{id}/download", "backup:export", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		token := r.URL.Query().Get("token")
		dir, err := plainbackup.ConsumeDownloadToken(r.Context(), a.Store, p.TenantID, r.PathValue("id"), token)
		if err != nil {
			return Error(http.StatusForbidden, "DOWNLOAD_DENIED", err.Error())
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="plaintext-backup-`+r.PathValue("id")+`.zip"`)
		zw := zip.NewWriter(w)
		defer zw.Close()
		entries, err := os.ReadDir(dir)
		if err != nil {
			return Error(http.StatusInternalServerError, "EXPORT_FAILED", "备份包不可读")
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			src, err := os.Open(filepath.Join(dir, entry.Name()))
			if err != nil {
				return Error(http.StatusInternalServerError, "EXPORT_FAILED", "备份包不可读")
			}
			dst, err := zw.Create(entry.Name())
			if err != nil {
				src.Close()
				return err
			}
			_, copyErr := io.Copy(dst, src)
			src.Close()
			if copyErr != nil {
				return copyErr
			}
		}
		return nil
	})
}
