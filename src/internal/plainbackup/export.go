// Package plainbackup 密文库解密明文离线备份（设计 3.9 受控例外）。
//
// 导出：从 secure 库读取本厂数据，服务端解密敏感字段后写出 JSONL（每行一条记录）+ manifest
// （表名/行数/SHA-256 + 包级校验值）；不含密钥、会话、登录票据与各类日志。
// 校验：只校验备份包结构、行数和 SHA-256，不导入。下载：一次性短期凭证，取包即作废；
// 打包在响应流内完成，不在公开目录落临时文件；日志不写业务明文。
package plainbackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clothp/internal/datastore"
	"clothp/internal/dualwrite"
	"clothp/internal/platform"
	"clothp/internal/seccrypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const FormatVersion = "v1"

/* excludedTables 导出排除：密钥/会话/票据/日志与同步控制表（设计 3.9：备份包不含密钥、会话、票据）。 */
var excludedTables = map[string]bool{
	"tenant_crypto_key": true, "auth_session": true, "platform_session": true,
	"auth_login_log": true, "audit_log": true, "platform_audit_log": true, "deletion_log": true,
	"dual_write_journal": true, "dual_write_saga": true, "dual_write_saga_step": true,
	"data_transfer_job": true, "data_transfer_item": true,
	"outbox_event": true, "command_request": true, "sync_queue": true, "print_job": true,
}

/* querier 连接池与事务的公共读接口（快照事务内复用）。 */
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type tableManifest struct {
	Name    string `json:"name"`
	Rows    int64  `json:"rows"`
	SHA256  string `json:"sha256"`
	Columns int    `json:"columns"`
}

type manifest struct {
	FormatVersion string           `json:"format_version"`
	TenantID      string           `json:"tenant_id"`
	SnapshotAt    int64            `json:"snapshot_at"`
	CreatedBy     string           `json:"created_by"`
	TotalRows     int64            `json:"total_rows"`
	Tables        []*tableManifest `json:"tables"`
}

/* Export 执行明文导出（同步；数据量由单租户范围控制）。返回任务 ID 与摘要。 */
func Export(ctx context.Context, store *datastore.Store, crypto *seccrypto.Service, backupDir, tenantID, requestedBy, reason string) (map[string]any, error) {
	if store.Secure == nil {
		return nil, fmt.Errorf("secure database not configured")
	}
	secure := store.Secure
	now := platform.Now()
	jobID := platform.ID()
	dir := filepath.Join(backupDir, jobID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	rowCrypto := dualwrite.NewRowCrypto(crypto)
	/* 备份口径：只保证备份时刻的快照一致——REPEATABLE READ 只读事务定格全表同一快照，
	 * MVCC 不阻塞任何写入；snapshot_at 即快照建立时刻。 */
	snapTx, err := secure.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("begin snapshot: %w", err)
	}
	defer func() { _ = snapTx.Rollback(ctx) }()
	tables, err := tenantTables(ctx, snapTx, tenantID)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	m := &manifest{FormatVersion: FormatVersion, TenantID: tenantID, SnapshotAt: now, CreatedBy: requestedBy}
	for _, table := range tables {
		tm, err := exportTable(ctx, snapTx, rowCrypto, tenantID, table, dir)
		if err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("export table %s: %w", table, err)
		}
		m.Tables = append(m.Tables, tm)
		m.TotalRows += tm.Rows
	}
	if err := snapTx.Commit(ctx); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("close snapshot: %w", err)
	}
	manifestBody, _ := json.MarshalIndent(m, "", "  ")
	manifestHash := sha256.Sum256(manifestBody)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifestBody, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	/* 任务元数据双写两库（运行期读取一律走主库 secure，明文库为对账副本；
	 * 失败清理目录，不留部分明文包——设计 3.9.5） */
	insertJob := `INSERT INTO backup_job(tenant_id,id,node_id,backup_type,storage_key,manifest_sha256,includes_attachments,status,
		started_at,finished_at,source_db,format_version,snapshot_at,requested_by,total_rows,table_count)
		VALUES($1,$2,'center','plaintext_export',$3,$4,TRUE,'succeeded',$5,$5,'secure',$6,$5,$7,$8,$9)`
	for _, pool := range []*pgxpool.Pool{store.Secure, store.Plain} {
		if pool == nil {
			continue
		}
		if _, err := pool.Exec(ctx, insertJob,
			tenantID, jobID, dir, hex.EncodeToString(manifestHash[:]), now, FormatVersion, requestedBy, m.TotalRows, len(m.Tables)); err != nil {
			_ = os.RemoveAll(dir)
			return nil, err
		}
	}
	return map[string]any{
		"job_id": jobID, "tables": len(m.Tables), "total_rows": m.TotalRows,
		"manifest_sha256": hex.EncodeToString(manifestHash[:]),
	}, nil
}

