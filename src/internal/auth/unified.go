package auth

import (
	"context"
	"errors"
)

var ErrAmbiguousLogin = errors.New("ambiguous login")

func (s *Service) UnifiedLogin(ctx context.Context, input PlatformLoginInput) (map[string]any, string, error) {
	platformResult, err := s.PlatformLogin(ctx, input)
	if err == nil {
		platformResult["account_type"] = "platform"
		return platformResult, "platform", nil
	}
	/* 租户必须已审批通过且启用：驳回/待审的租户即使残留 active 账号也禁止登录 */
	rows, err := s.db.Query(ctx, `SELECT u.tenant_id,u.id,u.employee_id,u.password_verifier FROM user_account u JOIN tenant t ON t.id=u.tenant_id WHERE u.login_name=$1 AND u.status='active' AND t.status='active' AND t.registration_status='approved'`, input.LoginName)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	type candidate struct{ tenantID, userID, employeeID, verifier string }
	matches := []candidate{}
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.tenantID, &item.userID, &item.employeeID, &item.verifier); err != nil {
			return nil, "", err
		}
		if verifyPassword(item.verifier, input.Password) {
			matches = append(matches, item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(matches) == 0 {
		return nil, "", ErrInvalidCredentials
	}
	if len(matches) > 1 {
		return nil, "", ErrAmbiguousLogin
	}
	match := matches[0]
	result, err := s.issueTokens(ctx, match.tenantID, match.userID, match.employeeID, input.NodeID, "", "pc")
	if err != nil {
		return nil, "", err
	}
	result["account_type"] = "tenant"
	return result, "tenant", nil
}
