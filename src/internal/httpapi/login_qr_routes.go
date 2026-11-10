package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"clothp/internal/auth"
)

type wechatLoginInput struct {
	Ticket    string `json:"ticket"`
	DeviceID  string `json:"device_id"`
	Phone     string `json:"phone"`
	Code      string `json:"code"`
	Password  string `json:"password"`
	ShortCode string `json:"short_code"`
	NodeID    string `json:"node_id"`
}

/**
 * 三模块入口的扫码登录票据（演示版）
 * - 二维码由前端生成，内容指向 /qr-login?ticket=xxx&app=xxx
 * - PC 端轮询 status，手机端确认后 PC 端自动进入对应模块
 * - 票据存进程内存，5 分钟有效；正式环境应换成 Redis 并绑定真实用户会话
 */
const loginQRTTL = 5 * time.Minute

var loginQRApps = map[string]string{
	"zhisuan":  "一智算",
	"couhe":    "二撮合",
	"sansheng": "三生万",
}

type loginQRTicket struct {
	app         string
	status      string // pending | scanned | confirmed | cancelled
	createdAt   time.Time
	loginResult map[string]any /* 扫码手机完成身份校验后的真实会话，等待 PC 端一次性取走 */
	loginTicket string
}

var (
	loginQRMu      sync.Mutex
	loginQRTickets = make(map[string]*loginQRTicket)
)

func newQRToken() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