/* tenantTables 本厂相关、且不在排除清单内的表（拓扑无关：JSONL 按表独立文件）。 */
func tenantTables(ctx context.Context, db querier, tenantID string) ([]string, error) {
	rows, err := db.Query(ctx, `SELECT DISTINCT c.relname
		FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='tenant_id' AND a.attnum>0 AND NOT a.attisdropped
		WHERE n.nspname='public' AND c.relkind='r' ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		if excludedTables[t] {
			continue
		}
		tables = append(tables, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	/* tenant 表自身（按 id 归属） */
	var tenantTable bool
	_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname='public' AND c.relname='tenant' AND c.relkind='r')`).Scan(&tenantTable)
	if tenantTable && !excludedTables["tenant"] {
		tables = append([]string{"tenant"}, tables...)
	}
	return tables, nil
}

/* exportTable 单表导出：快照读 → 解密 → JSONL 写盘（0600，私有目录）。 */
func exportTable(ctx context.Context, db querier, rowCrypto *dualwrite.RowCrypto, tenantID, table, dir string) (*tableManifest, error) {
	var rows pgx.Rows
	var err error
	if table == "tenant" {
		rows, err = db.Query(ctx, `SELECT * FROM "tenant" WHERE id=$1`, tenantID)
	} else {
		rows, err = db.Query(ctx, fmt.Sprintf(`SELECT * FROM "%s" WHERE tenant_id=$1`, table), tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = string(f.Name)
	}
	file, err := os.OpenFile(filepath.Join(dir, table+".jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	var count int64
	flushErr := func() error {
		enc := json.NewEncoder(file)
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return err
			}
			row := map[string]any{}
			for i, col := range columns {
				row[col] = values[i]
			}
			primaryKey := ""
			if v, ok := row["id"]; ok && v != nil {
				primaryKey = fmt.Sprint(v)
			}
			/* 解密敏感列（服务端按 DEK 版本解包；AAD 绑定租户/表/行/字段） */
			decrypted, err := rowCrypto.FromSecure(ctx, tenantID, table, primaryKey, row)
			if err != nil {
				return fmt.Errorf("decrypt %s.%s: %w", table, primaryKey, err)
			}
			if err := enc.Encode(decrypted); err != nil {
				return err
			}
			count++
		}
		return rows.Err()
	}()
	/* 把写出的内容摘要进哈希：读回文件比流式双写简单且文件已是唯一事实 */
	_ = file.Close()
	if flushErr != nil {
		return nil, flushErr
	}
	written, err := os.ReadFile(filepath.Join(dir, table+".jsonl"))
	if err != nil {
		return nil, err
	}
	hash.Write(written)
	return &tableManifest{Name: table, Rows: count, SHA256: hex.EncodeToString(hash.Sum(nil)), Columns: len(columns)}, nil
}

/* VerifyResult 校验结果。 */
type VerifyResult struct {
	OK       bool     `json:"ok"`
	Checked  int      `json:"checked_tables"`
	Mismatch []string `json:"mismatch"`
}

/* Verify 只校验备份包结构、行数与 SHA-256，不导入（设计 3.9.3）。 */
func Verify(ctx context.Context, store *datastore.Store, tenantID, jobID string) (*VerifyResult, error) {
	jobDir, status, err := jobDirOf(ctx, store, tenantID, jobID)
	if err != nil {
		return nil, err
	}
	if status != "succeeded" {
		return nil, fmt.Errorf("backup job status=%s, cannot verify", status)
	}
	var m manifest
	body, err := os.ReadFile(filepath.Join(jobDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("manifest parse: %w", err)
	}
	result := &VerifyResult{OK: true, Checked: len(m.Tables)}
	for _, tm := range m.Tables {
		data, err := os.ReadFile(filepath.Join(jobDir, tm.Name+".jsonl"))
		if err != nil {
			result.OK = false
			result.Mismatch = append(result.Mismatch, tm.Name+": file missing")
			continue
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != tm.SHA256 {
			result.OK = false
			result.Mismatch = append(result.Mismatch, tm.Name+": sha256 mismatch")
			continue
		}
		var lines int64
		for _, b := range data {
			if b == '\n' {
				lines++
			}
		}
		if lines != tm.Rows {
			result.OK = false
			result.Mismatch = append(result.Mismatch, fmt.Sprintf("%s: rows %d != manifest %d", tm.Name, lines, tm.Rows))
		}
	}
	/* 包级 manifest 哈希与任务记录一致（任务记录主库优先，明文库兜底） */
	manifestHash := sha256.Sum256(body)
	_, _, recorded, err := jobRowOf(ctx, store, tenantID, jobID)
	if err != nil {
		return nil, err
	}
	if recorded != hex.EncodeToString(manifestHash[:]) {
		result.OK = false
		result.Mismatch = append(result.Mismatch, "manifest: sha256 mismatch with job record")
	}
	return result, nil
}

/* jobRowOf 读取备份任务记录：主库 yzws-v1-sec 优先，明文库（对账副本）兜底；
 * 兼容双写切换前仅明文库存在记录的历史任务。 */
func jobRowOf(ctx context.Context, store *datastore.Store, tenantID, jobID string) (dir, status, manifestSHA string, err error) {
	for _, pool := range []*pgxpool.Pool{store.Secure, store.Plain} {
		if pool == nil {
			continue
		}
		err = pool.QueryRow(ctx, `SELECT storage_key,status,COALESCE(manifest_sha256,'') FROM backup_job WHERE tenant_id=$1 AND id=$2`,
			tenantID, jobID).Scan(&dir, &status, &manifestSHA)
		if err == nil {
			return dir, status, manifestSHA, nil
		}
		if err != pgx.ErrNoRows {
			return "", "", "", err
		}
	}
	return "", "", "", fmt.Errorf("backup job not found")
}

func jobDirOf(ctx context.Context, store *datastore.Store, tenantID, jobID string) (string, string, error) {
	dir, status, _, err := jobRowOf(ctx, store, tenantID, jobID)
	if err != nil {
		return "", "", err
	}
	return dir, status, nil
}

/* IssueDownloadToken 生成一次性下载凭证（10 分钟有效；只存哈希）。 */
func IssueDownloadToken(ctx context.Context, store *datastore.Store, tenantID, jobID string) (string, error) {
	dir, status, err := jobDirOf(ctx, store, tenantID, jobID)
	if err != nil {
		return "", err
	}
	if status != "succeeded" {
		return "", fmt.Errorf("backup job status=%s, cannot download", status)
	}
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		return "", fmt.Errorf("backup package missing")
	}
	token := platform.Token(32)
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	expires := platform.Now() + int64((10 * time.Minute).Microseconds())
	/* 未下载过才允许签发新凭证；下载一次后凭证通道关闭（设计 3.9.4 一次性）。
	 * 两库同写：主库为准，明文库对账副本保持同步。 */
	updateSQL := `UPDATE backup_job SET download_token_hash=$3,download_expires_at=$4
		WHERE tenant_id=$1 AND id=$2 AND downloaded_at IS NULL`
	affected := int64(0)
	for _, pool := range []*pgxpool.Pool{store.Secure, store.Plain} {
		if pool == nil {
			continue
		}
		result, err := pool.Exec(ctx, updateSQL, tenantID, jobID, tokenHash, expires)
		if err != nil {
			return "", err
		}
		affected += result.RowsAffected()
	}
	if affected == 0 {
		return "", fmt.Errorf("backup package already downloaded (one-time)")
	}
	return token, nil
}

/* ConsumeDownloadToken 校验并消费下载凭证：返回备份目录；错误表示拒绝。 */
func ConsumeDownloadToken(ctx context.Context, store *datastore.Store, tenantID, jobID, token string) (string, error) {
	dir, status, err := jobDirOf(ctx, store, tenantID, jobID)
	if err != nil {
		return "", err
	}
	if status != "succeeded" {
		return "", fmt.Errorf("backup job status=%s, cannot download", status)
	}
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(token))))
	consumeSQL := `UPDATE backup_job SET downloaded_at=$4
		WHERE tenant_id=$1 AND id=$2 AND download_token_hash=$3 AND downloaded_at IS NULL AND download_expires_at>$5`
	affected := int64(0)
	for _, pool := range []*pgxpool.Pool{store.Secure, store.Plain} {
		if pool == nil {
			continue
		}
		result, err := pool.Exec(ctx, consumeSQL, tenantID, jobID, tokenHash, platform.Now(), platform.Now())
		if err != nil {
			return "", err
		}
		affected += result.RowsAffected()
	}
	if affected == 0 {
		return "", fmt.Errorf("download token invalid, expired or already used")
	}
	return dir, nil
}

/* SortKeys 便于稳定输出（保留：manifest 内表顺序即导出顺序，已排序）。 */
func SortKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
