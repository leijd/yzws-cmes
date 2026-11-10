package production

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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

type markerRatio struct {
	Size     string `json:"size"`
	RatioQty int64  `json:"ratio_qty"`
	SortNo   int    `json:"sort_no"`
}

type markerRequest struct {
	Version             int64          `json:"version"`
	MarkerNo            *string        `json:"marker_no"`
	MarkerDate          *string        `json:"marker_date"`
	WorkOrderID         *string        `json:"work_order_id"`
	Color               *string        `json:"color"`
	MaterialID          *string        `json:"material_id"`
	FabricWidthCM       *string        `json:"fabric_width_cm"`
	FabricLayers        *int64         `json:"fabric_layers"`
	MarkerLengthM       *string        `json:"marker_length_m"`
	UtilizationRate     *string        `json:"utilization_rate"`
	DrawingAttachmentID *string        `json:"drawing_attachment_id"`
	LayoutEmployeeID    *string        `json:"layout_employee_id"`
	Remark              *string        `json:"remark"`
	Ratios              *[]markerRatio `json:"ratios"`
}

func (in markerRequest) touchesBusinessFields() bool {
	return in.MarkerNo != nil || in.MarkerDate != nil || in.WorkOrderID != nil || in.Color != nil ||
		in.MaterialID != nil || in.FabricWidthCM != nil || in.FabricLayers != nil || in.MarkerLengthM != nil ||
		in.UtilizationRate != nil || in.DrawingAttachmentID != nil || in.LayoutEmployeeID != nil || in.Ratios != nil
}

type markerState struct {
	MarkerNo, MarkerDate, WorkOrderID, Color, MaterialID, LayoutEmployeeID string
	FabricWidthCM, UtilizationRate, DrawingAttachmentID, Remark            *string
	FabricLayers                                                           int64
	MarkerLengthM                                                          string
	Ratios                                                                 []markerRatio
}

type markerSnapshot struct {
	ProjectID, ProductID, ProductCode, ProductName string
	MaterialCode, MaterialName                     string
	MaterialSpec                                   *string
	Pieces, CutQty                                 int64
	EstimatedFabricM                               string
}

type dbQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type scanner interface {
	Scan(dest ...any) error
}

const markerSelect = `SELECT m.id,m.marker_no,m.marker_date,m.work_order_id,m.project_id,m.product_id,m.product_code,m.product_name,m.color,m.material_id,m.material_code,m.material_name,m.material_specification,m.fabric_width_cm::text,m.pieces_per_marker,m.fabric_layers,m.cut_qty,m.marker_length_m::text,m.estimated_fabric_m::text,m.utilization_rate::text,m.drawing_attachment_id,m.layout_employee_id,m.reviewer_employee_id,m.review_status,m.review_reason,m.reviewed_at,m.remark,m.version FROM marker m`

