package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"clothp/internal/platform"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

var ErrUnauthenticated = errors.New("unauthenticated")
var ErrInvalidCredentials = errors.New("invalid credentials")
var ErrForbidden = errors.New("forbidden")

type Principal struct {
	TenantID               string
	UserID                 string
	EmployeeID             string
	LoginName              string
	SessionID              string
	PasswordChangeRequired bool
	Permissions            map[string]bool
	/* ClientType 会话终端（pc/mobile，设计 5.6.1）；工人账号仅允许 mobile */
	ClientType string
}

type Service struct {
	db                *pgxpool.Pool
	secret            []byte
	nodeID            string
	accessTokenTTL    time.Duration
	offlineSessionTTL time.Duration
}

type claims struct {
	TenantID   string `json:"tenant_id"`
	UserID     string `json:"user_id"`
	SessionID  string `json:"session_id"`
	ClientType string `json:"client_type,omitempty"`
	Platform   bool   `json:"platform,omitempty"`
	jwt.RegisteredClaims
}

type LoginInput struct {
	TenantCode string `json:"tenant_code"`
	LoginName  string `json:"login_name"`
	Password   string `json:"password"`
	NodeID     string `json:"node_id"`
}

func NewService(db *pgxpool.Pool, secret, nodeID string, accessTokenTTL, offlineSessionTTL time.Duration) *Service {
	return &Service{db: db, secret: []byte(secret), nodeID: nodeID, accessTokenTTL: accessTokenTTL, offlineSessionTTL: offlineSessionTTL}
}

