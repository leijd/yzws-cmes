package tenantreg

import (
	"errors"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

/**
 * 体验厂加急与体验延期：
 * - 加急：审批中的体验厂，体验用户可申请加急（tenant.expedite_requested_at 落标记，
 *   平台管理员审批列表显示加急图标；同一厂只能加急一次，审批/驳回后清除）。
 * - 延期：已审批通过的体验厂管理员可申请延长体验时间（trial_extension 表），
 *   平台管理员审批并填写新的体验截止时间，通过后更新 tenant_subscription.trial_ends_at。
 * 两个接口都要求持有本机进入令牌（entry_token，短信/微信验证通过后签发）。
 */

/* 凭进入令牌校验身份，返回 (phone, tenantCode, tenantID, registrationStatus) */
func trialIdentityByEntryToken(r *http.Request, api *httpapi.API, entryToken, tenantCode string) (string, string, string, string, error) {
	phone, tokenTenant, err := api.Auth.VerifyTrialEntryToken(r.Context(), entryToken)
	if err != nil {
		return "", "", "", "", httpapi.Error(http.StatusUnauthorized, "INVALID_ENTRY_TOKEN", "体验凭证已失效，请重新用手机验证码登录")
	}
	if tenantCode != "" && tokenTenant != strings.TrimSpace(tenantCode) {
		return "", "", "", "", httpapi.Error(http.StatusForbidden, "TENANT_MISMATCH", "体验凭证与该厂不匹配")
	}
	/* 不按 status 过滤：注册被驳回时租户已停用，仍需凭有效凭证让申请人查看驳回状态；
	 * 各写接口（修改/加急/延期）再按 registration_status 分别拦截。 */
	var tenantID, regStatus string
	err = api.DB.QueryRow(r.Context(), `SELECT id,registration_status FROM tenant
		WHERE code=$1 AND contact_phone=$2 AND customer_type='trial'
		ORDER BY registered_at DESC LIMIT 1`, tokenTenant, phone).
		Scan(&tenantID, &regStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", "", "", httpapi.Error(http.StatusNotFound, "NO_TRIAL_TENANT", "该手机号还没有体验制衣厂")
	}
	if err != nil {
		return "", "", "", "", err
	}
	return phone, tokenTenant, tenantID, regStatus, nil
}

func registerTrialExtras(api *httpapi.API) {
	/* 审批中的体验厂：查看注册信息（体验主页/审批中页面展示与修改） */
	api.Public("GET /api/v1/public/trial-info", func(w http.ResponseWriter, r *http.Request) error {
		entryToken := strings.TrimSpace(r.URL.Query().Get("entry_token"))
		tenantCode := strings.TrimSpace(r.URL.Query().Get("tenant_code"))
		_, _, tenantID, regStatus, err := trialIdentityByEntryToken(r, api, entryToken, tenantCode)
		if err != nil {
			return err
		}
		var code, name, region, scale, contactName, contactPhone, contactWechat, contactQQ, contactAlipay, address, remark string
		var expeditedAt *int64
		if err := api.DB.QueryRow(r.Context(), `SELECT code,name,COALESCE(region,''),COALESCE(scale,''),COALESCE(contact_name,''),
			COALESCE(contact_phone,''),COALESCE(contact_wechat,''),COALESCE(contact_qq,''),COALESCE(contact_alipay,''),
			COALESCE(address,''),COALESCE(remark,''),expedite_requested_at
			FROM tenant WHERE id=$1`, tenantID).
			Scan(&code, &name, &region, &scale, &contactName, &contactPhone, &contactWechat, &contactQQ, &contactAlipay,
				&address, &remark, &expeditedAt); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"code": code, "name": name, "region": region, "scale": scale,
			"contact_name": contactName, "contact_phone": contactPhone, "contact_wechat": contactWechat,
			"contact_qq": contactQQ, "contact_alipay": contactAlipay, "address": address, "remark": remark,
			"registration_status": regStatus, "expedited": expeditedAt != nil})
	})

	/* 审批中的体验厂：修改注册信息（联系电话 = 体验用户登录手机号，不可修改） */
	api.Public("POST /api/v1/public/trial-update", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			EntryToken    string `json:"entry_token"`
			TenantCode    string `json:"tenant_code"`
			Name          string `json:"name"`
			Region        string `json:"region"`
			Scale         string `json:"scale"`
			ContactName   string `json:"contact_name"`
			ContactWechat string `json:"contact_wechat"`
			ContactQQ     string `json:"contact_qq"`
			ContactAlipay string `json:"contact_alipay"`
			Address       string `json:"address"`
			Remark        string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		_, _, tenantID, regStatus, err := trialIdentityByEntryToken(r, api, in.EntryToken, in.TenantCode)
		if err != nil {
			return err
		}
		if regStatus != "pending" {
			return httpapi.Error(http.StatusConflict, "NOT_PENDING", "该体验厂已审批处理，注册信息不可再修改")
		}
		name := strings.TrimSpace(in.Name)
		if name != "" && len(name) < 2 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "制衣厂名称至少 2 个字符")
		}
		scale := strings.TrimSpace(in.Scale)
		if scale != "" && !scaleSet[scale] {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "厂规模不合法")
		}
		contactWechat := strings.TrimSpace(in.ContactWechat)
		contactQQ := strings.TrimSpace(in.ContactQQ)
		contactAlipay := strings.TrimSpace(in.ContactAlipay)
		if len(contactWechat) > 64 || len(contactQQ) > 32 || len(contactAlipay) > 64 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "微信号/QQ/支付宝账号过长")
		}
		tag, err := api.DB.Exec(r.Context(), `UPDATE tenant SET
			name=CASE WHEN $1<>'' THEN $1 ELSE name END,
			region=NULLIF($2,''),scale=NULLIF($3,''),
			contact_name=CASE WHEN $4<>'' THEN $4 ELSE contact_name END,
			address=NULLIF($5,''),remark=NULLIF($6,''),
			contact_wechat=NULLIF($7,''),contact_qq=NULLIF($8,''),contact_alipay=NULLIF($9,''),version=version+1
			WHERE id=$10 AND registration_status='pending'`,
			name, strings.TrimSpace(in.Region), scale, strings.TrimSpace(in.ContactName),
			strings.TrimSpace(in.Address), strings.TrimSpace(in.Remark), contactWechat, contactQQ, contactAlipay, tenantID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "NOT_PENDING", "该体验厂已审批处理，注册信息不可再修改")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"message": "注册信息已更新，平台管理员审批时将看到最新内容"})
	})

	/* 加急申请：审批中的体验厂 */
	api.Public("POST /api/v1/public/trial-expedite", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			EntryToken string `json:"entry_token"`
			TenantCode string `json:"tenant_code"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.EntryToken) == "" {
			return httpapi.Error(http.StatusUnauthorized, "MISSING_CREDENTIAL", "请先通过手机验证码登录")
		}
		_, _, tenantID, regStatus, err := trialIdentityByEntryToken(r, api, in.EntryToken, in.TenantCode)
		if err != nil {
			return err
		}
		if regStatus != "pending" {
			return httpapi.Error(http.StatusConflict, "NOT_PENDING", "该体验厂不在待审批状态，无需加急")
		}
		var expeditedAt *int64
		if err := api.DB.QueryRow(r.Context(), `SELECT expedite_requested_at FROM tenant WHERE id=$1`, tenantID).Scan(&expeditedAt); err != nil {
			return err
		}
		if expeditedAt != nil {
			return httpapi.JSON(w, http.StatusOK, map[string]any{"already": true, "message": "已提交过加急申请，平台管理员将优先处理"})
		}
		if _, err := api.DB.Exec(r.Context(), `UPDATE tenant SET expedite_requested_at=$1,version=version+1 WHERE id=$2`,
			platform.Now(), tenantID); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"already": false, "message": "加急申请已提交，平台管理员将优先处理"})
	})

	/* 延期申请：已审批通过的体验厂管理员 */
	api.Public("POST /api/v1/public/trial-extend", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			EntryToken string `json:"entry_token"`
			TenantCode string `json:"tenant_code"`
			Reason     string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.EntryToken) == "" {
			return httpapi.Error(http.StatusUnauthorized, "MISSING_CREDENTIAL", "请先通过手机验证码登录")
		}
		phone, _, tenantID, regStatus, err := trialIdentityByEntryToken(r, api, in.EntryToken, in.TenantCode)
		if err != nil {
			return err
		}
		if regStatus != "approved" {
			return httpapi.Error(http.StatusConflict, "NOT_APPROVED", "体验厂尚在审批中，通过后才能申请延长体验")
		}
		var pendingCount int
		if err := api.DB.QueryRow(r.Context(), `SELECT COUNT(*) FROM trial_extension WHERE tenant_id=$1 AND status='pending'`, tenantID).Scan(&pendingCount); err != nil {
			return err
		}
		if pendingCount > 0 {
			return httpapi.Error(http.StatusConflict, "REQUEST_EXISTS", "已有延期申请在审核中，请等待平台管理员处理")
		}
		if _, err := api.DB.Exec(r.Context(), `INSERT INTO trial_extension(id,tenant_id,requested_phone,reason,status,created_at)
			VALUES($1,$2,$3,NULLIF($4,''),'pending',$5)`,
			platform.ID(), tenantID, phone, strings.TrimSpace(in.Reason), platform.Now()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"message": "延期申请已提交，请等待平台管理员审批"})
	})

	/* 延期状态查询：体验主页展示当前申请进度与最新体验截止时间 */
	api.Public("GET /api/v1/public/trial-extend/status", func(w http.ResponseWriter, r *http.Request) error {
		entryToken := strings.TrimSpace(r.URL.Query().Get("entry_token"))
		tenantCode := strings.TrimSpace(r.URL.Query().Get("tenant_code"))
		_, _, tenantID, _, err := trialIdentityByEntryToken(r, api, entryToken, tenantCode)
		if err != nil {
			return err
		}
		var trialEnds *int64
		_ = api.DB.QueryRow(r.Context(), `SELECT trial_ends_at FROM tenant_subscription WHERE tenant_id=$1 AND customer_type='trial'`, tenantID).Scan(&trialEnds)
		var reqID, reqStatus, reqReason, reviewReason *string
		var newEnds, reviewedAt *int64
		var createdAt *int64
		err = api.DB.QueryRow(r.Context(), `SELECT id,status,NULLIF(reason,''),review_reason,new_trial_ends_at,created_at,reviewed_at
			FROM trial_extension WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT 1`, tenantID).
			Scan(&reqID, &reqStatus, &reqReason, &reviewReason, &newEnds, &createdAt, &reviewedAt)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var request any
		if reqID != nil {
			request = map[string]any{"id": *reqID, "status": *reqStatus, "reason": reqReason,
				"review_reason": reviewReason, "new_trial_ends_at": newEnds, "created_at": createdAt, "reviewed_at": reviewedAt}
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"trial_ends_at": trialEnds, "request": request})
	})
}

/* ===== 平台管理员：延期申请审批 ===== */
func registerTrialPlatform(api *httpapi.API) {
	/* 延期申请列表（status 缺省 pending；all 返回全部） */
	api.PlatformHandle("GET /api/v1/platform/trial-extensions", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "pending"
		}
		query := `SELECT e.id,t.code,t.name,COALESCE(t.short_code,''),e.requested_phone,COALESCE(e.reason,''),
			e.status,e.new_trial_ends_at,e.created_at,e.reviewed_at,COALESCE(e.review_reason,''),
			COALESCE(ts.trial_ends_at,0)
			FROM trial_extension e JOIN tenant t ON t.id=e.tenant_id
			LEFT JOIN tenant_subscription ts ON ts.tenant_id=e.tenant_id`
		args := []any{}
		if status != "all" {
			query += ` WHERE e.status=$1`
			args = append(args, status)
		}
		query += ` ORDER BY e.created_at DESC LIMIT 200`
		rows, err := api.DB.Query(r.Context(), query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, shortCode, phone, reason, reqStatus, reviewReason string
			var createdAt int64
			var newEnds, reviewedAt, currentEnds *int64
			if err := rows.Scan(&id, &code, &name, &shortCode, &phone, &reason,
				&reqStatus, &newEnds, &createdAt, &reviewedAt, &reviewReason, &currentEnds); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "tenant_code": code, "tenant_name": name,
				"short_code": shortCode, "requested_phone": phone, "reason": reason, "status": reqStatus,
				"new_trial_ends_at": newEnds, "created_at": createdAt, "reviewed_at": reviewedAt,
				"review_reason": reviewReason, "current_trial_ends_at": currentEnds})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	/* 通过延期：平台管理员填写/选择新的体验截止时间（必须晚于当前截止时间） */
	api.PlatformHandle("POST /api/v1/platform/trial-extensions/{id}/approve", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			NewTrialEndsAt int64  `json:"new_trial_ends_at"` /* 微秒时间戳 */
			Reason         string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if in.NewTrialEndsAt <= platform.Now() {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "新的体验截止时间必须晚于当前时间")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var tenantID, reqStatus string
		var currentEnds *int64
		err = tx.QueryRow(r.Context(), `SELECT e.tenant_id,e.status,ts.trial_ends_at
			FROM trial_extension e
			LEFT JOIN tenant_subscription ts ON ts.tenant_id=e.tenant_id
			WHERE e.id=$1 FOR UPDATE OF e`, r.PathValue("id")).Scan(&tenantID, &reqStatus, &currentEnds)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "延期申请不存在")
		}
		if err != nil {
			return err
		}
		if reqStatus != "pending" {
			return httpapi.Error(http.StatusConflict, "NOT_PENDING", "该申请已处理，不能重复审批")
		}
		if currentEnds != nil && in.NewTrialEndsAt <= *currentEnds {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "新的体验截止时间必须晚于当前截止时间")
		}
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), `UPDATE trial_extension SET status='approved',new_trial_ends_at=$1,
			reviewed_at=$2,reviewed_by=$3,review_reason=NULLIF($4,'') WHERE id=$5`,
			in.NewTrialEndsAt, now, p.AdminID, in.Reason, r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE tenant_subscription SET trial_ends_at=$1,updated_at=$2,updated_by=$3,
			version=version+1 WHERE tenant_id=$4 AND customer_type='trial'`,
			in.NewTrialEndsAt, now, "platform:"+p.AdminID, tenantID); err != nil {
			return err
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "trial_extension_approve", "tenant", tenantID,
			map[string]any{"status": "pending"},
			map[string]any{"status": "approved", "new_trial_ends_at": in.NewTrialEndsAt}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"status": "approved", "new_trial_ends_at": in.NewTrialEndsAt,
			"message": "已批准延期，体验厂截止时间已更新"})
	})

	/* 驳回延期 */
	api.PlatformHandle("POST /api/v1/platform/trial-extensions/{id}/reject", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tag, err := api.DB.Exec(r.Context(), `UPDATE trial_extension SET status='rejected',reviewed_at=$1,reviewed_by=$2,
			review_reason=NULLIF($3,'') WHERE id=$4 AND status='pending'`,
			platform.Now(), p.AdminID, strings.TrimSpace(in.Reason), r.PathValue("id"))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "NOT_PENDING", "该申请不存在或已处理")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"status": "rejected", "message": "已驳回该延期申请"})
	})
}
