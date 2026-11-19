package scenario

import (
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/orgapp"
)

/* 分析中心 · 员工薪水分析（设计 6.12.2 / 6.12.9）：
 * 薪水情况（工资单汇总）+ 工作量情况（报工件数/工时/合格率），
 * 按 scene_type 分 Tab 或筛选；数据范围由 5.7 与 salary:read 权限约束。 */

func registerSalary(api *httpapi.API) {
	api.Handle("GET /api/v1/analysis/salary", "salary:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		period := strings.TrimSpace(r.URL.Query().Get("period"))
		scene := strings.TrimSpace(r.URL.Query().Get("scene_type"))
		employeeID := strings.TrimSpace(r.URL.Query().Get("employee_id"))
		/* 强制接入 5.7 数据范围：车间/产线/班组长只看本范围人员，SelfOnly 仅本人，
		 * 与 payroll_wide.go 的工资查询基线一致；employee_id 参数不能越过该范围。 */
		access, err := orgapp.ComputeAccess(r.Context(), api.DB, p)
		if err != nil {
			return err
		}
		baseArgs := []any{p.TenantID, period, employeeID}
		scopeCond, scopeArgs := access.OrgScopeSQL("e", len(baseArgs)+1)
		args := append(append([]any{}, baseArgs...), scopeArgs...)
		rows, err := api.DB.Query(r.Context(), `SELECT e.id,e.employee_no,e.name,
			COALESCE((SELECT SUM(ps.gross_amount) FROM payroll_sheet ps WHERE ps.tenant_id=e.tenant_id AND ps.employee_id=e.id
				AND ($2='' OR ps.payroll_period=$2)),0)::text,
			COALESCE((SELECT SUM(ps.net_amount) FROM payroll_sheet ps WHERE ps.tenant_id=e.tenant_id AND ps.employee_id=e.id
				AND ($2='' OR ps.payroll_period=$2)),0)::text,
			COALESCE((SELECT SUM(ps.paid_amount) FROM payroll_sheet ps WHERE ps.tenant_id=e.tenant_id AND ps.employee_id=e.id
				AND ($2='' OR ps.payroll_period=$2)),0)::text,
			COALESCE((SELECT COUNT(*) FROM payroll_sheet ps WHERE ps.tenant_id=e.tenant_id AND ps.employee_id=e.id
				AND ($2='' OR ps.payroll_period=$2)),0),
			COALESCE((SELECT SUM(wr.good_qty) FROM work_report wr WHERE wr.tenant_id=e.tenant_id AND wr.employee_id=e.id
				AND wr.review_status='approved'),0),
			COALESCE((SELECT SUM(wr.defect_qty) FROM work_report wr WHERE wr.tenant_id=e.tenant_id AND wr.employee_id=e.id
				AND wr.review_status='approved'),0),
			COALESCE((SELECT COUNT(*) FROM work_report wr WHERE wr.tenant_id=e.tenant_id AND wr.employee_id=e.id),0),
			COALESCE((SELECT SUM(wr.standard_work_seconds) FROM work_report wr WHERE wr.tenant_id=e.tenant_id AND wr.employee_id=e.id
				AND wr.review_status='approved'),0)
			FROM employee e WHERE e.tenant_id=$1 AND ($3='' OR e.id=$3)`+scopeCond+`
			ORDER BY e.employee_no LIMIT 200`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, name, gross, net, paid string
			var sheetCount, goodQty, defectQty, reportCount, seconds int64
			if err := rows.Scan(&id, &no, &name, &gross, &net, &paid, &sheetCount, &goodQty, &defectQty, &reportCount, &seconds); err != nil {
				return err
			}
			passRate := 0.0
			if goodQty+defectQty > 0 {
				passRate = float64(goodQty) / float64(goodQty+defectQty)
			}
			items = append(items, map[string]any{"employee_id": id, "employee_no": no, "name": name,
				"gross_amount": gross, "net_amount": net, "paid_amount": paid, "sheet_count": sheetCount,
				"good_qty": goodQty, "defect_qty": defectQty, "report_count": reportCount,
				"standard_work_seconds": seconds, "standard_work_hours": float64(seconds) / 3600,
				"pass_rate": passRate, "period": period, "scene_type": scene})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "period": period, "scene_type": scene,
			"scenes": sceneDefinitions()})
	})

	/* 分析中心 · 场景维度进度/工资/工作量合计（6.12.2 按场景扩展统计视图） */
	api.Handle("GET /api/v1/analysis/scene-summary", "salary:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		/* 全厂工单合计属薪酬分析口径：只用 salary:read 单点鉴权，
		 * 既消除原 OR 语义让 progress:read 越权取数，也避免存量租户因未回填 progress:read 被误挡。 */
		args := []any{p.TenantID}
		where := "w.tenant_id=$1"
		if v := strings.TrimSpace(r.URL.Query().Get("scene_type")); v != "" {
			args = append(args, v)
			where += fmt.Sprintf(" AND w.scene_type=$%d", len(args))
		}
		rows, err := api.DB.Query(r.Context(), `SELECT w.scene_type,w.scene_sub,COUNT(*),SUM(w.planned_product_qty),
			SUM(CASE WHEN w.status IN ('pending_release','in_production','paused') THEN 1 ELSE 0 END),
			SUM(CASE WHEN w.status='completed' THEN 1 ELSE 0 END)
			FROM work_order w WHERE `+where+` GROUP BY w.scene_type,w.scene_sub ORDER BY w.scene_type,w.scene_sub`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var st, sub string
			var count, qty, running, completed int64
			if err := rows.Scan(&st, &sub, &count, &qty, &running, &completed); err != nil {
				return err
			}
			items = append(items, map[string]any{"scene_type": st, "scene_sub": sub, "work_order_count": count,
				"planned_qty": qty, "running_count": running, "completed_count": completed,
				"party": partyOf(st, "own"), "outsource": isOutsourceScene(st)})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
}
