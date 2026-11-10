package production

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerWorkOrderReview(api *httpapi.API) {
	api.Handle("POST /api/v1/work-orders/{id}/review", "work_order:review", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version  int64  `json:"version"`
			Decision string `json:"decision"`
			Reason   string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Decision != "approve" && in.Decision != "reject" {
			return httpapi.Error(400, "VALIDATION_ERROR", "审核结论只能是 approve 或 reject")
		}
		in.Reason = strings.TrimSpace(in.Reason)
		if in.Decision == "reject" && in.Reason == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "驳回必须填写原因")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		id := r.PathValue("id")
		var status, reviewStatus string
		var version int64
		err = tx.QueryRow(r.Context(), "SELECT status,review_status,version FROM work_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, id).Scan(&status, &reviewStatus, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "工单不存在")
		}
		if err != nil {
			return err
		}
		if in.Version != version {
			return httpapi.Error(409, "VERSION_CONFLICT", "工单已变更，请刷新后重试")
		}
		if status != "pending_release" {
			return httpapi.Error(409, "INVALID_STATE", "已下发的工单不能再修改审核结论")
		}
		if reviewStatus == "approved" {
			return httpapi.Error(409, "INVALID_STATE", "工单已审核通过")
		}
		newStatus := "approved"
		if in.Decision == "reject" {
			newStatus = "rejected"
		}
		var reviewer *string
		if p.EmployeeID != "" {
			reviewer = &p.EmployeeID
		}
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), "UPDATE work_order SET review_status=$1,reviewer_employee_id=$2,reviewed_at=$3,review_reason=$4,version=version+1,updated_at=$3,updated_by=$5,updated_node=$6 WHERE tenant_id=$7 AND id=$8", newStatus, reviewer, now, optionalText(&in.Reason), p.UserID, api.Config.NodeID, p.TenantID, id); err != nil {
			return err
		}
		response := map[string]any{"id": id, "review_status": newStatus, "version": version + 1}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "review", "work_order", id, map[string]any{"review_status": reviewStatus}, response, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
	})

	api.Handle("PUT /api/v1/work-orders/{id}/report-mode", "work_order:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version    int64  `json:"version"`
			ReportMode string `json:"report_mode"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.ReportMode != "A" && in.ReportMode != "B" {
			return httpapi.Error(400, "VALIDATION_ERROR", "报工模式只能是 A（派工受控）或 B（自主选工序）")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		id := r.PathValue("id")
		var currentMode string
		var locked bool
		var version int64
		err = tx.QueryRow(r.Context(), "SELECT report_mode,process_plan_locked,version FROM work_order WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, id).Scan(&currentMode, &locked, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "工单不存在")
		}
		if err != nil {
			return err
		}
		if in.Version != version {
			return httpapi.Error(409, "VERSION_CONFLICT", "工单已变更，请刷新后重试")
		}
		if currentMode == in.ReportMode {
			return httpapi.JSON(w, 200, map[string]any{"id": id, "report_mode": currentMode, "version": version})
		}
		if !locked {
			if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM work_report wr JOIN production_task t ON t.tenant_id=wr.tenant_id AND t.id=wr.task_id WHERE t.tenant_id=$1 AND t.work_order_id=$2 AND wr.report_kind<>'reversal')`, p.TenantID, id).Scan(&locked); err != nil {
				return err
			}
		}
		if locked {
			return httpapi.Error(409, "REPORT_MODE_LOCKED", "工单已有有效报工，报工模式和工序方案已锁定")
		}
		if _, err := tx.Exec(r.Context(), "UPDATE work_order SET report_mode=$1,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6", in.ReportMode, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, id); err != nil {
			return err
		}
		response := map[string]any{"id": id, "report_mode": in.ReportMode, "version": version + 1}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "report_mode", "work_order", id, map[string]any{"report_mode": currentMode}, response, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
	})
}

func validateWorkshopLine(ctx context.Context, tx pgx.Tx, tenantID string, workshopID, lineID *string) error {
	hasWorkshop := workshopID != nil && *workshopID != ""
	hasLine := lineID != nil && *lineID != ""
	if hasLine && !hasWorkshop {
		return httpapi.Error(400, "VALIDATION_ERROR", "选择生产线前必须先选择车间")
	}
	if !hasWorkshop {
		return nil
	}
	var workshopType string
	if err := tx.QueryRow(ctx, "SELECT org_type FROM org_unit WHERE tenant_id=$1 AND id=$2", tenantID, *workshopID).Scan(&workshopType); err != nil || workshopType != "workshop" {
		return httpapi.Error(400, "INVALID_WORKSHOP", "车间不存在或类型不正确")
	}
	if !hasLine {
		return nil
	}
	var lineType string
	var parent *string
	if err := tx.QueryRow(ctx, "SELECT org_type,parent_id FROM org_unit WHERE tenant_id=$1 AND id=$2", tenantID, *lineID).Scan(&lineType, &parent); err != nil || lineType != "line" || parent == nil || *parent != *workshopID {
		return httpapi.Error(400, "INVALID_LINE", "生产线不存在，或不隶属于所选车间")
	}
	return nil
}
