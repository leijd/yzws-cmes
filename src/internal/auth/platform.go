package auth

import (
	"context"
	"net/http"
	"strings"
	"time"

	"clothp/internal/platform"
	"github.com/golang-jwt/jwt/v5"
)

type PlatformPrincipal struct {
	AdminID   string
	LoginName string
	SessionID string
	// Role: super=平台管理员（全部权限）；assistant=辅助人员（按 Permissions 分配）
	Role        string
	Permissions map[string]bool
}

type PlatformLoginInput struct {
	LoginName string `json:"login_name"`
	Password  string `json:"password"`
	NodeID    string `json:"node_id"`
}

// HashPlatformPassword 平台账号口令哈希（供辅助人员创建/重置密码使用）。
func HashPlatformPassword(password string) (string, error) { return hashPassword(password) }

func (s *Service) BootstrapPlatformAdmin(ctx context.Context, password string) error {
	var count int
	if err := s.db.QueryRow(ctx, "SELECT COUNT(*) FROM platform_admin").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	verifier, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, "INSERT INTO platform_admin(id,login_name,password_verifier,status,created_at) VALUES($1,'platform',$2,'active',$3)", platform.ID(), verifier, platform.Now())
	return err
}

func (s *Service) PlatformLogin(ctx context.Context, input PlatformLoginInput) (map[string]any, error) {
	var adminID, verifier, status, role, name string
	err := s.db.QueryRow(ctx, "SELECT id,password_verifier,status,role,COALESCE(name,'') FROM platform_admin WHERE login_name=$1", input.LoginName).Scan(&adminID, &verifier, &status, &role, &name)
	if err != nil || status != "active" || !verifyPassword(verifier, input.Password) {
		return nil, ErrInvalidCredentials
	}
	permissions, err := s.loadPlatformPermissions(ctx, adminID, role)
	if err != nil {
		return nil, err
	}
	sessionID := platform.ID()
	expiresAt := time.Now().Add(s.accessTokenTTL)
	if _, err := s.db.Exec(ctx, "INSERT INTO platform_session(id,admin_id,expires_at,created_at) VALUES($1,$2,$3,$4)", sessionID, adminID, expiresAt.UnixMicro(), platform.Now()); err != nil {
		return nil, err
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		SessionID: sessionID, Platform: true,
		RegisteredClaims: jwt.RegisteredClaims{ID: platform.ID(), Issuer: "clothp-platform", Subject: adminID, ExpiresAt: jwt.NewNumericDate(expiresAt), IssuedAt: jwt.NewNumericDate(time.Now())},
	})
	accessToken, err := token.SignedString(s.secret)
	if err != nil {
		return nil, err
	}
	_, _ = s.db.Exec(ctx, "UPDATE platform_admin SET last_login_at=$1 WHERE id=$2", platform.Now(), adminID)
	return map[string]any{
		"access_token": accessToken, "expires_at": expiresAt.UnixMicro(),
		"admin":        map[string]any{"id": adminID, "login_name": input.LoginName, "role": role, "name": name, "permissions": permissions},
		"session":      map[string]any{"id": sessionID},
	}, nil
}

// loadPlatformPermissions：super 全量放行（返回空集，由网关按角色判断）；assistant 按分配权限点加载。
func (s *Service) loadPlatformPermissions(ctx context.Context, adminID, role string) ([]string, error) {
	if role != "assistant" {
		return []string{}, nil
	}
	rows, err := s.db.Query(ctx, "SELECT permission_code FROM platform_admin_permission WHERE admin_id=$1", adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	codes := []string{}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		codes = append(codes, code)
	}
	return codes, rows.Err()
}

func (s *Service) AuthenticatePlatformRequest(r *http.Request) (PlatformPrincipal, error) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return PlatformPrincipal{}, ErrUnauthenticated
	}
	token, err := jwt.ParseWithClaims(strings.TrimPrefix(header, "Bearer "), &claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrUnauthenticated
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return PlatformPrincipal{}, ErrUnauthenticated
	}
	claims, ok := token.Claims.(*claims)
	if !ok || !claims.Platform {
		return PlatformPrincipal{}, ErrUnauthenticated
	}
	var principal PlatformPrincipal
	var revokedAt *int64
	var status, role string
	err = s.db.QueryRow(r.Context(), `SELECT a.id,a.login_name,a.status,a.role,s.revoked_at FROM platform_session s JOIN platform_admin a ON a.id=s.admin_id WHERE s.id=$1 AND a.id=$2`, claims.SessionID, claims.Subject).Scan(&principal.AdminID, &principal.LoginName, &status, &role, &revokedAt)
	if err != nil || revokedAt != nil || status != "active" {
		return PlatformPrincipal{}, ErrUnauthenticated
	}
	principal.SessionID = claims.SessionID
	principal.Role = role
	if role == "assistant" {
		codes, err := s.loadPlatformPermissions(r.Context(), principal.AdminID, role)
		if err != nil {
			return PlatformPrincipal{}, err
		}
		principal.Permissions = make(map[string]bool, len(codes))
		for _, code := range codes {
			principal.Permissions[code] = true
		}
	}
	return principal, nil
}

func (s *Service) PlatformLogout(ctx context.Context, principal PlatformPrincipal) error {
	_, err := s.db.Exec(ctx, "UPDATE platform_session SET revoked_at=$1 WHERE id=$2 AND admin_id=$3", platform.Now(), principal.SessionID, principal.AdminID)
	return err
}
