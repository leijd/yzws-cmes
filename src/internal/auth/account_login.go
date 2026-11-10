package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"clothp/internal/platform"
	"clothp/internal/seccrypto"
	"github.com/jackc/pgx/v5"
)

/* 登录固定顺序校验失败错误（设计 5.6.3），由路由层映射为业务错误码。 */
var (
	ErrAccountNotFound = errors.New("ACCOUNT_NOT_FOUND")
	ErrNoTenantAccess  = errors.New("NO_TENANT_ACCESS")
	ErrInvalidPassword = errors.New("INVALID_PASSWORD")
	ErrAccountDisabled = errors.New("ACCOUNT_DISABLED")
	ErrClientForbidden = errors.New("CLIENT_FORBIDDEN")
)

/* AccountLoginInput 工号/手机号 + 密码登录（设计 5.6.1/5.6.3）。 */
type AccountLoginInput struct {
	TenantCode string `json:"tenant_code"`         // 必填：工号/手机号均租户内唯一
	Account    string `json:"account"`             // 工号（login_name）或手机号
	Password   string `json:"password"`            //
	ClientType string `json:"client_type"`         // pc/mobile；空按 pc
	NodeID     string `json:"node_id"`             //
	RequestID  string                            // 请求关联（登录日志）
	IP         string                            // 客户端 IP（密文库加密列，明文库同结构）
	DeviceInfo map[string]any                    // 设备摘要，不存 token
}

var accountPhonePattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

/* loginLogAttempt 登录日志行（auth_login_log，只允许 INSERT）。 */
type loginLogAttempt struct {
	TenantID           *string
	UserID             *string
	IdentifierMasked   string
	IdentifierHash     string
	Method             string
	ClientType         string
	Device             map[string]any
	IP                 string
	Result             string
	FailureCode        string
	FailureReason      string
	RequestID          string
	SessionID          *string
}

/* AccountLogin 固定顺序校验（设计 5.6.3）：
 * 1 账号/手机号是否存在 → 401 ACCOUNT_NOT_FOUND
 * 2 是否属于指定租户 → 403 NO_TENANT_ACCESS
 * 3 密码是否正确 → 401 INVALID_PASSWORD（未知账号用固定伪验证器，减小时序差）
 * 4 账号/租户是否启用 → 403 ACCOUNT_DISABLED
 * 5 终端是否允许 → 403 CLIENT_FORBIDDEN（工人账号仅 mobile）
 * 成功/失败均写 auth_login_log；日志失败不改变认证结果但随错误返回（不静默省略审计）。 */
