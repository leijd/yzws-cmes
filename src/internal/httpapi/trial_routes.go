package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"clothp/internal/auth"
	"clothp/internal/sms"
)

/**
 * 免费体验登录验证码
 * - 验证码 6 位，3 分钟内有效；60 秒内不可重复发送；最多 5 次校验错误
 * - 存储为进程内存，重启服务即失效；正式环境建议换成 Redis / 数据库并加 IP 级限流
 * - 短信由 sms.Router 按实时使用情况在多个服务商之间自动选路（见 internal/sms/router.go）
 *   未配置任何服务商时为演示模式，响应中返回 demo_code。
 */
const (
	trialCodeTTL     = 3 * time.Minute
	trialResendGap   = 60 * time.Second
	trialMaxAttempts = 5
	trialTicketTTL   = 30 * time.Minute
)

var trialPhonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

type trialChallenge struct {
	code      string
	expiresAt time.Time
	sentAt    time.Time
	attempts  int
}

var (
	trialMu         sync.Mutex
	trialChallenges = make(map[string]*trialChallenge)
	trialTickets    = make(map[string]*trialTicket) /* 一次性短信凭证：手机号验证通过 → 凭 ticket 选厂登录 */
)

type trialTicket struct {
	phone     string
	expiresAt time.Time
}

func trialRandomDigits(n int) (string, error) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		b.WriteByte(byte('0' + v.Int64()))
	}
	return b.String(), nil
}

func trialRandomToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func maskTrialPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}

/** 由多服务商路由自动选路发送；未配置服务商时保持演示模式。 */
func (a *API) sendTrialSMS(phone, code string) (bool, error) {
	if !a.SMS.Configured() {
		log.Printf("[trial] demo sms phone=%s code=%s (no sms provider configured)", maskTrialPhone(phone), code)
		return false, nil
	}
	provider, err := a.SMS.Send(phone, code, int(trialCodeTTL.Seconds()))
	if err != nil {
		log.Printf("[trial] all sms providers failed phone=%s error=%v", maskTrialPhone(phone), err)
		return false, err
	}
	log.Printf("[trial] sms sent phone=%s provider=%s", maskTrialPhone(phone), provider)
	return true, nil
}

