package production

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

func createReport(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	key, err := httpapi.IdempotencyKey(r)
	if err != nil {
		return err
	}
	var in reportInput
	if err := httpapi.Decode(r, &in); err != nil {
		return err
	}
	if in.ReportKind == "" {
		in.ReportKind = "normal"
	}
	if in.OccurredAt == 0 {
		in.OccurredAt = platform.Now()
	}
	total := in.GoodQty + in.DefectQty
	if in.TaskID == "" || total <= 0 || in.GoodQty < 0 || in.DefectQty < 0 {
		return httpapi.Error(400, "VALIDATION_ERROR", "任务和大于零的报工数量必填")
	}
	if in.ReportKind != "normal" && in.ReportKind != "rework" {
		return httpapi.Error(400, "VALIDATION_ERROR", "报工类型只能是正常报工或返工补报")
	}
	if p.EmployeeID == "" {
		return httpapi.Error(400, "EMPLOYEE_REQUIRED", "当前账号未绑定员工，不能报工")
	}
	worker := p.EmployeeID
	requestHash := platform.JSONHash(in)
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	var existingHash string
	var existingResponse []byte
	err = tx.QueryRow(r.Context(), "SELECT request_hash,response_payload FROM command_request WHERE tenant_id=$1 AND idempotency_key=$2", p.TenantID, key).Scan(&existingHash, &existingResponse)
	if err == nil {
		if existingHash != requestHash {
			return httpapi.Error(409, "IDEMPOTENCY_REUSED_WITH_DIFFERENT_BODY", "同一幂等键的请求内容不一致")
		}
		var response map[string]any
		if err := json.Unmarshal(existingResponse, &response); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, response)
	}
	if err != pgx.ErrNoRows {
		return err
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO command_request(tenant_id,id,idempotency_key,command_type,aggregate_id,request_hash,status,expires_at,created_at) VALUES($1,$2,$3,'work_report',$4,$5,'processing',$6,$7)`, p.TenantID, platform.ID(), key, in.TaskID, requestHash, time.Now().Add(30*24*time.Hour).UnixMicro(), platform.Now()); err != nil {
		return err
	}

	var workOrderID, taskStatus, workOrderStatus, reportMode, projectID, productCode, unitPrice, reportRatio string
	var planned, reported, pending, approved, standardSeconds int64
	var processSnapshotID *string
	err = tx.QueryRow(r.Context(), `SELECT t.work_order_id,t.planned_qty,t.reported_qty,t.pending_qty,t.approved_good_qty,t.status,w.status,w.report_mode,w.project_id,pr.product_code,t.unit_price::text,t.report_ratio::text,t.standard_work_seconds,s.id FROM production_task t JOIN work_order w ON w.tenant_id=t.tenant_id AND w.id=t.work_order_id JOIN product pr ON pr.tenant_id=w.tenant_id AND pr.id=w.product_id LEFT JOIN work_order_process_snapshot s ON s.tenant_id=t.tenant_id AND s.work_order_id=t.work_order_id AND s.product_process_id=t.product_process_id WHERE t.tenant_id=$1 AND t.id=$2 FOR UPDATE OF t`, p.TenantID, in.TaskID).Scan(&workOrderID, &planned, &reported, &pending, &approved, &taskStatus, &workOrderStatus, &reportMode, &projectID, &productCode, &unitPrice, &reportRatio, &standardSeconds, &processSnapshotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpapi.Error(404, "NOT_FOUND", "任务不存在")
	}
	if err != nil {
		return err
	}
	if workOrderStatus != "in_production" || taskStatus == "paused" || taskStatus == "completed" || taskStatus == "cancelled" {
		return httpapi.Error(409, "INVALID_STATE", "工单或任务状态不允许报工")
	}
	remaining := planned - reported
	if total > remaining {
		appErr := httpapi.Error(400, "QUANTITY_EXCEEDED", "本次报工超过任务剩余可报量")
		appErr.Details = map[string]any{"planned": planned, "reported": reported, "pending": pending, "remaining": remaining, "requested": total}
		return appErr
	}
	if reportMode == "A" && in.BundleID == "" {
		return httpapi.Error(400, "VALIDATION_ERROR", "模式 A 工单必须选择扎号")
	}

	var bundleNo, color, size, bundleWorkOrder string
	var bundleQty int64
	if in.BundleID != "" {
		err = tx.QueryRow(r.Context(), "SELECT bundle_no,color,size,current_qty,work_order_id FROM bundle WHERE tenant_id=$1 AND id=$2 FOR UPDATE", p.TenantID, in.BundleID).Scan(&bundleNo, &color, &size, &bundleQty, &bundleWorkOrder)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(404, "NOT_FOUND", "扎不存在")
		}
		if err != nil {
			return err
		}
		if bundleWorkOrder != workOrderID {
			return httpapi.Error(400, "BUNDLE_NOT_IN_TASK", "扎不属于该任务所在工单")
		}
	}

	var ticketID, partID *string
	if in.TicketID != "" {
		var ticketBundle, ticketPrint string
		var ticketPart *string
		err = tx.QueryRow(r.Context(), "SELECT bundle_id,bundle_part_id,print_status FROM ticket WHERE tenant_id=$1 AND id=$2", p.TenantID, in.TicketID).Scan(&ticketBundle, &ticketPart, &ticketPrint)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && (ticketBundle != in.BundleID || ticketPrint == "voided")) {
			return httpapi.Error(400, "BUNDLE_PART_INVALID", "菲票不存在、与扎不匹配或已作废")
		}
		if err != nil {
			return err
		}
		ticketID = &in.TicketID
		partID = ticketPart
	}
	if in.BundlePartID != "" {
		var partBundle string
		err = tx.QueryRow(r.Context(), "SELECT bundle_id FROM bundle_part WHERE tenant_id=$1 AND id=$2", p.TenantID, in.BundlePartID).Scan(&partBundle)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && partBundle != in.BundleID) {
			return httpapi.Error(400, "BUNDLE_PART_INVALID", "部位不属于该扎")
		}
		if err != nil {
			return err
		}
		if partID != nil && *partID != in.BundlePartID {
			return httpapi.Error(400, "BUNDLE_PART_INVALID", "部位与菲票不一致")
		}
		partID = &in.BundlePartID
	}

	if in.ReportKind == "normal" && in.BundleID != "" {
		var existingID string
		err = tx.QueryRow(r.Context(), `SELECT wr.id FROM work_report wr WHERE wr.tenant_id=$1 AND wr.task_id=$2 AND wr.bundle_id=$3 AND wr.report_kind='normal' AND wr.review_status IN ('pending','approved') AND NOT EXISTS (SELECT 1 FROM work_report rv WHERE rv.tenant_id=wr.tenant_id AND rv.original_report_id=wr.id AND rv.report_kind='reversal') LIMIT 1`, p.TenantID, in.TaskID, in.BundleID).Scan(&existingID)
		if err == nil {
			appErr := httpapi.Error(409, "DUPLICATE_BUNDLE_PROCESS", "该扎本道工序已有待审核或已通过的报工")
			appErr.Details = map[string]any{"existing_report_id": existingID, "bundle_no": bundleNo}
			return appErr
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}

	if reportMode == "A" {
		var assignedQty int64
		err = tx.QueryRow(r.Context(), "SELECT planned_qty FROM task_bundle_assignment WHERE tenant_id=$1 AND task_id=$2 AND bundle_id=$3 AND employee_id=$4 AND status='active'", p.TenantID, in.TaskID, in.BundleID, worker).Scan(&assignedQty)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(403, "BUNDLE_NOT_ASSIGNED", "该扎该工序未派给当前员工")
		}
		if err != nil {
			return err
		}
		workerUsed, err := bundleUsage(r.Context(), tx, p.TenantID, in.TaskID, in.BundleID, worker)
		if err != nil {
			return err
		}
		if workerUsed+total > assignedQty {
			appErr := httpapi.Error(400, "QUANTITY_EXCEEDED", "本次报工超过派给当前员工的扎计划量")
			appErr.Details = map[string]any{"bundle_no": bundleNo, "assigned": assignedQty, "reported": workerUsed, "requested": total}
			return appErr
		}
	}

	bundleUsed := int64(0)
	if in.BundleID != "" {
		bundleUsed, err = bundleUsage(r.Context(), tx, p.TenantID, in.TaskID, in.BundleID, "")
		if err != nil {
			return err
		}
		if bundleUsed+total > bundleQty {
			appErr := httpapi.Error(400, "QUANTITY_EXCEEDED", "本次报工超过该扎在此工序的剩余件数")
			appErr.Details = map[string]any{"bundle_no": bundleNo, "bundle_qty": bundleQty, "reported": bundleUsed, "remaining": bundleQty - bundleUsed, "requested": total}
			return appErr
		}

		sequenceRequired, err := boolFeatureSetting(r.Context(), tx, p.TenantID, "report", "bundle_sequence_required", true)
		if err != nil {
			return err
		}
		if sequenceRequired {
			early, err := listFeatureSetting(r.Context(), tx, p.TenantID, "report", "bundle_early_report")
			if err != nil {
				return err
			}
			if !containsString(early, in.BundleID) {
				var previousID, previousNo string
				err = tx.QueryRow(r.Context(), `SELECT id,bundle_no FROM bundle WHERE tenant_id=$1 AND work_order_id=$2 AND bundle_no::integer < $3::integer ORDER BY bundle_no::integer DESC LIMIT 1`, p.TenantID, workOrderID, bundleNo).Scan(&previousID, &previousNo)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
				if err == nil {
					previousUsed, err := bundleUsage(r.Context(), tx, p.TenantID, in.TaskID, previousID, "")
					if err != nil {
						return err
					}
					if previousUsed == 0 {
						appErr := httpapi.Error(409, "BUNDLE_SEQUENCE_INCOMPLETE", "前一扎尚未报本道工序，不能报当前扎")
						appErr.Details = map[string]any{"bundle_no": bundleNo, "previous_bundle_no": previousNo}
						return appErr
					}
				}
			}
		}
	}

	if err = checkPreviousProcess(r.Context(), tx, api, p.TenantID, in.TaskID, total); err != nil {
		return err
	}
	autoApprove := false
	if in.ReportKind == "normal" && in.DefectQty == 0 {
		_ = tx.QueryRow(r.Context(), `SELECT COALESCE((SELECT (setting_value #>> '{}')::boolean FROM feature_setting WHERE tenant_id=$1 AND setting_domain='report' AND setting_key='auto_approve' ORDER BY version DESC LIMIT 1),'false')`, p.TenantID).Scan(&autoApprove)
	}
	reviewStatus := "pending"
	var autoRule *int64
	if autoApprove {
		reviewStatus = "approved"
		one := int64(1)
		autoRule = &one
	}
	id, now := platform.ID(), platform.Now()
	defects, _ := json.Marshal(in.DefectItems)
	if defects == nil {
		defects = []byte("[]")
	}
	var bundleIDArg any
	if in.BundleID != "" {
		bundleIDArg = in.BundleID
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO work_report(tenant_id,id,task_id,report_kind,good_qty,defect_qty,defect_items,employee_id,unit_price,standard_work_seconds,actual_work_seconds,remark,review_status,auto_rule_version,idempotency_key,occurred_at,created_node,created_at,bundle_id,bundle_part_id,ticket_id,process_snapshot_id,device_code,project_id,work_order_id,product_code,bundle_no,color,size,client_occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30)`, p.TenantID, id, in.TaskID, in.ReportKind, in.GoodQty, in.DefectQty, defects, worker, unitPrice, standardSeconds, in.ActualWorkSeconds, in.Remark, reviewStatus, autoRule, key, in.OccurredAt, api.Config.NodeID, now, bundleIDArg, partID, ticketID, processSnapshotID, in.DeviceCode, projectID, workOrderID, productCode, bundleNo, color, size, in.ClientOccurredAt)
	if err != nil {
		return err
	}
	if autoApprove {
		if _, err = tx.Exec(r.Context(), `UPDATE production_task SET reported_qty=reported_qty+$1,approved_good_qty=approved_good_qty+$2,status='in_progress',actual_start_at=COALESCE(actual_start_at,$3),version=version+1,updated_at=$3,updated_by=$4,updated_node=$5 WHERE tenant_id=$6 AND id=$7`, total, in.GoodQty, now, p.UserID, api.Config.NodeID, p.TenantID, in.TaskID); err != nil {
			return err
		}
		if err = insertReview(r.Context(), tx, p.TenantID, id, "approve", "system", nil, nil, nil, 1, api.Config.NodeID); err != nil {
			return err
		}
		if err = insertPiecework(r.Context(), tx, p.TenantID, id, worker, "confirmed", in.GoodQty, unitPrice, in.OccurredAt, nil); err != nil {
			return err
		}
	} else {
		if _, err = tx.Exec(r.Context(), `UPDATE production_task SET reported_qty=reported_qty+$1,pending_qty=pending_qty+$1,status='in_progress',actual_start_at=COALESCE(actual_start_at,$2),version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6`, total, now, p.UserID, api.Config.NodeID, p.TenantID, in.TaskID); err != nil {
			return err
		}
	}
	for _, attachmentID := range in.AttachmentIDs {
		if _, err = tx.Exec(r.Context(), `INSERT INTO attachment_link(tenant_id,id,attachment_id,owner_type,owner_id,usage_type) SELECT tenant_id,$1,id,'report',$2,'evidence' FROM attachment WHERE tenant_id=$3 AND id=$4`, platform.ID(), id, p.TenantID, attachmentID); err != nil {
			return err
		}
	}
	bundleRemaining := int64(0)
	if in.BundleID != "" {
		bundleRemaining = bundleQty - bundleUsed - total
	}
	response := map[string]any{"report_id": id, "review_status": reviewStatus, "remaining_qty": remaining - total, "bundle_id": bundleIDArg, "bundle_remaining_qty": bundleRemaining, "sync_status": "pending", "estimated_piecework": nil}
	if autoApprove {
		amount, _ := decimal.NewFromString(unitPrice)
		response["estimated_piecework"] = amount.Mul(decimal.NewFromInt(in.GoodQty)).StringFixed(6)
		response["auto_rule_version"] = 1
	}
	if err = refreshWorkOrderProgress(r.Context(), tx, p.TenantID, workOrderID); err != nil {
		return err
	}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "report", "work_report", id, nil, in, ""); err != nil {
		return err
	}
	if err = httpapi.Outbox(r.Context(), tx, p.TenantID, api.Config.NodeID, "work_report", id, "work_report.created", response); err != nil {
		return err
	}
	responseBody, _ := json.Marshal(response)
	if _, err = tx.Exec(r.Context(), "UPDATE command_request SET status='succeeded',response_payload=$1 WHERE tenant_id=$2 AND idempotency_key=$3", responseBody, p.TenantID, key); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, 201, response)
}

func bundleUsage(ctx context.Context, tx pgx.Tx, tenantID, taskID, bundleID, employeeID string) (int64, error) {
	var used int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(wr.good_qty+wr.defect_qty),0)::bigint FROM work_report wr WHERE wr.tenant_id=$1 AND wr.task_id=$2 AND wr.bundle_id=$3 AND ($4='' OR wr.employee_id=$4) AND wr.report_kind<>'reversal' AND wr.review_status IN ('pending','approved') AND NOT EXISTS (SELECT 1 FROM work_report rv WHERE rv.tenant_id=wr.tenant_id AND rv.original_report_id=wr.id AND rv.report_kind='reversal')`, tenantID, taskID, bundleID, employeeID).Scan(&used)
	return used, err
}

func boolFeatureSetting(ctx context.Context, tx pgx.Tx, tenantID, domain, key string, fallback bool) (bool, error) {
	var value bool
	err := tx.QueryRow(ctx, `SELECT (setting_value #>> '{}')::boolean FROM feature_setting WHERE tenant_id=$1 AND setting_domain=$2 AND setting_key=$3 ORDER BY version DESC LIMIT 1`, tenantID, domain, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return fallback, nil
	}
	return value, err
}

func listFeatureSetting(ctx context.Context, tx pgx.Tx, tenantID, domain, key string) ([]string, error) {
	var raw string
	err := tx.QueryRow(ctx, `SELECT setting_value::text FROM feature_setting WHERE tenant_id=$1 AND setting_domain=$2 AND setting_key=$3 ORDER BY version DESC LIMIT 1`, tenantID, domain, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, httpapi.Error(500, "INVALID_SETTING", "配置 "+domain+"/"+key+" 格式不正确")
	}
	return values, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
