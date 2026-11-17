package auth

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

/* 登录验证方式（按账号配置，见 072_user_auth_methods.sql）：
 * sms 是基础验证，恒开且不允许关闭；password/wechat 由本人或厂管理员开关。 */
type AuthMethods struct {
	SMS      bool `json:"sms"`
	Password bool `json:"password"`
	Wechat   bool `json:"wechat"`
}

func DefaultAuthMethods() AuthMethods { return AuthMethods{SMS: true} }

// NormalizeAuthMethods 强制短信验证保持开启，忽略请求中对 sms 的任何修改。
func NormalizeAuthMethods(m AuthMethods) AuthMethods {
	m.SMS = true
	return m
}

type authMethodQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

/* LoadAuthMethods 读取账号验证方式；读取失败或字段缺失时按默认（仅短信）处理，
 * 保证登录链路不因配置读取异常而整体不可用。 */
func LoadAuthMethods(ctx context.Context, db authMethodQuerier, tenantID, userID string) AuthMethods {
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT COALESCE(auth_methods,'{}'::jsonb) FROM user_account WHERE tenant_id=$1 AND id=$2`, tenantID, userID).Scan(&raw); err != nil {
		return DefaultAuthMethods()
	}
	m := DefaultAuthMethods()
	if err := json.Unmarshal(raw, &m); err != nil {
		return DefaultAuthMethods()
	}
	return NormalizeAuthMethods(m)
}