func registerMarkers(api *httpapi.API) {
	api.Handle("GET /api/v1/markers", "marker:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		limit, cursor := httpapi.Page(r)
		args := []any{p.TenantID}
		where := "m.tenant_id=$1"
		if cursor != "" {
			args = append(args, cursor)
			where += fmt.Sprintf(" AND m.id>$%d", len(args))
		}
		for param, column := range map[string]string{"work_order_id": "m.work_order_id", "review_status": "m.review_status", "product_code": "m.product_code", "color": "m.color"} {
			if value := strings.TrimSpace(r.URL.Query().Get(param)); value != "" {
				args = append(args, value)
				where += fmt.Sprintf(" AND %s=$%d", column, len(args))
			}
		}
		args = append(args, limit+1)
		rows, err := api.DB.Query(r.Context(), fmt.Sprintf(markerSelect+" WHERE %s ORDER BY m.id LIMIT $%d", where, len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			item, err := scanMarker(rows)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		hasMore := len(items) > limit
		if hasMore {
			items = items[:limit]
		}
		var next any
		if hasMore {
			next = items[len(items)-1]["id"]
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items, "next_cursor": next, "has_more": hasMore})
	})

	api.Handle("GET /api/v1/markers/{id}", "marker:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		item, err := scanMarker(api.DB.QueryRow(r.Context(), markerSelect+" WHERE m.tenant_id=$1 AND m.id=$2", p.TenantID, r.PathValue("id")))
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "唛架不存在")
		}
		if err != nil {
			return err
		}
		ratios, err := markerRatioMaps(r.Context(), api.DB, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		var references int64
		if err := api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM cutting_order WHERE tenant_id=$1 AND marker_id=$2", p.TenantID, r.PathValue("id")).Scan(&references); err != nil {
			return err
		}
		item["ratios"] = ratios
		item["reference_count"] = references
		return httpapi.JSON(w, 200, item)
	})

	api.Handle("GET /api/v1/markers/{id}/references", "marker:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if err := requireMarker(r.Context(), api.DB, p.TenantID, r.PathValue("id")); err != nil {
			return err
		}
		rows, err := api.DB.Query(r.Context(), `SELECT id,cutting_no,COALESCE(bed_no,''),fabric_layers,cut_qty,status FROM cutting_order WHERE tenant_id=$1 AND marker_id=$2 ORDER BY cutting_no,id`, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, cuttingNo, bedNo, status string
			var layers, cutQty int64
			if err := rows.Scan(&id, &cuttingNo, &bedNo, &layers, &cutQty, &status); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "cutting_no": cuttingNo, "bed_no": bedNo, "fabric_layers": layers, "cut_qty": cutQty, "status": status})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/markers", "marker:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in markerRequest
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		var st markerState
		st.apply(in)
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		snap, err := resolveMarker(r.Context(), tx, p.TenantID, &st)
		if err != nil {
			return err
		}
		id, now := platform.ID(), platform.Now()
		if err := insertMarker(r.Context(), tx, p, api.Config.NodeID, id, st, snap, now); err != nil {
			return markerWriteError(err)
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create", "marker", id, nil, in, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 201, markerCommandResponse(id, 1, "pending", snap))
	})

	api.Handle("PATCH /api/v1/markers/{id}", "marker:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in markerRequest
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		id := r.PathValue("id")
		st, version, _, err := loadMarkerState(r.Context(), tx, p.TenantID, id)
		if err != nil {
			return err
		}
		if in.Version != version {
			return httpapi.Error(409, "VERSION_CONFLICT", "唛架已变更，请刷新后重试")
		}
		var references int64
		if err := tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM cutting_order WHERE tenant_id=$1 AND marker_id=$2", p.TenantID, id).Scan(&references); err != nil {
			return err
		}
		if references > 0 {
			if in.touchesBusinessFields() {
				return httpapi.Error(409, "MARKER_LOCKED", "唛架已被裁床单引用，配比、层数、用料和审核字段已锁定，仅可修改备注")
			}
			if _, err := tx.Exec(r.Context(), "UPDATE marker SET remark=$1,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6", optionalText(in.Remark), platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, id); err != nil {
				return err
			}
			if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "update_remark", "marker", id, nil, in, ""); err != nil {
				return err
			}
			if err := tx.Commit(r.Context()); err != nil {
				return err
			}
			return httpapi.JSON(w, 200, map[string]any{"id": id, "version": version + 1, "review_status": "approved", "sync_status": "pending"})
		}
		st.apply(in)
		snap, err := resolveMarker(r.Context(), tx, p.TenantID, &st)
		if err != nil {
			return err
		}
		now := platform.Now()
		if err := updateMarker(r.Context(), tx, p, api.Config.NodeID, id, st, snap, version, now); err != nil {
			return markerWriteError(err)
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "update", "marker", id, nil, in, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, markerCommandResponse(id, version+1, "pending", snap))
	})

	api.Handle("POST /api/v1/markers/{id}/submit-review", "marker:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version int64 `json:"version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		id := r.PathValue("id")
		st, version, status, err := loadMarkerState(r.Context(), tx, p.TenantID, id)
		if err != nil {
			return err
		}
		if in.Version != version {
			return httpapi.Error(409, "VERSION_CONFLICT", "唛架已变更，请刷新后重试")
		}
		if status == "approved" {
			return httpapi.Error(409, "INVALID_STATE", "已通过审核的唛架需先修改后再提交")
		}
		if st.DrawingAttachmentID == nil {
			return httpapi.Error(400, "VALIDATION_ERROR", "提交审核前必须上传唛架图")
		}
		if _, err := resolveMarker(r.Context(), tx, p.TenantID, &st); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "UPDATE marker SET review_status='pending',review_reason=NULL,version=version+1,updated_at=$1,updated_by=$2,updated_node=$3 WHERE tenant_id=$4 AND id=$5", platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, id); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "submit_review", "marker", id, map[string]any{"review_status": status}, map[string]any{"review_status": "pending"}, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": id, "version": version + 1, "review_status": "pending", "sync_status": "pending"})
	})

	api.Handle("POST /api/v1/markers/{id}/review", "marker:review", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Version            int64   `json:"version"`
			Decision           string  `json:"decision"`
			Reason             string  `json:"reason"`
			ReviewerEmployeeID *string `json:"reviewer_employee_id"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.Decision != "approve" && in.Decision != "reject" {
			return httpapi.Error(400, "VALIDATION_ERROR", "审核结论只能是 approve 或 reject")
		}
		in.Reason = strings.TrimSpace(in.Reason)
		if in.Decision == "reject" && in.Reason == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "驳回必须填写意见")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		id := r.PathValue("id")
		_, version, status, err := loadMarkerState(r.Context(), tx, p.TenantID, id)
		if err != nil {
			return err
		}
		if in.Version != version {
			return httpapi.Error(409, "VERSION_CONFLICT", "唛架已变更，请刷新后重试")
		}
		if status != "pending" {
			return httpapi.Error(409, "INVALID_STATE", "只有待审核的唛架可以审核")
		}
		reviewer := optionalText(in.ReviewerEmployeeID)
		if reviewer == nil && p.EmployeeID != "" {
			reviewer = &p.EmployeeID
		}
		newStatus := "approved"
		if in.Decision == "reject" {
			newStatus = "rejected"
		}
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), "UPDATE marker SET review_status=$1,reviewer_employee_id=$2,reviewed_at=$3,review_reason=$4,version=version+1,updated_at=$3,updated_by=$5,updated_node=$6 WHERE tenant_id=$7 AND id=$8", newStatus, reviewer, now, optionalText(&in.Reason), p.UserID, api.Config.NodeID, p.TenantID, id); err != nil {
			return markerWriteError(err)
		}
		response := map[string]any{"id": id, "review_status": newStatus, "version": version + 1, "sync_status": "pending"}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "review", "marker", id, map[string]any{"review_status": status}, response, in.Reason); err != nil {
			return err
		}
		if err := httpapi.Outbox(r.Context(), tx, p.TenantID, api.Config.NodeID, "marker", id, "marker.reviewed", response); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
	})

	api.Handle("DELETE /api/v1/markers/{id}", "marker:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		id := r.PathValue("id")
		_, _, status, err := loadMarkerState(r.Context(), tx, p.TenantID, id)
		if err != nil {
			return err
		}
		if status != "pending" {
			return httpapi.Error(409, "INVALID_STATE", "只有待审核的唛架可以删除")
		}
		var references int64
		if err := tx.QueryRow(r.Context(), "SELECT COUNT(*) FROM cutting_order WHERE tenant_id=$1 AND marker_id=$2", p.TenantID, id).Scan(&references); err != nil {
			return err
		}
		if references > 0 {
			appErr := httpapi.Error(409, "MARKER_IN_USE", "唛架已被裁床单引用，不能删除")
			appErr.Details = map[string]any{"reference_count": references}
			return appErr
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM marker_size_ratio WHERE tenant_id=$1 AND marker_id=$2", p.TenantID, id); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), "DELETE FROM marker WHERE tenant_id=$1 AND id=$2", p.TenantID, id); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "delete", "marker", id, nil, nil, "待审核唛架删除"); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"deleted": true})
	})
}

func (st *markerState) apply(in markerRequest) {
	if in.MarkerNo != nil {
		st.MarkerNo = strings.TrimSpace(*in.MarkerNo)
	}
	if in.MarkerDate != nil {
		st.MarkerDate = strings.TrimSpace(*in.MarkerDate)
	}
	if in.WorkOrderID != nil {
		st.WorkOrderID = strings.TrimSpace(*in.WorkOrderID)
	}
	if in.Color != nil {
		st.Color = strings.TrimSpace(*in.Color)
	}
	if in.MaterialID != nil {
		st.MaterialID = strings.TrimSpace(*in.MaterialID)
	}
	if in.LayoutEmployeeID != nil {
		st.LayoutEmployeeID = strings.TrimSpace(*in.LayoutEmployeeID)
	}
	if in.FabricWidthCM != nil {
		st.FabricWidthCM = optionalText(in.FabricWidthCM)
	}
	if in.FabricLayers != nil {
		st.FabricLayers = *in.FabricLayers
	}
	if in.MarkerLengthM != nil {
		st.MarkerLengthM = strings.TrimSpace(*in.MarkerLengthM)
	}
	if in.UtilizationRate != nil {
		st.UtilizationRate = optionalText(in.UtilizationRate)
	}
	if in.DrawingAttachmentID != nil {
		st.DrawingAttachmentID = optionalText(in.DrawingAttachmentID)
	}
	if in.Remark != nil {
		st.Remark = optionalText(in.Remark)
	}
	if in.Ratios != nil {
		st.Ratios = *in.Ratios
	}
}

func optionalText(value *string) *string {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(*value)
	if text == "" {
		return nil
	}
	return &text
}

func resolveMarker(ctx context.Context, tx pgx.Tx, tenantID string, st *markerState) (markerSnapshot, error) {
	var snap markerSnapshot
	if st.MarkerNo == "" || st.MarkerDate == "" || st.WorkOrderID == "" || st.Color == "" || st.MaterialID == "" || st.LayoutEmployeeID == "" {
		return snap, httpapi.Error(400, "VALIDATION_ERROR", "唛架编号、排版日期、工单、颜色、面料和排版人必填")
	}
	if _, err := time.Parse("2006-01-02", st.MarkerDate); err != nil {
		return snap, httpapi.Error(400, "VALIDATION_ERROR", "排版日期格式应为 YYYY-MM-DD")
	}
	if st.FabricLayers <= 0 {
		return snap, httpapi.Error(400, "VALIDATION_ERROR", "拉布层数必须大于零")
	}
	length, err := decimal.NewFromString(st.MarkerLengthM)
	if err != nil || !length.IsPositive() {
		return snap, httpapi.Error(400, "VALIDATION_ERROR", "唛架长度必须大于零")
	}
	if st.FabricWidthCM != nil {
		width, err := decimal.NewFromString(*st.FabricWidthCM)
		if err != nil || !width.IsPositive() {
			return snap, httpapi.Error(400, "VALIDATION_ERROR", "门幅必须大于零")
		}
	}
	if st.UtilizationRate != nil {
		rate, err := decimal.NewFromString(*st.UtilizationRate)
		if err != nil || rate.IsNegative() || rate.GreaterThan(decimal.NewFromInt(1)) {
			return snap, httpapi.Error(400, "VALIDATION_ERROR", "面料利用率应在 0～1 之间")
		}
	}
	if len(st.Ratios) == 0 {
		return snap, httpapi.Error(400, "VALIDATION_ERROR", "尺码配比不能为空")
	}
	seen := map[string]bool{}
	for i := range st.Ratios {
		ratio := &st.Ratios[i]
		ratio.Size = strings.TrimSpace(ratio.Size)
		if ratio.Size == "" || ratio.RatioQty <= 0 {
			return snap, httpapi.Error(400, "VALIDATION_ERROR", "尺码和每唛件数必须有效")
		}
		if seen[ratio.Size] {
			return snap, httpapi.Error(400, "VALIDATION_ERROR", "同一唛架的尺码不可重复: "+ratio.Size)
		}
		seen[ratio.Size] = true
		if ratio.SortNo == 0 {
			ratio.SortNo = i + 1
		}
		snap.Pieces += ratio.RatioQty
	}
	err = tx.QueryRow(ctx, `SELECT w.project_id,w.product_id,p.product_code,p.name FROM work_order w JOIN product p ON p.tenant_id=w.tenant_id AND p.id=w.product_id WHERE w.tenant_id=$1 AND w.id=$2`, tenantID, st.WorkOrderID).Scan(&snap.ProjectID, &snap.ProductID, &snap.ProductCode, &snap.ProductName)
	if errors.Is(err, pgx.ErrNoRows) {
		return snap, httpapi.Error(400, "INVALID_WORK_ORDER", "工单不存在")
	}
	if err != nil {
		return snap, err
	}
	err = tx.QueryRow(ctx, `SELECT code,name,specification FROM material WHERE tenant_id=$1 AND id=$2 AND status='active'`, tenantID, st.MaterialID).Scan(&snap.MaterialCode, &snap.MaterialName, &snap.MaterialSpec)
	if errors.Is(err, pgx.ErrNoRows) {
		return snap, httpapi.Error(400, "INVALID_MATERIAL", "面料不存在或已停用")
	}
	if err != nil {
		return snap, err
	}
	var employeeExists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND id=$2)", tenantID, st.LayoutEmployeeID).Scan(&employeeExists); err != nil {
		return snap, err
	}
	if !employeeExists {
		return snap, httpapi.Error(400, "INVALID_EMPLOYEE", "排版人不存在")
	}
	if st.DrawingAttachmentID != nil {
		var attachmentExists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM attachment WHERE tenant_id=$1 AND id=$2)", tenantID, *st.DrawingAttachmentID).Scan(&attachmentExists); err != nil {
			return snap, err
		}
		if !attachmentExists {
			return snap, httpapi.Error(400, "INVALID_ATTACHMENT", "唛架图附件不存在")
		}
	}
	snap.CutQty = snap.Pieces * st.FabricLayers
	snap.EstimatedFabricM = length.Mul(decimal.NewFromInt(st.FabricLayers)).Round(2).StringFixed(2)
	return snap, nil
}

func insertMarker(ctx context.Context, tx pgx.Tx, p auth.Principal, node, id string, st markerState, snap markerSnapshot, now int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO marker(tenant_id,id,marker_no,marker_date,work_order_id,project_id,product_id,product_code,product_name,color,material_id,material_code,material_name,material_specification,fabric_width_cm,pieces_per_marker,fabric_layers,cut_qty,marker_length_m,estimated_fabric_m,utilization_rate,drawing_attachment_id,layout_employee_id,review_status,remark,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,'pending',$24,$25,$26,$27,$25,$26,$27)`,
		p.TenantID, id, st.MarkerNo, st.MarkerDate, st.WorkOrderID, snap.ProjectID, snap.ProductID, snap.ProductCode, snap.ProductName, st.Color, st.MaterialID, snap.MaterialCode, snap.MaterialName, snap.MaterialSpec, st.FabricWidthCM, snap.Pieces, st.FabricLayers, snap.CutQty, st.MarkerLengthM, snap.EstimatedFabricM, st.UtilizationRate, st.DrawingAttachmentID, st.LayoutEmployeeID, st.Remark, now, p.UserID, node)
	if err != nil {
		return err
	}
	return replaceMarkerRatios(ctx, tx, p, node, id, st.Ratios, now)
}