func (a *API) RegisterTrialRoutes() {
	a.Public("POST /api/v1/auth/trial/send-code", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Phone string `json:"phone"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		phone := strings.TrimSpace(input.Phone)
		if !trialPhonePattern.MatchString(phone) {
			return Error(http.StatusBadRequest, "INVALID_PHONE", "请输入正确的 11 位手机号")
		}

		trialMu.Lock()
		for key, old := range trialChallenges {
			if time.Now().After(old.expiresAt.Add(10 * time.Minute)) {
				delete(trialChallenges, key)
			}
		}
		for key, old := range trialTickets {
			if time.Now().After(old.expiresAt) {
				delete(trialTickets, key)
			}
		}
		if previous := trialChallenges[phone]; previous != nil && time.Since(previous.sentAt) < trialResendGap {
			retry := int((trialResendGap - time.Since(previous.sentAt)).Seconds()) + 1
			trialMu.Unlock()
			return Error(http.StatusTooManyRequests, "TOO_FREQUENT", fmt.Sprintf("请 %d 秒后再获取验证码", retry))
		}
		trialMu.Unlock()

		code, err := trialRandomDigits(6)
		if err != nil {
			return err
		}
		smsSent, err := a.sendTrialSMS(phone, code)
		if err != nil {
			return Error(http.StatusBadGateway, "SMS_FAILED", "验证码发送失败，请稍后重试")
		}
		now := time.Now()
		trialMu.Lock()
		trialChallenges[phone] = &trialChallenge{code: code, sentAt: now, expiresAt: now.Add(trialCodeTTL)}
		trialMu.Unlock()

		result := map[string]any{"expires_in": int(trialCodeTTL.Seconds()), "sms_sent": smsSent}
		if !smsSent {
			result["demo_code"] = code
			result["message"] = "演示模式：未配置短信服务商，验证码在响应中返回"
		}
		return JSON(w, http.StatusOK, result)
	})

	a.Public("POST /api/v1/auth/trial/verify", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Phone string `json:"phone"`
			Code  string `json:"code"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		phone := strings.TrimSpace(input.Phone)
		code := strings.TrimSpace(input.Code)
		if !trialPhonePattern.MatchString(phone) || len(code) != 6 {
			return Error(http.StatusBadRequest, "INVALID_REQUEST", "手机号或验证码格式不正确")
		}

		trialMu.Lock()
		challenge := trialChallenges[phone]
		if challenge == nil {
			trialMu.Unlock()
			return Error(http.StatusBadRequest, "CODE_EXPIRED", "请先获取验证码")
		}
		if time.Now().After(challenge.expiresAt) {
			delete(trialChallenges, phone)
			trialMu.Unlock()
			return Error(http.StatusBadRequest, "CODE_EXPIRED", "验证码已超过 3 分钟有效期，请重新获取")
		}
		if challenge.attempts >= trialMaxAttempts {
			delete(trialChallenges, phone)
			trialMu.Unlock()
			return Error(http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", "错误次数过多，请重新获取验证码")
		}
		if challenge.code != code {
			challenge.attempts++
			left := trialMaxAttempts - challenge.attempts
			trialMu.Unlock()
			return Error(http.StatusBadRequest, "INVALID_CODE", fmt.Sprintf("验证码不正确，还可尝试 %d 次", left))
		}
		delete(trialChallenges, phone)
		trialMu.Unlock()

		/* 手机号关联的所有厂：≤1 个体验厂（手机号只能占用一个体验用户）+ N 个收费制衣厂
		 * （体验厂：注册联系电话命中，或本厂任意启用账号的员工手机号/登录名命中——厂长等
		 *   非管理员员工由此识别，进入时按本人账号签发会话；兼容旧数据租户；
		 *   收费厂：员工档案绑定该手机号，或该厂「制衣厂信息」的联系电话即该手机号），前端据此决定直接进入门户还是弹出选厂列表 */
		tenants := []map[string]any{}
		var trialTenant map[string]any
		if a.DB != nil {
			addTrialTenant := func(tCode, tName, tShortCode, tAdminLogin string, trialEnds int64, tRegStatus string) {
				trialTenant = map[string]any{"kind": "trial", "code": tCode, "name": tName, "short_code": tShortCode,
					"admin_login_name": tAdminLogin, "trial_ends_at": trialEnds, "returning": true,
					"status": tRegStatus, "registration_status": tRegStatus}
				/* 本机保存进入令牌：体验主页点击“开始体验”即可换取管理员会话，无需再次短信验证 */
				if entryToken, tokenErr := a.Auth.IssueTrialEntryToken(phone, tCode); tokenErr == nil {
					trialTenant["entry_token"] = entryToken
				}
				tenants = append(tenants, trialTenant)
			}
			if phoneCrypto := a.phoneCrypto(); phoneCrypto != nil {
				/* 盲索引模式：注册联系电话/员工手机号列在密文库为 enc:v1 密文，等值比较永远
				 * 匹配不到；按候选体验厂逐个计算本厂哈希匹配 phone_hash（体验厂数量少，可接受）。 */
				candRows, err := a.DB.Query(r.Context(), `SELECT t.id,t.code,t.name,COALESCE(t.short_code,''),COALESCE(t.admin_login_name,''),COALESCE(s.trial_ends_at,0),t.registration_status
					FROM tenant t LEFT JOIN tenant_subscription s ON s.tenant_id=t.id
					WHERE t.customer_type='trial' AND t.status='active' AND t.registration_status<>'rejected'
					ORDER BY t.registered_at DESC`)
				if err != nil {
					return err
				}
				type trialCand struct {
					id, code, name, short, adminLogin, regStatus string
					trialEnds                                    int64
				}
				var cands []trialCand
				for candRows.Next() {
					var c trialCand
					if err := candRows.Scan(&c.id, &c.code, &c.name, &c.short, &c.adminLogin, &c.trialEnds, &c.regStatus); err != nil {
						candRows.Close()
						return err
					}
					cands = append(cands, c)
				}
				candRows.Close()
				for _, c := range cands {
					contactHash, errH := phoneCrypto.PhoneHash(r.Context(), c.id, "tenant.contact_phone", phone)
					if errH != nil {
						return errH
					}
					empHash, errH2 := phoneCrypto.PhoneHash(r.Context(), c.id, "employee.phone", phone)
					if errH2 != nil {
						return errH2
					}
					var hit bool
					_ = a.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM tenant WHERE id=$1 AND (contact_phone_hash=$2 OR contact_phone=$3))`, c.id, contactHash, phone).Scan(&hit)
					if !hit {
						/* phone_hash 优先；明文兜底兼容历史明文行（早期写入路径未加密/未建盲索引），
						 * 密文行等值比较自然不中，不会绕过盲索引。 */
						_ = a.DB.QueryRow(r.Context(), `SELECT EXISTS(
								SELECT 1 FROM user_account u
								LEFT JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id
								WHERE u.tenant_id=$1 AND u.status='active' AND (e.phone_hash=$3 OR e.phone=$4 OR u.login_name=$2))`, c.id, phone, empHash, phone).Scan(&hit)
					}
					if hit {
						addTrialTenant(c.code, c.name, c.short, c.adminLogin, c.trialEnds, c.regStatus)
						break
					}
				}
			} else {
				/* 明文库模式：明文等值比较 */
				var tCode, tName, tShortCode, tAdminLogin, tRegStatus string
				var trialEnds int64
				tenantErr := a.DB.QueryRow(r.Context(), `SELECT t.code,t.name,t.short_code,COALESCE(t.admin_login_name,''),COALESCE(s.trial_ends_at,0),t.registration_status
					FROM tenant t LEFT JOIN tenant_subscription s ON s.tenant_id=t.id
					WHERE t.customer_type='trial' AND t.status='active' AND t.registration_status<>'rejected'
					AND (t.contact_phone=$1 OR EXISTS(
						SELECT 1 FROM user_account u
						LEFT JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id
						WHERE u.tenant_id=t.id AND u.status='active' AND (e.phone=$1 OR u.login_name=$1)))
					ORDER BY t.registered_at DESC LIMIT 1`, phone).
					Scan(&tCode, &tName, &tShortCode, &tAdminLogin, &trialEnds, &tRegStatus)
				if tenantErr == nil {
					addTrialTenant(tCode, tName, tShortCode, tAdminLogin, trialEnds, tRegStatus)
				}
			}
			/* 收费制衣厂：员工档案绑定该手机号，或该厂「制衣厂信息」的联系电话即该手机号（对应厂管理员账号）。 */
			if factories, err := a.Auth.PhoneTenants(r.Context(), []string{phone}, a.phoneCrypto()); err == nil {
				for _, f := range factories {
					tenants = append(tenants, map[string]any{"kind": "factory", "code": f.Code, "name": f.Name,
						"short_code": f.ShortCode, "login_name": f.LoginName, "user_name": f.UserName})
				}
			}
		}

		ticket, err := trialRandomToken()
		if err != nil {
			return err
		}
		trialMu.Lock()
		trialTickets[ticket] = &trialTicket{phone: phone, expiresAt: time.Now().Add(trialTicketTTL)}
		trialMu.Unlock()
		result := map[string]any{
			"ticket":     ticket,
			"expires_in": int(trialTicketTTL.Seconds()),
			"trial_days": 30,
			"tenants":    tenants,
		}
		if trialTenant != nil {
			result["tenant"] = trialTenant /* 兼容旧字段 */
		}
		return JSON(w, http.StatusOK, result)
	})

	/* 手机号 + 制衣厂登录：短信验证通过的一次性 ticket + 厂编码 → 换取该厂绑定的员工账号真实会话 */
	a.Public("POST /api/v1/auth/phone-login", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Ticket     string `json:"ticket"`
			TenantCode string `json:"tenant_code"`
			Password   string `json:"password"`
			NodeID     string `json:"node_id"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		ticket := strings.TrimSpace(input.Ticket)
		tenantCode := strings.TrimSpace(input.TenantCode)
		if ticket == "" || tenantCode == "" {
			return Error(http.StatusBadRequest, "INVALID_REQUEST", "缺少短信凭证或制衣厂编码")
		}
		trialMu.Lock()
		tt := trialTickets[ticket]
		if tt == nil || time.Now().After(tt.expiresAt) {
			delete(trialTickets, ticket)
			trialMu.Unlock()
			return Error(http.StatusUnauthorized, "INVALID_TICKET", "短信验证已过期，请重新验证")
		}
		phone := tt.phone
		trialMu.Unlock()

		result, err := a.Auth.LoginByPhone(r.Context(), phone, tenantCode, input.Password, input.NodeID, a.phoneCrypto())
		/* 首次密码错误时保留 ticket，允许用户补充密码后重试；登录成功后再作废。 */
		if err == nil {
			trialMu.Lock()
			delete(trialTickets, ticket)
			trialMu.Unlock()
		}
		if errors.Is(err, auth.ErrPasswordRequired) {
			return Error(http.StatusForbidden, "PASSWORD_REQUIRED", "该手机号在本厂首次登录，请输入厂管理员提供的密码")
		}
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return Error(http.StatusForbidden, "NO_ACCOUNT", "该手机号在此制衣厂没有绑定的账号，或首次登录密码不正确")
		}
		if err != nil {
			return err
		}
		var tenantName string
		_ = a.DB.QueryRow(r.Context(), `SELECT name FROM tenant WHERE code=$1`, tenantCode).Scan(&tenantName)
		result["tenant"] = map[string]any{"code": tenantCode, "name": tenantName}
		return JSON(w, http.StatusOK, result)
	})

	/* 体验主页“开始体验”：凭本机进入令牌（或一次性短信 ticket）换取体验厂账号的真实 MES 会话。
	 * 手机号命中本厂哪个账号就以哪个身份进入（管理员或厂长等员工，菜单按本人权限显示）；
	 * 普通员工首次进入须提供厂管理员设置的密码。返回的 permissions 用于前端选择首页。 */
	a.Public("POST /api/v1/public/trial-enter", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			EntryToken string `json:"entry_token"`
			Ticket     string `json:"ticket"`
			Phone      string `json:"phone"`
			Password   string `json:"password"`
			TenantCode string `json:"tenant_code"`
			NodeID     string `json:"node_id"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		tenantCode := strings.TrimSpace(input.TenantCode)
		phone := strings.TrimSpace(input.Phone)
		authenticated := false
		if token := strings.TrimSpace(input.EntryToken); token != "" {
			tokenPhone, tokenTenant, err := a.Auth.VerifyTrialEntryToken(token)
			if err != nil {
				return Error(http.StatusUnauthorized, "INVALID_ENTRY_TOKEN", "体验凭证已失效，请重新用手机验证码登录")
			}
			phone, tenantCode = tokenPhone, tokenTenant
			authenticated = true
		} else {
			if ticket := strings.TrimSpace(input.Ticket); ticket != "" {
				trialMu.Lock()
				tt := trialTickets[ticket]
				if tt == nil || time.Now().After(tt.expiresAt) {
					delete(trialTickets, ticket)
					trialMu.Unlock()
					return Error(http.StatusUnauthorized, "INVALID_TICKET", "短信验证已过期，请重新验证手机号")
				}
				delete(trialTickets, ticket)
				trialMu.Unlock()
				phone = tt.phone
				authenticated = true
			}
		}
		/* 必须持本机进入令牌或一次性短信 ticket，禁止仅凭手机号+厂编码进入。 */
		if !authenticated {
			return Error(http.StatusUnauthorized, "MISSING_CREDENTIAL", "请先通过手机验证码或体验凭证进入")
		}
		if tenantCode == "" || phone == "" {
			return Error(http.StatusBadRequest, "INVALID_REQUEST", "缺少体验厂编码或手机号，请重新验证")
		}
		result, err := a.Auth.LoginTrialUser(r.Context(), phone, tenantCode, input.Password, input.NodeID, a.phoneCrypto())
		switch {
		case errors.Is(err, auth.ErrTrialPending):
			return Error(http.StatusConflict, "TRIAL_PENDING", "体验制衣厂已提交，平台管理员审核通过后即可进入工作台")
		case errors.Is(err, auth.ErrTrialExpired):
			return Error(http.StatusForbidden, "TRIAL_EXPIRED", "体验期已结束，请联系平台转为正式订阅")
		case errors.Is(err, auth.ErrNoTrialTenant):
			return Error(http.StatusNotFound, "NO_TRIAL_TENANT", "该手机号还没有体验制衣厂，请先完成体验注册")
		case errors.Is(err, auth.ErrPasswordRequired):
			return Error(http.StatusForbidden, "PASSWORD_REQUIRED", "该手机号在本厂首次进入，请输入厂管理员提供的密码")
		case errors.Is(err, auth.ErrInvalidCredentials):
			return Error(http.StatusForbidden, "INVALID_PASSWORD", "首次登录密码不正确，请向厂管理员确认后重试")
		case err != nil:
			return err
		}
		var tenantName string
		_ = a.DB.QueryRow(r.Context(), `SELECT name FROM tenant WHERE code=$1`, tenantCode).Scan(&tenantName)
		result["tenant"] = map[string]any{"code": tenantCode, "name": tenantName, "customer_type": "trial"}
		result["password_change_required"] = false
		if entryToken, tokenErr := a.Auth.IssueTrialEntryToken(phone, tenantCode); tokenErr == nil {
			result["entry_token"] = entryToken
		}
		return JSON(w, http.StatusOK, result)
	})

	/* 前端据此判断是否允许降级为演示模式：配置了真服务商时禁止降级 */
	a.Public("GET /api/v1/auth/trial/sms-mode", func(w http.ResponseWriter, r *http.Request) error {
		return JSON(w, http.StatusOK, map[string]any{"demo_mode": !a.SMS.Configured()})
	})

	/* 运维视图：查看各服务商今日用量、连续失败、熔断状态与当前有效权重（需登录） */
	a.Handle("GET /api/v1/auth/trial/sms-status", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		status := []sms.ProviderStatus{}
		if a.SMS != nil {
			status = a.SMS.Status()
		}
		return JSON(w, http.StatusOK, map[string]any{
			"demo_mode": !a.SMS.Configured(),
			"providers": status,
		})
	})
}
