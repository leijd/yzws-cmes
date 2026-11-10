package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"

	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

var ErrWeakPassword = errors.New("weak password")

func (s *Service) ChangePassword(ctx context.Context, principal Principal, oldPassword, newPassword string) (int64, error) {
	if len(newPassword) < 12 || newPassword == oldPassword {
		return 0, ErrWeakPassword
	}
	var verifier string
	if err := s.db.QueryRow(ctx, "SELECT password_verifier FROM user_account WHERE tenant_id=$1 AND id=$2", principal.TenantID, principal.UserID).Scan(&verifier); err != nil {
		return 0, ErrUnauthenticated
	}
	if !verifyPassword(verifier, oldPassword) {
		return 0, ErrInvalidCredentials
	}
	updatedVerifier, err := hashPassword(newPassword)
	if err != nil {
		return 0, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	now := platform.Now()
	if _, err := tx.Exec(ctx, `UPDATE user_account SET password_verifier=$1,password_change_required=FALSE,password_reset_at=$2,password_reset_by=$3,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6`, updatedVerifier, now, principal.UserID, s.nodeID, principal.TenantID, principal.UserID); err != nil {
		return 0, err
	}
	result, err := tx.Exec(ctx, "UPDATE auth_session SET revoked_at=$1,reason='password_changed' WHERE tenant_id=$2 AND user_id=$3 AND id<>$4 AND revoked_at IS NULL", now, principal.TenantID, principal.UserID, principal.SessionID)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

func (s *Service) ResetTenantAdminPassword(ctx context.Context, tx pgx.Tx, tenantID, userID, temporaryPassword, actorID string) error {
	verifier, err := hashPassword(temporaryPassword)
	if err != nil {
		return err
	}
	now := platform.Now()
	result, err := tx.Exec(ctx, `UPDATE user_account u SET password_verifier=$1,password_change_required=TRUE,password_reset_at=$2,password_reset_by=$3,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6 AND EXISTS(SELECT 1 FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id WHERE ur.tenant_id=u.tenant_id AND ur.user_id=u.id AND r.code='tenant_admin')`, verifier, now, actorID, s.nodeID, tenantID, userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	_, err = tx.Exec(ctx, "UPDATE auth_session SET revoked_at=$1,reason='platform_password_reset' WHERE tenant_id=$2 AND user_id=$3 AND revoked_at IS NULL", now, tenantID, userID)
	return err
}

var temporaryPasswordGroups = []string{
	"abcdefghijklmnopqrstuvwxyz",
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	"0123456789",
	"!@#$%&*?",
}

func GenerateTemporaryPassword(length int) (string, error) {
	if length < len(temporaryPasswordGroups) || length > 12 {
		return "", ErrWeakPassword
	}
	all := temporaryPasswordGroups[0] + temporaryPasswordGroups[1] + temporaryPasswordGroups[2] + temporaryPasswordGroups[3]
	password := make([]byte, 0, length)
	for _, group := range temporaryPasswordGroups {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(group))))
		if err != nil {
			return "", err
		}
		password = append(password, group[index.Int64()])
	}
	for len(password) < length {
		index, err := rand.Int(rand.Reader, big.NewInt(int64(len(all))))
		if err != nil {
			return "", err
		}
		password = append(password, all[index.Int64()])
	}
	for i := len(password) - 1; i > 0; i-- {
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		password[i], password[j.Int64()] = password[j.Int64()], password[i]
	}
	return string(password), nil
}
