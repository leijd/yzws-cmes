package orgapp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/codegen"
	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* Register 车间/产线专用 API（设计 5.2/5.3）。编码分类型唯一：车间码在车间内唯一，
 * 产线码仅在所属车间内唯一（跨车间允许同码，4.2/附录 A.7）；API 一律以 ID 定位。 */
func Register(api *httpapi.API) {
	registerWorkshops(api)
	registerLines(api)
}

/* withDualTx 双写事务助手：组织表无敏感列，同一 SQL 在两库各执行一次后 2PC 提交；
 * 单库/单写降级时仅写目标库。写失败回滚，COMMIT 阶段失败如实返回（恢复任务兜底）。 */
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

/* execBoth 在无敏感列的表上把同一语句写入两库（本事务内）。 */
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

func registerWorkshops(api *httpapi.API) {
	/* 本厂车间列表：含直接产线数与当前人数（来自 employee_org_binding，设计 4.3） */
	api.Handle("GET /api/v1/workshops", "workshop:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		access, err := ComputeAccess(r.Context(), api.DB, p)
		if err != nil {
			return err
		}
		name := "%" + strings.TrimSpace(r.URL.Query().Get("name")) + "%"
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		statusCond := ""
		if status != "" {
			statusCond = " AND o.status='" + status + "'"
		}
		scopeCond, scopeArgs := workshopScopeSQL(access, 1)
		args := append([]any{p.TenantID, name}, scopeArgs...)
		rows, err := api.DB.Query(r.Context(), `SELECT o.id,o.code,o.name,o.status,o.manager_employee_id,o.workshop_nature,o.outsourced_partner,
			o.established_at,o.capacity_note,o.remark,o.version,
			(SELECT COUNT(*) FROM org_unit l WHERE l.tenant_id=o.tenant_id AND l.parent_id=o.id AND l.org_type='line' AND l.status='active') AS line_count,
			(SELECT COUNT(*) FROM employee_org_binding b WHERE b.tenant_id=o.tenant_id AND b.workshop_org_id=o.id AND b.is_primary=TRUE AND b.valid_to IS NULL) AS employee_count
			FROM org_unit o WHERE o.tenant_id=$1 AND o.org_type='workshop' AND o.name LIKE $2`+statusCond+scopeCond+` ORDER BY o.code`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, status, nature, remark string
			var managerID, partner, capacity *string
			var established *int64
			var version, lineCount, employeeCount int64
			if err := rows.Scan(&id, &code, &name, &status, &managerID, &nature, &partner, &established, &capacity, &remark, &version, &lineCount, &employeeCount); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "status": status,
				"manager_employee_id": managerID, "workshop_nature": nature, "outsourced_partner": partner,
				"established_at": established, "capacity_note": capacity, "remark": remark, "version": version,
				"line_count": lineCount, "employee_count": employeeCount})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	/* 新增车间：编码在车间类型内唯一；负责人必须是本厂 active 管理人员 */
	api.Handle("POST /api/v1/workshops", "workshop:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Code              string `json:"code"`
			Name              string `json:"name"`
			ManagerEmployeeID string `json:"manager_employee_id"`
			Status            string `json:"status"`
			WorkshopNature    string `json:"workshop_nature"`
			OutsourcedPartner string `json:"outsourced_partner"`
			Remark            string `json:"remark"`
			EstablishedAt     *int64 `json:"established_at"`
			CapacityNote      string `json:"capacity_note"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		code, name := strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
		if name == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "车间名称必填")
		}
		if code == "" {
			/* 编码留空自动生成：{类型名拼音首字母}-{5位流水}（车间→CJ-00001） */
			generated, err := codegen.Generate(r.Context(), api.DB, api.Writer, "org_unit", "code", p.TenantID, "org.workshop")
			if err != nil {
				return err
			}
			code = generated
		}
		nature := in.WorkshopNature
		if nature == "" {
			nature = "own"
		}
		if nature != "own" && nature != "outsourced" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "车间性质必须为 own 或 outsourced")
		}
		if nature == "outsourced" && strings.TrimSpace(in.OutsourcedPartner) == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "外包车间必须填写外包方名称")
		}
		status := in.Status
		if status == "" {
			status = "active"
		}
		if status != "active" && status != "disabled" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "状态必须为 active 或 disabled")
		}
		if err := validateManager(r.Context(), api.DB, p.TenantID, in.ManagerEmployeeID); err != nil {
			return err
		}
		var dup bool
		if err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM org_unit WHERE tenant_id=$1 AND org_type='workshop' AND code=$2)`, p.TenantID, code).Scan(&dup); err != nil {
			return err
		}
		if dup {
			return httpapi.Error(http.StatusConflict, "DUPLICATE_WORKSHOP_CODE", "车间编码已存在，请更换")
		}
		id := platform.ID()
		now := platform.Now()
		path := "/" + id + "/"
		err := withDualTx(r.Context(), api, p.TenantID, "workshop", id, func(tx *dualwrite.Tx) error {
			return execBoth(r.Context(), tx, `INSERT INTO org_unit(tenant_id,id,parent_id,org_type,code,name,manager_employee_id,path,status,
				workshop_nature,outsourced_partner,established_at,capacity_note,remark,created_at,updated_at,created_by,created_node,updated_by,updated_node,version)
				VALUES($1,$2,NULL,'workshop',$3,$4,NULLIF($5,''),$6,$7,$8,NULLIF($9,''),$10,NULLIF($11,''),NULLIF($12,''),$13,$13,$14,$15,$14,$15,1)`,
				p.TenantID, id, code, name, in.ManagerEmployeeID, path, status, nature, in.OutsourcedPartner,
				in.EstablishedAt, in.CapacityNote, in.Remark, now, p.UserID, api.Config.NodeID)
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "code": code, "path": path})
	})

	/* 编辑车间：名称/负责人/性质/备注，乐观锁 version */
	api.Handle("PATCH /api/v1/workshops/{id}", "workshop:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name              string `json:"name"`
			ManagerEmployeeID string `json:"manager_employee_id"`
			WorkshopNature    string `json:"workshop_nature"`
			OutsourcedPartner string `json:"outsourced_partner"`
			Remark            string `json:"remark"`
			CapacityNote      string `json:"capacity_note"`
			Version           int64  `json:"version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		id := r.PathValue("id")
		var currentVersion int64
		var nature string
		if err := api.DB.QueryRow(r.Context(), `SELECT version,workshop_nature FROM org_unit WHERE tenant_id=$1 AND id=$2 AND org_type='workshop'`, p.TenantID, id).Scan(&currentVersion, &nature); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "车间不存在")
		}
		if in.Version != currentVersion {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "车间信息已变更，请刷新后重试")
		}
		if err := validateManager(r.Context(), api.DB, p.TenantID, in.ManagerEmployeeID); err != nil {
			return err
		}
		newNature := in.WorkshopNature
		if newNature == "" {
			newNature = nature
		}
		if newNature == "outsourced" && strings.TrimSpace(in.OutsourcedPartner) == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "外包车间必须填写外包方名称")
		}
		now := platform.Now()
		err := withDualTx(r.Context(), api, p.TenantID, "workshop", id, func(tx *dualwrite.Tx) error {
			result, err := tx.Plain().Exec(r.Context(), `UPDATE org_unit SET name=COALESCE(NULLIF($3,''),name),
				manager_employee_id=NULLIF($4,''),workshop_nature=$5,outsourced_partner=NULLIF($6,''),
				capacity_note=NULLIF($7,''),remark=NULLIF($8,''),version=version+1,updated_at=$9,updated_by=$10,updated_node=$11
				WHERE tenant_id=$1 AND id=$2 AND org_type='workshop' AND version=$12`,
				p.TenantID, id, in.Name, in.ManagerEmployeeID, newNature, in.OutsourcedPartner, in.CapacityNote, in.Remark, now, p.UserID, api.Config.NodeID, in.Version)
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "车间信息已变更，请刷新后重试")
			}
			if tx.Secure() != nil {
				_, err = tx.Secure().Exec(r.Context(), `UPDATE org_unit SET name=COALESCE(NULLIF($3,''),name),
					manager_employee_id=NULLIF($4,''),workshop_nature=$5,outsourced_partner=NULLIF($6,''),
					capacity_note=NULLIF($7,''),remark=NULLIF($8,''),version=version+1,updated_at=$9,updated_by=$10,updated_node=$11
					WHERE tenant_id=$1 AND id=$2 AND org_type='workshop' AND version=$12`,
					p.TenantID, id, in.Name, in.ManagerEmployeeID, newNature, in.OutsourcedPartner, in.CapacityNote, in.Remark, now, p.UserID, api.Config.NodeID, in.Version)
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"saved": true, "version": in.Version + 1})
	})

	/* 停用车间：有启用产线时拒绝（设计 4.3） */
	api.Handle("POST /api/v1/workshops/{id}/disable", "workshop:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		id := r.PathValue("id")
		var activeLines int
		if err := api.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM org_unit WHERE tenant_id=$1 AND parent_id=$2 AND org_type='line' AND status='active'`, p.TenantID, id).Scan(&activeLines); err != nil {
			return err
		}
		if activeLines > 0 {
			return httpapi.Error(http.StatusConflict, "ORG_HAS_ACTIVE_CHILDREN", "车间下仍有启用产线，请先停用或迁出")
		}
		now := platform.Now()
		err := withDualTx(r.Context(), api, p.TenantID, "workshop", id, func(tx *dualwrite.Tx) error {
			return execBoth(r.Context(), tx, `UPDATE org_unit SET status='disabled',version=version+1,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$1 AND id=$2 AND org_type='workshop'`, p.TenantID, id, now, p.UserID, api.Config.NodeID)
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"disabled": true})
	})
	/* 产线下的班组列表（人员建档级联用，line:read） */
	api.Handle("GET /api/v1/lines/{id}/teams", "line:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		id := r.PathValue("id")
		rows, err := api.DB.Query(r.Context(), `SELECT o.id,o.code,o.name,o.status FROM org_unit o
			WHERE o.tenant_id=$1 AND o.parent_id=$2 AND o.org_type='team' ORDER BY o.code`, p.TenantID, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, status string
			if err := rows.Scan(&id, &code, &name, &status); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "status": status})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
}