/* 登录二维码用 16 字节票据：URL 更短、二维码模块更少，手机更容易识别 */
func newShortQRToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func qrTicketState(ticket *loginQRTicket) (string, int) {
	remaining := int((loginQRTTL - time.Since(ticket.createdAt)).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	status := ticket.status
	if remaining == 0 && status != "confirmed" {
		status = "expired"
	}
	return status, remaining
}

func (a *API) RegisterLoginQRRoutes() {
	create := func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			App string `json:"app"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		app := strings.TrimSpace(input.App)
		if _, ok := loginQRApps[app]; !ok {
			return Error(http.StatusBadRequest, "INVALID_APP", "未知的模块")
		}
		ticketID, err := newShortQRToken()
		if err != nil {
			return err
		}
		loginQRMu.Lock()
		for key, old := range loginQRTickets {
			if time.Since(old.createdAt) > loginQRTTL+time.Minute {
				delete(loginQRTickets, key)
			}
		}
		loginQRTickets[ticketID] = &loginQRTicket{app: app, status: "pending", createdAt: time.Now()}
		loginQRMu.Unlock()
		return JSON(w, http.StatusOK, map[string]any{
			"ticket": ticketID, "app": app, "app_name": loginQRApps[app],
			"expires_in": int(loginQRTTL.Seconds()),
		})
	}
	a.Public("POST /api/v1/public/login-qr/create", create)

	a.Public("GET /api/v1/public/login-qr/status", func(w http.ResponseWriter, r *http.Request) error {
		ticketID := strings.TrimSpace(r.URL.Query().Get("ticket"))
		loginQRMu.Lock()
		ticket := loginQRTickets[ticketID]
		loginQRMu.Unlock()
		if ticket == nil {
			return Error(http.StatusNotFound, "TICKET_NOT_FOUND", "二维码已失效，请刷新后重试")
		}
		status, remaining := qrTicketState(ticket)
		result := map[string]any{
			"status": status, "app": ticket.app, "app_name": loginQRApps[ticket.app],
			"expires_in": remaining,
		}
		if status == "confirmed" && ticket.loginTicket != "" {
			result["login_ticket"] = ticket.loginTicket
		}
		return JSON(w, http.StatusOK, result)
	})

	transition := func(allowed map[string]bool, target string) func(http.ResponseWriter, *http.Request) error {
		return func(w http.ResponseWriter, r *http.Request) error {
			var input struct {
				Ticket string `json:"ticket"`
			}
			if err := Decode(r, &input); err != nil {
				return err
			}
			loginQRMu.Lock()
			defer loginQRMu.Unlock()
			ticket := loginQRTickets[strings.TrimSpace(input.Ticket)]
			if ticket == nil {
				return Error(http.StatusNotFound, "TICKET_NOT_FOUND", "二维码已失效，请刷新后重试")
			}
			status, remaining := qrTicketState(ticket)
			if remaining == 0 && status != "confirmed" {
				return Error(http.StatusGone, "TICKET_EXPIRED", "二维码已过期，请刷新后重试")
			}
			if !allowed[status] {
				return Error(http.StatusConflict, "INVALID_STATE", "二维码状态已变化，请刷新后重试")
			}
			ticket.status = target
			return JSON(w, http.StatusOK, map[string]any{
				"status": target, "app": ticket.app, "app_name": loginQRApps[ticket.app],
			})
		}
	}
	a.Public("POST /api/v1/public/login-qr/scan", transition(map[string]bool{"pending": true}, "scanned"))
	a.Public("POST /api/v1/public/login-qr/confirm", transition(map[string]bool{"pending": true, "scanned": true}, "confirmed"))
	a.Public("POST /api/v1/public/login-qr/cancel", transition(map[string]bool{"pending": true, "scanned": true}, "cancelled"))

	/* 扫码手机的微信身份状态：已绑定过本厂账号时，页面只显示“一键确认登录”。 */
	a.Public("GET /api/v1/public/login-qr/wechat-status", func(w http.ResponseWriter, r *http.Request) error {
		deviceID := strings.TrimSpace(r.URL.Query().Get("device_id"))
		if deviceID == "" {
			return JSON(w, http.StatusOK, map[string]any{"bound": false})
		}
		account, err := a.Auth.WechatBoundAccount(r.Context(), deviceID)
		if errors.Is(err, auth.ErrUnauthenticated) || account == nil {
			return JSON(w, http.StatusOK, map[string]any{"bound": false})
		}
		if err != nil {
			return JSON(w, http.StatusOK, map[string]any{"bound": false})
		}
		return JSON(w, http.StatusOK, map[string]any{"bound": true, "account": account})
	})

	/* 微信扫码登录：
	 * - 首次：手机号 + 验证码 + 厂管理员提供的密码（管理员免密码），成功后绑定微信身份；
	 * - 以后：手机浏览器已保存微信身份，直接确认即可（无需密码和验证码）。 */
	a.Public("POST /api/v1/public/login-qr/wechat-login", func(w http.ResponseWriter, r *http.Request) error {
		var input wechatLoginInput
		if err := Decode(r, &input); err != nil {
			return err
		}
		ticketID := strings.TrimSpace(input.Ticket)
		loginQRMu.Lock()
		ticket := loginQRTickets[ticketID]
		if ticket == nil {
			loginQRMu.Unlock()
			return Error(http.StatusNotFound, "TICKET_NOT_FOUND", "二维码已失效，请刷新后重试")
		}
		status, remaining := qrTicketState(ticket)
		loginQRMu.Unlock()
		if remaining == 0 && status != "confirmed" {
			return Error(http.StatusGone, "TICKET_EXPIRED", "二维码已过期，请刷新后重试")
		}
		if status != "pending" && status != "scanned" {
			return Error(http.StatusConflict, "INVALID_STATE", "二维码状态已变化，请刷新后重试")
		}

		deviceID := strings.TrimSpace(input.DeviceID)
		if deviceID == "" {
			return Error(http.StatusBadRequest, "DEVICE_REQUIRED", "缺少微信身份标识，请重新扫码")
		}
		phone := strings.TrimSpace(input.Phone)
		tenantCode := ""
		/* 已绑定微信身份：直接登录，不再要求密码/验证码 */
		if account, err := a.Auth.WechatBoundAccount(r.Context(), deviceID); err == nil && account != nil {
			phone, _ = account["phone"].(string)
			tenantCode, _ = account["tenant_code"].(string)
		} else {
			if !trialPhonePattern.MatchString(phone) {
				return Error(http.StatusBadRequest, "INVALID_PHONE", "请输入正确的 11 位手机号")
			}
			if len(strings.TrimSpace(input.Code)) != 6 {
				return Error(http.StatusBadRequest, "INVALID_CODE", "请输入 6 位短信验证码")
			}
			tenantCode = resolveWechatTenant(r.Context(), a, phone, strings.TrimSpace(input.ShortCode))
			if tenantCode == "" {
				return Error(http.StatusForbidden, "NO_ACCOUNT", "该手机号没有可登录的制衣厂账号，请联系厂管理员")
			}
			if err := consumeTrialSMSCode(phone, strings.TrimSpace(input.Code)); err != nil {
				return err
			}
		}

		result, err := a.Auth.LoginByWechatDevice(r.Context(), deviceID, phone, tenantCode, input.Password, input.NodeID)
		switch {
		case errors.Is(err, auth.ErrPasswordRequired):
			return Error(http.StatusForbidden, "PASSWORD_REQUIRED", "该手机号在本厂首次微信登录，请输入厂管理员提供的密码")
		case errors.Is(err, auth.ErrInvalidCredentials):
			return Error(http.StatusForbidden, "NO_ACCOUNT", "微信登录失败：账号不存在或首次登录密码不正确")
		case err != nil:
			return err
		}
		loginTicket, err := newShortQRToken()
		if err != nil {
			return err
		}
		loginQRMu.Lock()
		if current := loginQRTickets[ticketID]; current != nil {
			current.status = "confirmed"
			current.loginResult = result
			current.loginTicket = loginTicket
		}
		loginQRMu.Unlock()
		return JSON(w, http.StatusOK, map[string]any{"status": "confirmed", "ticket": loginTicket})
	})

	/* PC 端取走扫码登录结果：一次性，取走后立即作废。 */
	a.Public("POST /api/v1/public/login-qr/exchange", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			LoginTicket string `json:"login_ticket"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		loginQRMu.Lock()
		defer loginQRMu.Unlock()
		for _, ticket := range loginQRTickets {
			if ticket.loginTicket != "" && ticket.loginTicket == strings.TrimSpace(input.LoginTicket) {
				result := ticket.loginResult
				ticket.loginTicket = ""
				ticket.loginResult = nil
				if result == nil {
					return Error(http.StatusConflict, "LOGIN_RESULT_CONSUMED", "登录结果已被使用，请重新扫码")
				}
				return JSON(w, http.StatusOK, result)
			}
		}
		return Error(http.StatusNotFound, "LOGIN_TICKET_NOT_FOUND", "登录结果已失效，请重新扫码")
	})
}

