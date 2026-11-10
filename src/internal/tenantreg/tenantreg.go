// Package tenantreg 实现制衣厂自助注册与平台审批：
// 注册 → 平台管理员确认订阅版本/订阅规模并分配管理员账户与初始密码 → 启用。
package tenantreg

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

var (
	codePattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{2,32}$`)
	shortCodePattern = regexp.MustCompile(`^[A-Za-z0-9]{5}$`)
	phonePattern     = regexp.MustCompile(`^1[3-9]\d{9}$`)
	editionSet       = map[string]bool{"basic": true, "standard": true, "pro": true}
	scaleSet         = map[string]bool{"micro": true, "small": true, "medium": true, "large": true}
	registrationSet  = map[string]bool{"pending": true, "approved": true, "rejected": true}
)

// ===== 注册手机验证码（当前无短信服务商，一律演示模式：验证码由响应返回并在页面显示） =====
const (
	regCodeTTL     = 3 * time.Minute
	regResendGap   = 60 * time.Second
	regMaxAttempts = 5
)

type regCode struct {
	code      string
	sentAt    time.Time
	expiresAt time.Time
	attempts  int
}

var (
	regMu    sync.Mutex
	regCodes = map[string]*regCode{}
)

func regRandomCode() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	digits := make([]byte, 6)
	for i, b := range buf {
		digits[i] = '0' + b%10
	}
	return string(digits), nil
}

// verifyRegCode 校验注册手机验证码；通过后立即失效（一次性）。
func verifyRegCode(phone, code string) error {
	regMu.Lock()
	defer regMu.Unlock()
	challenge := regCodes[phone]
	if challenge == nil {
		return httpapi.Error(http.StatusBadRequest, "CODE_EXPIRED", "请先获取短信验证码")
	}
	if time.Now().After(challenge.expiresAt) {
		delete(regCodes, phone)
		return httpapi.Error(http.StatusBadRequest, "CODE_EXPIRED", "验证码已超过 3 分钟有效期，请重新获取")
	}
	if challenge.attempts >= regMaxAttempts {
		delete(regCodes, phone)
		return httpapi.Error(http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", "验证码错误次数过多，请重新获取")
	}
	if challenge.code != code {
		challenge.attempts++
		return httpapi.Error(http.StatusBadRequest, "INVALID_CODE", fmt.Sprintf("验证码不正确，还可尝试 %d 次", regMaxAttempts-challenge.attempts))
	}
	delete(regCodes, phone)
	return nil
}

func Register(api *httpapi.API) {
	registerPublic(api)
	registerPlatform(api)
	registerTenantProfile(api)
	registerTrialExperience(api)
	registerTrialExtras(api)
	registerTrialPlatform(api)
	registerTenantPortal(api)
}

// ===== 制衣厂自助注册 =====

func registerPublic(api *httpapi.API) {
	/* 注册手机验证码：无短信服务商配置时走演示模式，验证码在响应中返回并由页面显示。 */
	api.Public("POST /api/v1/public/tenant-register/send-code", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Phone string `json:"phone"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		phone := strings.TrimSpace(in.Phone)
		if !phonePattern.MatchString(phone) {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请输入正确的 11 位手机号")
		}
		regMu.Lock()
		for key, old := range regCodes {
			if time.Now().After(old.expiresAt.Add(10 * time.Minute)) {
				delete(regCodes, key)
			}
		}
		if previous := regCodes[phone]; previous != nil && time.Since(previous.sentAt) < regResendGap {
			retry := int((regResendGap - time.Since(previous.sentAt)).Seconds()) + 1
			regMu.Unlock()
			return httpapi.Error(http.StatusTooManyRequests, "TOO_FREQUENT", fmt.Sprintf("请 %d 秒后再获取验证码", retry))
		}
		regMu.Unlock()

		code, err := regRandomCode()
		if err != nil {
			return err
		}
		now := time.Now()
		regMu.Lock()
		regCodes[phone] = &regCode{code: code, sentAt: now, expiresAt: now.Add(regCodeTTL)}
		regMu.Unlock()
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"expires_in": int(regCodeTTL.Seconds()),
			"sms_sent":   false,
			"demo_code":  code,
			"message":    "演示模式：未配置短信服务商，验证码在响应中返回",
		})
	})

	registerFactoryAndTrial(api)
}