func registerLines(api *httpapi.API) {
	/* 产线列表：始终返回所属车间编码/名称及人数 */
	api.Handle("GET /api/v1/lines", "line:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		access, err := ComputeAccess(r.Context(), api.DB, p)
		if err != nil {
			return err
		}
		workshopID := strings.TrimSpace(r.URL.Query().Get("workshop_id"))
		status := strings.TrimSpace(r.URL.Query().Get("status"))
		conds := " AND o.org_type='line'"
		args := []any{p.TenantID}
		if workshopID != "" {
			conds += fmt.Sprintf(" AND o.parent_id=$%d", len(args)+1)
			args = append(args, workshopID)
		}
		if status != "" {
			conds += " AND o.status='" + status + "'"
		}
		scopeCond, scopeArgs := lineScopeSQL(access, len(args)+1)
		args = append(args, scopeArgs...)
		rows, err := api.DB.Query(r.Context(), `SELECT o.id,o.code,o.name,o.status,o.parent_id,w.code,w.name,o.manager_employee_id,
			o.line_nature,o.default_process_segment,o.version,
			(SELECT COUNT(*) FROM employee_org_binding b WHERE b.tenant_id=o.tenant_id AND b.line_org_id=o.id AND b.is_primary=TRUE AND b.valid_to IS NULL) AS employee_count
			FROM org_unit o JOIN org_unit w ON w.tenant_id=o.tenant_id AND w.id=o.parent_id
			WHERE o.tenant_id=$1`+conds+scopeCond+` ORDER BY w.code,o.code`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, status, workshopID, workshopCode, workshopName, lineNature string
			var managerID, segment *string
			var version, employeeCount int64
			if err := rows.Scan(&id, &code, &name, &status, &workshopID, &workshopCode, &workshopName, &managerID, &lineNature, &segment, &version, &employeeCount); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "status": status,
				"workshop_id": workshopID, "workshop_code": workshopCode, "workshop_name": workshopName,
				"manager_employee_id": managerID, "line_nature": lineNature, "default_process_segment": segment,
				"version": version, "employee_count": employeeCount})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	/* 新增产线：车间必须 active；同车间编码唯一；active 产线必须有线长（设计 4.3 暂存例外） */
	api.Handle("POST /api/v1/lines", "line:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			WorkshopID            string `json:"workshop_id"`
			Code                  string `json:"code"`
			Name                  string `json:"name"`
			ManagerEmployeeID     string `json:"manager_employee_id"`
			Status                string `json:"status"`
			LineNature            string `json:"line_nature"`
			DefaultProcessSegment string `json:"default_process_segment"`
			Remark                string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		workshopID, code, name := strings.TrimSpace(in.WorkshopID), strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
		if workshopID == "" || name == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "所属车间与产线名称必填")
		}
		if code == "" {
			/* 编码留空自动生成：{类型名拼音首字母}-{5位流水}（产线→CX-00001） */
			generated, err := codegen.Generate(r.Context(), api.DB, api.Writer, "org_unit", "code", p.TenantID, "org.line")
			if err != nil {
				return err
			}
			code = generated
		}
		var workshopStatus string
		if err := api.DB.QueryRow(r.Context(), `SELECT status FROM org_unit WHERE tenant_id=$1 AND id=$2 AND org_type='workshop'`, p.TenantID, workshopID).Scan(&workshopStatus); err != nil {
			return httpapi.Error(http.StatusNotFound, "WORKSHOP_NOT_FOUND", "所属车间不存在")
		}
		if workshopStatus != "active" {
			return httpapi.Error(http.StatusConflict, "WORKSHOP_DISABLED", "所属车间已停用，不能新增产线")
		}
		status := in.Status
		if status == "" {
			status = "active"
		}
		if status != "active" && status != "disabled" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "状态必须为 active 或 disabled")
		}
		/* active 产线必须指定有效线长；disabled 可暂存无线长（附录 A.6） */
		if status == "active" {
			if err := validateManager(r.Context(), api.DB, p.TenantID, in.ManagerEmployeeID); err != nil {
				return err
			}
		} else if in.ManagerEmployeeID != "" {
			if err := validateManager(r.Context(), api.DB, p.TenantID, in.ManagerEmployeeID); err != nil {
				return err
			}
		}
		var dup bool
		if err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM org_unit WHERE tenant_id=$1 AND parent_id=$2 AND org_type='line' AND code=$3)`, p.TenantID, workshopID, code).Scan(&dup); err != nil {
			return err
		}
		if dup {
			return httpapi.Error(http.StatusConflict, "DUPLICATE_LINE_CODE", "同一车间内产线编码已存在，请更换")
		}
		var workshopPath string
		if err := api.DB.QueryRow(r.Context(), `SELECT path FROM org_unit WHERE tenant_id=$1 AND id=$2`, p.TenantID, workshopID).Scan(&workshopPath); err != nil {
			return err
		}
		id := platform.ID()
		now := platform.Now()
		path := workshopPath + id + "/"
		err := withDualTx(r.Context(), api, p.TenantID, "line", id, func(tx *dualwrite.Tx) error {
			return execBoth(r.Context(), tx, `INSERT INTO org_unit(tenant_id,id,parent_id,org_type,code,name,manager_employee_id,path,status,
				line_nature,default_process_segment,remark,created_at,updated_at,created_by,created_node,updated_by,updated_node,version)
				VALUES($1,$2,$3,'line',$4,$5,NULLIF($6,''),$7,$8,$9,NULLIF($10,''),NULLIF($11,''),$12,$12,$13,$14,$13,$14,1)`,
				p.TenantID, id, workshopID, code, name, in.ManagerEmployeeID, path, status,
				orDefault(in.LineNature, "fixed"), in.DefaultProcessSegment, in.Remark, now, p.UserID, api.Config.NodeID)
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": id, "code": code, "path": path})
	})

	/* 编辑产线：不允许直接改 parent_id（组织移动走换线/事务，附录 A.7） */
	api.Handle("PATCH /api/v1/lines/{id}", "line:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name                  string `json:"name"`
			ManagerEmployeeID     string `json:"manager_employee_id"`
			DefaultProcessSegment string `json:"default_process_segment"`
			Remark                string `json:"remark"`
			Status                string `json:"status"`
			LineNature            string `json:"line_nature"`
			Version               int64  `json:"version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		id := r.PathValue("id")
		var currentVersion int64
		var currentStatus string
		if err := api.DB.QueryRow(r.Context(), `SELECT version,status FROM org_unit WHERE tenant_id=$1 AND id=$2 AND org_type='line'`, p.TenantID, id).Scan(&currentVersion, &currentStatus); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "产线不存在")
		}
		if in.Version != currentVersion {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "产线信息已变更，请刷新后重试")
		}
		targetStatus := in.Status
		if targetStatus == "" {
			targetStatus = currentStatus
		}
		/* 启用（或由 disabled 转 active）必须指定线长；active 状态不允许清空线长 */
		if targetStatus == "active" {
			if err := validateManager(r.Context(), api.DB, p.TenantID, in.ManagerEmployeeID); err != nil {
				return err
			}
		} else if in.ManagerEmployeeID != "" {
			if err := validateManager(r.Context(), api.DB, p.TenantID, in.ManagerEmployeeID); err != nil {
				return err
			}
		}
		now := platform.Now()
		err := withDualTx(r.Context(), api, p.TenantID, "line", id, func(tx *dualwrite.Tx) error {
			result, err := tx.Plain().Exec(r.Context(), `UPDATE org_unit SET name=COALESCE(NULLIF($3,''),name),
				manager_employee_id=NULLIF($4,''),default_process_segment=NULLIF($5,''),remark=NULLIF($6,''),
				status=$7,line_nature=COALESCE(NULLIF($8,''),line_nature),
				version=version+1,updated_at=$9,updated_by=$10,updated_node=$11
				WHERE tenant_id=$1 AND id=$2 AND org_type='line' AND version=$12`,
				p.TenantID, id, in.Name, in.ManagerEmployeeID, in.DefaultProcessSegment, in.Remark,
				targetStatus, in.LineNature, now, p.UserID, api.Config.NodeID, in.Version)
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "产线信息已变更，请刷新后重试")
			}
			if tx.Secure() != nil {
				_, err = tx.Secure().Exec(r.Context(), `UPDATE org_unit SET name=COALESCE(NULLIF($3,''),name),
					manager_employee_id=NULLIF($4,''),default_process_segment=NULLIF($5,''),remark=NULLIF($6,''),
					status=$7,line_nature=COALESCE(NULLIF($8,''),line_nature),
					version=version+1,updated_at=$9,updated_by=$10,updated_node=$11
					WHERE tenant_id=$1 AND id=$2 AND org_type='line' AND version=$12`,
					p.TenantID, id, in.Name, in.ManagerEmployeeID, in.DefaultProcessSegment, in.Remark,
					targetStatus, in.LineNature, now, p.UserID, api.Config.NodeID, in.Version)
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"saved": true, "version": in.Version + 1})
	})

	/* 停用产线：不得留下指向 disabled 产线的有效主归属（设计 4.3） */
	api.Handle("POST /api/v1/lines/{id}/disable", "line:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		id := r.PathValue("id")
		var members int
		if err := api.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM employee_org_binding WHERE tenant_id=$1 AND line_org_id=$2 AND is_primary=TRUE AND valid_to IS NULL`, p.TenantID, id).Scan(&members); err != nil {
			return err
		}
		if members > 0 {
			return httpapi.Error(http.StatusConflict, "LINE_HAS_MEMBERS", "产线下仍有在职人员，请先迁移归属或停用相关账号")
		}
		now := platform.Now()
		err := withDualTx(r.Context(), api, p.TenantID, "line", id, func(tx *dualwrite.Tx) error {
			return execBoth(r.Context(), tx, `UPDATE org_unit SET status='disabled',version=version+1,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$1 AND id=$2 AND org_type='line'`, p.TenantID, id, now, p.UserID, api.Config.NodeID)
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"disabled": true})
	})

	/* 产线当前人员：组长只能看本线（数据范围，设计 5.3） */
	api.Handle("GET /api/v1/lines/{id}/members", "line:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		id := r.PathValue("id")
		var workshopID string
		if err := api.DB.QueryRow(r.Context(), `SELECT parent_id FROM org_unit WHERE tenant_id=$1 AND id=$2 AND org_type='line'`, p.TenantID, id).Scan(&workshopID); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "产线不存在")
		}
		access, err := ComputeAccess(r.Context(), api.DB, p)
		if err != nil {
			return err
		}
		if !access.AllowLine(id, workshopID) {
			recordDenied(r.Context(), api, p, "line", id, "超出本线数据范围")
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "只能查看本产线人员")
		}
		rows, err := api.DB.Query(r.Context(), `SELECT e.id,e.employee_no,e.name,e.person_type,e.position,e.status
			FROM employee_org_binding b JOIN employee e ON e.tenant_id=b.tenant_id AND e.id=b.employee_id
			WHERE b.tenant_id=$1 AND b.line_org_id=$2 AND b.is_primary=TRUE AND b.valid_to IS NULL
			ORDER BY e.employee_no`, p.TenantID, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, name, ptype, position, status string
			if err := rows.Scan(&id, &no, &name, &ptype, &position, &status); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "employee_no": no, "name": name, "person_type": ptype, "position": position, "status": status})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
}

/* validateManager 负责人必须是本厂 active 管理人员（设计 4.3/4.5）。 */
func validateManager(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tenantID, employeeID string) error {
	if strings.TrimSpace(employeeID) == "" {
		return nil
	}
	var ok bool
	err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND id=$2 AND person_type='manager' AND status='active')`, tenantID, employeeID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return httpapi.Error(http.StatusBadRequest, "MANAGER_INVALID", "负责人必须是本厂在职管理人员")
	}
	return nil
}

