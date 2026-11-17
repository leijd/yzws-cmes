package tech

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* processLineInput 工序表明细行（设计 8.2：车间流转顺序即 seq_no 顺序）。 */
type processLineInput struct {
	SeqNo       int      `json:"seq_no"`
	ProcessID   string   `json:"process_id"`
	ProcessCode string   `json:"process_code"`
	ProcessName string   `json:"process_name"`
	Category    string   `json:"category"`
	WorkSeconds *float64 `json:"work_seconds"`
	UnitPrice   *float64 `json:"unit_price"`
	Equipment   string   `json:"equipment"`
	Instruction string   `json:"instruction"`
	Remark      string   `json:"remark"`
}

func processWrite(p auth.Principal) bool { return p.Permissions["process:write"] }

/* registerProcessStandard 生产标准工序表（设计 8.1~8.4）：
 * 草稿→（工艺员提交+线长确认，两确认）→生产标准；修改走 revising 重新两确认；他人查看须授权。 */
func registerProcessStandard(api *httpapi.API) {
	api.Handle("GET /api/v1/process-standards", "process:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "s.tenant_id=$1"
		for param, column := range map[string]string{"status": "s.status", "product_ref": "s.product_ref", "project_ref": "s.project_ref"} {
			if value := strings.TrimSpace(r.URL.Query().Get(param)); value != "" {
				args = append(args, value)
				where += fmt.Sprintf(" AND %s=$%d", column, len(args))
			}
		}
		if keyword := strings.TrimSpace(r.URL.Query().Get("keyword")); keyword != "" {
			args = append(args, "%"+keyword+"%")
			where += fmt.Sprintf(" AND (s.sheet_no ILIKE $%d OR COALESCE(s.product_ref,'') ILIKE $%d)", len(args), len(args))
		}
		if !processWrite(p) {
			/* 非技术内核维护人员只能看到自己制订的表，或已获授权的表 */
			args = append(args, p.UserID)
			where += fmt.Sprintf(" AND (s.created_by=$%d OR EXISTS(SELECT 1 FROM process_standard_grant g WHERE g.tenant_id=s.tenant_id AND g.sheet_id=s.id AND g.grantee_user_id=$%d AND g.revoked_at IS NULL AND (g.valid_to IS NULL OR g.valid_to>$%d)))", len(args), len(args), len(args)+1)
			args = append(args, platform.Now())
		}
		rows, err := api.DB.Query(r.Context(), `SELECT s.id,s.sheet_no,COALESCE(s.product_ref,''),COALESCE(s.project_ref,''),s.version_no,s.revision,s.status,
			s.confirmed_round,COALESCE(s.effective_at,0),COALESCE(s.remark,''),s.created_by,COALESCE(u.login_name,''),s.updated_at,
			(SELECT COUNT(*) FROM process_standard_line l WHERE l.tenant_id=s.tenant_id AND l.sheet_id=s.id)
			FROM process_standard s LEFT JOIN user_account u ON u.tenant_id=s.tenant_id AND u.id=s.created_by
			WHERE `+where+` ORDER BY s.updated_at DESC LIMIT 200`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, sheetNo, productRef, projectRef, status, remark, createdBy, createdByName string
			var versionNo, revision, updatedAt int64
			var confirmedRound, effectiveAt, lineCount int
			if err := rows.Scan(&id, &sheetNo, &productRef, &projectRef, &versionNo, &revision, &status, &confirmedRound,
				&effectiveAt, &remark, &createdBy, &createdByName, &updatedAt, &lineCount); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "sheet_no": sheetNo, "product_ref": productRef, "project_ref": projectRef,
				"version_no": versionNo, "revision": revision, "status": status, "confirmed_round": confirmedRound,
				"effective_at": effectiveAt, "remark": remark, "created_by": createdBy, "created_by_name": createdByName,
				"updated_at": updatedAt, "line_count": lineCount})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
	/* 新建草稿 */
	api.Handle("POST /api/v1/process-standards", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ProductRef  string             `json:"product_ref"`
			ProjectRef  string             `json:"project_ref"`
			Remark      string             `json:"remark"`
			CopiedFrom  string             `json:"copied_from_id"`
			Lines       []processLineInput `json:"lines"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		if err := validateProcessLines(in.Lines, false); err != nil {
			return err
		}
		sheetNo, err := nextSheetNo(ctx, api, p.TenantID)
		if err != nil {
			return err
		}
		sheetID := platform.ID()
		now := platform.Now()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `INSERT INTO process_standard(tenant_id,id,sheet_no,product_ref,project_ref,version_no,revision,status,
			created_by,copied_from_id,confirmed_round,remark,created_at,created_node,updated_at,updated_by,updated_node,version)
			VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,''),1,0,'draft',$6,NULLIF($7,''),0,$8,$9,$10,$9,$6,$10,1)`,
			p.TenantID, sheetID, sheetNo, in.ProductRef, in.ProjectRef, p.UserID, in.CopiedFrom, in.Remark, now, api.Config.NodeID); err != nil {
			return err
		}
		if err := insertProcessLines(ctx, tx, p.TenantID, sheetID, in.Lines); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "create_process_standard", "process_standard", sheetID, nil,
			map[string]any{"sheet_no": sheetNo, "line_count": len(in.Lines)}, ""); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": sheetID, "sheet_no": sheetNo, "status": "draft"})
	})
	/* 详情：制订人/技术内核维护人员可直接看，其他人须在有效期内的查看授权（设计 8.4） */
	api.Handle("GET /api/v1/process-standards/{id}", "process:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		sheetID := r.PathValue("id")
		ctx := r.Context()
		var sheet map[string]any
		err := api.DB.QueryRow(ctx, `SELECT row_to_json(t) FROM (SELECT s.id,s.sheet_no,COALESCE(s.product_ref,'') AS product_ref,
			COALESCE(s.project_ref,'') AS project_ref,s.version_no,s.revision,s.status,s.confirmed_round,
			COALESCE(s.effective_at,0) AS effective_at,COALESCE(s.remark,'') AS remark,s.created_by,
			COALESCE(s.copied_from_id,'') AS copied_from_id,COALESCE(s.superseded_by,'') AS superseded_by,s.updated_at
			FROM process_standard s WHERE s.tenant_id=$1 AND s.id=$2) t`, p.TenantID, sheetID).Scan(&sheet)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "工序表不存在")
		}
		if err != nil {
			return err
		}
		if err := ensureSheetVisible(ctx, api, p, sheetID, fmt.Sprint(sheet["created_by"])); err != nil {
			return err
		}
		rows, err := api.DB.Query(ctx, `SELECT id,seq_no,COALESCE(process_id,''),process_code,process_name,COALESCE(category,''),
			COALESCE(work_seconds::text,''),COALESCE(unit_price::text,''),COALESCE(equipment,''),COALESCE(instruction,''),COALESCE(remark,'')
			FROM process_standard_line WHERE tenant_id=$1 AND sheet_id=$2 ORDER BY seq_no`, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		defer rows.Close()
		lines := []map[string]any{}
		for rows.Next() {
			var id, processID, code, name, category, workSeconds, unitPrice, equipment, instruction, remark string
			var seqNo int
			if err := rows.Scan(&id, &seqNo, &processID, &code, &name, &category, &workSeconds, &unitPrice, &equipment, &instruction, &remark); err != nil {
				return err
			}
			lines = append(lines, map[string]any{"id": id, "seq_no": seqNo, "process_id": processID, "process_code": code,
				"process_name": name, "category": category, "work_seconds": workSeconds, "unit_price": unitPrice,
				"equipment": equipment, "instruction": instruction, "remark": remark})
		}
		sheet["lines"] = lines
		return httpapi.JSON(w, http.StatusOK, sheet)
	})
	/* 修改内容：仅 draft/revising 可改；整体替换明细 */
	api.Handle("PATCH /api/v1/process-standards/{id}", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ProductRef string             `json:"product_ref"`
			ProjectRef string             `json:"project_ref"`
			Remark     string             `json:"remark"`
			Lines      []processLineInput `json:"lines"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		sheetID := r.PathValue("id")
		status, err := sheetStatus(ctx, api, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		if status != "draft" && status != "revising" {
			return httpapi.Error(http.StatusConflict, "SHEET_NOT_EDITABLE", "只有草稿或修订中的工序表可以修改")
		}
		if err := validateProcessLines(in.Lines, false); err != nil {
			return err
		}
		now := platform.Now()
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `UPDATE process_standard SET product_ref=NULLIF($3,''),project_ref=NULLIF($4,''),remark=$5,
			version=version+1,updated_at=$6,updated_by=$7,updated_node=$8 WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, sheetID, in.ProductRef, in.ProjectRef, in.Remark, now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM process_standard_line WHERE tenant_id=$1 AND sheet_id=$2`, p.TenantID, sheetID); err != nil {
			return err
		}
		if err := insertProcessLines(ctx, tx, p.TenantID, sheetID, in.Lines); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "update_process_standard", "process_standard", sheetID, nil,
			map[string]any{"line_count": len(in.Lines)}, ""); err != nil {
			return err
		}
		return tx.Commit(ctx)
	})
	/* 第一确认：工艺员提交（起草人） */
	api.Handle("POST /api/v1/process-standards/{id}/submit", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return confirmSheet(api, w, r, p, "submit")
	})
	/* 第二确认：线长确认，两次确认人不得相同（设计 8.3） */
	api.Handle("POST /api/v1/process-standards/{id}/confirm", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return confirmSheet(api, w, r, p, "confirm")
	})
	/* 驳回：回到草稿/修订状态，重新走两确认 */
	api.Handle("POST /api/v1/process-standards/{id}/reject", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		return confirmSheet(api, w, r, p, "reject")
	})
	/* 发起修订：standard → revising，版本号+1，确认轮次保留，需重新两确认（设计 8.3） */
	api.Handle("POST /api/v1/process-standards/{id}/revise", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		sheetID := r.PathValue("id")
		status, err := sheetStatus(ctx, api, p.TenantID, sheetID)
		if err != nil {
			return err
		}
		if status != "standard" {
			return httpapi.Error(http.StatusConflict, "SHEET_NOT_STANDARD", "只有已生效的生产标准可以发起修订")
		}
		result, err := api.DB.Exec(ctx, `UPDATE process_standard SET status='revising',revision=revision+1,effective_at=NULL,
			version=version+1,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$1 AND id=$2 AND status='standard'`,
			p.TenantID, sheetID, platform.Now(), p.UserID, api.Config.NodeID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "工序表状态已变化，请刷新后重试")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"status": "revising"})
	})
	/* 查看授权（设计 8.4）：按人按表按有效期，仅只读 */
	api.Handle("POST /api/v1/process-standards/{id}/grants", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			GranteeUserID string `json:"grantee_user_id"`
			ValidMinutes  int    `json:"valid_minutes"`
			Reason        string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		sheetID := r.PathValue("id")
		grantee := strings.TrimSpace(in.GranteeUserID)
		if grantee == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "被授权账号必填")
		}
		var ok bool
		if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM process_standard WHERE tenant_id=$1 AND id=$2)`, p.TenantID, sheetID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "工序表不存在")
		}
		if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_account WHERE tenant_id=$1 AND id=$2 AND status='active')`, p.TenantID, grantee).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return httpapi.Error(http.StatusBadRequest, "GRANTEE_INVALID", "被授权账号不存在或已停用")
		}
		now := platform.Now()
		var validTo *int64
		if in.ValidMinutes > 0 {
			value := now + int64(in.ValidMinutes)*60000
			validTo = &value
		}
		grantID := platform.ID()
		if _, err := api.DB.Exec(ctx, `INSERT INTO process_standard_grant(tenant_id,id,sheet_id,grantee_user_id,grantee_role,granted_by,scope,valid_from,valid_to,reason,created_at)
			VALUES($1,$2,$3,$4,NULL,$5,'view',$6,$7,$8,$6)`, p.TenantID, grantID, sheetID, grantee, p.UserID, now, validTo, in.Reason); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": grantID, "valid_to": validTo})
	})
	api.Handle("GET /api/v1/process-standards/{id}/grants", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT g.id,g.grantee_user_id,COALESCE(u.login_name,''),COALESCE(g.granted_by,''),
			COALESCE(g.valid_to,0),COALESCE(g.reason,''),COALESCE(g.revoked_at,0),g.created_at
			FROM process_standard_grant g LEFT JOIN user_account u ON u.tenant_id=g.tenant_id AND u.id=g.grantee_user_id
			WHERE g.tenant_id=$1 AND g.sheet_id=$2 ORDER BY g.created_at DESC`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, granteeID, loginName, grantedBy, reason string
			var validTo, revokedAt, createdAt int64
			if err := rows.Scan(&id, &granteeID, &loginName, &grantedBy, &validTo, &reason, &revokedAt, &createdAt); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "grantee_user_id": granteeID, "grantee_name": loginName,
				"granted_by": grantedBy, "valid_to": validTo, "reason": reason, "revoked_at": revokedAt, "created_at": createdAt})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
	api.Handle("DELETE /api/v1/process-standards/{id}/grants/{grantID}", "process:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		result, err := api.DB.Exec(r.Context(), `UPDATE process_standard_grant SET revoked_at=$4,revoked_by=$5
			WHERE tenant_id=$1 AND sheet_id=$2 AND id=$3 AND revoked_at IS NULL`, p.TenantID, r.PathValue("id"), r.PathValue("grantID"), platform.Now(), p.UserID)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "授权不存在或已撤销")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"revoked": true})
	})
}

/* ensureSheetVisible 工序表可见性：技术内核维护人员/制订人/有效授权三者之一。 */
func ensureSheetVisible(ctx context.Context, api *httpapi.API, p auth.Principal, sheetID, createdBy string) error {
	if processWrite(p) || createdBy == p.UserID {
		return nil
	}
	var granted bool
	if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM process_standard_grant
		WHERE tenant_id=$1 AND sheet_id=$2 AND grantee_user_id=$3 AND revoked_at IS NULL
		AND (valid_to IS NULL OR valid_to>$4))`, p.TenantID, sheetID, p.UserID, platform.Now()).Scan(&granted); err != nil {
		return err
	}
	if !granted {
		api.RecordDenied(ctx, p, "process_standard", sheetID, "未获授权查看他人工序表")
		return httpapi.Error(http.StatusForbidden, "SHEET_NOT_AUTHORIZED", "查看该工序表需要厂长/经理授权")
	}
	return nil
}

