// Package codegen 自动编码生成：{编码类型名拼音首字母}-{5位流水}。
//
// 规则（需求）：
//   - 所有设计文档未特别规定编码格式的实体，新建时编码留空即自动生成，填了仍用手填值；
//   - 前缀 = 编码类型名拼音首字母（车间→CJ、产线→CX、厂检→CJ 冲突→CJ1），前缀在
//     code_prefix 表全局注册（先到先得，冲突追加数字后缀），平台级资源；
//   - 流水号 5 位、按前缀各自连续（CJ-00001…、CJ1-00001… 独立计数），按租户隔离；
//   - 工号（{租户标识}-流水）、业务单号（GZB 等）、租户编码/短码在设计文档中有专门规则，不走本包。
package codegen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"clothp/internal/dualwrite"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mozillazg/go-pinyin"
)

/* preferredTypes 已知编码类型（迁移 065 已种子注册）：owner_code → 类型中文名（拼音首字母来源）。 */
var preferredTypes = map[string]string{
	"org.workshop":          "车间",
	"org.line":              "产线",
	"org.team":              "班组",
	"masterdata.customer":   "客户",
	"masterdata.supplier":   "供应商",
	"masterdata.warehouse":  "仓库",
	"masterdata.material":   "物料",
	"masterdata.dictionary": "字典项",
	"production.product":    "产品",
	"production.process":    "工序",
}

/* PinyinInitials 取中文名的拼音首字母大写（拉丁字母/数字原样保留）。 */
func PinyinInitials(name string) string {
	args := pinyin.NewArgs()
	args.Style = pinyin.FirstLetter
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 128 && ((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')):
			b.WriteRune(unicode.ToUpper(r))
		case unicode.Is(unicode.Han, r):
			if py := pinyin.Pinyin(string(r), args); len(py) > 0 && len(py[0]) > 0 && py[0][0] != "" {
				b.WriteString(strings.ToUpper(string([]rune(py[0][0])[0])))
			}
		}
	}
	return b.String()
}

type dbtx interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

/* prefixCandidates 期望前缀 → 冲突时的候选序列：期望、+1..+9、+10..+99。
 * 确定性顺序（需求"后跟随机数字"的可复现实现：在空闲候选中按序选取）。 */
func prefixCandidates(desired string) []string {
	if desired == "" {
		desired = "CODE"
	}
	out := []string{desired}
	for i := 1; i <= 9; i++ {
		out = append(out, fmt.Sprintf("%s%d", desired, i))
	}
	for i := 10; i <= 99; i++ {
		out = append(out, fmt.Sprintf("%s%d", desired, i))
	}
	return out
}

/* resolvePrefix 取该类型的已注册前缀；未注册时按拼音首字母占用可用前缀并注册
 * （期望前缀被占则依次追加数字后缀 1-9、10-99——确定性顺序，效果等价于在空闲候选中选取）。 */
func resolvePrefix(ctx context.Context, db dbtx, writer *dualwrite.Writer, ownerCode string) (string, error) {
	var prefix string
	err := db.QueryRow(ctx, `SELECT prefix FROM code_prefix WHERE owner_code=$1`, ownerCode).Scan(&prefix)
	if err == nil {
		return prefix, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	typeName := preferredTypes[ownerCode]
	if typeName == "" {
		/* 未预置的新类型：退化为 owner 末段大写 */
		typeName = strings.ToUpper(ownerCode[strings.LastIndex(ownerCode, ".")+1:])
	}
	for _, candidate := range prefixCandidates(PinyinInitials(typeName)) {
		var taken bool
		if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM code_prefix WHERE prefix=$1)`, candidate).Scan(&taken); err != nil {
			return "", err
		}
		if taken {
			continue
		}
		/* 占用：writer 可用时双写两库（平台级事务），单库模式只写读主库 */
		now := platform.Now()
		if writer != nil && writer.Store() != nil && writer.Store().Plain != nil && writer.Store().Secure != nil {
			tx, err := writer.Begin(ctx, "")
			if err != nil {
				return "", err
			}
			defer tx.Rollback(ctx)
			if err := dualwrite.ExecBoth(ctx, tx,
				`INSERT INTO code_prefix(prefix,owner_code,owner_name,created_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
				candidate, ownerCode, typeName, now); err != nil {
				return "", err
			}
			if err := tx.Commit(ctx); err != nil {
				return "", err
			}
		} else {
			if _, err := db.Exec(ctx, `INSERT INTO code_prefix(prefix,owner_code,owner_name,created_at) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, candidate, ownerCode, typeName, now); err != nil {
				return "", err
			}
		}
		/* 并发下可能被别人抢先注册：提交后复读确认归属，失败则继续尝试下一个候选 */
		var owner string
		if err := db.QueryRow(ctx, `SELECT owner_code FROM code_prefix WHERE prefix=$1`, candidate).Scan(&owner); err == nil && owner == ownerCode {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("code prefix exhausted for %s", ownerCode)
}

/* Generate 为留空的编码字段生成 {前缀}-{5位流水}。
 * table/column 为内部常量（非用户输入）；serial 取该租户下该前缀现有最大流水 +1，
 * 并发冲突由业务表唯一索引兜底（调用方按既有 409 处理）。 */
func Generate(ctx context.Context, db *pgxpool.Pool, writer *dualwrite.Writer, table, column, tenantID, ownerCode string) (string, error) {
	prefix, err := resolvePrefix(ctx, db, writer, ownerCode)
	if err != nil {
		return "", err
	}
	/* 前缀仅含大写字母/数字，可安全拼入 LIKE 与正则 */
	var next int
	err = db.QueryRow(ctx, fmt.Sprintf(
		`SELECT COALESCE(MAX((regexp_match(%s, '^%s-([0-9]{1,5})$'))[1]::int),0)+1 FROM %s WHERE tenant_id=$1 AND %s LIKE '%s-%%'`,
		column, prefix, table, column, prefix), tenantID).Scan(&next)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%05d", prefix, next), nil
}
