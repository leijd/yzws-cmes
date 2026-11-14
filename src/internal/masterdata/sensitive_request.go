package masterdata

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/* ===== 敏感信息查看授权申请（审批链路） =====
 * 非管理员在「解锁敏感信息」弹窗发起申请 → 租户管理员的提醒列表新增一条消息 →
 * 点击详情展示申请信息 → 允许时签发授权码并短信下发给申请人手机；未接真实短信服务商
 * （演示模式）时把授权码回显在申请人页面。拒绝时仅通知申请人。
 * 授权校验、5 分钟解锁票据仍走 /employees/{id}/reveal，无需改动。 ===== */

const (
	viewRequestCodeTTLMinutes = 30
	viewRequestPendingTTL     = 30 * time.Minute
)

var viewRequestPhonePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)

type viewRequest struct {
	ID              string
	TenantID        string
	RequesterUserID string
	RequesterName   string
	RequesterPhone  string
	EmployeeID      string
	EmployeeName    string
	FieldKey        string
	FieldLabel      string
	Reason          string
	Status          string
	GrantID         string
	DemoCode        string
	CodeExpiresAt   int64
	SMSSent         bool
	ReviewedBy      string
	ReviewedAt      int64
	CreatedAt       int64
	UpdatedAt       int64
}

const viewRequestColumns = `id,tenant_id,requester_user_id,requester_name,requester_phone,employee_id,employee_name,
	field_key,field_label,reason,status,grant_id,demo_code,code_expires_at,sms_sent,reviewed_by,reviewed_at,created_at,updated_at`

func scanViewRequest(rows pgx.Rows) (viewRequest, error) {
	var v viewRequest
	err := rows.Scan(&v.ID, &v.TenantID, &v.RequesterUserID, &v.RequesterName, &v.RequesterPhone,
		&v.EmployeeID, &v.EmployeeName, &v.FieldKey, &v.FieldLabel, &v.Reason, &v.Status,
		&v.GrantID, &v.DemoCode, &v.CodeExpiresAt, &v.SMSSent, &v.ReviewedBy, &v.ReviewedAt,
		&v.CreatedAt, &v.UpdatedAt)
	return v, err
}

/* viewRequestJSON 只在授权码仍有效时返回演示码；真实短信已下发时不返回明文。 */
func viewRequestJSON(v viewRequest) map[string]any {
	now := platform.Now()
	codeExpired := v.Status == "approved" && v.CodeExpiresAt > 0 && v.CodeExpiresAt <= now
	demoCode := ""
	if !codeExpired && !v.SMSSent {
		demoCode = v.DemoCode
	}
	return map[string]any{
		"id": v.ID, "requester_user_id": v.RequesterUserID, "requester_name": v.RequesterName,
		"requester_phone": maskRequestPhone(v.RequesterPhone), "employee_id": v.EmployeeID,
		"employee_name": v.EmployeeName, "field_key": v.FieldKey, "field_label": v.FieldLabel,
		"reason": v.Reason, "status": v.Status, "grant_id": v.GrantID,
		"demo_code": demoCode, "demo_code_visible": demoCode != "", "code_expired": codeExpired,
		"code_expires_at": v.CodeExpiresAt, "sms_sent": v.SMSSent,
		"reviewed_by": v.ReviewedBy, "reviewed_at": v.ReviewedAt,
		"created_at": v.CreatedAt, "updated_at": v.UpdatedAt,
	}
}

func maskRequestPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func registerSensitiveViewRequests(api *httpapi.API) {
	/* 申请人：发起授权码申请。同一人对同一员工已有待审批申请时直接复用，避免重复提醒。 */
	api.Handle("POST /api/v1/sensitive-view-requests", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			EmployeeID string `json:"employee_id"`
			FieldKey   string `json:"field_key"`
			FieldLabel string `json:"field_label"`
			Reason     string `json:"reason"`
			Phone      string `json:"phone"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		employeeID := strings.TrimSpace(in.EmployeeID)
		if employeeID == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请选择要查看的员工档案")
		}
		var employeeName string
		if err := api.DB.QueryRow(ctx, `SELECT name FROM employee WHERE tenant_id=$1 AND id=$2`, p.TenantID, employeeID).Scan(&employeeName); err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "员工档案不存在")
		} else if err != nil {
			return err
		}

		phone := strings.TrimSpace(in.Phone)
		if phone == "" {
			phone = requesterProfilePhone(ctx, api, p)
		}
		if phone != "" && !viewRequestPhonePattern.MatchString(phone) {
			return httpapi.Error(http.StatusBadRequest, "INVALID_PHONE", "请输入正确的 11 位手机号")
		}
		requesterName := requesterDisplayName(ctx, api, p)

		/* 复用最近 30 分钟内仍待审批的同一申请人+同一员工申请 */
		var existing viewRequest
		rows, err := api.DB.Query(ctx, `SELECT `+viewRequestColumns+` FROM sensitive_view_request
			WHERE tenant_id=$1 AND requester_user_id=$2 AND employee_id=$3 AND status='pending' AND created_at>$4
			ORDER BY created_at DESC LIMIT 1`, p.TenantID, p.UserID, employeeID, platform.Now()-int64(viewRequestPendingTTL/time.Microsecond))
		if err != nil {
			return err
		}
		if rows.Next() {
			existing, err = scanViewRequest(rows)
			rows.Close()
			if err != nil {
				return err
			}
			/* 重新提交时允许补充手机号/事由，避免待审批申请缺少短信接收号码后无法补救 */
			reason := strings.TrimSpace(in.Reason)
			newPhone := firstNonEmpty(phone, existing.RequesterPhone)
			newReason := firstNonEmpty(reason, existing.Reason)
			if newPhone != existing.RequesterPhone || newReason != existing.Reason {
				if _, err := api.DB.Exec(ctx, `UPDATE sensitive_view_request SET requester_phone=$1,reason=$2,updated_at=$3
					WHERE tenant_id=$4 AND id=$5`, newPhone, newReason, platform.Now(), p.TenantID, existing.ID); err != nil {
					return err
				}
				existing.RequesterPhone, existing.Reason = newPhone, newReason
			}
			return httpapi.JSON(w, http.StatusOK, map[string]any{"request": viewRequestJSON(existing), "existing": true})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		now := platform.Now()
		v := viewRequest{
			ID: platform.ID(), TenantID: p.TenantID, RequesterUserID: p.UserID,
			RequesterName: requesterName, RequesterPhone: phone, EmployeeID: employeeID,
			EmployeeName: employeeName, FieldKey: strings.TrimSpace(in.FieldKey),
			FieldLabel: strings.TrimSpace(in.FieldLabel), Reason: strings.TrimSpace(in.Reason),
			Status: "pending", CreatedAt: now, UpdatedAt: now,
		}
		if _, err := api.DB.Exec(ctx, `INSERT INTO sensitive_view_request(id,tenant_id,requester_user_id,requester_name,requester_phone,
			employee_id,employee_name,field_key,field_label,reason,status,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'pending',$11,$11)`,
			v.ID, v.TenantID, v.RequesterUserID, v.RequesterName, v.RequesterPhone, v.EmployeeID,
			v.EmployeeName, v.FieldKey, v.FieldLabel, v.Reason, now); err != nil {
			return err
		}

		/* 租户管理员提醒：列表里必须有这条消息，owner 指向申请记录便于点开详情 */
		admins, err := tenantAdminUserIDs(ctx, api, p.TenantID)
		if err != nil {
			return err
		}
		fieldText := v.FieldLabel
		if fieldText == "" {
			fieldText = "敏感信息"
		}
		body := fmt.Sprintf("%s 向租户管理员发出查看敏感信息授权码申请：员工「%s」的%s。请在提醒详情中查看请求信息并审批。",
			v.RequesterName, v.EmployeeName, fieldText)
		if err := insertNotifications(ctx, api, p.TenantID, admins, "sensitive_view_request",
			"sensitive_view_request", v.ID, "敏感信息查看授权码申请", body); err != nil {
			return err
		}
		if len(admins) == 0 {
			log.Printf("[sensitive-view] request=%s no tenant_admin found, notification skipped", v.ID)
		}
		if err := auditOnce(ctx, api, p, "sensitive_view_request_create", "sensitive_view_request", v.ID,
			nil, map[string]any{"employee_id": v.EmployeeID, "field_key": v.FieldKey}, v.Reason); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"request": viewRequestJSON(v), "existing": false})
	})

	/* 申请人页面：查询自己提交过的申请（含演示模式授权码），用于弹窗轮询展示。 */
	api.Handle("GET /api/v1/sensitive-view-requests/mine", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID, p.UserID}
		where := `tenant_id=$1 AND requester_user_id=$2`
		if employeeID := strings.TrimSpace(r.URL.Query().Get("employee_id")); employeeID != "" {
			args = append(args, employeeID)
			where += ` AND employee_id=$` + fmt.Sprint(len(args))
		}
		args = append(args, 30)
		rows, err := api.DB.Query(r.Context(), `SELECT `+viewRequestColumns+` FROM sensitive_view_request WHERE `+where+`
			ORDER BY created_at DESC LIMIT $`+fmt.Sprint(len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			v, err := scanViewRequest(rows)
			if err != nil {
				return err
			}
			items = append(items, viewRequestJSON(v))
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	/* 租户管理员：授权申请列表（可按状态过滤，默认只看待审批）。 */
	api.Handle("GET /api/v1/sensitive-view-requests", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := `tenant_id=$1`
		if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" && status != "all" {
			args = append(args, status)
			where += ` AND status=$` + fmt.Sprint(len(args))
		}
		args = append(args, 50)
		rows, err := api.DB.Query(r.Context(), `SELECT `+viewRequestColumns+` FROM sensitive_view_request WHERE `+where+`
			ORDER BY created_at DESC LIMIT $`+fmt.Sprint(len(args)), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			v, err := scanViewRequest(rows)
			if err != nil {
				return err
			}
			items = append(items, viewRequestJSON(v))
		}
		if err := rows.Err(); err != nil {
			return err
		}
		var pending int64
		if err := api.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM sensitive_view_request WHERE tenant_id=$1 AND status='pending'`, p.TenantID).Scan(&pending); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "pending_count": pending})
	})

	/* 详情：申请人本人或租户管理员可看（提醒列表点「详情」时调用）。 */
	api.Handle("GET /api/v1/sensitive-view-requests/{id}", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		v, err := loadViewRequest(r.Context(), api, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if v.RequesterUserID != p.UserID && !p.Permissions["identity:write"] {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "没有查看该申请的权限")
		}
		return httpapi.JSON(w, http.StatusOK, viewRequestJSON(v))
	})

	/* 允许：签发限定到该员工的授权码 → 短信下发；演示模式则把授权码留在申请记录中，
	 * 由申请人页面展示。同时给申请人回一条结果提醒，并记审计（不含授权码明文）。 */
	api.Handle("POST /api/v1/sensitive-view-requests/{id}/approve", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		v, err := loadViewRequest(ctx, api, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if v.Status != "pending" {
			return httpapi.Error(http.StatusConflict, "REQUEST_HANDLED", "该申请已处理，请刷新后重试")
		}
		code, err := randomGrantCode()
		if err != nil {
			return err
		}
		now := platform.Now()
		expiresAt := now + int64(viewRequestCodeTTLMinutes)*int64(time.Minute/time.Microsecond)

		/* 先发短信再落库：短信失败时申请仍为待审批，管理员可重试，不会产生无效授权码。 */
		smsSent := false
		demoCode := ""
		if api.SMS != nil && api.SMS.Configured() {
			if !viewRequestPhonePattern.MatchString(v.RequesterPhone) {
				return httpapi.Error(http.StatusBadRequest, "PHONE_REQUIRED", "申请人未登记正确的手机号，无法短信下发授权码")
			}
			if _, err := api.SMS.Send(v.RequesterPhone, code, viewRequestCodeTTLMinutes*60); err != nil {
				log.Printf("[sensitive-view] sms failed request=%s phone=%s error=%v", v.ID, maskRequestPhone(v.RequesterPhone), err)
				return httpapi.Error(http.StatusBadGateway, "SMS_FAILED", "授权码短信发送失败，请稍后重试")
			}
			smsSent = true
		} else {
			demoCode = code
			log.Printf("[sensitive-view] demo sms request=%s phone=%s code=%s (no sms provider configured)", v.ID, maskRequestPhone(v.RequesterPhone), code)
		}

		grantID := platform.ID()
		if _, err := api.DB.Exec(ctx, `INSERT INTO sensitive_view_grant(id,tenant_id,table_name,row_id,code_hash,status,expires_at,created_by,created_at)
			VALUES($1,$2,'employee',$3,$4,'active',$5,$6,$7)`,
			grantID, p.TenantID, v.EmployeeID, sha256Hex(code), expiresAt, p.UserID, now); err != nil {
			return err
		}
		if _, err := api.DB.Exec(ctx, `UPDATE sensitive_view_request SET status='approved',grant_id=$1,demo_code=$2,
			code_expires_at=$3,sms_sent=$4,reviewed_by=$5,reviewed_at=$6,updated_at=$6
			WHERE tenant_id=$7 AND id=$8 AND status='pending'`,
			grantID, demoCode, expiresAt, smsSent, p.UserID, now, p.TenantID, v.ID); err != nil {
			return err
		}
		resultBody := fmt.Sprintf("您申请查看员工「%s」敏感信息的授权码已通过审批，%s",
			v.EmployeeName, "请查收手机短信。")
		if demoCode != "" {
			resultBody = fmt.Sprintf("您申请查看员工「%s」敏感信息的授权码已通过审批。当前未接入短信服务商（演示模式），请在解锁弹窗/申请页面查看授权码。", v.EmployeeName)
		}
		if err := insertNotifications(ctx, api, p.TenantID, []string{v.RequesterUserID}, "sensitive_view_request_result",
			"sensitive_view_request", v.ID, "敏感信息查看授权申请已通过", resultBody); err != nil {
			return err
		}
		if err := auditOnce(ctx, api, p, "sensitive_view_request_approve", "sensitive_view_request", v.ID,
			nil, map[string]any{"grant_id": grantID, "employee_id": v.EmployeeID, "sms_sent": smsSent, "expires_at": expiresAt}, ""); err != nil {
			return err
		}
		v.Status, v.GrantID, v.DemoCode, v.CodeExpiresAt, v.SMSSent = "approved", grantID, demoCode, expiresAt, smsSent
		v.ReviewedBy, v.ReviewedAt, v.UpdatedAt = p.UserID, now, now
		out := viewRequestJSON(v)
		if smsSent {
			out["message"] = "已允许申请，授权码已短信发送至申请人手机"
		} else {
			out["message"] = "已允许申请；当前未连接真实短信环境，授权码已在申请人页面显示"
		}
		return httpapi.JSON(w, http.StatusOK, out)
	})

	/* 拒绝：不签发授权码，仅通知申请人并记审计。 */
	api.Handle("POST /api/v1/sensitive-view-requests/{id}/reject", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		v, err := loadViewRequest(ctx, api, p.TenantID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if v.Status != "pending" {
			return httpapi.Error(http.StatusConflict, "REQUEST_HANDLED", "该申请已处理，请刷新后重试")
		}
		now := platform.Now()
		if _, err := api.DB.Exec(ctx, `UPDATE sensitive_view_request SET status='rejected',reviewed_by=$1,
			reviewed_at=$2,updated_at=$2 WHERE tenant_id=$3 AND id=$4 AND status='pending'`,
			p.UserID, now, p.TenantID, v.ID); err != nil {
			return err
		}
		if err := insertNotifications(ctx, api, p.TenantID, []string{v.RequesterUserID}, "sensitive_view_request_result",
			"sensitive_view_request", v.ID, "敏感信息查看授权申请已拒绝",
			fmt.Sprintf("您申请查看员工「%s」敏感信息的授权码未获批准，如有需要请联系租户管理员。", v.EmployeeName)); err != nil {
			return err
		}
		if err := auditOnce(ctx, api, p, "sensitive_view_request_reject", "sensitive_view_request", v.ID,
			nil, map[string]any{"employee_id": v.EmployeeID}, in.Reason); err != nil {
			return err
		}
		v.Status, v.ReviewedBy, v.ReviewedAt, v.UpdatedAt = "rejected", p.UserID, now, now
		return httpapi.JSON(w, http.StatusOK, viewRequestJSON(v))
	})
}

