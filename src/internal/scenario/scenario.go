// Package scenario 业务场景分类与场景化菜单（设计 v0.02-002 第 6.12 / 4.19 / 5.17 / 11 章）。
//
// 提供：场景元数据与数据规则、裁床单两级录入、场景向导、员工薪水分析、
// 外协双视角标识数据、直播取证会话。所有接口按 tenant_id 隔离，权限点由 079 迁移注册。
package scenario

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
)

func Register(api *httpapi.API) {
	registerScenes(api)
	registerCuttingSheets(api)
	registerIntake(api)
	registerSalary(api)
	registerShipments(api)
	registerLiveEvidence(api)
}

/* ---------- 公共助手 ---------- */

func platformNow() int64    { return platform.Now() }
func platformNewID() string { return platform.ID() }

/* requireAll：Handle 的 permission 参数是逗号 OR 语义（任一满足即放行），
 * 设计 5.17/6.12.8 中标注「A + B」的写操作必须改为显式 AND 校验，避免只读权限越权写库。 */
func requireAll(p auth.Principal, codes ...string) error {
	for _, code := range codes {
		if !p.Permissions[code] {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "缺少必要权限："+code)
		}
	}
	return nil
}

/* auditWrite：无事务的轻量写（自动保存草稿、打印任务等）同样落审计（设计 5.1）。
 * 与事务内写共用同一 audit_log 结构，after 只记聚合标识与计数，不记业务明文。 */
func auditWrite(ctx context.Context, api *httpapi.API, p auth.Principal, action, ownerType, ownerID string, after any) error {
	tx, err := api.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, action, ownerType, ownerID, nil, after, ""); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func jsonString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func validScene(t, sub string) bool {
	if t != "A" && t != "B" && t != "C" {
		return false
	}
	switch sub {
	case "A1", "A2", "A3", "B1", "B2", "C1":
		return strings.HasPrefix(sub, t)
	}
	return false
}

func sceneOrderSource(t string) string {
	if t == "A" {
		return "own"
	}
	return "contracted"
}

func isOutsourceScene(t string) bool { return t == "B" || t == "C" }

/* ---------- 场景元数据与数据规则（6.12.1 / 6.12.7） ---------- */

func registerScenes(api *httpapi.API) {
	api.Handle("GET /api/v1/scenes", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": sceneDefinitions()})
	})

	api.Handle("GET /api/v1/scenes/rules", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": outsourceDataRules()})
	})

	/* 项目工单列表：场景徽标 + 本厂/外协分组（委托方视角 6.12.7） */
	api.Handle("GET /api/v1/scene/work-orders", "work_order:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "w.tenant_id=$1"
		if v := r.URL.Query().Get("scene_type"); v != "" {
			args = append(args, v)
			where += " AND w.scene_type=$" + itoa(len(args))
		}
		if v := r.URL.Query().Get("state"); v != "" {
			switch v {
			case "running":
				where += " AND w.status IN ('pending_release','in_production','paused')"
			case "history":
				where += " AND w.status IN ('completed','cancelled')"
			case "new":
				where += " AND w.status='draft'"
			}
		}
		args = append(args, 200)
		rows, err := api.DB.Query(r.Context(), `SELECT w.id,w.work_order_code,w.scene_type,w.scene_sub,w.order_source,
			COALESCE(w.source_order_ref,''),w.status,COALESCE(pr.project_code,''),COALESCE(c.name,''),
			w.planned_product_qty,w.version
			FROM work_order w
			LEFT JOIN project pr ON pr.tenant_id=w.tenant_id AND pr.id=w.project_id
			LEFT JOIN customer c ON c.tenant_id=pr.tenant_id AND c.id=pr.customer_id
			WHERE `+where+` ORDER BY w.id DESC LIMIT $`+itoa(len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, st, sub, src, ref, status, projectCode, customer string
			var qty, version int64
			if err := rows.Scan(&id, &code, &st, &sub, &src, &ref, &status, &projectCode, &customer, &qty, &version); err != nil {
				return err
			}
			items = append(items, map[string]any{
				"id": id, "work_order_code": code, "scene_type": st, "scene_sub": sub,
				"order_source": src, "source_order_ref": ref, "status": status,
				"project_code": projectCode, "counterparty": customer, "planned_product_qty": qty,
				"version": version, "party": partyOf(st, src), "outsource": isOutsourceScene(st) || src == "contracted",
			})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
}

func partyOf(scene, source string) string {
	if source == "contracted" || isOutsourceScene(scene) {
		return "外协"
	}
	return "本厂"
}

func sceneDefinitions() []map[string]any {
	return []map[string]any{
		{"scene_type": "A", "label": "接服贸客商的单", "color": "#2F6BFF", "badge": "A", "subs": []map[string]any{
			{"scene_sub": "A1", "label": "自己生产"},
			{"scene_sub": "A2", "label": "外包部分业务"},
			{"scene_sub": "A3", "label": "外包部分工序"},
		}},
		{"scene_type": "B", "label": "接外包单", "color": "#F59E0B", "badge": "B", "subs": []map[string]any{
			{"scene_sub": "B1", "label": "完全自己生产"},
			{"scene_sub": "B2", "label": "外包部分工序"},
		}},
		{"scene_type": "C", "label": "接外包工序", "color": "#8B5CF6", "badge": "C", "subs": []map[string]any{
			{"scene_sub": "C1", "label": "完全自己生产"},
		}},
	}
}

func outsourceDataRules() []map[string]any {
	return []map[string]any{
		{"scene_sub": "A2", "auto_fill": []string{"委外数量", "款号", "交期"}, "required": []string{"受托方", "数量", "单价"}, "allow_empty": []string{"唛架", "部位"}},
		{"scene_sub": "A3", "auto_fill": []string{"受托工序", "数量"}, "required": []string{"受托方", "工序", "单价"}, "allow_empty": []string{"产品图样"}},
		{"scene_sub": "B1", "auto_fill": []string{"场景属性 contracted", "客户=委托方"}, "required": []string{"对方单号", "款号", "数量", "交期"}, "allow_empty": []string{"产品图", "裁床"}},
		{"scene_sub": "B2", "auto_fill": []string{"场景属性 contracted", "客户=委托方"}, "required": []string{"对方单号", "款号", "数量", "交期"}, "allow_empty": []string{"产品图", "裁床"}},
		{"scene_sub": "C1", "auto_fill": []string{"受托工序快照"}, "required": []string{"对方单号", "工序段", "数量"}, "allow_empty": []string{"产品信息", "裁床产品字段"}},
	}
}

func itoa(n int) string {
	if n < 0 {
		return "0"
	}
	const digits = "0123456789"
	if n < 10 {
		return string(digits[n : n+1])
	}
	return itoa(n/10) + string(digits[n%10:n%10+1])
}

func nowID() (string, int64) { return platformNewID(), platformNow() }