// allocateTenantIdentity 生成全平台唯一的厂编码与 5 位短码（同时保证 {短码}admin 管理账号可用）。
func allocateTenantIdentity(ctx context.Context, tx pgx.Tx, name, requestedCode string) (string, string, error) {
	code := strings.ToUpper(strings.TrimSpace(requestedCode))
	explicit := code != ""
	if code == "" {
		code = platform.TenantCodeBase(name, "")
	}
	if !codePattern.MatchString(code) {
		return "", "", httpapi.Error(http.StatusBadRequest, "INVALID_TENANT_CODE", "厂编码须为 2-32 位字母、数字、_ 或 -")
	}
	base := code
	for i := 0; ; i++ {
		var taken bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant WHERE code=$1)", code).Scan(&taken); err != nil {
			return "", "", err
		}
		if !taken {
			break
		}
		if explicit {
			return "", "", httpapi.Error(http.StatusConflict, "TENANT_CODE_EXISTS", "厂编码已存在，请更换")
		}
		if i >= 50 {
			return "", "", httpapi.Error(http.StatusConflict, "TENANT_CODE_EXHAUSTED", "无法生成可用的厂编码")
		}
		code = fmt.Sprintf("%s%02d", base, i+2)
	}

	usedShort := map[string]bool{}
	rows, err := tx.Query(ctx, "SELECT short_code FROM tenant")
	if err != nil {
		return "", "", err
	}
	for rows.Next() {
		var existing string
		if err := rows.Scan(&existing); err != nil {
			rows.Close()
			return "", "", err
		}
		usedShort[existing] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", "", err
	}
	usedLogin := map[string]bool{}
	loginRows, err := tx.Query(ctx, "SELECT lower(login_name) FROM user_account")
	if err != nil {
		return "", "", err
	}
	for loginRows.Next() {
		var existing string
		if err := loginRows.Scan(&existing); err != nil {
			loginRows.Close()
			return "", "", err
		}
		usedLogin[existing] = true
	}
	loginRows.Close()
	if err := loginRows.Err(); err != nil {
		return "", "", err
	}
	for _, candidate := range platform.ShortCodeCandidates(platform.TenantShortCodeBase(name, code)) {
		if usedShort[candidate] || usedLogin[strings.ToLower(candidate+"admin")] {
			continue
		}
		return code, candidate, nil
	}
	return "", "", httpapi.Error(http.StatusConflict, "SHORT_CODE_EXHAUSTED", "无法生成可用的厂标识")
}

