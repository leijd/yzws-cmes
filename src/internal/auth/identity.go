package auth

import (
	"context"

	"clothp/internal/dualwrite"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

type TenantUserEmployee struct {
	EmployeeID string /* 预生成：密文库加密手机号需要主键（信封绑定行） */
	EmployeeNo string
	Name       string
	Gender     *string
	BirthDate  *string
	Phone      *string /* 明文库写入值 */
	SecurePhone *string /* 密文库写入值（enc:v1）；未配置加密时等同 Phone */
	PhoneHash  string  /* employee.phone 盲索引；空则不写 */
	OrgUnitID  *string
	Position   *string
}

/* CreateTenantUser 员工+账号一体创建（用户管理页）。双库写入：敏感列密文侧加密、
 * phone_hash 盲索引两侧一致。历史单库直写会把明文手机号落进密文库且不建盲索引，
 * 导致短信登录按 phone_hash 永远查不到人。 */
func (s *Service) CreateTenantUser(ctx context.Context, tx *dualwrite.Tx, tenantID, loginName, password string, roleIDs []string, employee TenantUserEmployee) (string, string, error) {
	verifier, err := hashPassword(password)
	if err != nil {
		return "", "", err
	}
	now := platform.Now()
	securePhone := employee.SecurePhone
	if securePhone == nil {
		securePhone = employee.Phone
	}
	insertEmployee := `INSERT INTO employee(tenant_id,id,employee_no,name,gender,birth_date,phone,phone_hash,org_unit_id,position,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),$9,$10,$11,$12,$13,$11,$12,$13)`
	if tx.Plain() != nil {
		if _, err := tx.Plain().Exec(ctx, insertEmployee, tenantID, employee.EmployeeID, employee.EmployeeNo, employee.Name, employee.Gender, employee.BirthDate, employee.Phone, employee.PhoneHash, employee.OrgUnitID, employee.Position, now, employee.EmployeeID, s.nodeID); err != nil {
			return "", "", err
		}
	}
	if tx.Secure() != nil {
		if _, err := tx.Secure().Exec(ctx, insertEmployee, tenantID, employee.EmployeeID, employee.EmployeeNo, employee.Name, employee.Gender, employee.BirthDate, securePhone, employee.PhoneHash, employee.OrgUnitID, employee.Position, now, employee.EmployeeID, s.nodeID); err != nil {
			return "", "", err
		}
	}
	id := platform.ID()
	if err := dualwrite.ExecBoth(ctx, tx, `INSERT INTO user_account(tenant_id,id,employee_id,login_name,password_verifier,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$2,$7,$6,$2,$7)`, tenantID, id, employee.EmployeeID, loginName, verifier, now, s.nodeID); err != nil {
		return "", "", err
	}
	for _, roleID := range roleIDs {
		if err := dualwrite.ExecBoth(ctx, tx, `INSERT INTO user_role(tenant_id,id,user_id,role_id,valid_from) SELECT $1,$2,$3,id,$4 FROM role WHERE tenant_id=$1 AND id=$5`, tenantID, platform.ID(), id, now, roleID); err != nil {
			return "", "", err
		}
	}
	return id, employee.EmployeeID, nil
}

// CreateEmployeeAccount 为已有员工开通登录账号，不重复创建员工档案。
func (s *Service) CreateEmployeeAccount(ctx context.Context, tx *dualwrite.Tx, tenantID, employeeID, loginName, password, actorID string, roleIDs []string) (string, error) {
	verifier, err := hashPassword(password)
	if err != nil {
		return "", err
	}
	now := platform.Now()
	id := platform.ID()
	if err := dualwrite.ExecBoth(ctx, tx, `INSERT INTO user_account(tenant_id,id,employee_id,login_name,password_verifier,created_at,created_by,created_node,updated_at,updated_by,updated_node) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$6,$7,$8)`, tenantID, id, employeeID, loginName, verifier, now, actorID, s.nodeID); err != nil {
		return "", err
	}
	for _, roleID := range roleIDs {
		if err := dualwrite.ExecBoth(ctx, tx, `INSERT INTO user_role(tenant_id,id,user_id,role_id,valid_from) SELECT $1,$2,$3,id,$4 FROM role WHERE tenant_id=$1 AND id=$5`, tenantID, platform.ID(), id, now, roleID); err != nil {
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
