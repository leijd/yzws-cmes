// Package person 管理人员与工人建档、组织归属（换线）API（设计 5.4）。
//
// 建档事务：人员 + 当前主归属（employee_org_binding）+ 登录账号 + 角色 + 工序专长（工人）
// 在同一 2PC 事务写入两库；employee 敏感列在密文库为 enc:v1 信封，明文库为明文 + phone_hash。
// 工人账号强制 client_scope=mobile 且不授予 PC 菜单；管理人员携带工序专长返回 FIELD_NOT_APPLICABLE。
package person

import (
	"context"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"clothp/internal/seccrypto"
	"github.com/jackc/pgx/v5"
)

func Register(api *httpapi.API) {
	if api.Writer == nil || api.Crypto == nil {
		return // 缺少双写/加密依赖时不注册（单库旧模式由既有 identity 端点承担）
	}
	registerCreate(api)
	registerOrgBinding(api)
	registerDisable(api)
	registerQuery(api)
}

/* execTwo 无敏感列语句双写；secureRow 为敏感行（已按密文库口径加密）。 */
func execTwo(ctx context.Context, tx *dualwrite.Tx, sql string, args ...any) error {
	if tx.Plain() != nil {
		if _, err := tx.Plain().Exec(ctx, sql, args...); err != nil {
			return err
		}
	}
	if tx.Secure() != nil {
		if _, err := tx.Secure().Exec(ctx, sql, args...); err != nil {
			return err
		}
	}
	return nil
}