// registerFactoryAndTrial 注册接口：制衣厂注册（pending，平台审批后启用）与
// 体验注册（trial，即时开通，与正式厂同一套功能与页面，仅客户类型与订阅状态不同）。
func registerFactoryAndTrial(api *httpapi.API) {
	api.Public("POST /api/v1/public/tenant-register", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Name          string `json:"name"`
			Code          string `json:"code"`
			Region        string `json:"region"`
			Scale         string `json:"scale"`
			ContactName   string `json:"contact_name"`
			ContactPhone  string `json:"contact_phone"`
			ContactWechat string `json:"contact_wechat"`
			ContactQQ     string `json:"contact_qq"`
			ContactAlipay string `json:"contact_alipay"`
			SmsCode       string `json:"sms_code"`
			Address       string `json:"address"`
			Remark        string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		name := strings.TrimSpace(in.Name)
		contactName := strings.TrimSpace(in.ContactName)
		contactPhone := strings.TrimSpace(in.ContactPhone)
		contactWechat := strings.TrimSpace(in.ContactWechat)
		contactQQ := strings.TrimSpace(in.ContactQQ)
		contactAlipay := strings.TrimSpace(in.ContactAlipay)
		scale := strings.TrimSpace(in.Scale)
		if len(name) < 2 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "制衣厂名称必填（至少 2 个字符）")
		}
		if contactName == "" || contactPhone == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "联系人和联系电话必填")
		}
		if len(contactWechat) > 64 || len(contactQQ) > 32 || len(contactAlipay) > 64 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "微信号/QQ/支付宝账号过长")
		}
		if !phonePattern.MatchString(contactPhone) {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请输入正确的 11 位联系电话")
		}
		if scale != "" && !scaleSet[scale] {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "厂规模不合法")
		}
		if err := verifyRegCode(contactPhone, strings.TrimSpace(in.SmsCode)); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())

		code, shortCode, err := allocateTenantIdentity(r.Context(), tx, name, in.Code)
		if err != nil {
			return err
		}

		now := platform.Now()
		tenantID := platform.ID()
		var scaleValue *string
		if scale != "" {
			scaleValue = &scale
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO tenant(id,code,name,short_code,status,customer_type,registration_status,
			registered_at,contact_name,contact_phone,contact_wechat,contact_qq,contact_alipay,address,region,scale,remark,created_at)
			VALUES($1,$2,$3,$4,'active','paid','pending',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			tenantID, code, name, shortCode, now, contactName, contactPhone, contactWechat, contactQQ, contactAlipay,
			strings.TrimSpace(in.Address), strings.TrimSpace(in.Region), scaleValue, strings.TrimSpace(in.Remark), now); err != nil {
			return err
		}

		leadNo, err := nextLeadNo(r.Context(), tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO visitor_lead(id,lead_no,factory_name,contact_name,contact_phone,address,
			source,intent_status,converted_tenant_id,remark,created_at,created_by,updated_at,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,'online','promotion',$7,'线上自助注册',$8,'public-register',$8,'public-register')`,
			platform.ID(), leadNo, name, contactName, contactPhone, strings.TrimSpace(in.Address), tenantID, now); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{
			"id": tenantID, "code": code, "name": name, "status": "pending",
			"message": "注册申请已提交，平台管理员确认订阅版本与规模并分配管理员账户后即可登录使用",
		})
	})

	// 体验注册：与制衣厂注册同一套流程与页面，即时开通体验制衣厂（trial）——
	// 真实租户 + 管理员账号 + 种子数据 + 30 天体验订阅；功能与页面与正式厂相同，仅客户类型与订阅状态不同。
	api.Public("POST /api/v1/public/trial-register", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Name          string `json:"name"`
			Region        string `json:"region"`
			Scale         string `json:"scale"`
			ContactName   string `json:"contact_name"`
			ContactPhone  string `json:"contact_phone"`
			ContactWechat string `json:"contact_wechat"`
			ContactQQ     string `json:"contact_qq"`
			ContactAlipay string `json:"contact_alipay"`
			Phone         string `json:"phone"`
			SmsCode       string `json:"sms_code"`
			Address       string `json:"address"`
			Remark        string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		name := strings.TrimSpace(in.Name)
		contactName := strings.TrimSpace(in.ContactName)
		contactPhone := strings.TrimSpace(in.ContactPhone)
		contactWechat := strings.TrimSpace(in.ContactWechat)
		contactQQ := strings.TrimSpace(in.ContactQQ)
		contactAlipay := strings.TrimSpace(in.ContactAlipay)
		if contactPhone == "" {
			contactPhone = strings.TrimSpace(in.Phone)
		}
		scale := strings.TrimSpace(in.Scale)
		/* 一键免费体验：厂名可不填（自动生成“体验厂-短码”），点免费体验即自动在制衣厂表生成记录 */
		if name != "" && len(name) < 2 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "制衣厂名称至少 2 个字符")
		}
		if contactPhone == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "联系电话必填")
		}
		if !phonePattern.MatchString(contactPhone) {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请输入正确的 11 位联系电话")
		}
		if len(contactWechat) > 64 || len(contactQQ) > 32 || len(contactAlipay) > 64 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "微信号/QQ/支付宝账号过长")
		}
		if scale != "" && !scaleSet[scale] {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "厂规模不合法")
		}
		if err := verifyRegCode(contactPhone, strings.TrimSpace(in.SmsCode)); err != nil {
			return err
		}

		/* 记录手机号码：该手机号已提交过体验厂时不再重复建厂，直接返回既有体验厂（待审/已通过均返回）。 */
		var existID, existCode, existName, existShortCode, existAdminLogin, existRegStatus string
		var existTrialEnds int64
		err := api.DB.QueryRow(r.Context(), `SELECT t.id,t.code,t.name,t.short_code,COALESCE(t.admin_login_name,''),
			COALESCE(s.trial_ends_at,0),t.registration_status
			FROM tenant t LEFT JOIN tenant_subscription s ON s.tenant_id=t.id
			WHERE t.contact_phone=$1 AND t.customer_type='trial' AND t.status='active' AND t.registration_status<>'rejected'
			ORDER BY t.registered_at DESC LIMIT 1`, contactPhone).
			Scan(&existID, &existCode, &existName, &existShortCode, &existAdminLogin, &existTrialEnds, &existRegStatus)
		if err == nil {
			result := map[string]any{
				"returning": true, "id": existID, "code": existCode, "name": existName, "short_code": existShortCode,
				"admin_login_name": existAdminLogin, "trial_ends_at": existTrialEnds,
				"trial_days": 30, "status": existRegStatus, "registration_status": existRegStatus,
			}
			if existRegStatus == "approved" {
				result["message"] = "该手机号已开通过体验厂，已为你直接进入体验主页"
			} else {
				result["message"] = "该手机号提交的体验厂正在等待平台管理员审核，通过后即可进入工作台"
			}
			/* 待审阶段也签发进入令牌：审批通过后无需再次验证手机号即可进入。 */
			if entryToken, tokenErr := api.Auth.IssueTrialEntryToken(contactPhone, existCode); tokenErr == nil {
				result["entry_token"] = entryToken
			}
			return httpapi.JSON(w, http.StatusOK, result)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())

		baseName := name
		if baseName == "" {
			baseName = "体验厂"
		}
		code, shortCode, err := allocateTenantIdentity(r.Context(), tx, baseName, "")
		if err != nil {
			return err
		}
		if name == "" {
			name = "体验厂-" + shortCode
		}
		if contactName == "" {
			contactName = "体验用户"
		}
		now := platform.Now()
		tenantID := platform.ID()
		trialEnds := now + int64((30*24*time.Hour)/time.Microsecond)
		var scaleValue *string
		if scale != "" {
			scaleValue = &scale
		}
		/* 体验厂与正式厂走同一套注册/审批：先落 pending，平台管理员审批通过后
		 * 以注册手机号作为管理员账户启用，并开始计算 30 天体验期。 */
		if _, err := tx.Exec(r.Context(), `INSERT INTO tenant(id,code,name,short_code,status,customer_type,registration_status,
			registered_at,contact_name,contact_phone,contact_wechat,contact_qq,contact_alipay,address,region,scale,remark,created_at)
			VALUES($1,$2,$3,$4,'active','trial','pending',$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$5)`,
			tenantID, code, name, shortCode, now, contactName, contactPhone, contactWechat, contactQQ, contactAlipay,
			strings.TrimSpace(in.Address), strings.TrimSpace(in.Region), scaleValue, strings.TrimSpace(in.Remark)); err != nil {
			return err
		}
		// 体验订阅：审批前为 trial 状态但尚未生效（tenant.registration_status=pending）；审批通过时重新计算 30 天有效期。
		if _, err := tx.Exec(r.Context(), `INSERT INTO tenant_subscription(id,tenant_id,edition,features,customer_type,status,
			trial_ends_at,created_at,created_by,updated_at,updated_by)
			VALUES($1,$2,'basic','{}','trial','trial',$3,$4,'trial-register',$4,'trial-register')`,
			platform.ID(), tenantID, trialEnds, now); err != nil {
			return err
		}
		leadNo, err := nextLeadNo(r.Context(), tx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO visitor_lead(id,lead_no,factory_name,contact_name,contact_phone,address,
			source,intent_status,trial_tenant_id,trial_started_at,trial_expires_at,remark,created_at,created_by,updated_at,updated_by)
			VALUES($1,$2,$3,$4,$5,$6,'online','promotion',$7,$8,$9,'线上自助体验注册',$8,'trial-register',$8,'trial-register')`,
			platform.ID(), leadNo, name, contactName, contactPhone, strings.TrimSpace(in.Address), tenantID, now, trialEnds); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		result := map[string]any{
			"id": tenantID, "code": code, "name": name, "short_code": shortCode,
			"trial_days": 30, "trial_ends_at": trialEnds, "status": "pending", "registration_status": "pending",
			"message": "体验注册申请已提交：平台管理员审核通过后，将以该手机号作为体验厂管理员账户开通，并开始 30 天体验期",
		}
		if entryToken, tokenErr := api.Auth.IssueTrialEntryToken(contactPhone, code); tokenErr == nil {
			result["entry_token"] = entryToken
		}
		return httpapi.JSON(w, http.StatusCreated, result)
	})
}

