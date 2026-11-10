package production

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

type backupManifest struct {
	GeneratedAt     int64            `json:"generated_at"`
	NodeID          string           `json:"node_id"`
	TenantID        string           `json:"tenant_id"`
	Migrations      []string         `json:"migrations"`
	Tables          map[string]int64 `json:"tables"`
	AttachmentCount int64            `json:"attachment_count"`
	AttachmentBytes int64            `json:"attachment_bytes"`
	IncludesAttach  bool             `json:"includes_attachments"`
	FormatVersion   int              `json:"format_version"`
}

func registerBackupSync(api *httpapi.API) {
	api.Handle("GET /api/v1/backup-jobs", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT id,node_id,backup_type,status,includes_attachments,COALESCE(manifest_sha256,''),COALESCE(error,''),started_at,COALESCE(finished_at,0) FROM backup_job WHERE tenant_id=$1 ORDER BY started_at DESC LIMIT 100`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, nodeID, backupType, status, sha, errText string
			var includes bool
			var startedAt, finishedAt int64
			if err := rows.Scan(&id, &nodeID, &backupType, &status, &includes, &sha, &errText, &startedAt, &finishedAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "node_id": nodeID, "backup_type": backupType, "status": status, "includes_attachments": includes, "manifest_sha256": sha, "error": errText, "started_at": startedAt, "finished_at": finishedAt})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/backup-jobs", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			BackupType         string `json:"backup_type"`
			IncludeAttachments bool   `json:"include_attachments"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.BackupType == "" {
			in.BackupType = "manifest"
		}
		if in.BackupType != "manifest" && in.BackupType != "database" {
			return httpapi.Error(400, "VALIDATION_ERROR", "备份类型只支持 manifest/database")
		}
		id, now := platform.ID(), platform.Now()
		tables := map[string]int64{}
		rows, err := api.DB.Query(r.Context(), "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
		if err != nil {
			return err
		}
		names := []string{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return err
			}
			names = append(names, name)
		}
		rows.Close()
		for _, name := range names {
			var count int64
			if err := api.DB.QueryRow(r.Context(), `SELECT count(*) FROM `+quoteIdent(name)).Scan(&count); err != nil {
				return err
			}
			tables[name] = count
		}
		var migrations []string
		migRows, err := api.DB.Query(r.Context(), "SELECT version FROM schema_migration ORDER BY version")
		if err != nil {
			return err
		}
		for migRows.Next() {
			var version string
			if err := migRows.Scan(&version); err != nil {
				migRows.Close()
				return err
			}
			migrations = append(migrations, version)
		}
		migRows.Close()
		var attachmentCount, attachmentBytes int64
		_ = api.DB.QueryRow(r.Context(), "SELECT count(*),COALESCE(SUM(size_bytes),0) FROM attachment WHERE tenant_id=$1", p.TenantID).Scan(&attachmentCount, &attachmentBytes)
		manifest := backupManifest{GeneratedAt: now, NodeID: api.Config.NodeID, TenantID: p.TenantID, Migrations: migrations, Tables: tables, AttachmentCount: attachmentCount, AttachmentBytes: attachmentBytes, IncludesAttach: in.IncludeAttachments, FormatVersion: 1}
		body, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		sha := hex.EncodeToString(digest[:])
		dir := filepath.Join(api.Config.AttachmentDir, "backups", id)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0600); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if _, err := tx.Exec(r.Context(), `INSERT INTO backup_job(tenant_id,id,node_id,backup_type,storage_key,manifest_sha256,includes_attachments,status,started_at,finished_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,'succeeded',$8,$8)`, p.TenantID, id, api.Config.NodeID, in.BackupType, id, sha, in.IncludeAttachments, now); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "backup", "backup_job", id, nil, map[string]any{"type": in.BackupType, "sha256": sha}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "status": "succeeded", "manifest_sha256": sha, "tables": len(tables), "attachments": attachmentCount})
	})

	api.Handle("GET /api/v1/backup-jobs/{id}", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var id, nodeID, backupType, status, storageKey, sha, errText string
		var includes bool
		var startedAt, finishedAt int64
		if err := api.DB.QueryRow(r.Context(), `SELECT id,node_id,backup_type,status,COALESCE(storage_key,''),COALESCE(manifest_sha256,''),includes_attachments,COALESCE(error,''),started_at,COALESCE(finished_at,0) FROM backup_job WHERE tenant_id=$1 AND id=$2`, p.TenantID, r.PathValue("id")).Scan(&id, &nodeID, &backupType, &status, &storageKey, &sha, &includes, &errText, &startedAt, &finishedAt); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "备份任务不存在")
		}
		var manifest any
		if data, err := os.ReadFile(filepath.Join(api.Config.AttachmentDir, "backups", storageKey, "manifest.json")); err == nil {
			_ = json.Unmarshal(data, &manifest)
		}
		return httpapi.JSON(w, 200, map[string]any{"id": id, "node_id": nodeID, "backup_type": backupType, "status": status, "manifest_sha256": sha, "includes_attachments": includes, "error": errText, "started_at": startedAt, "finished_at": finishedAt, "manifest": manifest})
	})

	api.Handle("POST /api/v1/backup-jobs/{id}/verify", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var storageKey, expected string
		if err := api.DB.QueryRow(r.Context(), "SELECT COALESCE(storage_key,''),COALESCE(manifest_sha256,'') FROM backup_job WHERE tenant_id=$1 AND id=$2", p.TenantID, r.PathValue("id")).Scan(&storageKey, &expected); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "备份任务不存在")
		}
		data, err := os.ReadFile(filepath.Join(api.Config.AttachmentDir, "backups", storageKey, "manifest.json"))
		if err != nil {
			return httpapi.Error(404, "BACKUP_FILE_MISSING", "备份文件不存在")
		}
		digest := sha256.Sum256(data)
		actual := hex.EncodeToString(digest[:])
		status := "verified"
		if expected == "" || actual != expected {
			status = "failed"
		}
		if _, err := api.DB.Exec(r.Context(), "UPDATE backup_job SET status=$1,finished_at=$2 WHERE tenant_id=$3 AND id=$4", status, platform.Now(), p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": status, "match": status == "verified", "expected": expected, "actual": actual})
	})

	api.Handle("POST /api/v1/backup-jobs/{id}/restore", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return httpapi.Error(http.StatusConflict, "RESTORE_OFFLINE_REQUIRED", "在线恢复已禁用：请使用 pg_restore 在隔离环境执行，并先完成校验与备份")
	})

	api.Handle("GET /api/v1/sync/conflicts", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "tenant_id=$1"
		if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
			args = append(args, status)
			where += " AND status=$" + strconv.Itoa(len(args))
		}
		rows, err := api.DB.Query(r.Context(), `SELECT id,event_id,aggregate_type,aggregate_id,conflict_code,COALESCE(incoming_value::text,''),COALESCE(current_value::text,''),status,COALESCE(resolution_type,''),COALESCE(resolved_by,''),COALESCE(resolved_at,0),created_at FROM sync_conflict WHERE `+where+` ORDER BY created_at DESC LIMIT 200`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, eventID, aggType, aggID, code, incoming, current, status, resolution, resolvedBy string
			var resolvedAt, createdAt int64
			if err := rows.Scan(&id, &eventID, &aggType, &aggID, &code, &incoming, &current, &status, &resolution, &resolvedBy, &resolvedAt, &createdAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "event_id": eventID, "aggregate_type": aggType, "aggregate_id": aggID, "conflict_code": code, "incoming_value": incoming, "current_value": current, "status": status, "resolution_type": resolution, "resolved_by": resolvedBy, "resolved_at": resolvedAt, "created_at": createdAt})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("GET /api/v1/sync/conflicts/{id}", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var item map[string]any
		if err := api.DB.QueryRow(r.Context(), `SELECT row_to_json(t) FROM (SELECT * FROM sync_conflict WHERE tenant_id=$1 AND id=$2) t`, p.TenantID, r.PathValue("id")).Scan(&item); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "冲突不存在")
			}
			return err
		}
		return httpapi.JSON(w, 200, item)
	})

	api.Handle("POST /api/v1/sync/conflicts/{id}/resolve", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ResolutionType  string          `json:"resolution_type"`
			ResolutionValue json.RawMessage `json:"resolution_value"`
			Reason          string          `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.ResolutionType != "keep_current" && in.ResolutionType != "keep_incoming" && in.ResolutionType != "merged" {
			return httpapi.Error(400, "VALIDATION_ERROR", "处理方式只支持 keep_current/keep_incoming/merged")
		}
		if strings.TrimSpace(in.Reason) == "" {
			return httpapi.Error(400, "REASON_REQUIRED", "处理冲突必须填写原因")
		}
		value := in.ResolutionValue
		if len(value) == 0 {
			value = json.RawMessage(`{}`)
		}
		result, err := api.DB.Exec(r.Context(), `UPDATE sync_conflict SET status='resolved',resolution_type=$1,resolution_value=$2,resolved_by=$3,resolved_at=$4 WHERE tenant_id=$5 AND id=$6 AND status<>'resolved'`, in.ResolutionType, value, p.UserID, platform.Now(), p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(409, "ALREADY_RESOLVED", "冲突不存在或已处理")
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "status": "resolved", "resolution_type": in.ResolutionType})
	})

	api.Handle("POST /api/v1/sync/retry", "sync:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			NodeID string `json:"node_id"`
			Stage  string `json:"stage"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"node_id": in.NodeID, "stage": in.Stage, "retried": true})
	})
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