func (s *Service) Bootstrap(ctx context.Context, adminPassword string) error {
	var count int
	if err := s.db.QueryRow(ctx, "SELECT COUNT(*) FROM tenant").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	// 平台控制面已初始化时不再自动创建默认租户：租户统一由平台管理员注册，
	// 否则清空租户数据后每次启动都会重新生成 DEFAULT 租户。
	var platformAdmins int
	if err := s.db.QueryRow(ctx, "SELECT COUNT(*) FROM platform_admin").Scan(&platformAdmins); err != nil {
		return err
	}
	if platformAdmins > 0 {
		log.Printf("bootstrap: platform admin initialized, skip default tenant seeding")
		return nil
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := s.SeedTenant(ctx, tx, TenantSeed{Code: "DEFAULT", AdminLoginName: "admin", Name: "默认工厂", AdminPassword: adminPassword}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) Login(ctx context.Context, input LoginInput) (map[string]any, error) {
	var tenantID, userID, employeeID, verifier, status string
	var tenantStatus string
	err := s.db.QueryRow(ctx, `SELECT t.id,t.status,u.id,u.employee_id,u.password_verifier,u.status
		FROM tenant t JOIN user_account u ON u.tenant_id=t.id WHERE t.code=$1 AND u.login_name=$2`,
		input.TenantCode, input.LoginName).Scan(&tenantID, &tenantStatus, &userID, &employeeID, &verifier, &status)
	if err != nil || tenantStatus != "active" || status != "active" || !verifyPassword(verifier, input.Password) {
		return nil, ErrInvalidCredentials
	}
	return s.issueTokens(ctx, tenantID, userID, employeeID, input.NodeID, "", "pc")
}

/** 手机号 + 制衣厂登录（短信验证通过后凭一次性 ticket 换取真实会话）：一个手机号可在多个厂各绑定员工账号。
 * 匹配规则：员工档案手机号（e.phone），或该厂「制衣厂信息」登记的注册联系电话（t.contact_phone，限厂管理员账号）。
 * 非管理员用户首次登录还须输入厂管理员提供的密码；成功登录后，以后只需手机验证码。 */
func (s *Service) LoginByPhone(ctx context.Context, phone, tenantCode, password, nodeID string) (map[string]any, error) {
	account, err := s.lookupPhoneAccount(ctx, []string{strings.TrimSpace(phone)}, tenantCode)
	if err != nil {
		return nil, err
	}
	/* 首次短信登录：普通用户必须同时提供厂管理员设置的密码；管理员或已登录过的用户只需验证码。 */
	if !account.isAdmin && account.lastLogin == nil {
		if strings.TrimSpace(password) == "" {
			return nil, ErrPasswordRequired
		}
		if !verifyPassword(account.verifier, password) {
			return nil, ErrInvalidCredentials
		}
	}
	/* 首次密码+验证码校验通过后，不再强制改密（后续仅凭验证码登录）。 */
	if _, err := s.db.Exec(ctx, `UPDATE user_account SET password_change_required=FALSE
		WHERE tenant_id=$1 AND id=$2`, account.tenantID, account.userID); err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, account.tenantID, account.userID, account.employeeID, nodeID, strings.TrimSpace(phone), "mobile")
}

type phoneAccount struct {
	tenantID, userID, employeeID, verifier string
	lastLogin                              *int64
	isAdmin                                bool
}

/** 按手机号定位某制衣厂下可登录的账号：优先员工档案手机号，其次厂联系电话对应的管理员账号。 */
func (s *Service) lookupPhoneAccount(ctx context.Context, phones []string, tenantCode string) (phoneAccount, error) {
	var account phoneAccount
	err := s.db.QueryRow(ctx, `SELECT t.id,u.id,COALESCE(u.employee_id,''),u.password_verifier,u.last_login_at,
		EXISTS(SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
			WHERE ur.tenant_id=t.id AND ur.user_id=u.id AND r.code='tenant_admin')
		FROM tenant t
		JOIN user_account u ON u.tenant_id=t.id AND u.status='active'
		LEFT JOIN employee e ON e.tenant_id=t.id AND e.id=u.employee_id AND e.status='active'
		LEFT JOIN tenant_subscription ts ON ts.tenant_id=t.id
		WHERE t.code=$1 AND t.status='active'
		AND (e.phone = ANY($2::text[]) OR (t.contact_phone = ANY($2::text[]) AND EXISTS(
			SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
			WHERE ur.tenant_id=t.id AND ur.user_id=u.id AND r.code='tenant_admin')))
		AND (t.customer_type<>'trial' OR (ts.status='trial' AND ts.trial_ends_at>$3))
		ORDER BY COALESCE(e.phone = ANY($2::text[]),FALSE) DESC,
		EXISTS(SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
			WHERE ur.tenant_id=t.id AND ur.user_id=u.id AND r.code='tenant_admin') DESC,
		u.created_at ASC LIMIT 1`, tenantCode, phones, platform.Now()).
		Scan(&account.tenantID, &account.userID, &account.employeeID, &account.verifier, &account.lastLogin, &account.isAdmin)
	if err != nil {
		return phoneAccount{}, ErrInvalidCredentials
	}
	return account, nil
}

/* PhoneTenant 手机号可进入的制衣厂（员工档案手机号或该厂注册联系电话）。 */
type PhoneTenant struct {
	Code      string
	Name      string
	ShortCode string
	LoginName string
	UserName  string
}

/** 按手机号列出可进入的收费制衣厂（已审批）：员工手机号命中，或注册联系电话命中且账号为厂管理员。
 * 同一厂只返回一条，优先员工手机号匹配的账号，其次厂管理员账号；厂按注册时间倒序。 */
func (s *Service) PhoneTenants(ctx context.Context, phones []string) ([]PhoneTenant, error) {
	if len(phones) == 0 {
		return []PhoneTenant{}, nil
	}
	rows, err := s.db.Query(ctx, `SELECT t.code,t.name,t.short_code,u.login_name,COALESCE(e.name,'')
		FROM tenant t
		JOIN user_account u ON u.tenant_id=t.id AND u.status='active'
		LEFT JOIN employee e ON e.tenant_id=t.id AND e.id=u.employee_id AND e.status='active'
		WHERE (e.phone = ANY($1::text[]) OR (t.contact_phone = ANY($1::text[]) AND EXISTS(
			SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
			WHERE ur.tenant_id=t.id AND ur.user_id=u.id AND r.code='tenant_admin')))
		AND t.status='active' AND t.registration_status='approved' AND t.customer_type<>'trial'
		ORDER BY COALESCE(e.phone = ANY($1::text[]),FALSE) DESC, t.registered_at DESC,
		EXISTS(SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
			WHERE ur.tenant_id=t.id AND ur.user_id=u.id AND r.code='tenant_admin') DESC,
		u.created_at ASC`, phones)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]PhoneTenant, 0, 4)
	seen := map[string]bool{}
	for rows.Next() {
		var item PhoneTenant
		if err := rows.Scan(&item.Code, &item.Name, &item.ShortCode, &item.LoginName, &item.UserName); err != nil {
			return nil, err
		}
		if seen[item.Code] {
			continue
		}
		seen[item.Code] = true
		items = append(items, item)
	}
	return items, rows.Err()
}