func updateMarker(ctx context.Context, tx pgx.Tx, p auth.Principal, node, id string, st markerState, snap markerSnapshot, version, now int64) error {
	result, err := tx.Exec(ctx, `UPDATE marker SET marker_no=$1,marker_date=$2,work_order_id=$3,project_id=$4,product_id=$5,product_code=$6,product_name=$7,color=$8,material_id=$9,material_code=$10,material_name=$11,material_specification=$12,fabric_width_cm=$13,pieces_per_marker=$14,fabric_layers=$15,cut_qty=$16,marker_length_m=$17,estimated_fabric_m=$18,utilization_rate=$19,drawing_attachment_id=$20,layout_employee_id=$21,remark=$22,review_status='pending',review_reason=NULL,reviewed_at=NULL,reviewer_employee_id=NULL,version=version+1,updated_at=$23,updated_by=$24,updated_node=$25 WHERE tenant_id=$26 AND id=$27 AND version=$28`,
		st.MarkerNo, st.MarkerDate, st.WorkOrderID, snap.ProjectID, snap.ProductID, snap.ProductCode, snap.ProductName, st.Color, st.MaterialID, snap.MaterialCode, snap.MaterialName, snap.MaterialSpec, st.FabricWidthCM, snap.Pieces, st.FabricLayers, snap.CutQty, st.MarkerLengthM, snap.EstimatedFabricM, st.UtilizationRate, st.DrawingAttachmentID, st.LayoutEmployeeID, st.Remark, now, p.UserID, node, p.TenantID, id, version)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return httpapi.Error(409, "VERSION_CONFLICT", "唛架已变更，请刷新后重试")
	}
	return replaceMarkerRatios(ctx, tx, p, node, id, st.Ratios, now)
}

