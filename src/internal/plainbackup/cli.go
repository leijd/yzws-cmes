package plainbackup

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"clothp/internal/datastore"
)

var safeTableName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

/* VerifyDir 按目录校验备份包（设计 3.9.3/3.9.4）：行数 + 文件 SHA-256，不依赖数据库任务记录，
 * 供运维在迁移/异地恢复前离线校验。 */
func VerifyDir(ctx context.Context, dir string) (*VerifyResult, error) {
	result := &VerifyResult{OK: true}
	var m manifest
	body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("读取 manifest.json 失败: %w", err)
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("manifest.json 解析失败: %w", err)
	}
	if m.FormatVersion == "" {
		result.OK = false
		result.Mismatch = append(result.Mismatch, "manifest.json 缺少 format_version")
	}
	for _, table := range m.Tables {
		if !safeTableName.MatchString(table.Name) {
			result.OK = false
			result.Mismatch = append(result.Mismatch, "非法表名: "+table.Name)
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, table.Name+".jsonl"))
		if err != nil {
			result.OK = false
			result.Mismatch = append(result.Mismatch, table.Name+": 文件缺失")
			continue
		}
		sum := sha256.Sum256(data)
		actual := hex.EncodeToString(sum[:])
		if actual != table.SHA256 {
			result.OK = false
			result.Mismatch = append(result.Mismatch, table.Name+": SHA-256 不匹配")
			continue
		}
		var lines int64
		for _, line := range splitLines(data) {
			if len(line) > 0 {
				lines++
			}
		}
		if lines != table.Rows {
			result.OK = false
			result.Mismatch = append(result.Mismatch, fmt.Sprintf("%s: 行数 %d 与清单 %d 不一致", table.Name, lines, table.Rows))
			continue
		}
		result.Checked++
	}
	return result, nil
}

func splitLines(data []byte) [][]byte {
	lines := [][]byte{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Bytes())
	}
	return lines
}

/* Restore 把明文备份包导回明文库（设计 3.9.4 restore-plain）：
 * 先离线校验整包，再按清单顺序逐行 jsonb_populate_record 写入；
 * 仅导入 tenantID 范围内的行，已存在主键跳过（ON CONFLICT DO NOTHING），可重复执行。 */
func Restore(ctx context.Context, store *datastore.Store, dir, tenantID string, dryRun bool) (map[string]any, error) {
	verify, err := VerifyDir(ctx, dir)
	if err != nil {
		return nil, err
	}
	if !verify.OK {
		return nil, fmt.Errorf("备份包校验失败: %v", verify.Mismatch)
	}
	body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, err
	}
	if tenantID == "" {
		tenantID = m.TenantID
	}
	if tenantID == "" {
		return nil, fmt.Errorf("备份包未记录租户，请用 -backup-tenant 指定")
	}
	summary := map[string]any{"tenant_id": tenantID, "dry_run": dryRun, "tables": map[string]int64{}, "restored": int64(0)}
	tableCounts := summary["tables"].(map[string]int64)
	/* 事务内延迟外键检查：备份文件按表名排序，父子表可能乱序 */
	tx, err := store.Plain.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET CONSTRAINTS ALL DEFERRED"); err != nil {
		return nil, err
	}
	for _, table := range m.Tables {
		if !safeTableName.MatchString(table.Name) {
			return nil, fmt.Errorf("非法表名: %s", table.Name)
		}
		data, err := os.ReadFile(filepath.Join(dir, table.Name+".jsonl"))
		if err != nil {
			return nil, err
		}
		var inserted int64
		scanner := bufio.NewScanner(bytes.NewReader(data))
		scanner.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var row map[string]any
			if err := json.Unmarshal(line, &row); err != nil {
				return nil, fmt.Errorf("%s 行解析失败: %w", table.Name, err)
			}
			if rowTenant, ok := row["tenant_id"].(string); ok && rowTenant != tenantID {
				continue
			}
			if dryRun {
				inserted++
				continue
			}
			/* 使用复合类型自动映射列，避免手写列清单与表结构漂移 */
			tag, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s SELECT * FROM jsonb_populate_record(NULL::%s, $1::jsonb) ON CONFLICT DO NOTHING`, table.Name, table.Name), string(line))
			if err != nil {
				return nil, fmt.Errorf("%s 导入失败: %w", table.Name, err)
			}
			inserted += tag.RowsAffected()
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		tableCounts[table.Name] = inserted
	}
	if dryRun {
		_ = tx.Rollback(ctx)
	} else if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	for _, count := range tableCounts {
		summary["restored"] = summary["restored"].(int64) + count
	}
	return summary, nil
}