// regRandomPassword 生成 16 位初始密码（去除易混字符的字母数字），仅创建响应中返回一次。
func regRandomPassword() (string, error) {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789"
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = charset[int(b)%len(charset)]
	}
	return string(buf), nil
}

// ===== 体验页动态内容与体验功能点亮 =====
// 内容由平台管理员在平台控制面编辑（PUT /platform/settings/trial_page_content，含版本与审计），
// 体验页公开只读展示：欢迎词 → 功能介绍（痛点）→ 解决方案 + 三种选择（继续/暂停/退出）。

const trialContentSettingKey = "trial_page_content"

type trialPainPoint struct {
	Title string `json:"title"`
	Desc  string `json:"desc"`
}

type trialPageContent struct {
	WelcomeTitle  string           `json:"welcome_title"`
	WelcomeText   string           `json:"welcome_text"`
	IntroTitle    string           `json:"intro_title"`
	PainPoints    []trialPainPoint `json:"pain_points"`
	SolutionTitle string           `json:"solution_title"`
	SolutionText  string           `json:"solution_text"`
}

func defaultTrialContent() trialPageContent {
	return trialPageContent{
		WelcomeTitle: "欢迎使用一智算制衣 MES",
		WelcomeText:  "免费体验已开通：这是一个真实的制衣厂数字化车间系统，订单、扫菲、计件工资一气呵成。您可以亲手把一个订单跑起来。",
		IntroTitle:   "这些场景，您是不是也遇到过？",
		PainPoints: []trialPainPoint{
			{Title: "订单进度靠问", Desc: "货期临近才发现某道工序堆货，追责无依据。谁做没做、做到哪，全靠车间喊话。"},
			{Title: "计件工资对不清", Desc: "菲票手抄、月底对不上数，工人有意见，老板心里没底。"},
			{Title: "小厂留不住数据", Desc: "客户、单价、工艺标准都在老师傅脑子里，人走经验就走。"},
		},
		SolutionTitle: "一智算的解决方案",
		SolutionText:  "订单工单全流程跟踪、扫菲计件实时汇总、工资自动核算——后台为您准备了完整的功能体验，点击下方“继续体验”，每使用一个功能就会为您点亮一项。",
	}
}

