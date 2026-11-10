package platformadmin

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
	"time"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"clothp/internal/seccrypto"
)

/* phoneLookupLimiter 按平台账号限流：60 秒窗口最多 10 次查询（设计 5.6.3 限流要求）。 */
type phoneLookupLimiter struct {
	mu       sync.Mutex
	window   map[string][]time.Time
	max      int
	windowDur time.Duration
}

func newPhoneLookupLimiter() *phoneLookupLimiter {
	return &phoneLookupLimiter{window: map[string][]time.Time{}, max: 10, windowDur: time.Minute}
}

func (l *phoneLookupLimiter) allow(actorID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-l.windowDur)
	kept := l.window[actorID][:0]
	for _, t := range l.window[actorID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		l.window[actorID] = kept
		return false
	}
	l.window[actorID] = append(kept, now)
	return true
}

var phoneLookupRate = newPhoneLookupLimiter()

/* registerPhoneLookup 平台侧手机号归属受控查询（明文库精确匹配；不返回完整手机号；全量平台审计）。 */
func registerPhoneLookup(api *httpapi.API) {
	api.PlatformHandle("POST /api/v1/platform/phone-lookup", func(w http.ResponseWriter, r *http.Request, p auth.PlatformPrincipal) error {
		if !phoneLookupRate.allow(p.AdminID) {
			return httpapi.Error(http.StatusTooManyRequests, "RATE_LIMITED", "查询过于频繁，请一分钟后再试")
		}
		var in struct {
			Phone string `json:"phone"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		phone := seccrypto.NormalizePhone(in.Phone)
		if !phoneLookupPattern.MatchString(phone) {
			return httpapi.Error(http.StatusBadRequest, "INVALID_PHONE", "请输入正确的 11 位手机号")
		}
		/* 查明文库（明文库）：平台侧存在性验证，返回归属而非 PII 明文 */
		db := api.Store.Plain
		type match struct {
			Kind       string `json:"kind"`
			TenantCode string `json:"tenant_code"`
			TenantName string `json:"tenant_name"`
			EmployeeNo string `json:"employee_no,omitempty"`
			Name       string `json:"name,omitempty"`
			PersonType string `json:"person_type,omitempty"`
			Status     string `json:"status,omitempty"`
			Role       string `json:"role,omitempty"`
		}
		matches := []match{}
		/* 1) 员工档案手机号（含租户管理员添加的员工，任何身份） */
		rows, err := db.Query(r.Context(), `SELECT t.code,t.name,e.employee_no,e.name,e.person_type,e.status
			FROM employee e JOIN tenant t ON t.id=e.tenant_id WHERE e.phone=$1 ORDER BY t.code`, phone)
		if err != nil {
			return err
		}
		for rows.Next() {
			var m match
			m.Kind = "employee"
			if err := rows.Scan(&m.TenantCode, &m.TenantName, &m.EmployeeNo, &m.Name, &m.PersonType, &m.Status); err != nil {
				rows.Close()
				return err
			}
			matches = append(matches, m)
		}
		rows.Close()
		/* 2) 厂注册联系电话（体验厂管理员 / 付费厂联系人） */
		trows, err := db.Query(r.Context(), `SELECT t.code,t.name,t.customer_type,
			COALESCE((SELECT r.name FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id
				JOIN user_account u ON u.tenant_id=ur.tenant_id AND u.id=ur.user_id
				WHERE u.tenant_id=t.id AND u.employee_id=t.admin_employee_id AND r.code='tenant_admin' LIMIT 1),'')
			FROM tenant t WHERE t.contact_phone=$1 ORDER BY t.code`, phone)
		if err != nil {
			return err
		}
		for trows.Next() {
			var code, name, ctype, adminRole string
			if err := trows.Scan(&code, &name, &ctype, &adminRole); err != nil {
				trows.Close()
				return err
			}
			role := "注册联系电话"
			if ctype == "trial" {
				role = "体验厂管理员手机号"
			}
			matches = append(matches, match{Kind: "tenant_contact", TenantCode: code, TenantName: name, Role: role})
		}
		trows.Close()
		/* 强制平台审计（设计 7.3：查看敏感标识必须留痕；不记录完整号码） */
		auditBody, _ := json.Marshal(map[string]any{
			"phone_masked": maskPhoneLookup(phone), "hits": len(matches),
			"result": map[bool]string{true: "found", false: "not_found"}[len(matches) > 0],
		})
		_, _ = db.Exec(r.Context(), `INSERT INTO platform_audit_log(id,request_id,actor_id,action,owner_type,owner_id,after_values,reason,created_at)
			VALUES($1,$2,$3,'phone_lookup','phone',$4,$5,'平台手机号归属核实',$6)`,
			"paudit-"+platform.ID(), httpapi.RequestID(r.Context()), p.AdminID, maskPhoneLookup(phone), auditBody, platform.Now())
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"phone_masked": maskPhoneLookup(phone),
			"found":        len(matches) > 0,
			"matches":      matches,
		})
	})
}

var phoneLookupPattern = regexp.MustCompile(`^1[3-9]\d{9}$`)

func maskPhoneLookup(phone string) string {
	if len(phone) == 11 {
		return phone[:3] + "****" + phone[9:]
	}
	return "***"
}
