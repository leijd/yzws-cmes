// Package outsource 外协管理域（设计 4.25）：外协厂商档案、跨租户协作授权、
// 委外单/明细/物料流水/进度投影/结算、受托侧承接（assigned/incoming）。
// 建模原则：业务单据单一租户归属；跨租户可见性仅经 tenant_collaboration 显式授权；
// 对方可见数据只走白名单投影（不含工价/成本/人员）。
package outsource

import (
	"context"
	"fmt"
	"time"

	"clothp/internal/httpapi"
	"github.com/jackc/pgx/v5"
)

func Register(api *httpapi.API) {
	registerPartners(api)
	registerCollaborations(api)
	registerOrders(api)
	registerSettlements(api)
	registerAssigned(api)
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

/* nextSeqNo 业务单号：前缀+yyyyMM+3位流水（WW 委外单 / JS 结算单 / ST 受托项目 / SG 受托工单）。 */
func nextSeqNo(ctx context.Context, q querier, tenantID, table, column, prefix string) (string, error) {
	ym := time.Now().Format("200601")
	like := prefix + ym + "%"
	var n int
	if err := q.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE tenant_id=$1 AND %s LIKE $2", table, column),
		tenantID, like).Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%03d", prefix, ym, n+1), nil
}