func registerTrialExperience(api *httpapi.API) {
	api.Public("GET /api/v1/public/trial-content", func(w http.ResponseWriter, r *http.Request) error {
		var raw json.RawMessage
		err := api.DB.QueryRow(r.Context(), `SELECT setting_value FROM platform_setting
			WHERE setting_key=$1 ORDER BY version DESC LIMIT 1`, trialContentSettingKey).Scan(&raw)
		content := defaultTrialContent()
		if err == nil {
			edited := content
			if err := json.Unmarshal(raw, &edited); err == nil {
				if edited.WelcomeTitle != "" {
					content = edited
				}
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"content": content})
	})

	// 体验功能列表（platform_module 一级菜单）+ 本厂点亮进度（tenant_subscription.features：体验到=1，否则=0）。
	api.Public("GET /api/v1/public/trial-features", func(w http.ResponseWriter, r *http.Request) error {
		tenantCode := strings.TrimSpace(r.URL.Query().Get("tenant"))
		if tenantCode == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "缺少体验厂编码")
		}
		var features map[string]any
		var subStatus string
		var trialEnds *int64
		err := api.DB.QueryRow(r.Context(), `SELECT ts.features,ts.status,ts.trial_ends_at FROM tenant_subscription ts
			JOIN tenant t ON t.id=ts.tenant_id WHERE t.code=$1`, tenantCode).Scan(&features, &subStatus, &trialEnds)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "体验厂不存在")
		}
		if err != nil {
			return err
		}
		rows, err := api.DB.Query(r.Context(), `SELECT code,name,COALESCE(path,'') FROM platform_module
			WHERE module_type='menu' AND parent_id IS NULL AND status='active' ORDER BY sort_no,code`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var code, name, path string
			if err := rows.Scan(&code, &name, &path); err != nil {
				return err
			}
			lit := 0
			if v, ok := features[code]; ok {
				if n, ok := v.(float64); ok && n >= 1 {
					lit = 1
				}
			}
			items = append(items, map[string]any{"code": code, "name": name, "path": path, "lit": lit})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "status": subStatus, "trial_ends_at": trialEnds})
	})

	// 点亮：体验用户使用到某功能即置 1（只增不减），仅体验中订阅可点亮。
	api.Public("POST /api/v1/public/trial-features/light", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Tenant string `json:"tenant"`
			Code   string `json:"code"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tenantCode := strings.TrimSpace(in.Tenant)
		featureCode := strings.TrimSpace(in.Code)
		if tenantCode == "" || featureCode == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "缺少体验厂编码或功能编码")
		}
		tag, err := api.DB.Exec(r.Context(), `UPDATE tenant_subscription SET
			features=jsonb_set(COALESCE(features,'{}'::jsonb), ARRAY[$2::text], '1'::jsonb, true),
			updated_at=$3, updated_by='trial-user', version=version+1
			WHERE tenant_id=(SELECT id FROM tenant WHERE code=$1) AND status='trial'`,
			tenantCode, featureCode, platform.Now())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpapi.Error(http.StatusNotFound, "TRIAL_NOT_ACTIVE", "体验厂不存在或体验已结束")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"tenant": tenantCode, "code": featureCode, "lit": 1})
	})
}

func nextLeadNo(ctx context.Context, tx pgx.Tx) (string, error) {
	prefix := "XK" + time.Now().UTC().Format("20060102")
	var count int
	if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM visitor_lead WHERE lead_no LIKE $1", prefix+"%").Scan(&count); err != nil {
		return "", err
	}
	for seq := count + 1; seq < 1000; seq++ {
		candidate := fmt.Sprintf("%s%03d", prefix, seq)
		var taken bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM visitor_lead WHERE lead_no=$1)", candidate).Scan(&taken); err != nil {
			return "", err
		}
		if !taken {
			return candidate, nil
		}
	}
	return "", errors.New("lead no exhausted")
}

// ===== 平台管理员审批 =====

func registerPlatform(api *httpapi.API) {
	api.PlatformHandle("GET /api/v1/platform/tenant-registrations", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		status := r.URL.Query().Get("status")
		if status == "" {
			status = "pending"
		}
		if _, ok := registrationSet[status]; !ok {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "status 只能为 pending、approved 或 rejected")
		}
		rows, err := api.DB.Query(r.Context(), `SELECT t.id,t.code,t.name,t.short_code,COALESCE(t.contact_name,''),
			COALESCE(t.contact_phone,''),COALESCE(t.contact_wechat,''),COALESCE(t.contact_qq,''),COALESCE(t.contact_alipay,''),
			COALESCE(t.address,''),COALESCE(t.region,''),COALESCE(t.scale,''),
			COALESCE(t.remark,''),t.registration_status,t.created_at,t.approved_at,t.customer_type,
			EXISTS(SELECT 1 FROM tenant_subscription ts WHERE ts.tenant_id=t.id AND ts.customer_type='trial'),
			(t.expedite_requested_at IS NOT NULL)
			FROM tenant t WHERE t.registration_status=$1 ORDER BY t.expedite_requested_at DESC NULLS LAST, t.created_at DESC`, status)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, code, name, shortCode, contactName, contactPhone, contactWechat, contactQQ, contactAlipay, address, region, scale, remark, regStatus, customerType string
			var created int64
			var approvedAt *int64
			var isTrial, expedited bool
			if err := rows.Scan(&id, &code, &name, &shortCode, &contactName, &contactPhone, &contactWechat, &contactQQ, &contactAlipay,
				&address, &region, &scale, &remark, &regStatus, &created, &approvedAt, &customerType, &isTrial, &expedited); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "code": code, "name": name, "short_code": shortCode,
				"contact_name": contactName, "contact_phone": contactPhone, "contact_wechat": contactWechat,
				"contact_qq": contactQQ, "contact_alipay": contactAlipay, "address": address, "region": region,
				"scale": scale, "remark": remark, "registration_status": regStatus, "created_at": created, "approved_at": approvedAt,
				"customer_type": customerType, "is_trial": isTrial, "expedited": expedited})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})

	api.PlatformHandle("POST /api/v1/platform/tenant-registrations/{id}/approve", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			Edition        string `json:"edition"`
			UserLimit      int    `json:"user_limit"`
			ShortCode      string `json:"short_code"`
			AdminLoginName string `json:"admin_login_name"`
			AdminName      string `json:"admin_name"`
			AdminPassword  string `json:"admin_password"`
			Reason         string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if !editionSet[in.Edition] {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "订阅版本只能为 basic、standard 或 pro")
		}
		if in.UserLimit < 1 || in.UserLimit > 10000 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "订阅规模（启用用户数上限）须为 1-10000")
		}
		if len(in.AdminPassword) < 12 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "初始管理员密码至少 12 位")
		}
		shortCode := strings.ToUpper(strings.TrimSpace(in.ShortCode))
		if shortCode != "" && !shortCodePattern.MatchString(shortCode) {
			return httpapi.Error(http.StatusBadRequest, "INVALID_SHORT_CODE", "租户标识须为 5 位英文字母或数字")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		tenantID := r.PathValue("id")
		var code, name, currentShort, regStatus, contactPhone string
		var isTrial bool
		err = tx.QueryRow(r.Context(), `SELECT code,name,short_code,registration_status,COALESCE(contact_phone,''),
			EXISTS(SELECT 1 FROM tenant_subscription ts WHERE ts.tenant_id=tenant.id AND ts.customer_type='trial')
			FROM tenant WHERE id=$1 FOR UPDATE`, tenantID).
			Scan(&code, &name, &currentShort, &regStatus, &contactPhone, &isTrial)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "注册申请不存在")
		}
		if err != nil {
			return err
		}
		if regStatus != "pending" {
			return httpapi.Error(http.StatusConflict, "NOT_PENDING", "该注册申请已处理，不能重复审批")
		}
		if shortCode == "" {
			shortCode = currentShort
		}
		adminLoginName := strings.TrimSpace(in.AdminLoginName)
		/* 体验厂：以注册手机号作为管理员账户（平台管理员留空时自动使用联系电话）。 */
		if isTrial && adminLoginName == "" {
			adminLoginName = contactPhone
		}
		ids, err := api.Auth.SeedTenant(r.Context(), tx, auth.TenantSeed{
			ExistingTenantID: tenantID, Code: code, Name: name, ShortCode: shortCode,
			AdminLoginName: adminLoginName, AdminName: strings.TrimSpace(in.AdminName),
			AdminPassword: in.AdminPassword,
		})
		if err != nil {
			return httpapi.Error(http.StatusConflict, "SEED_TENANT_FAILED", "启用失败：短码或管理员账号不可用（"+err.Error()+"）")
		}
		adminUserID := ids["tenant_admin_user_id"]
		var adminLogin, verifier, employeeID string
		if err := tx.QueryRow(r.Context(), `SELECT login_name,password_verifier,COALESCE(employee_id,'') FROM user_account WHERE tenant_id=$1 AND id=$2`, tenantID, adminUserID).Scan(&adminLogin, &verifier, &employeeID); err != nil {
			return err
		}
		now := platform.Now()
		customerType := "paid"
		if isTrial {
			customerType = "trial"
		}
		if _, err := tx.Exec(r.Context(), `UPDATE tenant SET registration_status='approved',customer_type=$1,approved_at=$2,approved_by=$3,
			admin_login_name=$4,admin_password_verifier=$5,admin_employee_id=NULLIF($6,''),expedite_requested_at=NULL,version=version+1 WHERE id=$7`,
			customerType, now, p.AdminID, adminLogin, verifier, employeeID, tenantID); err != nil {
			return err
		}
		if isTrial {
			/* 体验订阅：审批通过才正式开始 30 天，功能点亮进度保留。 */
			trialEnds := now + int64((30*24*time.Hour)/time.Microsecond)
			if _, err := tx.Exec(r.Context(), `INSERT INTO tenant_subscription(id,tenant_id,edition,features,customer_type,status,user_limit,
				trial_ends_at,activated_at,created_at,created_by,updated_at,updated_by)
				VALUES($1,$2,$3,'{}','trial','trial',$4,$5,$6,$6,'platform:' || $7,$6,'platform:' || $7)
				ON CONFLICT(tenant_id) DO UPDATE SET edition=EXCLUDED.edition,user_limit=EXCLUDED.user_limit,
				customer_type='trial',status='trial',trial_ends_at=EXCLUDED.trial_ends_at,activated_at=EXCLUDED.activated_at,
				updated_at=EXCLUDED.updated_at,updated_by=EXCLUDED.updated_by,version=tenant_subscription.version+1`,
				platform.ID(), tenantID, in.Edition, in.UserLimit, trialEnds, now, p.AdminID); err != nil {
				return err
			}
			/* 管理员员工档案回填注册手机号：微信扫码/短信登录都按该手机号识别到管理员。 */
			if contactPhone != "" {
				if _, err := tx.Exec(r.Context(), `UPDATE employee SET phone=$1,updated_at=$2
					WHERE tenant_id=$3 AND id=$4`, contactPhone, now, tenantID, employeeID); err != nil {
					return err
				}
			}
			/* 体验厂管理员以手机号/微信登录，不强制改密；初始密码供其他员工首次短信/微信登录使用。 */
			if _, err := tx.Exec(r.Context(), `UPDATE user_account SET password_change_required=FALSE
				WHERE tenant_id=$1 AND id=$2`, tenantID, adminUserID); err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(r.Context(), `INSERT INTO tenant_subscription(id,tenant_id,edition,features,customer_type,status,user_limit,
				activated_at,features_confirmed_by,features_confirmed_at,created_at,created_by,updated_at,updated_by)
				VALUES($1,$2,$3,'[]','paid','active',$4,$5,$6,$5,$5,$6,$5,$6)
				ON CONFLICT(tenant_id) DO UPDATE SET edition=EXCLUDED.edition,user_limit=EXCLUDED.user_limit,
				customer_type='paid',status='active',activated_at=EXCLUDED.activated_at,
				features_confirmed_by=EXCLUDED.features_confirmed_by,features_confirmed_at=EXCLUDED.features_confirmed_at,
				updated_at=EXCLUDED.updated_at,updated_by=EXCLUDED.updated_by,version=tenant_subscription.version+1`,
				platform.ID(), tenantID, in.Edition, in.UserLimit, now, "platform:"+p.AdminID); err != nil {
				return err
			}
		}
		if err := ensureTenantCryptoKey(r.Context(), api, tx, tenantID, p.AdminID, now); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE visitor_lead SET intent_status='won',updated_at=$1,updated_by=$2
			WHERE converted_tenant_id=$3 AND intent_status NOT IN ('won','lost')`, now, "platform:"+p.AdminID, tenantID); err != nil {
			return err
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "tenant_registration_approve", "tenant", tenantID,
			map[string]any{"registration_status": "pending"},
			map[string]any{"registration_status": "approved", "customer_type": customerType, "edition": in.Edition,
				"user_limit": in.UserLimit, "admin_login_name": adminLogin}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		message := "已启用该制衣厂；请将管理员账号与初始密码安全传达给厂家，首次登录须修改密码"
		if isTrial {
			message = "体验制衣厂已通过审核：注册手机号即该厂管理员账户，初始密码供本厂其他员工首次短信/微信登录使用"
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": tenantID, "status": "approved", "is_trial": isTrial,
			"admin_login_name": adminLogin, "edition": in.Edition, "user_limit": in.UserLimit, "message": message})
	})

	api.PlatformHandle("POST /api/v1/platform/tenant-registrations/{id}/reject", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		var in struct {
			Reason string `json:"reason"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		if strings.TrimSpace(in.Reason) == "" {
			return httpapi.Error(http.StatusBadRequest, "REASON_REQUIRED", "驳回必须填写原因")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var regStatus string
		err = tx.QueryRow(r.Context(), "SELECT registration_status FROM tenant WHERE id=$1 FOR UPDATE", r.PathValue("id")).Scan(&regStatus)
		if errors.Is(err, pgx.ErrNoRows) {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "注册申请不存在")
		}
		if err != nil {
			return err
		}
		if regStatus != "pending" {
			return httpapi.Error(http.StatusConflict, "NOT_PENDING", "该注册申请已处理，不能重复审批")
		}
		now := platform.Now()
		if _, err := tx.Exec(r.Context(), "UPDATE tenant SET registration_status='rejected',remark=$1,expedite_requested_at=NULL,version=version+1 WHERE id=$2", strings.TrimSpace(in.Reason), r.PathValue("id")); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE visitor_lead SET intent_status='lost',lost_reason=$1,updated_at=$2,updated_by=$3
			WHERE converted_tenant_id=$4 AND intent_status NOT IN ('won','lost')`, strings.TrimSpace(in.Reason), now, "platform:"+p.AdminID, r.PathValue("id")); err != nil {
			return err
		}
		if err := platformAudit(r.Context(), tx, p.AdminID, "tenant_registration_reject", "tenant", r.PathValue("id"),
			map[string]any{"registration_status": "pending"}, map[string]any{"registration_status": "rejected"}, in.Reason); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "status": "rejected"})
	})
}

