package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"clothp/internal/platform"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
)

// 体验厂进入令牌：手机号验证通过（或体验厂审批通过）后签发给本机，
// 用于在体验主页点击“开始体验”时换取体验厂管理员的真实 MES 会话。
// 令牌只对 customer_type='trial' 的厂有效，不能用于付费厂。
var (
	ErrTrialPending  = errors.New("trial tenant pending approval")
	ErrTrialExpired  = errors.New("trial subscription expired")
	ErrNoTrialTenant = errors.New("trial tenant not found")
)

// ErrPasswordRequired：该厂普通用户首次短信/微信登录，必须先输入厂管理员提供的密码。
var ErrPasswordRequired = errors.New("factory password required for first login")

const trialEntryTokenTTL = 30 * 24 * time.Hour

type trialEntryClaims struct {
	Phone      string `json:"phone"`
	TenantCode string `json:"tenant_code"`
	TrialEntry bool   `json:"trial_entry"`
	jwt.RegisteredClaims
}

// IssueTrialEntryToken 为体验厂签发本机可保存的进入令牌。
func (s *Service) IssueTrialEntryToken(phone, tenantCode string) (string, error) {
	now := time.Now()
	claims := trialEntryClaims{
		Phone:      strings.TrimSpace(phone),
		TenantCode: strings.TrimSpace(tenantCode),
		TrialEntry: true,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        platform.ID(),
			Issuer:    "clothp-trial",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(trialEntryTokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// VerifyTrialEntryToken 校验体验厂进入令牌。
func (s *Service) VerifyTrialEntryToken(token string) (string, string, error) {
	parsed, err := jwt.ParseWithClaims(strings.TrimSpace(token), &trialEntryClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, ErrUnauthenticated
		}
		return s.secret, nil
	})
	if err != nil || !parsed.Valid {
		return "", "", ErrUnauthenticated
	}
	claims, ok := parsed.Claims.(*trialEntryClaims)
	if !ok || !claims.TrialEntry || claims.Phone == "" || claims.TenantCode == "" {
		return "", "", ErrUnauthenticated
	}
	return claims.Phone, claims.TenantCode, nil
}

// LoginTrialAdmin 以体验厂管理员的手机号登录该体验厂。
// 手机号与厂的对应关系优先按「手机号 → 本厂 tenant_admin 账号」反查（员工档案手机号或账号登录名命中），
// 这样旧数据租户即使注册信息里没有登记联系电话也能进入；未命中时退回注册联系电话匹配。
// 体验用户的手机号/微信号就是体验厂管理员账户，因此这里不再要求密码。
func (s *Service) LoginTrialAdmin(ctx context.Context, phone, tenantCode, nodeID string) (map[string]any, error) {
	phone = strings.TrimSpace(phone)
	var tenantID, tenantStatus, regStatus, adminEmployeeID, subStatus string
	var trialEnds *int64
	err := s.db.QueryRow(ctx, `SELECT t.id,t.status,t.registration_status,COALESCE(t.admin_employee_id,''),
		COALESCE(ts.status,''),ts.trial_ends_at
		FROM tenant t LEFT JOIN tenant_subscription ts ON ts.tenant_id=t.id
		WHERE t.code=$1 AND t.customer_type='trial'
		ORDER BY t.registered_at DESC LIMIT 1`, strings.TrimSpace(tenantCode)).
		Scan(&tenantID, &tenantStatus, &regStatus, &adminEmployeeID, &subStatus, &trialEnds)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoTrialTenant
	}
	if err != nil {
		return nil, err
	}
	if regStatus == "pending" {
		return nil, ErrTrialPending
	}
	if regStatus != "approved" || tenantStatus != "active" {
		return nil, ErrNoTrialTenant
	}
	if subStatus != "trial" || trialEnds == nil || *trialEnds <= platform.Now() {
		return nil, ErrTrialExpired
	}

	// 管理员账号：按手机号反查本厂 tenant_admin 账号（员工档案手机号或账号登录名命中）。
	var userID, employeeID string
	err = s.db.QueryRow(ctx, `SELECT u.id,COALESCE(u.employee_id,'') FROM user_account u
		JOIN user_role ur ON ur.tenant_id=u.tenant_id AND ur.user_id=u.id
		JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id AND r.code='tenant_admin'
		LEFT JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id
		WHERE u.tenant_id=$1 AND u.status='active' AND (e.phone=$2 OR u.login_name=$2)
		ORDER BY u.created_at LIMIT 1`, tenantID, phone).Scan(&userID, &employeeID)
	if errors.Is(err, pgx.ErrNoRows) {
		/* 旧数据：管理员账号没登记手机号，但「制衣厂信息」的注册联系电话就是该手机号 → 按原规则定位管理员账号 */
		var contactPhone string
		_ = s.db.QueryRow(ctx, `SELECT COALESCE(contact_phone,'') FROM tenant WHERE id=$1`, tenantID).Scan(&contactPhone)
		if strings.TrimSpace(contactPhone) != phone {
			return nil, ErrNoTrialTenant
		}
		if adminEmployeeID != "" {
			err = s.db.QueryRow(ctx, `SELECT id,employee_id FROM user_account
				WHERE tenant_id=$1 AND employee_id=$2 AND status='active'`, tenantID, adminEmployeeID).Scan(&userID, &employeeID)
		}
		if adminEmployeeID == "" || errors.Is(err, pgx.ErrNoRows) {
			err = s.db.QueryRow(ctx, `SELECT u.id,u.employee_id FROM user_account u
				JOIN user_role ur ON ur.tenant_id=u.tenant_id AND ur.user_id=u.id
				JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id AND r.code='tenant_admin'
				WHERE u.tenant_id=$1 AND u.status='active'
				ORDER BY u.created_at LIMIT 1`, tenantID).Scan(&userID, &employeeID)
		}
	}
	if err != nil {
		return nil, ErrNoTrialTenant
	}
	// 体验管理员以手机号/微信为主要登录方式，不强制修改密码。
	if _, err := s.db.Exec(ctx, `UPDATE user_account SET password_change_required=FALSE
		WHERE tenant_id=$1 AND id=$2`, tenantID, userID); err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, tenantID, userID, employeeID, nodeID, strings.TrimSpace(phone), "mobile")
}