func (s *Service) AccountLogin(ctx context.Context, in AccountLoginInput, crypto *seccrypto.Service) (map[string]any, error) {
	if crypto == nil {
		return nil, fmt.Errorf("login requires crypto service for phone hash")
	}
	clientType := strings.TrimSpace(in.ClientType)
	if clientType == "" {
		clientType = "pc"
	}
	if clientType != "pc" && clientType != "mobile" {
		return nil, fmt.Errorf("client_type must be pc or mobile")
	}
	account := strings.TrimSpace(in.Account)
	attempt := &loginLogAttempt{
		Method: "employee_no_password", ClientType: clientType,
		Device: in.DeviceInfo, IP: strings.TrimSpace(in.IP), RequestID: in.RequestID,
	}
	if attempt.RequestID == "" {
		attempt.RequestID = "req-" + platform.ID()
	}
	if account == "" || in.Password == "" {
		attempt.Result, attempt.FailureCode, attempt.FailureReason = "failure", "INVALID_REQUEST", "账号或密码为空"
		s.writeLoginLog(ctx, attempt)
		return nil, ErrInvalidCredentials
	}
	attempt.IdentifierMasked = maskIdentifier(account)
	attempt.IdentifierHash = identifierHMAC(ctx, crypto, account)

	fail := func(code, reason string) error {
		attempt.Result, attempt.FailureCode, attempt.FailureReason = "failure", code, reason
		s.writeLoginLog(ctx, attempt)
		switch code {
		case "ACCOUNT_NOT_FOUND":
			return ErrAccountNotFound
		case "NO_TENANT_ACCESS":
			return ErrNoTenantAccess
		case "INVALID_PASSWORD":
			return ErrInvalidPassword
		case "ACCOUNT_DISABLED":
			return ErrAccountDisabled
		case "CLIENT_FORBIDDEN":
			return ErrClientForbidden
		default:
			return ErrInvalidCredentials
		}
	}

	/* 解析租户；未知租户时无法在租户内定位账号（手机号/工号均租户内唯一） */
	var tenantID, tenantStatus string
	tenantErr := s.db.QueryRow(ctx, `SELECT id,status FROM tenant WHERE code=$1`, strings.TrimSpace(in.TenantCode)).Scan(&tenantID, &tenantStatus)
	tenantKnown := tenantErr == nil

	/* 定位账号：工号等值 login_name；手机号经盲索引（employee.phone_hash / tenant.contact_phone_hash） */
	var userID, employeeID, verifier, accountStatus, clientScope string
	var userTenantID string
	found := false
	isPhone := accountPhonePattern.MatchString(account)
	if tenantKnown {
		var err error
		userID, employeeID, verifier, accountStatus, clientScope, userTenantID, found, err = s.lookupAccount(ctx, tenantID, account, isPhone, crypto)
		if err != nil {
			return nil, err
		}
	}
	/* 顺序 1：账号是否存在（任何租户都找不到） */
	if !found {
		/* 未知账号固定伪验证器：减小时序差（设计 5.6.3） */
		_ = verifyPassword(dummyVerifier, in.Password)
		return nil, fail("ACCOUNT_NOT_FOUND", "账号或手机号未注册")
	}
	/* 顺序 2：是否属于指定租户 */
	if !tenantKnown || userTenantID != tenantID {
		return nil, fail("NO_TENANT_ACCESS", "无本租户访问权限")
	}
	/* 顺序 3：密码 */
	if !verifyPassword(verifier, in.Password) {
		return nil, fail("INVALID_PASSWORD", "密码错误")
	}
	/* 顺序 4：账号/租户启用 */
	if tenantStatus != "active" {
		return nil, fail("ACCOUNT_DISABLED", "租户已停用")
	}
	if accountStatus != "active" {
		return nil, fail("ACCOUNT_DISABLED", "账号已停用")
	}
	/* 顺序 5：终端允许（client_scope=all/pc/mobile；mobile 仅允许 mobile 端） */
	if clientScope == "mobile" && clientType != "mobile" {
		return nil, fail("CLIENT_FORBIDDEN", "工人账号仅允许登录手机端")
	}
	if clientScope == "pc" && clientType != "pc" {
		return nil, fail("CLIENT_FORBIDDEN", "该账号仅允许登录 PC 端")
	}

	result, err := s.issueTokens(ctx, tenantID, userID, employeeID, in.NodeID, "", clientType)
	if err != nil {
		return nil, err
	}
	attempt.Result = "success"
	attempt.TenantID = &tenantID
	attempt.UserID = &userID
	if session, ok := result["session"].(map[string]any); ok {
		if sid, ok2 := session["id"].(string); ok2 {
			attempt.SessionID = &sid
		}
	}
	s.writeLoginLog(ctx, attempt)
	return result, nil
}