// ensureTenantCryptoKey 为制衣厂生成本厂数据加密主密钥（DEK），用本地主密钥（由 JWTSecret 派生）包装后落库。
func ensureTenantCryptoKey(ctx context.Context, api *httpapi.API, tx pgx.Tx, tenantID, adminID string, now int64) error {
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tenant_crypto_key WHERE tenant_id=$1 AND status='active')", tenantID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return err
	}
	master := sha256.Sum256([]byte("yzws-tenant-crypto:v1:" + api.Config.JWTSecret))
	block, err := aes.NewCipher(master[:])
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	wrapped := base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, dek, nil))
	_, err = tx.Exec(ctx, `INSERT INTO tenant_crypto_key(id,tenant_id,key_version,algorithm,wrapped_key,key_wrap_ref,status,activated_at,created_by,created_at)
		VALUES($1,$2,1,'AES-256-GCM',$3,'local-master:v1','active',$4,$5,$4)
		ON CONFLICT(tenant_id,key_version) DO NOTHING`, platform.ID(), tenantID, wrapped, now, "platform:"+adminID)
	return err
}

func platformAudit(ctx context.Context, tx pgx.Tx, adminID, action, ownerType, ownerID string, before, after any, reason string) error {
	var beforeBody, afterBody []byte
	if before != nil {
		beforeBody, _ = json.Marshal(before)
	}
	if after != nil {
		afterBody, _ = json.Marshal(after)
	}
	_, err := tx.Exec(ctx, `INSERT INTO platform_audit_log(id,request_id,actor_id,action,owner_type,owner_id,before_values,after_values,reason,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, platform.ID(), httpapi.RequestID(ctx), adminID, action, ownerType, ownerID, beforeBody, afterBody, reason, platform.Now())
	return err
}

// ===== 制衣厂侧：本厂资料与订阅状态 =====

func registerTenantProfile(api *httpapi.API) {
	api.Handle("GET /api/v1/tenant/profile", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var code, name, shortCode, customerType, regStatus string
		var registeredAt *int64
		var contactName, contactPhone, contactWechat, contactQQ, contactAlipay, address, region, scale, remark *string
		var edition, subStatus *string
		var userLimit *int
		var expiresAt *int64
		var version int64
		err := api.DB.QueryRow(r.Context(), `SELECT t.code,t.name,t.short_code,t.customer_type,t.registration_status,t.registered_at,
			t.contact_name,t.contact_phone,t.contact_wechat,t.contact_qq,t.contact_alipay,t.address,t.region,t.scale,t.remark,t.version,
			s.edition,s.user_limit,s.status,s.expires_at
			FROM tenant t LEFT JOIN tenant_subscription s ON s.tenant_id=t.id WHERE t.id=$1`, p.TenantID).Scan(
			&code, &name, &shortCode, &customerType, &regStatus, &registeredAt,
			&contactName, &contactPhone, &contactWechat, &contactQQ, &contactAlipay, &address, &region, &scale, &remark, &version,
			&edition, &userLimit, &subStatus, &expiresAt)
		if err != nil {
			return err
		}
		var activeUsers int64
		if err := api.DB.QueryRow(r.Context(), "SELECT COUNT(*) FROM user_account WHERE tenant_id=$1 AND status='active'", p.TenantID).Scan(&activeUsers); err != nil {
			return err
		}
		profile := map[string]any{
			"id": p.TenantID, "code": code, "name": name, "short_code": shortCode,
			"customer_type": customerType, "registration_status": regStatus, "registered_at": registeredAt,
			"contact_name": contactName, "contact_phone": contactPhone,
			"contact_wechat": contactWechat, "contact_qq": contactQQ, "contact_alipay": contactAlipay,
			"address": address, "region": region, "scale": scale, "remark": remark, "version": version,
			"user_quota": map[string]any{"used": activeUsers, "limit": userLimit},
		}
		if edition != nil {
			profile["subscription"] = map[string]any{"edition": *edition, "user_limit": userLimit, "status": *subStatus, "expires_at": expiresAt}
		} else {
			profile["subscription"] = nil
		}
		return httpapi.JSON(w, http.StatusOK, profile)
	})

	api.Handle("PUT /api/v1/tenant/profile", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			ContactName   string `json:"contact_name"`
			ContactPhone  string `json:"contact_phone"`
			ContactWechat string `json:"contact_wechat"`
			ContactQQ     string `json:"contact_qq"`
			ContactAlipay string `json:"contact_alipay"`
			Address       string `json:"address"`
			Region        string `json:"region"`
			Remark        string `json:"remark"`
			Version       int64  `json:"version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		contactName := strings.TrimSpace(in.ContactName)
		contactPhone := strings.TrimSpace(in.ContactPhone)
		contactWechat := strings.TrimSpace(in.ContactWechat)
		contactQQ := strings.TrimSpace(in.ContactQQ)
		contactAlipay := strings.TrimSpace(in.ContactAlipay)
		if len(contactName) > 30 || len(contactPhone) > 20 || len(contactWechat) > 64 || len(contactQQ) > 32 || len(contactAlipay) > 64 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "联系人/电话/微信/QQ/支付宝信息过长")
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var before map[string]any
		if err := tx.QueryRow(r.Context(), `SELECT json_build_object('contact_name',COALESCE(contact_name,''),'contact_phone',COALESCE(contact_phone,''),
			'contact_wechat',COALESCE(contact_wechat,''),'contact_qq',COALESCE(contact_qq,''),'contact_alipay',COALESCE(contact_alipay,''),
			'address',COALESCE(address,''),'region',COALESCE(region,''),'remark',COALESCE(remark,'')) FROM tenant WHERE id=$1`, p.TenantID).Scan(&before); err != nil {
			return err
		}
		/* tenant 表只有 updated_at（无 updated_by/updated_node），改动留痕由 audit_log 负责 */
		result, err := tx.Exec(r.Context(), `UPDATE tenant SET contact_name=NULLIF($1,''),contact_phone=NULLIF($2,''),
			contact_wechat=NULLIF($3,''),contact_qq=NULLIF($4,''),contact_alipay=NULLIF($5,''),
			address=NULLIF($6,''),region=NULLIF($7,''),remark=NULLIF($8,''),
			version=version+1,updated_at=$9
			WHERE id=$10 AND version=$11`,
			contactName, contactPhone, contactWechat, contactQQ, contactAlipay,
			strings.TrimSpace(in.Address), strings.TrimSpace(in.Region), strings.TrimSpace(in.Remark),
			platform.Now(), p.TenantID, in.Version)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "厂资料已变更，请刷新后重试")
		}
		after := map[string]any{"contact_name": contactName, "contact_phone": contactPhone, "contact_wechat": contactWechat,
			"contact_qq": contactQQ, "contact_alipay": contactAlipay, "address": in.Address, "region": in.Region, "remark": in.Remark}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "update_tenant_profile", "tenant", p.TenantID, before, after, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"saved": true, "version": in.Version + 1})
	})
}