func registerCreate(api *httpapi.API) {
	/* 建档：人员 + 账号 + 角色 + 归属 + 专长（设计 5.4 创建事务顺序） */
	api.Handle("POST /api/v1/people", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			PersonType, EmployeeNo, Name, Gender, Phone, Password string
			RoleIDs                                               []string
			WorkshopOrgID, LineOrgID, TeamOrgID                   string
			SkillProcessIDs                                       []string
			IDCardNo, BankName, BankCard                          string
			EmergencyContact, EmergencyPhone, Address             string
			HireDate, Position, Remark, ManagerEmployeeID         string
			AsLineManager                                         bool // 同时登记为产线线长
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		if in.PersonType != "manager" && in.PersonType != "worker" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "人员类型必须为 manager 或 worker")
		}
		employeeNo, name := strings.TrimSpace(in.EmployeeNo), strings.TrimSpace(in.Name)
		if employeeNo == "" || name == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "工号与姓名必填")
		}
		if len(in.Password) < 12 {
			return httpapi.Error(http.StatusBadRequest, "WEAK_PASSWORD", "初始密码至少 12 位")
		}
		if in.PersonType == "manager" && len(in.SkillProcessIDs) > 0 {
			return httpapi.Error(http.StatusBadRequest, "FIELD_NOT_APPLICABLE", "管理人员不维护工序专长")
		}
		/* 唯一性：工号、手机号 hash（设计 5.4 顺序 1） */
		var dupNo bool
		if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND employee_no=$2)`, p.TenantID, employeeNo).Scan(&dupNo); err != nil {
			return err
		}
		if dupNo {
			return httpapi.Error(http.StatusConflict, "DUPLICATE_EMPLOYEE_NO", "工号已存在，请更换")
		}
		phone := seccrypto.NormalizePhone(in.Phone)
		phoneHash := ""
		if phone != "" {
			h, err := api.Crypto.PhoneHash(ctx, p.TenantID, "employee.phone", phone)
			if err != nil {
				return err
			}
			phoneHash = h
			var dupPhone bool
			if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND phone_hash=$2)`, p.TenantID, phoneHash).Scan(&dupPhone); err != nil {
				return err
			}
			if dupPhone {
				return httpapi.Error(http.StatusConflict, "DUPLICATE_PHONE", "该手机号已被本厂其他人员使用")
			}
		}
		/* 组织层级校验（顺序 2） */
		if err := validateOrgChain(ctx, api.DB, p.TenantID, in.WorkshopOrgID, in.LineOrgID, in.TeamOrgID); err != nil {
			return err
		}
		/* 角色均属本厂 */
		for _, roleID := range in.RoleIDs {
			var ok bool
			if err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM role WHERE tenant_id=$1 AND id=$2)`, p.TenantID, roleID).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return httpapi.Error(http.StatusBadRequest, "ROLE_INVALID", "角色不存在或不属于本厂")
			}
		}
		verifier, err := auth.HashPassword(in.Password)
		if err != nil {
			return err
		}
		employeeID := platform.ID()
		accountID := platform.ID()
		now := platform.Now()
		rowCrypto := dualwrite.NewRowCrypto(api.Crypto)
		orgUnitID := in.TeamOrgID
		if orgUnitID == "" {
			orgUnitID = in.LineOrgID
		}
		err = withTx(ctx, api, p.TenantID, "employee", employeeID, func(tx *dualwrite.Tx) error {
			/* employee：明文库明文 + hash；密文库敏感列加密 */
			securePhone := phone
			secureIDCard := in.IDCardNo
			secureAddress := in.Address
			secureBankCard := in.BankCard
			secureBankName := in.BankName
			secureEmergencyContact := in.EmergencyContact
			secureEmergencyPhone := in.EmergencyPhone
			if tx.Secure() != nil {
				values := map[string]any{
					"phone": phone, "id_card_no": in.IDCardNo, "address": in.Address,
					"bank_card": in.BankCard, "bank_name": in.BankName,
					"emergency_contact": in.EmergencyContact, "emergency_phone": in.EmergencyPhone,
				}
				encrypted, err := rowCrypto.ForSecure(ctx, p.TenantID, "employee", employeeID, values)
				if err != nil {
					return err
				}
				securePhone = strOf(encrypted["phone"], phone)
				secureIDCard = strOf(encrypted["id_card_no"], "")
				secureAddress = strOf(encrypted["address"], "")
				secureBankCard = strOf(encrypted["bank_card"], "")
				secureBankName = strOf(encrypted["bank_name"], "")
				secureEmergencyContact = strOf(encrypted["emergency_contact"], "")
				secureEmergencyPhone = strOf(encrypted["emergency_phone"], "")
			}
			insertEmployee := `INSERT INTO employee(tenant_id,id,employee_no,name,gender,person_type,phone,phone_hash,birth_date,id_card_no,address,
					nation,native_place,hire_date,status,workshop_org_id,line_org_id,team_org_id,org_unit_id,manager_employee_id,position,remark,
					bank_card,bank_name,emergency_contact,emergency_phone,created_at,updated_at,created_by,created_node,version)
				VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7,NULLIF($8,''),NULL,NULLIF($9,''),NULLIF($10,''),NULL,NULL,NULLIF($11,''),'active',
					NULLIF($12,''),NULLIF($13,''),NULLIF($14,''),NULLIF($15,''),NULLIF($16,''),NULLIF($17,''),NULLIF($18,''),
					NULLIF($19,''),NULLIF($20,''),NULLIF($21,''),NULLIF($22,''),$23,$23,$24,$25,1)`
			if tx.Plain() != nil {
				if _, err := tx.Plain().Exec(ctx, insertEmployee, p.TenantID, employeeID, employeeNo, name, in.Gender, in.PersonType,
					phone, phoneHash, in.IDCardNo, in.Address, in.HireDate, in.WorkshopOrgID, in.LineOrgID, in.TeamOrgID, orgUnitID,
					in.ManagerEmployeeID, in.Position, in.Remark, in.BankCard, in.BankName, in.EmergencyContact, in.EmergencyPhone,
					now, p.UserID, api.Config.NodeID); err != nil {
					return err
				}
			}
			if tx.Secure() != nil {
				if _, err := tx.Secure().Exec(ctx, insertEmployee, p.TenantID, employeeID, employeeNo, name, in.Gender, in.PersonType,
					securePhone, phoneHash, secureIDCard, secureAddress, in.HireDate, in.WorkshopOrgID, in.LineOrgID, in.TeamOrgID, orgUnitID,
					in.ManagerEmployeeID, in.Position, in.Remark, secureBankCard, secureBankName, secureEmergencyContact, secureEmergencyPhone,
					now, p.UserID, api.Config.NodeID); err != nil {
					return err
				}
			}
			/* 当前主归属历史 */
			bindingID := platform.ID()
			insertBinding := `INSERT INTO employee_org_binding(tenant_id,id,employee_id,workshop_org_id,line_org_id,team_org_id,is_primary,valid_from,change_reason,created_at,created_by,created_node)
				VALUES($1,$2,$3,$4,$5,NULLIF($6,''),TRUE,$7,'建档',$7,$8,$9)`
			if err := execTwo(ctx, tx, insertBinding, p.TenantID, bindingID, employeeID, in.WorkshopOrgID, in.LineOrgID, in.TeamOrgID, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			/* 登录账号：工人强制 mobile 终端 */
			clientScope := "all"
			if in.PersonType == "worker" {
				clientScope = "mobile"
			}
			if err := execTwo(ctx, tx, `INSERT INTO user_account(tenant_id,id,employee_id,login_name,password_verifier,status,client_scope,password_change_required,created_at,updated_at,created_by,created_node,version)
				VALUES($1,$2,$3,$4,$5,'active',$6,TRUE,$7,$7,$8,$9,1)`,
				p.TenantID, accountID, employeeID, employeeNo, verifier, clientScope, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			/* 角色 */
			for _, roleID := range in.RoleIDs {
				if err := execTwo(ctx, tx, `INSERT INTO user_role(tenant_id,id,user_id,role_id,valid_from,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$5,$5)`,
					p.TenantID, platform.ID(), accountID, roleID, now); err != nil {
					return err
				}
			}
			/* 工序专长（仅工人，多行多选） */
			for _, processID := range in.SkillProcessIDs {
				if err := execTwo(ctx, tx, `INSERT INTO employee_skill(tenant_id,id,employee_id,process_id,created_at,updated_at,created_by,created_node,version)
					VALUES($1,$2,$3,$4,$5,$5,$6,$7,1) ON CONFLICT DO NOTHING`,
					p.TenantID, platform.ID(), employeeID, processID, now, p.UserID, api.Config.NodeID); err != nil {
					return err
				}
			}
			/* 登记为线长 */
			if in.AsLineManager && in.LineOrgID != "" {
				if err := execTwo(ctx, tx, `UPDATE org_unit SET manager_employee_id=$3,updated_at=$4,updated_by=$5,updated_node=$6,version=version+1 WHERE tenant_id=$1 AND id=$2 AND org_type='line'`,
					p.TenantID, in.LineOrgID, employeeID, now, p.UserID, api.Config.NodeID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"employee_id": employeeID, "account_id": accountID, "employee_no": employeeNo,
			"initial_password": in.Password, /* 初始密码仅此一次返回（设计 4.7） */
			"client_scope": map[bool]string{true: "mobile", false: "all"}[in.PersonType == "worker"],
		})
	})
}

func registerOrgBinding(api *httpapi.API) {
	/* 换线/调岗：先插入新 primary，再结束旧 primary，更新 employee 当前归属（设计 5.4 换线事务） */
	api.Handle("PUT /api/v1/people/{id}/org-binding", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			WorkshopOrgID, LineOrgID, TeamOrgID, Reason string
			EffectiveAt                                 *int64
			Version                                     int64
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		employeeID := r.PathValue("id")
		ctx := r.Context()
		if err := validateOrgChain(ctx, api.DB, p.TenantID, in.WorkshopOrgID, in.LineOrgID, in.TeamOrgID); err != nil {
			return err
		}
		var version int64
		var status string
		if err := api.DB.QueryRow(ctx, `SELECT version,status FROM employee WHERE tenant_id=$1 AND id=$2`, p.TenantID, employeeID).Scan(&version, &status); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "人员不存在")
		}
		if in.Version != version {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "人员信息已变更，请刷新后重试")
		}
		if status != "active" {
			return httpapi.Error(http.StatusConflict, "EMPLOYEE_NOT_ACTIVE", "仅在职人员可换线")
		}
		/* 线长换线需先指定继任（简化校验：该员工是原线线长则拒绝） */
		var oldLineID *string
		_ = api.DB.QueryRow(ctx, `SELECT line_org_id FROM employee WHERE tenant_id=$1 AND id=$2`, p.TenantID, employeeID).Scan(&oldLineID)
		if oldLineID != nil && *oldLineID != "" {
			var isManager bool
			_ = api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM org_unit WHERE tenant_id=$1 AND id=$2 AND manager_employee_id=$3)`, p.TenantID, *oldLineID, employeeID).Scan(&isManager)
			if isManager && *oldLineID != in.LineOrgID {
				return httpapi.Error(http.StatusConflict, "LINE_MANAGER_REQUIRED", "该员工是原产线线长，请先指定继任线长再换线")
			}
		}
		now := platform.Now()
		effective := now
		if in.EffectiveAt != nil && *in.EffectiveAt > 0 {
			effective = *in.EffectiveAt
		}
		newBindingID := platform.ID()
		err := withTx(ctx, api, p.TenantID, "employee", employeeID, func(tx *dualwrite.Tx) error {
			/* 结束旧主归属 */
			if err := execTwo(ctx, tx, `UPDATE employee_org_binding SET valid_to=$3 WHERE tenant_id=$1 AND employee_id=$2 AND is_primary=TRUE AND valid_to IS NULL`,
				p.TenantID, employeeID, effective); err != nil {
				return err
			}
			/* 插入新主归属 */
			if err := execTwo(ctx, tx, `INSERT INTO employee_org_binding(tenant_id,id,employee_id,workshop_org_id,line_org_id,team_org_id,is_primary,valid_from,change_reason,created_at,created_by,created_node)
				VALUES($1,$2,$3,$4,$5,NULLIF($6,''),TRUE,$7,NULLIF($8,''),$7,$9,$10)`,
				p.TenantID, newBindingID, employeeID, in.WorkshopOrgID, in.LineOrgID, in.TeamOrgID, effective, in.Reason, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			/* 更新 employee 当前归属 */
			orgUnitID := in.TeamOrgID
			if orgUnitID == "" {
				orgUnitID = in.LineOrgID
			}
			if err := execTwo(ctx, tx, `UPDATE employee SET workshop_org_id=$3,line_org_id=$4,team_org_id=NULLIF($5,''),org_unit_id=NULLIF($6,''),version=version+1,updated_at=$7,updated_by=$8,updated_node=$9
				WHERE tenant_id=$1 AND id=$2`,
				p.TenantID, employeeID, in.WorkshopOrgID, in.LineOrgID, in.TeamOrgID, orgUnitID, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"binding_id": newBindingID, "effective_at": effective})
	})
}