// LoginByWechatDevice 微信扫码登录：deviceID 为扫码手机浏览器上持久保存的微信身份标识。
// - 已绑定同一 deviceID：直接签发会话（以后只需扫码）；
// - 未绑定：普通用户必须提供厂管理员密码；体验厂管理员可免密码（手机号验证码已校验）；
// - 绑定到其他设备：需重新输入密码后换绑。
func (s *Service) LoginByWechatDevice(ctx context.Context, deviceID, phone, tenantCode, password, nodeID string) (map[string]any, error) {
	var tenantID, userID, employeeID, verifier, bound string
	var lastLogin *int64
	var isAdmin bool
	err := s.db.QueryRow(ctx, `SELECT t.id,u.id,u.employee_id,u.password_verifier,COALESCE(u.wechat_unionid,''),u.last_login_at,
		EXISTS(SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
			WHERE ur.tenant_id=t.id AND ur.user_id=u.id AND r.code='tenant_admin'
			AND ur.valid_from<=$3 AND (ur.valid_to IS NULL OR ur.valid_to>$3))
		FROM tenant t
		JOIN user_account u ON u.tenant_id=t.id AND u.status='active'
		JOIN employee e ON e.tenant_id=t.id AND e.id=u.employee_id AND e.status='active'
		LEFT JOIN tenant_subscription ts ON ts.tenant_id=t.id
		WHERE t.code=$1 AND t.status='active' AND t.registration_status='approved' AND e.phone=$2
		AND (t.customer_type<>'trial' OR (ts.status='trial' AND ts.trial_ends_at>$3))
		ORDER BY u.created_at DESC LIMIT 1`, strings.TrimSpace(tenantCode), strings.TrimSpace(phone), platform.Now()).
		Scan(&tenantID, &userID, &employeeID, &verifier, &bound, &lastLogin, &isAdmin)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	switch {
	case bound != "" && bound == deviceID:
		// 已绑定：后续微信扫码直接登录，不再要求密码或短信验证码。
	case !isAdmin:
		if strings.TrimSpace(password) == "" {
			return nil, ErrPasswordRequired
		}
		if !verifyPassword(verifier, password) {
			return nil, ErrInvalidCredentials
		}
	}
	// 首次绑定/换绑：写入微信身份，并解除首次登录的强制改密限制。
	if _, err := s.db.Exec(ctx, `UPDATE user_account SET wechat_unionid=$1,password_change_required=FALSE
		WHERE tenant_id=$2 AND id=$3`, deviceID, tenantID, userID); err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, tenantID, userID, employeeID, nodeID, strings.TrimSpace(phone), "mobile")
}

// WechatBoundAccount 查询本机微信身份已绑定的厂账号（用于扫码页显示“一键确认登录”）。
func (s *Service) WechatBoundAccount(ctx context.Context, deviceID string) (map[string]any, error) {
	var tenantCode, tenantName, phone, userName string
	err := s.db.QueryRow(ctx, `SELECT t.code,t.name,COALESCE(e.phone,''),e.name
		FROM user_account u
		JOIN tenant t ON t.id=u.tenant_id AND t.status='active'
		JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id AND e.status='active'
		WHERE u.wechat_unionid=$1 AND u.status='active'
		ORDER BY u.updated_at DESC LIMIT 1`, deviceID).
		Scan(&tenantCode, &tenantName, &phone, &userName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pgx.ErrNoRows
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"tenant_code": tenantCode, "tenant_name": tenantName, "phone": phone, "user_name": userName}, nil
}
