package masterdata

import (
	"fmt"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"clothp/internal/seccrypto"
)

/* createEmployeeSecure 员工建档专用写入：敏感列加密 + phone_hash 盲索引 + 双库写入。
 *
 * 背景事故：旧通用实体路径把明文手机号直接写进密文库且不建盲索引、不写明文库，
 * 短信登录按 phone_hash 盲索引查找永远 miss，厂长手机号登录被当成新用户带进体验注册页。
 * 员工建档必须走本函数；其他无主数据实体仍用通用路径（无敏感列/盲索引要求）。
 */
func createEmployeeSecure(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, definition entity, input map[string]any) error {
	for _, field := range definition.required {
		if strings.TrimSpace(fmt.Sprint(input[field])) == "" || input[field] == nil {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "缺少必填字段："+field)
		}
	}
	id, now := platform.ID(), platform.Now()

	/* 手机号：规范化 + 盲索引 + 本厂查重（phone_hash 唯一性依赖这里维护） */
	phoneHash := ""
	phoneTouched := false
	if v, ok := input["phone"].(string); ok {
		phoneTouched = true
		phone := seccrypto.NormalizePhone(v)
		input["phone"] = phone
		if phone != "" && api.Crypto != nil {
			h, err := api.Crypto.PhoneHash(r.Context(), p.TenantID, "employee.phone", phone)
			if err != nil {
				return err
			}
			phoneHash = h
			var dup bool
			if err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND phone_hash=$2)`, p.TenantID, phoneHash).Scan(&dup); err != nil {
				return err
			}
			if dup {
				return httpapi.Error(http.StatusConflict, "DUPLICATE_PHONE", "该手机号已被本厂其他人员使用")
			}
		}
	}

	columns := []string{"tenant_id", "id", "created_at", "created_by", "created_node", "updated_at", "updated_by", "updated_node"}
	valuesPlain := []any{p.TenantID, id, now, p.UserID, api.Config.NodeID, now, p.UserID, api.Config.NodeID}
	valuesSecure := append([]any{}, valuesPlain...)
	for field, column := range definition.fields {
		if value, ok := input[field]; ok {
			columns = append(columns, column)
			valuesPlain = append(valuesPlain, value)
			valuesSecure = append(valuesSecure, value)
		}
	}
	if phoneTouched {
		columns = append(columns, "phone_hash")
		var hashVal any
		if phoneHash != "" {
			hashVal = phoneHash
		}
		valuesPlain = append(valuesPlain, hashVal)
		valuesSecure = append(valuesSecure, hashVal)
	}
	placeholders := make([]string, len(valuesPlain))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	insertSQL := fmt.Sprintf("INSERT INTO employee(%s) VALUES(%s)", strings.Join(columns, ","), strings.Join(placeholders, ","))

	/* 未配置双写（单库旧部署）：保持旧的单库直写行为 */
	if api.Writer == nil || api.Store == nil || api.Store.Plain == nil || api.Store.Secure == nil {
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if _, err := tx.Exec(r.Context(), insertSQL, valuesPlain...); err != nil {
			return httpapi.Error(http.StatusConflict, "DUPLICATE_OR_INVALID_REFERENCE", "编码重复或引用数据无效")
		}
		if err := syncEmployeeSkills(r.Context(), tx, p.TenantID, id, input["skill_process_ids"]); err != nil {
			return err
		}
		if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create", "employee", id, nil, input, ""); err != nil {
			return err
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "version": 1, "status": "active", "sync_status": "pending"})
	}

	/* 敏感列在密文侧替换为 enc:v1 信封（明文侧保持明文） */
	rc := dualwrite.NewRowCrypto(api.Crypto)
	vals := map[string]any{}
	for i, col := range columns {
		vals[col] = valuesSecure[i]
	}
	enc, err := rc.ForSecure(r.Context(), p.TenantID, "employee", id, vals)
	if err != nil {
		return err
	}
	for i, col := range columns {
		if v, ok := enc[col]; ok {
			valuesSecure[i] = v
		}
	}

	dwtx, err := api.Writer.Begin(r.Context(), p.TenantID)
	if err != nil {
		return err
	}
	defer dwtx.Rollback(r.Context())
	dwtx.MarkAggregate("employee", id)
	if api.Store.WriteMode() != "secure_only" {
		if _, err := dwtx.Plain().Exec(r.Context(), insertSQL, valuesPlain...); err != nil {
			return httpapi.Error(http.StatusConflict, "DUPLICATE_OR_INVALID_REFERENCE", "编码重复或引用数据无效")
		}
	}
	if api.Store.WriteMode() != "plain_only" {
		if _, err := dwtx.Secure().Exec(r.Context(), insertSQL, valuesSecure...); err != nil {
			return httpapi.Error(http.StatusConflict, "DUPLICATE_OR_INVALID_REFERENCE", "编码重复或引用数据无效")
		}
	}
	if dwtx.Plain() != nil {
		if err := syncEmployeeSkills(r.Context(), dwtx.Plain(), p.TenantID, id, input["skill_process_ids"]); err != nil {
			return err
		}
	}
	if dwtx.Secure() != nil {
		if err := syncEmployeeSkills(r.Context(), dwtx.Secure(), p.TenantID, id, input["skill_process_ids"]); err != nil {
			return err
		}
	}
	auditTx := dwtx.Secure()
	if auditTx == nil {
		auditTx = dwtx.Plain()
	}
	if err := httpapi.Audit(r.Context(), auditTx, p, api.Config.NodeID, "create", "employee", id, nil, input, ""); err != nil {
		return err
	}
	if err := dwtx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "version": 1, "status": "active", "sync_status": "pending"})
}
