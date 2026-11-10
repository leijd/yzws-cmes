package auth

import (
	"context"

	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

type TenantUserEmployee struct {
	EmployeeNo string
	Name       string
	Gender     *string
	BirthDate  *string
	Phone      *string
	OrgUnitID  *string
	Position   *string
}

func (s *Service) CreateTenantUser(ctx context.Context, tx pgx.Tx, tenantID, loginName, password string, roleIDs []string, employee TenantUserEmployee) (string, string, error) {
	verifier, err := hashPassword(password)
	if err != nil {
		return "", "", err
	}
	now := platform.Now()
	employeeID := platform.ID()
	if _, err := tx.Exec(ctx, `INSERT INTO employee(tenant_id,id,employee_no,name,gender,birth_date,phone,org_unit_id,position,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$10,$11,$12)`, tenantID, employeeID, employee.EmployeeNo, employee.Name, employee.Gender, employee.BirthDate, employee.Phone, employee.OrgUnitID, employee.Position, now, employeeID, s.nodeID); err != nil {
		return "", "", err
	}
	id := platform.ID()
	if _, err := tx.Exec(ctx, `INSERT INTO user_account(tenant_id,id,employee_id,login_name,password_verifier,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$2,$7,$6,$2,$7)`, tenantID, id, employeeID, loginName, verifier, now, s.nodeID); err != nil {
		return "", "", err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO user_role(tenant_id,id,user_id,role_id,valid_from) SELECT $1,$2,$3,id,$4 FROM role WHERE tenant_id=$1 AND id=$5`, tenantID, platform.ID(), id, now, roleID); err != nil {
			return "", "", err
		}
	}
	return id, employeeID, nil
}

// CreateEmployeeAccount 为已有员工开通登录账号，不重复创建员工档案。
func (s *Service) CreateEmployeeAccount(ctx context.Context, tx pgx.Tx, tenantID, employeeID, loginName, password, actorID string, roleIDs []string) (string, error) {
	verifier, err := hashPassword(password)
	if err != nil {
		return "", err
	}
	now := platform.Now()
	id := platform.ID()
	if _, err := tx.Exec(ctx, `INSERT INTO user_account(tenant_id,id,employee_id,login_name,password_verifier,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$6,$7,$8)`, tenantID, id, employeeID, loginName, verifier, now, actorID, s.nodeID); err != nil {
		return "", err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO user_role(tenant_id,id,user_id,role_id,valid_from) SELECT $1,$2,$3,id,$4 FROM role WHERE tenant_id=$1 AND id=$5`, tenantID, platform.ID(), id, now, roleID); err != nil {
			return "", err
		}
	}
	return id, nil
}

// ResetAccountPassword 由管理员重置密码，并注销该账号现有会话。
func (s *Service) ResetAccountPassword(ctx context.Context, tx pgx.Tx, tenantID, userID, password, actorID string) error {
	verifier, err := hashPassword(password)
	if err != nil {
		return err
	}
	now := platform.Now()
	if _, err := tx.Exec(ctx, `UPDATE user_account SET password_verifier=$1,version=version+1,updated_at=$2,updated_by=$3,updated_node=$4 WHERE tenant_id=$5 AND id=$6`, verifier, now, actorID, s.nodeID, tenantID, userID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE auth_session SET revoked_at=$1,reason='password_reset' WHERE tenant_id=$2 AND user_id=$3 AND revoked_at IS NULL`, now, tenantID, userID)
	return err
}