func registerDisable(api *httpapi.API) {
	/* 停用账号并撤销全部会话；不等于删除员工档案（设计 4.7/5.4） */
	api.Handle("POST /api/v1/people/{id}/disable", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		employeeID := r.PathValue("id")
		ctx := r.Context()
		now := platform.Now()
		err := withTx(ctx, api, p.TenantID, "employee", employeeID, func(tx *dualwrite.Tx) error {
			if err := execTwo(ctx, tx, `UPDATE user_account SET status='disabled',version=version+1,updated_at=$3,updated_by=$4,updated_node=$5
				WHERE tenant_id=$1 AND employee_id=$2`, p.TenantID, employeeID, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			if err := execTwo(ctx, tx, `UPDATE auth_session SET revoked_at=$3,reason='account_disabled' WHERE tenant_id=$1 AND user_id IN
				(SELECT id FROM user_account WHERE tenant_id=$1 AND employee_id=$2) AND revoked_at IS NULL`, p.TenantID, employeeID, now); err != nil {
				return err
			}
			if err := execTwo(ctx, tx, `UPDATE employee SET status='terminated',version=version+1,updated_at=$3,updated_by=$4,updated_node=$5
				WHERE tenant_id=$1 AND id=$2`, p.TenantID, employeeID, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			/* 结束当前归属 */
			if err := execTwo(ctx, tx, `UPDATE employee_org_binding SET valid_to=$3 WHERE tenant_id=$1 AND employee_id=$2 AND is_primary=TRUE AND valid_to IS NULL`,
				p.TenantID, employeeID, now); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"disabled": true})
	})
}

/* validateOrgChain 车间→产线→班组层级与状态校验（设计 4.5/5.4）。 */
func validateOrgChain(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, tenantID, workshopID, lineID, teamID string) error {
	if strings.TrimSpace(workshopID) == "" || strings.TrimSpace(lineID) == "" {
		return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "在职人员必须指定车间与产线归属")
	}
	var workshopType, workshopStatus string
	if err := db.QueryRow(ctx, `SELECT org_type,status FROM org_unit WHERE tenant_id=$1 AND id=$2`, tenantID, workshopID).Scan(&workshopType, &workshopStatus); err != nil {
		return httpapi.Error(http.StatusBadRequest, "ORG_INVALID", "车间不存在")
	}
	if workshopType != "workshop" || workshopStatus != "active" {
		return httpapi.Error(http.StatusBadRequest, "ORG_INVALID", "归属车间不存在或已停用")
	}
	var lineParent, lineType, lineStatus string
	if err := db.QueryRow(ctx, `SELECT parent_id,org_type,status FROM org_unit WHERE tenant_id=$1 AND id=$2`, tenantID, lineID).Scan(&lineParent, &lineType, &lineStatus); err != nil {
		return httpapi.Error(http.StatusBadRequest, "ORG_INVALID", "产线不存在")
	}
	if lineType != "line" || lineParent != workshopID {
		return httpapi.Error(http.StatusBadRequest, "ORG_INVALID", "产线必须属于指定车间")
	}
	if lineStatus != "active" {
		return httpapi.Error(http.StatusBadRequest, "ORG_DISABLED", "归属产线已停用")
	}
	if strings.TrimSpace(teamID) != "" {
		var teamParent, teamType string
		if err := db.QueryRow(ctx, `SELECT parent_id,org_type FROM org_unit WHERE tenant_id=$1 AND id=$2`, tenantID, teamID).Scan(&teamParent, &teamType); err != nil {
			return httpapi.Error(http.StatusBadRequest, "ORG_INVALID", "班组不存在")
		}
		if teamType != "team" || teamParent != lineID {
			return httpapi.Error(http.StatusBadRequest, "ORG_INVALID", "班组必须属于指定产线")
		}
	}
	return nil
}

func withTx(ctx context.Context, api *httpapi.API, tenantID, aggregateType, aggregateID string, fn func(tx *dualwrite.Tx) error) error {
	tx, err := api.Writer.Begin(ctx, tenantID)
	if err != nil {
		return err
	}
	tx.MarkAggregate(aggregateType, aggregateID)
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func strOf(v any, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}