func replaceMarkerRatios(ctx context.Context, tx pgx.Tx, p auth.Principal, node, markerID string, ratios []markerRatio, now int64) error {
	if _, err := tx.Exec(ctx, "DELETE FROM marker_size_ratio WHERE tenant_id=$1 AND marker_id=$2", p.TenantID, markerID); err != nil {
		return err
	}
	for _, ratio := range ratios {
		if _, err := tx.Exec(ctx, `INSERT INTO marker_size_ratio(tenant_id,id,marker_id,size,ratio_qty,sort_no,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$7,$8,$9)`, p.TenantID, platform.ID(), markerID, ratio.Size, ratio.RatioQty, ratio.SortNo, now, p.UserID, node); err != nil {
			return err
		}
	}
	return nil
}

func loadMarkerState(ctx context.Context, tx pgx.Tx, tenantID, id string) (markerState, int64, string, error) {
	var st markerState
	var version int64
	var status string
	err := tx.QueryRow(ctx, `SELECT marker_no,marker_date,work_order_id,color,material_id,layout_employee_id,fabric_width_cm::text,fabric_layers,marker_length_m::text,utilization_rate::text,drawing_attachment_id,remark,version,review_status FROM marker WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenantID, id).Scan(&st.MarkerNo, &st.MarkerDate, &st.WorkOrderID, &st.Color, &st.MaterialID, &st.LayoutEmployeeID, &st.FabricWidthCM, &st.FabricLayers, &st.MarkerLengthM, &st.UtilizationRate, &st.DrawingAttachmentID, &st.Remark, &version, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, 0, "", httpapi.Error(404, "NOT_FOUND", "唛架不存在")
	}
	if err != nil {
		return st, 0, "", err
	}
	rows, err := tx.Query(ctx, `SELECT size,ratio_qty,sort_no FROM marker_size_ratio WHERE tenant_id=$1 AND marker_id=$2 ORDER BY sort_no,size`, tenantID, id)
	if err != nil {
		return st, 0, "", err
	}
	defer rows.Close()
	for rows.Next() {
		var ratio markerRatio
		if err := rows.Scan(&ratio.Size, &ratio.RatioQty, &ratio.SortNo); err != nil {
			return st, 0, "", err
		}
		st.Ratios = append(st.Ratios, ratio)
	}
	return st, version, status, rows.Err()
}

func requireMarker(ctx context.Context, q dbQuerier, tenantID, id string) error {
	var exists bool
	if err := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM marker WHERE tenant_id=$1 AND id=$2)", tenantID, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return httpapi.Error(404, "NOT_FOUND", "唛架不存在")
	}
	return nil
}

func markerRatioMaps(ctx context.Context, q dbQuerier, tenantID, markerID string) ([]map[string]any, error) {
	rows, err := q.Query(ctx, `SELECT size,ratio_qty,sort_no FROM marker_size_ratio WHERE tenant_id=$1 AND marker_id=$2 ORDER BY sort_no,size`, tenantID, markerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var size string
		var qty int64
		var sortNo int
		if err := rows.Scan(&size, &qty, &sortNo); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{"size": size, "ratio_qty": qty, "sort_no": sortNo})
	}
	return items, rows.Err()
}

func scanMarker(s scanner) (map[string]any, error) {
	var id, markerNo, markerDate, workOrderID, projectID, productID, productCode, productName string
	var color, materialID, materialCode, materialName, length, layoutEmployeeID, reviewStatus string
	var spec, width, estimated, utilization, drawing, reviewer, reason, remark *string
	var reviewedAt *int64
	var pieces, layers, cutQty, version int64
	if err := s.Scan(&id, &markerNo, &markerDate, &workOrderID, &projectID, &productID, &productCode, &productName, &color, &materialID, &materialCode, &materialName, &spec, &width, &pieces, &layers, &cutQty, &length, &estimated, &utilization, &drawing, &layoutEmployeeID, &reviewer, &reviewStatus, &reason, &reviewedAt, &remark, &version); err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "marker_no": markerNo, "marker_date": markerDate, "work_order_id": workOrderID,
		"project_id": projectID, "product_id": productID, "product_code": productCode, "product_name": productName,
		"color": color, "material_id": materialID, "material_code": materialCode, "material_name": materialName,
		"material_specification": spec, "fabric_width_cm": width, "pieces_per_marker": pieces, "fabric_layers": layers,
		"cut_qty": cutQty, "marker_length_m": length, "estimated_fabric_m": estimated, "utilization_rate": utilization,
		"drawing_attachment_id": drawing, "layout_employee_id": layoutEmployeeID, "reviewer_employee_id": reviewer,
		"review_status": reviewStatus, "review_reason": reason, "reviewed_at": reviewedAt, "remark": remark, "version": version,
	}, nil
}

func markerCommandResponse(id string, version int64, status string, snap markerSnapshot) map[string]any {
	return map[string]any{
		"id": id, "version": version, "review_status": status, "sync_status": "pending",
		"computed": map[string]any{"pieces_per_marker": snap.Pieces, "cut_qty": snap.CutQty, "estimated_fabric_m": snap.EstimatedFabricM},
	}
}

func markerWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			return httpapi.Error(409, "DUPLICATE_MARKER_NO", "唛架编号已存在")
		case "23503":
			return httpapi.Error(400, "INVALID_REFERENCE", "唛架引用的工单、面料、员工或附件无效")
		}
	}
	return err
}
