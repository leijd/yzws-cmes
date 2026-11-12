package person

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/orgapp"
	"clothp/internal/platform"
)

/* registerQuery 人员列表（数据范围 + 筛选 + 默认脱敏）与重置密码（设计 5.4/6.4）。 */
func registerQuery(api *httpapi.API) {
	/* 人员列表：identity:read + 数据范围；手机号默认脱敏，敏感字段不进列表（设计 6.4） */
	api.Handle("GET /api/v1/people", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		access, err := orgapp.ComputeAccess(ctx, api.DB, p)
		if err != nil {
			return err
		}
		q := r.URL.Query()
		conds := " e.tenant_id=$1"
		args := []any{p.TenantID}
		add := func(cond string, v any) {
			conds += fmt.Sprintf(" AND %s=$%d", cond, len(args)+1)
			args = append(args, v)
		}
		if v := strings.TrimSpace(q.Get("workshop_id")); v != "" {
			add("e.workshop_org_id", v)
		}
		if v := strings.TrimSpace(q.Get("line_id")); v != "" {
			add("e.line_org_id", v)
		}
		if v := strings.TrimSpace(q.Get("team_id")); v != "" {
			add("e.team_org_id", v)
		}
		if v := strings.TrimSpace(q.Get("person_type")); v != "" {
			add("e.person_type", v)
		}
		if v := strings.TrimSpace(q.Get("status")); v != "" {
			add("e.status", v)
		}
		if v := strings.TrimSpace(q.Get("role_id")); v != "" {
			conds += fmt.Sprintf(` AND EXISTS(SELECT 1 FROM user_role ur JOIN user_account ua ON ua.tenant_id=ur.tenant_id AND ua.id=ur.user_id
				WHERE ur.tenant_id=e.tenant_id AND ua.employee_id=e.id AND ur.role_id=$%d AND ur.valid_to IS NULL)`, len(args)+1)
			args = append(args, v)
		}
		if v := strings.TrimSpace(q.Get("keyword")); v != "" {
			conds += fmt.Sprintf(" AND (e.employee_no LIKE $%d OR e.name LIKE $%d)", len(args)+1, len(args)+2)
			args = append(args, "%"+v+"%", "%"+v+"%")
		}
		/* 数据范围谓词（设计 5.7 第 5 条） */
		if access.SelfOnly {
			conds += " AND e.id=$" + fmt.Sprint(len(args)+1)
			args = append(args, p.EmployeeID)
		} else if !access.All {
			scopeConds := []string{}
			if len(access.WorkshopIDs) > 0 {
				scopeConds = append(scopeConds, "e.workshop_org_id = ANY($"+fmt.Sprint(len(args)+1)+")")
				args = append(args, access.WorkshopIDs)
			}
			if len(access.LineIDs) > 0 {
				scopeConds = append(scopeConds, "e.line_org_id = ANY($"+fmt.Sprint(len(args)+1)+")")
				args = append(args, access.LineIDs)
			}
			if len(scopeConds) == 0 {
				/* 持 identity:read 但无业务角色（如 tenant_admin/体验厂管理员）：人员列表属于
				 * 身份管理域而非业务数据，不按业务范围拦死（否则建完档列表恒空）；无 identity:read
				 * 的纯业务账号才落入空范围。 */
				if !p.Permissions["identity:read"] {
					conds += " AND FALSE"
				}
			} else {
				conds += " AND (" + strings.Join(scopeConds, " OR ") + ")"
			}
		}
		rows, err := api.DB.Query(ctx, `SELECT e.id,e.employee_no,e.name,e.person_type,e.phone,e.status,e.position,e.remark,e.version,
				e.workshop_org_id,e.line_org_id,e.team_org_id,
				(SELECT w.name FROM org_unit w WHERE w.tenant_id=e.tenant_id AND w.id=e.workshop_org_id) AS workshop_name,
				(SELECT l.name FROM org_unit l WHERE l.tenant_id=e.tenant_id AND l.id=e.line_org_id) AS line_name,
				(SELECT t.name FROM org_unit t WHERE t.tenant_id=e.tenant_id AND t.id=e.team_org_id) AS team_name,
				(SELECT STRING_AGG(r.name,'、') FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
					JOIN user_account ua ON ua.tenant_id=ur.tenant_id AND ua.id=ur.user_id
					WHERE ur.tenant_id=e.tenant_id AND ua.employee_id=e.id AND ur.valid_to IS NULL) AS role_names,
				(SELECT u.status FROM user_account u WHERE u.tenant_id=e.tenant_id AND u.employee_id=e.id LIMIT 1) AS account_status
			FROM employee e WHERE `+conds+` ORDER BY e.employee_no LIMIT 500`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, no, name, ptype, status string
			var phone, position, remark *string
			var workshopID, lineID, teamID, workshopName, lineName, teamName, roleNames, accountStatus *string
			var version int64
			if err := rows.Scan(&id, &no, &name, &ptype, &phone, &status, &position, &remark, &version,
				&workshopID, &lineID, &teamID, &workshopName, &lineName, &teamName, &roleNames, &accountStatus); err != nil {
				return err
			}
			items = append(items, map[string]any{
				"id": id, "employee_no": no, "name": name, "person_type": ptype,
				"phone_masked": maskPhone(strVal(phone)), "status": status, "position": position, "remark": remark, "version": version,
				"workshop_id": workshopID, "line_id": lineID, "team_id": teamID,
				"workshop_name": workshopName, "line_name": lineName, "team_name": teamName,
				"role_names": roleNames, "account_status": accountStatus,
			})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "scope": access.ScopeSummary()})
	})

	/* 重置密码：临时密码仅此一次返回；撤销全部会话（设计 5.4/4.7） */
	api.Handle("POST /api/v1/people/{id}/reset-password", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		employeeID := r.PathValue("id")
		ctx := r.Context()
		var userID string
		if err := api.DB.QueryRow(ctx, `SELECT id FROM user_account WHERE tenant_id=$1 AND employee_id=$2`, p.TenantID, employeeID).Scan(&userID); err != nil {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "该人员没有登录账号")
		}
		temporary := randomPassword()
		verifier, err := auth.HashPassword(temporary)
		if err != nil {
			return err
		}
		now := platform.Now()
		err = withTx(ctx, api, p.TenantID, "employee", employeeID, func(tx *dualwrite.Tx) error {
			if err := execTwo(ctx, tx, `UPDATE user_account SET password_verifier=$3,password_change_required=TRUE,version=version+1,updated_at=$4,updated_by=$5,updated_node=$6
				WHERE tenant_id=$1 AND id=$2`, p.TenantID, userID, verifier, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
			return execTwo(ctx, tx, `UPDATE auth_session SET revoked_at=$3,reason='password_reset' WHERE tenant_id=$1 AND user_id=$2 AND revoked_at IS NULL`, p.TenantID, userID, now)
		})
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"temporary_password": temporary})
	})
}

func strVal(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

/* maskPhone 手机号脱敏（列表默认，设计 6.4）。 */
func maskPhone(phone string) string {
	if len(phone) == 11 {
		return phone[:3] + "****" + phone[7:]
	}
	return phone
}

/* randomPassword 生成 12 位临时密码（字母+数字）。 */
func randomPassword() string {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
	out := make([]byte, 12)
	for i := range out {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		out[i] = alphabet[n.Int64()]
	}
	return string(out)
}