/** 当前会话在登录时验证过的手机号（短信/微信登录时写入，切换制衣厂后仍沿用）。 */
func (s *Service) sessionLoginPhone(ctx context.Context, p Principal) string {
	var phone string
	_ = s.db.QueryRow(ctx, `SELECT COALESCE(login_phone,'') FROM auth_session WHERE tenant_id=$1 AND id=$2`,
		p.TenantID, p.SessionID).Scan(&phone)
	return strings.TrimSpace(phone)
}

/** 当前用户可用于识别名下制衣厂的手机号：优先会话验证过的登录手机号，其次员工手机号与本厂注册联系电话。
 * 登录手机号在切换制衣厂后保持不变，保证同一手机号名下的多个厂可以互相切换。 */
func (s *Service) PrincipalPhones(ctx context.Context, p Principal) ([]string, error) {
	if phone := s.sessionLoginPhone(ctx, p); phone != "" {
		return []string{phone}, nil
	}
	var employeePhone, contactPhone string
	err := s.db.QueryRow(ctx, `SELECT COALESCE(e.phone,''),COALESCE(t.contact_phone,'')
		FROM tenant t
		LEFT JOIN user_account u ON u.tenant_id=t.id AND u.id=$2
		LEFT JOIN employee e ON e.tenant_id=t.id AND e.id=u.employee_id
		WHERE t.id=$1`, p.TenantID, p.UserID).Scan(&employeePhone, &contactPhone)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	phones := make([]string, 0, 2)
	for _, phone := range []string{employeePhone, contactPhone} {
		phone = strings.TrimSpace(phone)
		if phone == "" || (len(phones) == 1 && phones[0] == phone) {
			continue
		}
		phones = append(phones, phone)
	}
	return phones, nil
}

/** 已登录用户切换到名下另一家制衣厂门户：手机号须登记在该厂（员工手机号或注册联系电话）。
 * 手机号在短信登录时已验证，切换门户不再重复要求首次登录密码。 */
func (s *Service) SwitchTenant(ctx context.Context, p Principal, tenantCode, nodeID string) (map[string]any, error) {
	phones, err := s.PrincipalPhones(ctx, p)
	if err != nil || len(phones) == 0 {
		return nil, ErrInvalidCredentials
	}
	account, err := s.lookupPhoneAccount(ctx, phones, tenantCode)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.Exec(ctx, `UPDATE user_account SET password_change_required=FALSE
		WHERE tenant_id=$1 AND id=$2`, account.tenantID, account.userID); err != nil {
		return nil, err
	}
	/* 新会话沿用同一登录手机号（老会话无记录时用当前识别出的手机号），切换后仍能切回其他厂。 */
	return s.issueTokens(ctx, account.tenantID, account.userID, account.employeeID, nodeID, phones[0], "mobile")
}