// consumeTrialSMSCode 复用语体验短信验证码（/auth/trial/send-code 发送），一次性。
func consumeTrialSMSCode(phone, code string) error {
	trialMu.Lock()
	defer trialMu.Unlock()
	challenge := trialChallenges[phone]
	if challenge == nil {
		return Error(http.StatusBadRequest, "CODE_EXPIRED", "请先获取短信验证码")
	}
	if time.Now().After(challenge.expiresAt) {
		delete(trialChallenges, phone)
		return Error(http.StatusBadRequest, "CODE_EXPIRED", "验证码已超过 3 分钟有效期，请重新获取")
	}
	if challenge.attempts >= trialMaxAttempts {
		delete(trialChallenges, phone)
		return Error(http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", "验证码错误次数过多，请重新获取")
	}
	if challenge.code != code {
		challenge.attempts++
		return Error(http.StatusBadRequest, "INVALID_CODE", fmt.Sprintf("验证码不正确，还可尝试 %d 次", trialMaxAttempts-challenge.attempts))
	}
	delete(trialChallenges, phone)
	return nil
}

// resolveWechatTenant 按手机号（可再加 5 位厂标识）定位要登录的制衣厂。
func resolveWechatTenant(ctx context.Context, a *API, phone, shortCode string) string {
	query := `SELECT t.code FROM tenant t
		JOIN user_account u ON u.tenant_id=t.id AND u.status='active'
		JOIN employee e ON e.tenant_id=t.id AND e.id=u.employee_id AND e.status='active'
		WHERE e.phone=$1 AND t.status='active' AND t.registration_status='approved'`
	args := []any{phone}
	if shortCode != "" {
		query += ` AND upper(t.short_code)=upper($2)`
		args = append(args, shortCode)
	}
	query += ` ORDER BY t.registered_at DESC`
	rows, err := a.DB.Query(ctx, query, args...)
	if err != nil {
		return ""
	}
	defer rows.Close()
	codes := []string{}
	for rows.Next() {
		var code string
		if rows.Scan(&code) == nil && code != "" {
			codes = append(codes, code)
		}
	}
	if len(codes) == 1 {
		return codes[0]
	}
	return ""
}
