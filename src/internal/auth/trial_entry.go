package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"clothp/internal/platform"
	"clothp/internal/seccrypto"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
)

// 体验厂进入令牌：手机号验证通过（或体验厂审批通过）后签发给本机，
// 用于在体验主页点击“开始体验”时换取体验厂管理员的真实 MES 会话。
// 令牌只对 customer_type='trial' 的厂有效，不能用于付费厂。
var (
	ErrTrialPending  = errors.New("trial tenant pending approval")
	ErrTrialRejected = errors.New("trial tenant registration rejected")
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
// 除签名与有效期外，还要查吊销记录：用户退出登录（平台注销）后，
// 吊销时刻之前签发的令牌一律作废，避免旧令牌继续免密进入工作台。
func (s *Service) VerifyTrialEntryToken(ctx context.Context, token string) (string, string, error) {
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
	revokedAt, err := s.trialEntryRevokedAt(ctx, claims.TenantCode, claims.Phone)
	if err != nil {
		return "", "", err
	}
	/* 只比较到秒：同一秒内签发的令牌放行，避免“退出后立即重新验证”被误判失效 */
	if revokedAt > 0 && claims.IssuedAt != nil && claims.IssuedAt.Unix() < revokedAt/trialEntryRevokeSecond {
		return "", "", ErrUnauthenticated
	}
	return claims.Phone, claims.TenantCode, nil
}

const trialEntryRevokeSecond = int64(time.Second / time.Microsecond)

// RevokeTrialEntryTokens 吊销某手机号在某厂的体验进入令牌（退出登录时调用）。
func (s *Service) RevokeTrialEntryTokens(ctx context.Context, tenantID, phone string) error {
	phone = strings.TrimSpace(phone)
	if s.db == nil || tenantID == "" || phone == "" {
		return nil
	}
	_, err := s.db.Exec(ctx, `INSERT INTO trial_entry_revocation(tenant_id,phone_hash,revoked_at)
		VALUES($1,$2,$3)
		ON CONFLICT(tenant_id,phone_hash) DO UPDATE SET revoked_at=EXCLUDED.revoked_at`,
		tenantID, platform.Hash([]byte(phone)), platform.Now())
	return err
}

/* trialEntryRevokedAt 返回该厂该手机号的吊销时刻（微秒）；无记录返回 0。
 * 租户不存在（令牌指向已被删除的厂）按未吊销处理，后续登录流程会自行拒绝。 */
func (s *Service) trialEntryRevokedAt(ctx context.Context, tenantCode, phone string) (int64, error) {
	if s.db == nil {
		return 0, nil
	}
	var revokedAt int64
	err := s.db.QueryRow(ctx, `SELECT r.revoked_at FROM trial_entry_revocation r
		JOIN tenant t ON t.id=r.tenant_id
		WHERE t.code=$1 AND r.phone_hash=$2`,
		strings.TrimSpace(tenantCode), platform.Hash([]byte(strings.TrimSpace(phone)))).Scan(&revokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return revokedAt, nil
}

// LoginTrialUser 以手机号登录体验厂：命中哪个账号就以哪个账号的身份进入。
// 优先按「手机号 → 本厂任意启用账号」反查（员工档案手机号盲索引或账号登录名命中），
// 厂长等普通角色员工也由此进入自己的工作页面（菜单按本人权限显示）；
// 未命中时退回注册联系电话匹配（旧数据租户注册信息里没有登记手机号）。
// 普通员工首次进入须输入厂管理员提供的密码（与收费厂短信登录规则一致）；
// 管理员或已登录过的员工凭短信验证/进入令牌即可。
func (s *Service) LoginTrialUser(ctx context.Context, phone, tenantCode, password, nodeID string, crypto *seccrypto.Service) (map[string]any, error) {
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
	if regStatus == "rejected" {
		return nil, ErrTrialRejected
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

	// 登录账号：按手机号反查本厂任意启用账号（员工档案手机号盲索引或账号登录名命中；
	// crypto 非 nil 时员工手机号列在密文库为 enc:v1 密文，等值比较失效，必须用 phone_hash）。
	// 优先员工档案手机号命中（其次登录名命中），同优先级取最早创建的账号。
	var userID, employeeID, verifier string
	var lastLogin *int64
	isAdmin := false
	if crypto != nil {
		empHash, errH := crypto.PhoneHash(ctx, tenantID, "employee.phone", phone)
		if errH != nil {
			return nil, errH
		}
		err = s.db.QueryRow(ctx, `SELECT u.id,COALESCE(u.employee_id,''),u.password_verifier,u.last_login_at,
			EXISTS(SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
				WHERE ur.tenant_id=u.tenant_id AND ur.user_id=u.id AND r.code='tenant_admin')
			FROM user_account u
			LEFT JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id
			WHERE u.tenant_id=$1 AND u.status='active' AND (e.phone_hash=$3 OR e.phone=$4 OR u.login_name=$2)
			ORDER BY COALESCE(e.phone_hash=$3,FALSE) DESC, u.created_at LIMIT 1`, tenantID, phone, empHash, phone).
			Scan(&userID, &employeeID, &verifier, &lastLogin, &isAdmin)
	} else {
		err = s.db.QueryRow(ctx, `SELECT u.id,COALESCE(u.employee_id,''),u.password_verifier,u.last_login_at,
			EXISTS(SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
				WHERE ur.tenant_id=u.tenant_id AND ur.user_id=u.id AND r.code='tenant_admin')
			FROM user_account u
			LEFT JOIN employee e ON e.tenant_id=u.tenant_id AND e.id=u.employee_id
			WHERE u.tenant_id=$1 AND u.status='active' AND (e.phone=$2 OR u.login_name=$2)
			ORDER BY COALESCE(e.phone=$2,FALSE) DESC, u.created_at LIMIT 1`, tenantID, phone).
			Scan(&userID, &employeeID, &verifier, &lastLogin, &isAdmin)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		/* 旧数据：管理员账号没登记手机号，但「制衣厂信息」的注册联系电话就是该手机号 → 按盲索引/原规则定位管理员账号 */
		contactHit := false
		if crypto != nil {
			contactHash, errH := crypto.PhoneHash(ctx, tenantID, "tenant.contact_phone", phone)
			if errH != nil {
				return nil, errH
			}
			_ = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tenant WHERE id=$1 AND (contact_phone_hash=$2 OR contact_phone=$3))`, tenantID, contactHash, phone).Scan(&contactHit)
		} else {
			var contactPhone string
			_ = s.db.QueryRow(ctx, `SELECT COALESCE(contact_phone,'') FROM tenant WHERE id=$1`, tenantID).Scan(&contactPhone)
			contactHit = strings.TrimSpace(contactPhone) == phone
		}
		if !contactHit {
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
		// 回退分支解析的一定是厂管理员账号：沿用旧数据免密进入规则。
		if err == nil {
			isAdmin = true
		}
	}
	if err != nil {
		return nil, ErrNoTrialTenant
	}
	// 普通员工首次进入须提供厂管理员设置的密码（与收费厂短信登录同一规则）；
	// 管理员或此前已登录过的员工，凭短信验证/进入令牌即可进入。
	if !isAdmin && lastLogin == nil {
		if strings.TrimSpace(password) == "" {
			return nil, ErrPasswordRequired
		}
		if !verifyPassword(verifier, password) {
			return nil, ErrInvalidCredentials
		}
	}
	// 体验用户以手机号/微信为主要登录方式，不强制修改密码。
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