/* lookupAccount 在指定租户内定位账号：工号走 login_name；手机号走盲索引。 */
func (s *Service) lookupAccount(ctx context.Context, tenantID, account string, isPhone bool, crypto *seccrypto.Service) (userID, employeeID, verifier, status, clientScope, userTenantID string, found bool, err error) {
	if isPhone {
		/* 员工手机号命中 */
		empHash, errH := crypto.PhoneHash(ctx, tenantID, "employee.phone", account)
		if errH != nil {
			return "", "", "", "", "", "", false, errH
		}
		err = s.db.QueryRow(ctx, `SELECT u.id,u.employee_id,u.password_verifier,u.status,COALESCE(u.client_scope,'all'),u.tenant_id
			FROM employee e JOIN user_account u ON u.tenant_id=e.tenant_id AND u.employee_id=e.id
			WHERE e.tenant_id=$1 AND e.phone_hash=$2 AND u.status<>'disabled'
			ORDER BY u.created_at LIMIT 1`, tenantID, empHash).Scan(&userID, &employeeID, &verifier, &status, &clientScope, &userTenantID)
		if err == nil {
			return userID, employeeID, verifier, status, clientScope, userTenantID, true, nil
		}
		if err != pgx.ErrNoRows {
			return "", "", "", "", "", "", false, err
		}
		/* 本厂注册联系电话命中（厂管理员账号） */
		contactHash, errC := crypto.PhoneHash(ctx, tenantID, "tenant.contact_phone", account)
		if errC != nil {
			return "", "", "", "", "", "", false, errC
		}
		err = s.db.QueryRow(ctx, `SELECT u.id,u.employee_id,u.password_verifier,u.status,COALESCE(u.client_scope,'all'),u.tenant_id
			FROM tenant t JOIN user_account u ON u.tenant_id=t.id AND u.employee_id=t.admin_employee_id
			WHERE t.id=$1 AND t.contact_phone_hash=$2`, tenantID, contactHash).Scan(&userID, &employeeID, &verifier, &status, &clientScope, &userTenantID)
		if err == nil {
			return userID, employeeID, verifier, status, clientScope, userTenantID, true, nil
		}
		if err != pgx.ErrNoRows {
			return "", "", "", "", "", "", false, err
		}
		return "", "", "", "", "", "", false, nil
	}
	err = s.db.QueryRow(ctx, `SELECT id,COALESCE(employee_id,''),password_verifier,status,COALESCE(client_scope,'all'),tenant_id
		FROM user_account WHERE tenant_id=$1 AND login_name=$2`, tenantID, account).Scan(&userID, &employeeID, &verifier, &status, &clientScope, &userTenantID)
	if err == nil {
		return userID, employeeID, verifier, status, clientScope, userTenantID, true, nil
	}
	if err == pgx.ErrNoRows {
		return "", "", "", "", "", "", false, nil
	}
	return "", "", "", "", "", "", false, err
}

/* writeLoginLog 写登录日志（只允许 INSERT；失败仅告警——日志失败不改变认证结果，设计 5.6.4）。 */
func (s *Service) writeLoginLog(ctx context.Context, a *loginLogAttempt) {
	device, _ := json.Marshal(a.Device)
	_, err := s.db.Exec(ctx, `INSERT INTO auth_login_log(id,tenant_id,user_id,login_identifier_masked,login_identifier_hash,
		login_method,client_type,device_info,ip,result,failure_code,failure_reason,request_id,session_id,occurred_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		platform.ID(), a.TenantID, a.UserID, a.IdentifierMasked, a.IdentifierHash, a.Method, a.ClientType,
		device, a.IP, a.Result, nullString(a.FailureCode), nullString(a.FailureReason), a.RequestID, a.SessionID, platform.Now())
	if err != nil {
		// 设计 5.6.4：登录日志不可用时必须告警，不得静默省略审计
		fmt.Printf("ALERT auth_login_log write failed request=%s result=%s: %v\n", a.RequestID, a.Result, err)
	}
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

/* maskIdentifier 登录标识脱敏：手机号保留前 3 后 2，工号保留前 2 位。 */
func maskIdentifier(account string) string {
	if accountPhonePattern.MatchString(account) && len(account) == 11 {
		return account[:3] + "****" + account[9:]
	}
	if len(account) <= 2 {
		return account
	}
	return account[:2] + "***"
}

/* identifierHMAC 登录标识 HMAC（安全统计用；明文库存同值）。 */
func identifierHMAC(ctx context.Context, crypto *seccrypto.Service, account string) string {
	h, err := crypto.BlindIndexForPlatform(ctx, "auth.login_identifier", account)
	if err != nil {
		return ""
	}
	return h
}

/* 未知账号固定伪验证器（argon2id 格式固定，用于抹平时序差）。 */
const dummyVerifier = "$argon2id$v=19$m=65536,t=2,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