func loadViewRequest(ctx context.Context, api *httpapi.API, tenantID, id string) (viewRequest, error) {
	rows, err := api.DB.Query(ctx, `SELECT `+viewRequestColumns+` FROM sensitive_view_request WHERE tenant_id=$1 AND id=$2`, tenantID, id)
	if err != nil {
		return viewRequest{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return viewRequest{}, httpapi.Error(http.StatusNotFound, "NOT_FOUND", "授权申请不存在")
	}
	v, err := scanViewRequest(rows)
	if err != nil {
		return viewRequest{}, err
	}
	if err := rows.Err(); err != nil {
		return viewRequest{}, err
	}
	return v, nil
}

/* tenantAdminUserIDs 该厂全部启用状态的租户管理员账号；角色缺失时用租户登记的管理员工号兜底。 */
func tenantAdminUserIDs(ctx context.Context, api *httpapi.API, tenantID string) ([]string, error) {
	rows, err := api.DB.Query(ctx, `SELECT DISTINCT u.id FROM user_account u
		JOIN user_role ur ON ur.tenant_id=u.tenant_id AND ur.user_id=u.id
		JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id AND r.code='tenant_admin'
		WHERE u.tenant_id=$1 AND u.status='active'`, tenantID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		return ids, nil
	}
	fallback := []string{}
	rows, err = api.DB.Query(ctx, `SELECT u.id FROM user_account u WHERE u.tenant_id=$1 AND u.status='active'
		AND u.employee_id=(SELECT admin_employee_id FROM tenant WHERE id=$1)`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		fallback = append(fallback, id)
	}
	return fallback, rows.Err()
}

/* insertNotifications 站内提醒写入（当前项目此前只有读取与标记已读，没有写入口）。 */
func insertNotifications(ctx context.Context, api *httpapi.API, tenantID string, userIDs []string,
	kind, ownerType, ownerID, title, body string) error {
	now := platform.Now()
	for _, userID := range userIDs {
		if strings.TrimSpace(userID) == "" {
			continue
		}
		if _, err := api.DB.Exec(ctx, `INSERT INTO notification(id,tenant_id,user_id,notification_type,owner_type,owner_id,title,body,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`,
			platform.ID(), tenantID, userID, kind, ownerType, ownerID, title, body, now); err != nil {
			return err
		}
	}
	return nil
}

func requesterDisplayName(ctx context.Context, api *httpapi.API, p auth.Principal) string {
	if p.EmployeeID != "" {
		var name string
		if err := api.DB.QueryRow(ctx, `SELECT name FROM employee WHERE tenant_id=$1 AND id=$2`, p.TenantID, p.EmployeeID).Scan(&name); err == nil && name != "" {
			return name
		}
	}
	return p.LoginName
}

/* requesterProfilePhone 从申请人自己的员工档案读取手机号（密文库读模式下需解密）。 */
func requesterProfilePhone(ctx context.Context, api *httpapi.API, p auth.Principal) string {
	if p.EmployeeID == "" {
		return ""
	}
	var raw map[string]any
	if err := api.DB.QueryRow(ctx, `SELECT row_to_json(t) FROM (SELECT * FROM employee WHERE tenant_id=$1 AND id=$2) t`,
		p.TenantID, p.EmployeeID).Scan(&raw); err != nil {
		return ""
	}
	dec, err := decryptEmployeeRow(ctx, api, p.TenantID, p.EmployeeID, raw)
	if err != nil {
		return ""
	}
	phone, _ := dec["phone"].(string)
	phone = strings.TrimSpace(phone)
	if !viewRequestPhonePattern.MatchString(phone) {
		return ""
	}
	return phone
}
