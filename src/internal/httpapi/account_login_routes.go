package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"clothp/internal/auth"
)

/* RegisterAccountLoginRoutes 工号/手机号 + 密码登录（设计 5.6.1/5.6.3）：
 * 固定校验顺序与统一错误提示；成功/失败均写 auth_login_log。 */
func (a *API) RegisterAccountLoginRoutes() {
	a.Public("POST /api/v1/auth/account-login", func(w http.ResponseWriter, r *http.Request) error {
		var input auth.AccountLoginInput
		if err := Decode(r, &input); err != nil {
			return err
		}
		if input.RequestID == "" {
			input.RequestID = RequestID(r.Context())
		}
		if input.IP == "" {
			input.IP = clientIP(r)
		}
		result, err := a.Auth.AccountLogin(r.Context(), input, a.Crypto)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrAccountNotFound):
				return Error(http.StatusUnauthorized, "ACCOUNT_NOT_FOUND", "账号或手机号未注册")
			case errors.Is(err, auth.ErrNoTenantAccess):
				return Error(http.StatusForbidden, "NO_TENANT_ACCESS", "无本租户访问权限")
			case errors.Is(err, auth.ErrInvalidPassword):
				return Error(http.StatusUnauthorized, "INVALID_PASSWORD", "密码错误")
			case errors.Is(err, auth.ErrAccountDisabled):
				return Error(http.StatusForbidden, "ACCOUNT_DISABLED", "账号已停用")
			case errors.Is(err, auth.ErrClientForbidden):
				return Error(http.StatusForbidden, "CLIENT_FORBIDDEN", "该账号不允许在此终端登录")
			default:
				return err
			}
		}
		return JSON(w, http.StatusOK, result)
	})
}

/* clientIP 客户端 IP：经可信代理解析 X-Forwarded-For 首个地址，否则用连接远端地址。 */
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if idx := strings.Index(forwarded, ","); idx > 0 {
			return strings.TrimSpace(forwarded[:idx])
		}
		return strings.TrimSpace(forwarded)
	}
	host := r.RemoteAddr
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		return host[:idx]
	}
	return host
}
