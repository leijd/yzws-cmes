package tech

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/orgapp"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* registerLineResource 产线资源配置（设计 4.4 / 5.9）：固定配置与动态配置，
 * 同一产线同一时刻仅一条 active 配置，历史配置置 expired 保留可追溯。 */
func registerLineResource(api *httpapi.API) {
	/* 当前生效配置 */
	api.Handle("GET /api/v1/lines/{id}/resource", "line:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		lineID := r.PathValue("id")
		if err := ensureLineAccess(r.Context(), api, p, lineID); err != nil {
			return err
		}
		item := map[string]any{}
		err := api.DB.QueryRow(r.Context(), `SELECT row_to_json(t) FROM (
			SELECT id,line_org_id,config_type,COALESCE(work_order_id,''),COALESCE(manager_employee_id,''),
				member_employee_ids,equipment_ids,COALESCE(process_standard_id,''),quality_ref,delivery_ref,
				COALESCE(valid_from,0),COALESCE(remark,''),status,version,created_at,updated_at
			FROM line_resource WHERE tenant_id=$1 AND line_org_id=$2 AND status='active'
			ORDER BY created_at DESC LIMIT 1) t`, p.TenantID, lineID).Scan(&item)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.JSON(w, http.StatusOK, map[string]any{"resource": nil})
		}
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"resource": item})
	})
	/* 配置历史 */
	api.Handle("GET /api/v1/lines/{id}/resources", "line:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		lineID := r.PathValue("id")
		if err := ensureLineAccess(r.Context(), api, p, lineID); err != nil {
			return err
		}
		rows, err := api.DB.Query(r.Context(), `SELECT id,config_type,COALESCE(work_order_id,''),COALESCE(manager_employee_id,''),
			member_employee_ids,status,COALESCE(valid_from,0),COALESCE(valid_to,0),created_at
			FROM line_resource WHERE tenant_id=$1 AND line_org_id=$2 ORDER BY created_at DESC LIMIT 100`, p.TenantID, lineID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, configType, workOrderID, managerID, status string
			var members []byte
			var validFrom, validTo, createdAt int64
			if err := rows.Scan(&id, &configType, &workOrderID, &managerID, &members, &status, &validFrom, &validTo, &createdAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "config_type": configType, "work_order_id": workOrderID,
				"manager_employee_id": managerID, "member_employee_ids": json.RawMessage(members), "status": status,
				"valid_from": validFrom, "valid_to": validTo, "created_at": createdAt})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
	/* 保存配置（固定/动态）：校验产线、线长、成员、工单归属后整体替换生效配置 */
	api.Handle("PUT /api/v1/lines/{id}/resource", "line:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ConfigType        string   `json:"config_type"`
			WorkOrderID       string   `json:"work_order_id"`
			ManagerEmployeeID string   `json:"manager_employee_id"`
			MemberEmployeeIDs []string `json:"member_employee_ids"`
			EquipmentIDs      []string `json:"equipment_ids"`
			ProcessStandardID string   `json:"process_standard_id"`
			Remark            string   `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		lineID := r.PathValue("id")
		if err := ensureLineAccess(ctx, api, p, lineID); err != nil {
			return err
		}
		var lineStatus string
		if err := api.DB.QueryRow(ctx, `SELECT status FROM org_unit WHERE tenant_id=$1 AND id=$2 AND org_type='line'`, p.TenantID, lineID).Scan(&lineStatus); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "产线不存在")
		}
		if lineStatus != "active" {
			return httpapi.Error(http.StatusConflict, "LINE_INACTIVE", "产线未启用，不能配置资源")
		}
		configType := strings.TrimSpace(in.ConfigType)
		if configType == "" {
			configType = "fixed"
		}
		if configType != "fixed" && configType != "dynamic" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "配置类型必须是 fixed 或 dynamic")
		}
		workOrderID := strings.TrimSpace(in.WorkOrderID)
		if configType == "dynamic" {
			if workOrderID == "" {
				return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "动态配置必须指定生产工单")
			}
			var ok bool
			if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM work_order WHERE tenant_id=$1 AND id=$2)`, p.TenantID, workOrderID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return httpapi.Error(http.StatusBadRequest, "WORK_ORDER_NOT_FOUND", "生产工单不存在")
			}
		} else {
			workOrderID = ""
		}
		managerID := strings.TrimSpace(in.ManagerEmployeeID)
		if managerID != "" {
			var active bool
			if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND id=$2 AND status='active')`, p.TenantID, managerID).Scan(&active); err != nil {
				return err
			}
			if !active {
				return httpapi.Error(http.StatusBadRequest, "MANAGER_INVALID", "线长必须是本厂在职员工")
			}
		}
		members := make([]string, 0, len(in.MemberEmployeeIDs))
		seen := map[string]bool{}
		for _, id := range in.MemberEmployeeIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			members = append(members, id)
		}
		if len(members) > 0 {
			var found int
			if err := api.DB.QueryRow(ctx, `SELECT COUNT(*) FROM employee WHERE tenant_id=$1 AND status='active' AND id = ANY($2)`, p.TenantID, members).Scan(&found); err != nil {
				return err
			}
			if found != len(members) {
				return httpapi.Error(http.StatusBadRequest, "MEMBER_INVALID", "配置成员必须全部为本厂在职员工")
			}
		}
		membersJSON, _ := json.Marshal(members)
		equipmentJSON, _ := json.Marshal(in.EquipmentIDs)
		now := platform.Now()
		resourceID := platform.ID()
		err := withDualTx(ctx, api, p.TenantID, "line_resource", resourceID, func(tx *dualwrite.Tx) error {
			/* 旧配置置 expired，保证「同一产线同一时刻仅一条 active」 */
			if err := execBoth(ctx, tx, `UPDATE line_resource SET status='expired',valid_to=$3,version=version+1,updated_at=$3,updated_by=$4,updated_node=$5
				WHERE tenant_id=$1 AND line_org_id=$2 AND status='active'`, p.TenantID, lineID, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			if err := execBoth(ctx, tx, `INSERT INTO line_resource(tenant_id,id,line_org_id,config_type,work_order_id,manager_employee_id,
				member_employee_ids,equipment_ids,process_standard_id,quality_ref,delivery_ref,valid_from,status,remark,
				created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
				VALUES($1,$2,$3,$4,NULLIF($5,''),NULLIF($6,''),$7,$8,NULLIF($9,''),'{}','{}',$10,'active',$11,$10,$12,$13,$10,$12,$13,1)`,
				p.TenantID, resourceID, lineID, configType, workOrderID, managerID, membersJSON, equipmentJSON, in.ProcessStandardID,
				now, in.Remark, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			return httpapi.Audit(ctx, tx.Plain(), p, api.Config.NodeID, "configure_line_resource", "line_resource", resourceID, nil, in, "")
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": resourceID, "saved": true})
	})
}

/* ensureLineAccess 产线数据范围校验（设计 5.7）：超范围返回 403 并写 access_denied 审计。 */
func ensureLineAccess(ctx context.Context, api *httpapi.API, p auth.Principal, lineID string) error {
	access, err := orgapp.ComputeAccess(ctx, api.DB, p)
	if err != nil {
		return err
	}
	var workshopID string
	if err := api.DB.QueryRow(ctx, `SELECT COALESCE(parent_id,'') FROM org_unit WHERE tenant_id=$1 AND id=$2 AND org_type='line'`, p.TenantID, lineID).Scan(&workshopID); err != nil {
		return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "产线不存在")
	}
	if !access.AllowLine(lineID, workshopID) {
		api.RecordDenied(ctx, p, "org_unit", lineID, "超出数据范围操作产线资源配置")
		return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "无权操作该产线")
	}
	return nil
}

/* withDualTx 双写事务封装（与组织模块保持一致）。 */
func withDualTx(ctx context.Context, api *httpapi.API, tenantID, aggregateType, aggregateID string, fn func(tx *dualwrite.Tx) error) error {
	tx, err := api.Writer.Begin(ctx, tenantID)
	if err != nil {
		return err
	}
	tx.MarkAggregate(aggregateType, aggregateID)
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

/* execBoth 无敏感列的表：同一语句写两库。 */
func execBoth(ctx context.Context, tx *dualwrite.Tx, sql string, args ...any) error {
	if tx.Plain() != nil {
		if _, err := tx.Plain().Exec(ctx, sql, args...); err != nil {
			return err
		}
	}
	if tx.Secure() != nil {
		if _, err := tx.Secure().Exec(ctx, sql, args...); err != nil {
			return err
		}
	}
	return nil
}