func (s *Service) issueTokens(ctx context.Context, tenantID, userID, employeeID, nodeID, loginPhone, clientType string) (map[string]any, error) {
	if clientType == "" {
		clientType = "pc"
	}
	if nodeID == "" {
		nodeID = s.nodeID
	}
	sessionID := platform.ID()
	refreshToken := platform.Token(48)
	expiresAt := time.Now().Add(s.accessTokenTTL)
	offlineExpiresAt := time.Now().Add(s.offlineSessionTTL)
	_, err := s.db.Exec(ctx, `INSERT INTO auth_session(tenant_id,id,user_id,node_id,refresh_token_hash,expires_at,offline_expires_at,created_at,login_phone)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,''))`, tenantID, sessionID, userID, nodeID, platform.Hash([]byte(refreshToken)),
		expiresAt.UnixMicro(), offlineExpiresAt.UnixMicro(), platform.Now(), strings.TrimSpace(loginPhone))
	if err != nil {
		return nil, err
	}
	accessToken, err := s.accessToken(tenantID, userID, sessionID, clientType, expiresAt)
	if err != nil {
		return nil, err
	}
	principal, err := s.loadPrincipal(ctx, tenantID, userID, sessionID)
	if err != nil {
		return nil, err
	}
	principal.ClientType = clientType
	_, _ = s.db.Exec(ctx, "UPDATE user_account SET last_login_at=$1 WHERE tenant_id=$2 AND id=$3", platform.Now(), tenantID, userID)
	permissions := make([]string, 0, len(principal.Permissions))
	for permission := range principal.Permissions {
		permissions = append(permissions, permission)
	}
	/* 登录后进入制衣厂门户：带上厂编码/名称/版本类型（体验用户=trial，注册用户=paid） */
	var tenantCode, tenantName, customerType, contactPhone string
	var trialEnds *int64
	_ = s.db.QueryRow(ctx, `SELECT t.code,t.name,t.customer_type,ts.trial_ends_at,COALESCE(t.contact_phone,'')
		FROM tenant t LEFT JOIN tenant_subscription ts ON ts.tenant_id=t.id AND ts.customer_type='trial'
		WHERE t.id=$1`, tenantID).Scan(&tenantCode, &tenantName, &customerType, &trialEnds, &contactPhone)
	contactPhone = strings.TrimSpace(contactPhone)
	edition := "paid"
	if customerType == "trial" {
		edition = "trial"
	}
	/* 旧数据租户的注册信息里没有手机号（一直用账号密码登录）：厂管理员登录后先补录，再进入工作台。
	 * 普通员工账号不强制，避免影响车间日常使用。 */
	needContactPhone := false
	if contactPhone == "" {
		if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND r.code='tenant_admin')`, tenantID, userID).Scan(&needContactPhone); err != nil {
			needContactPhone = false
		}
	}
	return map[string]any{
		"access_token": accessToken, "refresh_token": refreshToken, "expires_at": expiresAt.UnixMicro(),
		"user":        map[string]any{"id": userID, "employee_id": employeeID, "tenant_id": tenantID},
		"tenant":      map[string]any{"code": tenantCode, "name": tenantName, "edition": edition, "trial_ends_at": trialEnds, "contact_phone": contactPhone},
		"permissions": permissions, "session": map[string]any{"id": sessionID, "offline_expires_at": offlineExpiresAt.UnixMicro()},
		"password_change_required": principal.PasswordChangeRequired,
		"need_contact_phone":       needContactPhone,
	}, nil
}

func (s *Service) accessToken(tenantID, userID, sessionID, clientType string, expiresAt time.Time) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		TenantID: tenantID, UserID: userID, SessionID: sessionID, ClientType: clientType,
		RegisteredClaims: jwt.RegisteredClaims{ID: platform.ID(), Issuer: "clothp", ExpiresAt: jwt.NewNumericDate(expiresAt), IssuedAt: jwt.NewNumericDate(time.Now())},
	})
	return token.SignedString(s.secret)
}

func (s *Service) AuthenticateRequest(r *http.Request) (Principal, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return Principal{}, ErrUnauthenticated
	}
	token, err := jwt.ParseWithClaims(strings.TrimPrefix(header, "Bearer "), &claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrUnauthenticated
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return Principal{}, ErrUnauthenticated
	}
	claims, ok := token.Claims.(*claims)
	if !ok || claims.Platform {
		return Principal{}, ErrUnauthenticated
	}
	var revokedAt *int64
	err = s.db.QueryRow(r.Context(), `SELECT revoked_at FROM auth_session WHERE tenant_id=$1 AND id=$2 AND user_id=$3`, claims.TenantID, claims.SessionID, claims.UserID).Scan(&revokedAt)
	if err != nil || revokedAt != nil {
		return Principal{}, ErrUnauthenticated
	}
	principal, err := s.loadPrincipal(r.Context(), claims.TenantID, claims.UserID, claims.SessionID)
	if err != nil {
		return Principal{}, err
	}
	principal.ClientType = claims.ClientType
	return principal, nil
}

func (s *Service) loadPrincipal(ctx context.Context, tenantID, userID, sessionID string) (Principal, error) {
	var principal Principal
	var employeeID *string
	err := s.db.QueryRow(ctx, `SELECT u.login_name,u.employee_id,u.password_change_required FROM user_account u JOIN tenant t ON t.id=u.tenant_id WHERE u.tenant_id=$1 AND u.id=$2 AND u.status='active' AND t.status='active'`, tenantID, userID).Scan(&principal.LoginName, &employeeID, &principal.PasswordChangeRequired)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	principal.TenantID, principal.UserID, principal.SessionID = tenantID, userID, sessionID
	if employeeID != nil {
		principal.EmployeeID = *employeeID
	}
	rows, err := s.db.Query(ctx, `SELECT DISTINCT p.code FROM user_role ur JOIN role_permission rp ON rp.tenant_id=ur.tenant_id AND rp.role_id=ur.role_id
		JOIN permission p ON p.tenant_id=rp.tenant_id AND p.id=rp.permission_id
		WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND ur.valid_from<=$3 AND (ur.valid_to IS NULL OR ur.valid_to>$3)`, tenantID, userID, platform.Now())
	if err != nil {
		return Principal{}, err
	}
	defer rows.Close()
	principal.Permissions = map[string]bool{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return Principal{}, err
		}
		principal.Permissions[code] = true
	}
	return principal, nil
}

/* Refresh 刷新会话：clientType 沿用原会话终端，由调用方（客户端刷新请求）带入，
 * 不得用手机会话刷新出 PC 令牌（设计 5.6.1）。 */
func (s *Service) Refresh(ctx context.Context, refreshToken, nodeID, clientType string) (map[string]any, error) {
	hash := platform.Hash([]byte(refreshToken))
	var tenantID, sessionID, userID, employeeID, loginPhone string
	err := s.db.QueryRow(ctx, `SELECT s.tenant_id,s.id,s.user_id,u.employee_id,COALESCE(s.login_phone,'') FROM auth_session s
		JOIN user_account u ON u.tenant_id=s.tenant_id AND u.id=s.user_id
		WHERE s.refresh_token_hash=$1 AND s.revoked_at IS NULL AND s.offline_expires_at>$2`, hash, platform.Now()).Scan(&tenantID, &sessionID, &userID, &employeeID, &loginPhone)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	if _, err = s.db.Exec(ctx, "UPDATE auth_session SET revoked_at=$1,reason='rotated' WHERE tenant_id=$2 AND id=$3", platform.Now(), tenantID, sessionID); err != nil {
		return nil, err
	}
	return s.issueTokens(ctx, tenantID, userID, employeeID, nodeID, loginPhone, clientType)
}

func (s *Service) Logout(ctx context.Context, p Principal) error {
	_, err := s.db.Exec(ctx, "UPDATE auth_session SET revoked_at=$1,reason='logout' WHERE tenant_id=$2 AND id=$3", platform.Now(), p.TenantID, p.SessionID)
	return err
}

/* HashPassword 生成 Argon2id 密码验证器（供人员建档/密码重置使用；密码明文不落库）。 */
func HashPassword(password string) (string, error) {
	return hashPassword(password)
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 2, 64*1024, 4, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=2,p=4$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	expected, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 2, 64*1024, 4, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