func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

/* workshopScopeSQL / lineScopeSQL 数据范围谓词（设计 5.7 第 5 条：SQL 始终包含范围谓词）。 */
func workshopScopeSQL(a *Access, firstArg int) (string, []any) {
	if a.All {
		return "", nil
	}
	if len(a.WorkshopIDs) == 0 {
		return " AND FALSE", nil
	}
	var ph []string
	args := []any{}
	for _, id := range a.WorkshopIDs {
		ph = append(ph, fmt.Sprintf("$%d", firstArg+len(args)))
		args = append(args, id)
	}
	return " AND o.id IN (" + strings.Join(ph, ",") + ")", args
}

func lineScopeSQL(a *Access, firstArg int) (string, []any) {
	if a.All {
		return "", nil
	}
	conds := []string{}
	args := []any{}
	if len(a.LineIDs) > 0 {
		var ph []string
		for _, id := range a.LineIDs {
			ph = append(ph, fmt.Sprintf("$%d", firstArg+len(args)))
			args = append(args, id)
		}
		conds = append(conds, "o.id IN ("+strings.Join(ph, ",")+")")
	}
	if len(a.WorkshopIDs) > 0 {
		var ph []string
		for _, id := range a.WorkshopIDs {
			ph = append(ph, fmt.Sprintf("$%d", firstArg+len(args)))
			args = append(args, id)
		}
		conds = append(conds, "o.parent_id IN ("+strings.Join(ph, ",")+")")
	}
	if len(conds) == 0 {
		return " AND FALSE", nil
	}
	return " AND (" + strings.Join(conds, " OR ") + ")", args
}

/* recordDenied 越权访问审计（设计 4.9：access_denied 写 audit_log）。 */
func recordDenied(ctx context.Context, api *httpapi.API, p auth.Principal, ownerType, ownerID, reason string) {
	_, _ = api.DB.Exec(ctx, `INSERT INTO audit_log(tenant_id,id,request_id,actor_type,actor_id,action,owner_type,owner_id,reason,created_node,created_at)
		VALUES($1,$2,$3,'user',$4,'access_denied',$5,$6,$7,$8,$9)`,
		p.TenantID, "deny-"+platform.ID(), httpapi.RequestID(ctx), p.UserID, ownerType, ownerID, reason, api.Config.NodeID, platform.Now())
}
