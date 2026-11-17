package auth

import (
	"context"
	"errors"
	"strings"

	"clothp/internal/platform"
)

var ErrAmbiguousLogin = errors.New("ambiguous login")

/* UnifiedLogin 统一登录入口（账号+密码，自动识别平台账号/租户账号）。
 * 租户账号的成功与失败都写 auth_login_log（设计 4.9）；平台账号由平台审计记录。 */
func (s *Service) UnifiedLogin(ctx context.Context, input PlatformLoginInput) (map[string]any, string, error) {
	platformResult, err := s.PlatformLogin(ctx, input)
	if err == nil {
		platformResult["account_type"] = "platform"
		return platformResult, "platform", nil
	}
	loginName := strings.TrimSpace(input.LoginName)
	attempt := &loginLogAttempt{
		Method: "unified", ClientType: "pc", RequestID: "req-" + platform.ID(),
		IdentifierMasked: maskIdentifier(loginName), IdentifierHash: platform.Hash([]byte(loginName)),
	}
	fail := func(code, reason string, err error) (map[string]any, string, error) {
		attempt.Result, attempt.FailureCode, attempt.FailureReason = "failure", code, reason
		s.writeLoginLog(ctx, attempt)
		return nil, "", err
	}
	/* 租户必须已审批通过且启用：驳回/待审的租户即使残留 active 账号也禁止登录 */
	rows, err := s.db.Query(ctx, `SELECT u.tenant_id,u.id,u.employee_id,u.password_verifier FROM user_account u JOIN tenant t ON t.id=u.tenant_id WHERE u.login_name=$1 AND u.status='active' AND t.status='active' AND t.registration_status='approved'`, input.LoginName)
	if err != nil {
		return fail("INTERNAL_ERROR", "查询账号失败", err)
	}
	defer rows.Close()
	type candidate struct{ tenantID, userID, employeeID, verifier string }
	matches := []candidate{}
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.tenantID, &item.userID, &item.employeeID, &item.verifier); err != nil {
			return fail("INTERNAL_ERROR", "读取账号失败", err)
		}
		if verifyPassword(item.verifier, input.Password) {
			matches = append(matches, item)
		}
	}
	if err := rows.Err(); err != nil {
		return fail("INTERNAL_ERROR", "读取账号失败", err)
	}
	if len(matches) == 0 {
		return fail("INVALID_PASSWORD", "账号不存在或密码错误", ErrInvalidCredentials)
	}
	if len(matches) > 1 {
		return fail("AMBIGUOUS_LOGIN", "该账号在多个租户中存在", ErrAmbiguousLogin)
	}
	match := matches[0]
	result, err := s.issueTokens(ctx, match.tenantID, match.userID, match.employeeID, input.NodeID, "", "pc")
	if err != nil {
		return fail("INTERNAL_ERROR", "签发会话失败", err)
	}
	attempt.Result = "success"
	attempt.TenantID, attempt.UserID = &match.tenantID, &match.userID
	if session, ok := result["session"].(map[string]any); ok {
		if sid, ok2 := session["id"].(string); ok2 {
			attempt.SessionID = &sid
		}
	}
	s.writeLoginLog(ctx, attempt)
	result["account_type"] = "tenant"
	return result, "tenant", nil
}