/* confirmSheet 两确认流转：submit=第1确认（工艺员），confirm=第2确认（线长），reject=驳回。 */
func confirmSheet(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, action string) error {
	ctx := r.Context()
	sheetID := r.PathValue("id")
	var in struct {
		Opinion string `json:"opinion"`
	}
	_ = httpapi.Decode(r, &in)
	var status, createdBy string
	var revision, confirmedRound int64
	if err := api.DB.QueryRow(ctx, `SELECT status,created_by,revision,confirmed_round FROM process_standard WHERE tenant_id=$1 AND id=$2`, p.TenantID, sheetID).Scan(&status, &createdBy, &revision, &confirmedRound); err != nil {
		return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "工序表不存在")
	}
	now := platform.Now()
	switch action {
	case "submit":
		if status != "draft" && status != "revising" {
			return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "只有草稿或修订中的工序表可以提交确认")
		}
		/* 第一确认：再次提交前清除上一轮未完成的确认记录 */
		if _, err := api.DB.Exec(ctx, `DELETE FROM process_standard_confirm WHERE tenant_id=$1 AND sheet_id=$2 AND round_no=$3`,
			p.TenantID, sheetID, confirmedRound+1); err != nil {
			return err
		}
		if _, err := api.DB.Exec(ctx, `INSERT INTO process_standard_confirm(tenant_id,id,sheet_id,revision,round_no,confirm_seq,confirmer_id,confirmer_role,action,opinion,created_at)
			VALUES($1,$2,$3,$4,$5,1,$6,'process_engineer','submit',$7,$8)`,
			p.TenantID, platform.ID(), sheetID, revision, confirmedRound+1, p.UserID, in.Opinion, now); err != nil {
			return err
		}
		if _, err := api.DB.Exec(ctx, `UPDATE process_standard SET status='pending_confirm',version=version+1,updated_at=$3,updated_by=$4,updated_node=$5
			WHERE tenant_id=$1 AND id=$2`, p.TenantID, sheetID, now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"status": "pending_confirm"})
	case "confirm":
		if status != "pending_confirm" {
			return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "工序表尚未提交确认，不能执行第二确认")
		}
		if p.UserID == createdBy {
			return httpapi.Error(http.StatusConflict, "CONFIRM_SAME_USER", "两次确认人不得相同，请由线长确认")
		}
		var submitted bool
		if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM process_standard_confirm
			WHERE tenant_id=$1 AND sheet_id=$2 AND round_no=$3 AND confirm_seq=1)`, p.TenantID, sheetID, confirmedRound+1).Scan(&submitted); err != nil {
			return err
		}
		if !submitted {
			return httpapi.Error(http.StatusConflict, "SHEET_NOT_SUBMITTED", "缺少工艺员提交确认记录")
		}
		tx, err := api.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		if _, err := tx.Exec(ctx, `INSERT INTO process_standard_confirm(tenant_id,id,sheet_id,revision,round_no,confirm_seq,confirmer_id,confirmer_role,action,opinion,created_at)
			VALUES($1,$2,$3,$4,$5,2,$6,'group_leader','confirm',$7,$8)`,
			p.TenantID, platform.ID(), sheetID, revision, confirmedRound+1, p.UserID, in.Opinion, now); err != nil {
			return err
		}
		/* 前置表被替代：同 product_ref 的旧标准表置 superseded（新版生效后旧版不再有效） */
		var productRef string
		_ = tx.QueryRow(ctx, `SELECT COALESCE(product_ref,'') FROM process_standard WHERE tenant_id=$1 AND id=$2`, p.TenantID, sheetID).Scan(&productRef)
		if productRef != "" {
			if _, err := tx.Exec(ctx, `UPDATE process_standard SET status='superseded',superseded_by=$3
				WHERE tenant_id=$1 AND product_ref=$2 AND id<>$3 AND status='standard'`, p.TenantID, productRef, sheetID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE process_standard SET status='standard',confirmed_round=confirmed_round+1,effective_at=$3,
			version=version+1,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, sheetID, now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, "confirm_process_standard", "process_standard", sheetID, nil,
			map[string]any{"round": confirmedRound + 1}, in.Opinion); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"status": "standard", "confirmed_round": confirmedRound + 1})
	default: /* reject */
		if status != "pending_confirm" {
			return httpapi.Error(http.StatusConflict, "SHEET_STATUS_INVALID", "只有待确认的工序表可以驳回")
		}
		if _, err := api.DB.Exec(ctx, `INSERT INTO process_standard_confirm(tenant_id,id,sheet_id,revision,round_no,confirm_seq,confirmer_id,confirmer_role,action,opinion,created_at)
			VALUES($1,$2,$3,$4,$5,2,$6,'group_leader','reject',$7,$8)`,
			p.TenantID, platform.ID(), sheetID, revision, confirmedRound+1, p.UserID, in.Opinion, now); err != nil {
			return err
		}
		if _, err := api.DB.Exec(ctx, `UPDATE process_standard SET status=CASE WHEN revision>0 THEN 'revising' ELSE 'draft' END,
			version=version+1,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$1 AND id=$2`,
			p.TenantID, sheetID, now, p.UserID, api.Config.NodeID); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"status": "rejected"})
	}
}

/* validateProcessLines 明细校验：工序编码/名称必填，序号唯一且连续。 */
func validateProcessLines(lines []processLineInput, allowEmpty bool) error {
	if len(lines) == 0 && !allowEmpty {
		return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "工序明细至少一行")
	}
	seen := map[int]bool{}
	for index := range lines {
		line := &lines[index]
		line.ProcessCode = strings.TrimSpace(line.ProcessCode)
		line.ProcessName = strings.TrimSpace(line.ProcessName)
		if line.ProcessCode == "" || line.ProcessName == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "工序编码与工序名称必填")
		}
		if line.SeqNo == 0 {
			line.SeqNo = index + 1
		}
		if seen[line.SeqNo] {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "工序顺序号重复")
		}
		seen[line.SeqNo] = true
	}
	return nil
}

func insertProcessLines(ctx context.Context, tx pgx.Tx, tenantID, sheetID string, lines []processLineInput) error {
	for _, line := range lines {
		if _, err := tx.Exec(ctx, `INSERT INTO process_standard_line(tenant_id,id,sheet_id,seq_no,process_id,process_code,process_name,
			category,work_seconds,unit_price,equipment,instruction,remark) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,NULLIF($8,''),
			$9,$10,NULLIF($11,''),NULLIF($12,''),NULLIF($13,''))`,
			tenantID, platform.ID(), sheetID, line.SeqNo, line.ProcessID, line.ProcessCode, line.ProcessName,
			line.Category, line.WorkSeconds, line.UnitPrice, line.Equipment, line.Instruction, line.Remark); err != nil {
			return err
		}
	}
	return nil
}

/* nextSheetNo 工序表编号：GZB+年月+三位流水（同厂唯一）。 */
func nextSheetNo(ctx context.Context, api *httpapi.API, tenantID string) (string, error) {
	prefix := "GZB" + time.UnixMilli(platform.Now()).Format("200601")
	var count int
	if err := api.DB.QueryRow(ctx, `SELECT COUNT(*) FROM process_standard WHERE tenant_id=$1 AND sheet_no LIKE $2`, tenantID, prefix+"%").Scan(&count); err != nil {
		return "", err
	}
	for seq := count + 1; seq < count+1000; seq++ {
		candidate := fmt.Sprintf("%s%03d", prefix, seq)
		var exists bool
		if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM process_standard WHERE tenant_id=$1 AND sheet_no=$2)`, tenantID, candidate).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", httpapi.Error(http.StatusInternalServerError, "SHEET_NO_EXHAUSTED", "工序表编号生成失败")
}

func sheetStatus(ctx context.Context, api *httpapi.API, tenantID, sheetID string) (string, error) {
	var status string
	if err := api.DB.QueryRow(ctx, `SELECT status FROM process_standard WHERE tenant_id=$1 AND id=$2`, tenantID, sheetID).Scan(&status); err != nil {
		return "", httpapi.Error(http.StatusNotFound, "NOT_FOUND", "工序表不存在")
	}
	return status, nil
}
