package masterdata

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"clothp/internal/auth"
	"clothp/internal/codegen"
	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"clothp/internal/seccrypto"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

/* isUniqueViolation PostgreSQL 唯一约束冲突（23505）。 */
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

type entity struct {
	table, permission, codeField string
	fields                       map[string]string
	required                     []string
}

var entities = map[string]entity{
	"customers":  {table: "customer", permission: "masterdata", codeField: "code", required: []string{"code", "name"}, fields: map[string]string{"code": "code", "name": "name", "contact_name": "contact_name", "phone": "phone", "address": "address", "remark": "remark"}},
	"suppliers":  {table: "supplier", permission: "masterdata", codeField: "code", required: []string{"code", "name"}, fields: map[string]string{"code": "code", "name": "name", "contact_name": "contact_name", "phone": "phone", "address": "address", "remark": "remark"}},
	"materials":  {table: "material", permission: "masterdata", codeField: "code", required: []string{"code", "name", "unit"}, fields: map[string]string{"code": "code", "name": "name", "category": "category", "specification": "specification", "unit": "unit", "default_supplier_id": "default_supplier_id", "safety_stock": "safety_stock"}},
	"warehouses": {table: "warehouse", permission: "masterdata", codeField: "code", required: []string{"code", "name", "warehouse_type"}, fields: map[string]string{"code": "code", "name": "name", "warehouse_type": "warehouse_type", "org_unit_id": "org_unit_id", "manager_employee_id": "manager_employee_id"}},
	"org-units":  {table: "org_unit", permission: "masterdata", codeField: "code", required: []string{"code", "name", "org_type"}, fields: map[string]string{"code": "code", "name": "name", "org_type": "org_type", "parent_id": "parent_id", "manager_employee_id": "manager_employee_id"}},
	"employees":  {table: "employee", permission: "masterdata", codeField: "employee_no", required: []string{"employee_no", "name"}, fields: map[string]string{"employee_no": "employee_no", "name": "name", "gender": "gender", "birth_date": "birth_date", "phone": "phone", "id_card_no": "id_card_no", "address": "address", "nation": "nation", "native_place": "native_place", "hire_date": "hire_date", "org_unit_id": "org_unit_id", "position": "position", "workshop": "workshop", "production_line": "production_line", "skill_grade": "skill_grade", "pay_mode": "pay_mode", "base_salary": "base_salary", "rate_level": "rate_level", "bank_card": "bank_card", "bank_name": "bank_name", "emergency_contact": "emergency_contact", "emergency_phone": "emergency_phone", "remark": "remark"}},
}

func Register(api *httpapi.API) {
	for path, definition := range entities {
		registerEntity(api, path, definition)
	}
	registerSensitiveView(api)
	api.Handle("GET /api/v1/employees/next-no", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if !p.Permissions["masterdata:write"] && !p.Permissions["identity:write"] {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "没有执行此操作的权限")
		}
		employeeNo, err := NextEmployeeNo(r.Context(), api, p.TenantID)
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"employee_no": employeeNo})
	})
	registerDictionaries(api)
	registerMyProfile(api)
}

func registerEntity(api *httpapi.API, path string, definition entity) {
	base := "/api/v1/" + path
	readPermission := definition.permission + ":read"
	createPermission := definition.permission + ":write"
	patchPermission := definition.permission + ":write"
	if path == "employees" {
		// 员工页同时对“本人”开放：列表只返回本人，修改按字段范围校验。
		// 租户管理员（identity:write）也可新建员工并开通账号。
		readPermission = ""
		createPermission = ""
		patchPermission = ""
	}
	api.Handle("GET "+base, readPermission, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if path == "employees" {
			return listEmployees(api, w, r, p)
		}
		return list(api, w, r, p, definition)
	})
	api.Handle("POST "+base, createPermission, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if path == "employees" {
			if !p.Permissions["masterdata:write"] && !p.Permissions["identity:write"] {
				return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "没有执行此操作的权限")
			}
			return createEmployee(api, w, r, p, definition)
		}
		return create(api, w, r, p, definition)
	})
	api.Handle("GET "+base+"/{id}", readPermission, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if path == "employees" {
			return getEmployee(api, w, r, p)
		}
		return get(api, w, r, p, definition)
	})
	api.Handle("PATCH "+base+"/{id}", patchPermission, func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if path == "employees" {
			return patchEmployee(api, w, r, p, definition)
		}
		return patch(api, w, r, p, definition)
	})
	api.Handle("POST "+base+"/{id}/disable", definition.permission+":write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var input struct {
			Version int64  `json:"version"`
			Reason  string `json:"reason"`
		}
		if err := httpapi.Decode(r, &input); err != nil {
			return err
		}
		result, err := api.DB.Exec(r.Context(), `UPDATE `+definition.table+` SET status='disabled',version=version+1,updated_at=$1,updated_by=$2,updated_node=$3
			WHERE tenant_id=$4 AND id=$5 AND version=$6`, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), input.Version)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "记录已变更，请刷新后重试")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "status": "disabled", "version": input.Version + 1})
	})
}

func list(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, definition entity) error {
	limit, cursor := httpapi.Page(r)
	where := "tenant_id=$1"
	args := []any{p.TenantID}
	if cursor != "" {
		args = append(args, cursor)
		where += fmt.Sprintf(" AND id>$%d", len(args))
	}
	if status := r.URL.Query().Get("status"); status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND status=$%d", len(args))
	}
	if keyword := strings.TrimSpace(r.URL.Query().Get("keyword")); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where += fmt.Sprintf(" AND (name ILIKE $%d OR %s ILIKE $%d)", len(args), definition.codeField, len(args))
	}
	args = append(args, limit+1)
	query := fmt.Sprintf(`SELECT row_to_json(t) FROM (SELECT * FROM %s WHERE %s ORDER BY id LIMIT $%d) t`, definition.table, where, len(args))
	rows, err := api.DB.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var item map[string]any
		if err := rows.Scan(&item); err != nil {
			return err
		}
		items = append(items, item)
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var next any
	if hasMore {
		next = items[len(items)-1]["id"]
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next, "has_more": hasMore})
}

func create(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, definition entity) error {
	var input map[string]any
	if err := httpapi.Decode(r, &input); err != nil {
		return err
	}
	return createWithInput(api, w, r, p, definition, input)
}

// employeeNoPrefix 取租户编码中最多 5 位字母或数字作为工号前缀。
func employeeNoPrefix(tenantCode string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(tenantCode) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			if b.Len() == 5 {
				break
			}
		}
	}
	if b.Len() == 0 {
		return "T"
	}
	return b.String()
}

// prefixFromName 取租户名称首字母（中文按拼音），不足 5 位补 0。
func prefixFromName(name, code string) string {
	return platform.TenantShortCodeBase(name, code)
}

// uniquePrefix 标识冲突时用数字或字母区分。
func uniquePrefix(base string, used map[string]bool) string {
	return platform.UniqueShortCode(base, used)
}

// tenantPrefix 按租户创建顺序稳定地为每个租户分配唯一标识。
func tenantPrefix(ctx context.Context, api *httpapi.API, tenantID string) (string, error) {
	var shortCode string
	if err := api.DB.QueryRow(ctx, `SELECT COALESCE(short_code,'') FROM tenant WHERE id=$1`, tenantID).Scan(&shortCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", httpapi.Error(http.StatusNotFound, "TENANT_NOT_FOUND", "租户不存在")
		}
		return "", err
	}
	if shortCode != "" {
		return shortCode, nil
	}
	return derivedTenantPrefix(ctx, api, tenantID)
}

// derivedTenantPrefix 在租户标识缺失时按名称首字母/编码推导，作为兜底。
func derivedTenantPrefix(ctx context.Context, api *httpapi.API, tenantID string) (string, error) {
	rows, err := api.DB.Query(ctx, `SELECT id,code,COALESCE(name,'') FROM tenant ORDER BY created_at,id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	used := map[string]bool{}
	prefix := ""
	for rows.Next() {
		var id, code, name string
		if err := rows.Scan(&id, &code, &name); err != nil {
			return "", err
		}
		candidate := uniquePrefix(prefixFromName(name, code), used)
		used[candidate] = true
		if id == tenantID {
			prefix = candidate
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if prefix == "" {
		return "", httpapi.Error(http.StatusNotFound, "TENANT_NOT_FOUND", "租户不存在")
	}
	return prefix, nil
}

/* NextEmployeeNo 生成 {租户标识}-{5 位流水号} 工号，序号按前缀（即租户）递增。
 * 导出供 person 建档（employee_no 留空时自动生成）等场景复用。 */
func NextEmployeeNo(ctx context.Context, api *httpapi.API, tenantID string) (string, error) {
	prefix, err := tenantPrefix(ctx, api, tenantID)
	if err != nil {
		return "", err
	}
	var seq int64
	if err := api.DB.QueryRow(ctx, `SELECT COALESCE(MAX((regexp_match(employee_no, '-([0-9]{1,6})$'))[1]::bigint),0)+1 FROM employee WHERE upper(employee_no) LIKE $1 || '-%'`, prefix).Scan(&seq); err != nil {
		return "", err
	}
	if seq > 99999 {
		return "", httpapi.Error(http.StatusConflict, "EMPLOYEE_NO_EXHAUSTED", "该租户工号序号已用尽")
	}
	return fmt.Sprintf("%s-%05d", prefix, seq), nil
}

func employeeNoTaken(ctx context.Context, api *httpapi.API, employeeNo, exceptID string) (bool, error) {
	var exists bool
	err := api.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM employee WHERE upper(employee_no)=upper($1) AND ($2='' OR id<>$2))`, employeeNo, exceptID).Scan(&exists)
	return exists, err
}

func createEmployee(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, definition entity) error {
	var input map[string]any
	if err := httpapi.Decode(r, &input); err != nil {
		return err
	}
	employeeNo, _ := input["employee_no"].(string)
	employeeNo = strings.TrimSpace(employeeNo)
	if employeeNo == "" {
		generated, err := NextEmployeeNo(r.Context(), api, p.TenantID)
		if err != nil {
			return err
		}
		employeeNo = generated
	}
	taken, err := employeeNoTaken(r.Context(), api, employeeNo, "")
	if err != nil {
		return err
	}
	if taken {
		return httpapi.Error(http.StatusConflict, "DUPLICATE_EMPLOYEE_NO", "工号已存在（全库唯一）")
	}
	input["employee_no"] = employeeNo
	return createWithInput(api, w, r, p, definition, input)
}

/* codeOwnerFor 通用主数据实体的自动编码类型：编码留空时按 {类型名拼音首字母}-{5位流水} 生成。
 * 工号（employee_no）有设计专门规则（{租户标识}-流水），不在此列。 */
func codeOwnerFor(definition entity, input map[string]any) string {
	switch definition.table {
	case "customer":
		return "masterdata.customer"
	case "supplier":
		return "masterdata.supplier"
	case "warehouse":
		return "masterdata.warehouse"
	case "material":
		return "masterdata.material"
	case "org_unit":
		switch orgType, _ := input["org_type"].(string); orgType {
		case "workshop":
			return "org.workshop"
		case "line":
			return "org.line"
		case "team":
			return "org.team"
		}
	}
	return ""
}

func createWithInput(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, definition entity, input map[string]any) error {
	if definition.table == "employee" {
		/* 员工表走专用写入：敏感列加密 + phone_hash 盲索引 + 双库写入（见 employee_write.go） */
		return createEmployeeSecure(api, w, r, p, definition, input)
	}
	if definition.codeField != "" {
		if owner := codeOwnerFor(definition, input); owner != "" {
			if v, _ := input[definition.codeField].(string); strings.TrimSpace(v) == "" {
				code, err := codegen.Generate(r.Context(), api.DB, api.Writer, definition.table, definition.fields[definition.codeField], p.TenantID, owner)
				if err != nil {
					return err
				}
				input[definition.codeField] = code
			}
		}
	}
	for _, field := range definition.required {
		if strings.TrimSpace(fmt.Sprint(input[field])) == "" || input[field] == nil {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "缺少必填字段："+field)
		}
	}
	id, now := platform.ID(), platform.Now()
	columns := []string{"tenant_id", "id", "created_at", "created_by", "created_node", "updated_at", "updated_by", "updated_node"}
	values := []any{p.TenantID, id, now, p.UserID, api.Config.NodeID, now, p.UserID, api.Config.NodeID}
	for field, column := range definition.fields {
		if value, ok := input[field]; ok {
			columns = append(columns, column)
			values = append(values, value)
		}
	}
	if definition.table == "org_unit" {
		columns = append(columns, "path")
		values = append(values, id+"/")
	}
	placeholders := make([]string, len(values))
	for i := range values {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	_, err = tx.Exec(r.Context(), fmt.Sprintf("INSERT INTO %s(%s) VALUES(%s)", definition.table, strings.Join(columns, ","), strings.Join(placeholders, ",")), values...)
	if err != nil {
		return httpapi.Error(http.StatusConflict, "DUPLICATE_OR_INVALID_REFERENCE", "编码重复或引用数据无效")
	}
	if err = httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "create", definition.table, id, nil, input, ""); err != nil {
		return err
	}
	if definition.table == "employee" {
		if err := syncEmployeeSkills(r.Context(), tx, p.TenantID, id, input["skill_process_ids"]); err != nil {
			return err
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "version": 1, "status": "active", "sync_status": "pending"})
}

func get(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, definition entity) error {
	var item map[string]any
	err := api.DB.QueryRow(r.Context(), fmt.Sprintf("SELECT row_to_json(t) FROM (SELECT * FROM %s WHERE tenant_id=$1 AND id=$2) t", definition.table), p.TenantID, r.PathValue("id")).Scan(&item)
	if err == pgx.ErrNoRows {
		return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "记录不存在")
	}
	if err != nil {
		return err
	}
	delete(item, "id_card_no")
	delete(item, "bank_card")
	delete(item, "bank_name")
	delete(item, "address")
	return httpapi.JSON(w, http.StatusOK, item)
}

func patch(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, definition entity) error {
	var input map[string]any
	if err := httpapi.Decode(r, &input); err != nil {
		return err
	}
	version, ok := input["version"].(float64)
	if !ok {
		return httpapi.Error(http.StatusBadRequest, "VERSION_REQUIRED", "必须提供记录版本")
	}
	sets, args := []string{}, []any{}
	for field, column := range definition.fields {
		if value, exists := input[field]; exists {
			args = append(args, value)
			sets = append(sets, fmt.Sprintf("%s=$%d", column, len(args)))
		}
	}
	if len(sets) == 0 {
		return httpapi.Error(http.StatusBadRequest, "NO_CHANGES", "没有可保存的字段")
	}
	args = append(args, platform.Now(), p.UserID, api.Config.NodeID)
	sets = append(sets, fmt.Sprintf("updated_at=$%d", len(args)-2), fmt.Sprintf("updated_by=$%d", len(args)-1), fmt.Sprintf("updated_node=$%d", len(args)), "version=version+1")
	args = append(args, p.TenantID, r.PathValue("id"), int64(version))
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	result, err := tx.Exec(r.Context(), fmt.Sprintf("UPDATE %s SET %s WHERE tenant_id=$%d AND id=$%d AND version=$%d", definition.table, strings.Join(sets, ","), len(args)-2, len(args)-1, len(args)), args...)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "记录不存在或已变更")
	}
	if definition.table == "employee" {
		if err := syncEmployeeSkills(r.Context(), tx, p.TenantID, r.PathValue("id"), input["skill_process_ids"]); err != nil {
			return err
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "version": int64(version) + 1, "status": "active"})
}

// patchEmployee 按角色控制可改字段：
// 租户管理员（identity:write）可改全部；员工管理者（masterdata:write）可改除工号外全部；
// 本人只能改私有信息。
func patchEmployee(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, definition entity) error {
	var input map[string]any
	if err := httpapi.Decode(r, &input); err != nil {
		return err
	}
	employeeID := r.PathValue("id")
	self := p.EmployeeID != "" && employeeID == p.EmployeeID
	canManage := p.Permissions["masterdata:write"]
	canAdmin := p.Permissions["identity:write"]
	if !canAdmin && !canManage && !self {
		return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "没有执行此操作的权限")
	}
	allowed := map[string]string{}
	switch {
	case canAdmin:
		for field, column := range definition.fields {
			allowed[field] = column
		}
	case canManage:
		for field, column := range definition.fields {
			// 工号、姓名属于管理员维护的主要信息，员工管理者只能补充其他资料。
			if field == "employee_no" || field == "name" {
				continue
			}
			allowed[field] = column
		}
	default:
		for field, column := range selfEditableFields {
			allowed[field] = column
		}
	}
	if canAdmin {
		if employeeNo, exists := input["employee_no"].(string); exists {
			employeeNo = strings.TrimSpace(employeeNo)
			if employeeNo == "" {
				return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "工号不能为空")
			}
			taken, err := employeeNoTaken(r.Context(), api, employeeNo, employeeID)
			if err != nil {
				return err
			}
			if taken {
				return httpapi.Error(http.StatusConflict, "DUPLICATE_EMPLOYEE_NO", "工号已存在（全库唯一）")
			}
			input["employee_no"] = employeeNo
		}
	}
	version, ok := input["version"].(float64)
	if !ok {
		return httpapi.Error(http.StatusBadRequest, "VERSION_REQUIRED", "必须提供记录版本")
	}
	if v, ok := input["phone"].(string); ok && v != "" && !regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(v) {
		return httpapi.Error(http.StatusBadRequest, "INVALID_PHONE", "联系电话须为 11 位手机号")
	}
	if v, ok := input["id_card_no"].(string); ok && v != "" && !regexp.MustCompile(`^\d{17}[\dXx]$`).MatchString(v) {
		return httpapi.Error(http.StatusBadRequest, "INVALID_ID_CARD", "身份证号须为 18 位")
	}
	/* 手机号：规范化 + 盲索引维护（密文库读模式下短信登录按 phone_hash 找人，必须同步维护） */
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
			if err := api.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM employee WHERE tenant_id=$1 AND phone_hash=$2 AND id<>$3)`, p.TenantID, phoneHash, employeeID).Scan(&dup); err != nil {
				return err
			}
			if dup {
				return httpapi.Error(http.StatusConflict, "DUPLICATE_PHONE", "该手机号已被本厂其他人员使用")
			}
		}
	}
	/* 敏感列：密文侧值加密（enc:v1），明文侧保持明文；phone_hash 两侧一致 */
	secureValues := map[string]any{}
	if api.Crypto != nil && api.Store != nil && api.Store.Secure != nil {
		rc := dualwrite.NewRowCrypto(api.Crypto)
		vals := map[string]any{}
		for field, column := range allowed {
			if value, exists := input[field]; exists {
				vals[column] = value
			}
		}
		enc, err := rc.ForSecure(r.Context(), p.TenantID, "employee", employeeID, vals)
		if err != nil {
			return err
		}
		secureValues = enc
	}
	sets, argsSecure, argsPlain := []string{}, []any{}, []any{}
	for field, column := range allowed {
		if value, exists := input[field]; exists {
			argsPlain = append(argsPlain, value)
			if sv, ok := secureValues[column]; ok {
				argsSecure = append(argsSecure, sv)
			} else {
				argsSecure = append(argsSecure, value)
			}
			sets = append(sets, fmt.Sprintf("%s=$%d", column, len(argsPlain)))
		}
	}
	if phoneTouched {
		argsPlain = append(argsPlain, nilIfEmpty(phoneHash))
		argsSecure = append(argsSecure, nilIfEmpty(phoneHash))
		sets = append(sets, fmt.Sprintf("phone_hash=$%d", len(argsPlain)))
	}
	if len(sets) == 0 {
		return httpapi.Error(http.StatusBadRequest, "NO_CHANGES", "没有可保存的字段")
	}
	argsSecure = append(argsSecure, platform.Now(), p.UserID, api.Config.NodeID)
	argsPlain = append(argsPlain, platform.Now(), p.UserID, api.Config.NodeID)
	sets = append(sets, fmt.Sprintf("updated_at=$%d", len(argsSecure)-2), fmt.Sprintf("updated_by=$%d", len(argsSecure)-1), fmt.Sprintf("updated_node=$%d", len(argsSecure)), "version=version+1")
	argsSecure = append(argsSecure, p.TenantID, employeeID, int64(version))
	argsPlain = append(argsPlain, p.TenantID, employeeID, int64(version))
	updateSQL := fmt.Sprintf("UPDATE employee SET %s WHERE tenant_id=$%d AND id=$%d AND version=$%d", strings.Join(sets, ","), len(argsSecure)-2, len(argsSecure)-1, len(argsSecure))
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	result, err := tx.Exec(r.Context(), updateSQL, argsSecure...)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "记录不存在或已变更")
	}
	if canAdmin || canManage {
		if err := syncEmployeeSkills(r.Context(), tx, p.TenantID, employeeID, input["skill_process_ids"]); err != nil {
			return err
		}
	}
	if err := httpapi.Audit(r.Context(), tx, p, api.Config.NodeID, "update", "employee", employeeID, nil, input, ""); err != nil {
		return err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return err
	}
	/* 明文库镜像（读主库为密文库；行缺失/落后由对账补偿兜底，这里尽力同步） */
	if api.Store != nil && api.Store.Plain != nil {
		if _, err := api.Store.Plain.Exec(r.Context(), updateSQL, argsPlain...); err != nil {
			log.Printf("masterdata: mirror employee update to plain db: %v", err)
		}
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"id": employeeID, "version": int64(version) + 1})
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

/* ===== 下拉选项缓存：字典项是读多写少的参照数据，60 秒内存缓存（需求：选项从数据库实际
 * 数据获取，也支持缓存获取）；写操作按 租户+类型 精确失效。 ===== */
var (
	dictOptionsMu    sync.RWMutex
	dictOptionsCache = map[string]struct {
		at    time.Time
		items []map[string]any
	}{}
)

const dictOptionsTTL = 60 * time.Second

func dictOptionsInvalidate(tenantID, dictType string) {
	dictOptionsMu.Lock()
	delete(dictOptionsCache, tenantID+"/"+dictType)
	dictOptionsMu.Unlock()
}

/* dictOptionsInvalidateTenant 失效该租户全部字典选项缓存（按 id 修改、不带类型路径时用）。 */
func dictOptionsInvalidateTenant(tenantID string) {
	dictOptionsMu.Lock()
	for key := range dictOptionsCache {
		if strings.HasPrefix(key, tenantID+"/") {
			delete(dictOptionsCache, key)
		}
	}
	dictOptionsMu.Unlock()
}

func registerDictionaries(api *httpapi.API) {
	/* 下拉选项接口：任何登录的本厂用户可读（参照数据，无敏感字段；员工本人资料页也要用） */
	api.Handle("GET /api/v1/dictionaries/{type}/options", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		key := p.TenantID + "/" + r.PathValue("type")
		dictOptionsMu.RLock()
		entry, hit := dictOptionsCache[key]
		dictOptionsMu.RUnlock()
		if hit && time.Since(entry.at) < dictOptionsTTL {
			return httpapi.JSON(w, http.StatusOK, map[string]any{"items": entry.items})
		}
		rows, err := api.DB.Query(r.Context(), `SELECT json_build_object('code',code,'name',name) FROM dictionary_item
			WHERE tenant_id=$1 AND dictionary_type=$2 AND status='active' ORDER BY sort_no,code`, p.TenantID, r.PathValue("type"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var item map[string]any
			if err := rows.Scan(&item); err != nil {
				return err
			}
			items = append(items, item)
		}
		dictOptionsMu.Lock()
		dictOptionsCache[key] = struct {
			at    time.Time
			items []map[string]any
		}{at: time.Now(), items: items}
		dictOptionsMu.Unlock()
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
	/* 字典类型清单（维护页侧边栏）：本厂已有字典类型与项数 */
	api.Handle("GET /api/v1/dictionaries/types", "masterdata:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT dictionary_type,COUNT(*),COUNT(*) FILTER (WHERE status='active') FROM dictionary_item
			WHERE tenant_id=$1 GROUP BY dictionary_type ORDER BY dictionary_type`, p.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var dictType string
			var total, active int64
			if err := rows.Scan(&dictType, &total, &active); err != nil {
				return err
			}
			items = append(items, map[string]any{"type": dictType, "total": total, "active": active})
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
	api.Handle("GET /api/v1/dictionaries/{type}/items", "masterdata:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT json_build_object('id',id,'code',code,'name',name,'parent_id',parent_id,'sort_no',sort_no,'status',status,'version',version)
			FROM dictionary_item WHERE tenant_id=$1 AND dictionary_type=$2 ORDER BY sort_no,id`, p.TenantID, r.PathValue("type"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var item map[string]any
			if err := rows.Scan(&item); err != nil {
				return err
			}
			items = append(items, item)
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
	api.Handle("POST /api/v1/dictionaries/{type}/items", "masterdata:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var input struct {
			Code     string  `json:"code"`
			Name     string  `json:"name"`
			ParentID *string `json:"parent_id"`
			SortNo   int     `json:"sort_no"`
		}
		if err := httpapi.Decode(r, &input); err != nil {
			return err
		}
		if input.Name == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "名称必填")
		}
		if input.Code == "" {
			/* 编码留空自动生成：{类型名拼音首字母}-{5位流水}（字典项→ZD-00001） */
			code, err := codegen.Generate(r.Context(), api.DB, api.Writer, "dictionary_item", "code", p.TenantID, "masterdata.dictionary")
			if err != nil {
				return err
			}
			input.Code = code
		}
		id, now := platform.ID(), platform.Now()
		insertSQL := `INSERT INTO dictionary_item(tenant_id,id,dictionary_type,code,name,parent_id,sort_no,created_at,created_by,created_node,updated_at,updated_by,updated_node)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$8,$9,$10)`
		args := []any{p.TenantID, id, r.PathValue("type"), input.Code, input.Name, input.ParentID, input.SortNo, now, p.UserID, api.Config.NodeID}
		/* 字典数据存双库：双写事务两库各执行一次；单库模式保持旧行为 */
		if api.Writer != nil && api.Store != nil && api.Store.Plain != nil && api.Store.Secure != nil {
			tx, err := api.Writer.Begin(r.Context(), p.TenantID)
			if err != nil {
				return err
			}
			defer tx.Rollback(r.Context())
			tx.MarkAggregate("dictionary_item", id)
			if err := dualwrite.ExecBoth(r.Context(), tx, insertSQL, args...); err != nil {
				if isUniqueViolation(err) {
					return httpapi.Error(http.StatusConflict, "DUPLICATE_CODE", "字典编码已存在")
				}
				return err
			}
			auditTx := tx.Secure()
			if auditTx == nil {
				auditTx = tx.Plain()
			}
			if err := httpapi.Audit(r.Context(), auditTx, p, api.Config.NodeID, "create", "dictionary_item", id, nil,
				map[string]any{"dictionary_type": r.PathValue("type"), "code": input.Code, "name": input.Name}, ""); err != nil {
				return err
			}
			if err := tx.Commit(r.Context()); err != nil {
				return err
			}
		} else {
			if _, err := api.DB.Exec(r.Context(), insertSQL, args...); err != nil {
				return httpapi.Error(http.StatusConflict, "DUPLICATE_CODE", "字典编码已存在")
			}
		}
		dictOptionsInvalidate(p.TenantID, r.PathValue("type"))
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "code": input.Code, "version": 1, "status": "active"})
	})
	api.Handle("PATCH /api/v1/dictionaries/items/{id}", "masterdata:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var input struct {
			Version int64  `json:"version"`
			Name    string `json:"name"`
			SortNo  int    `json:"sort_no"`
			Status  string `json:"status"`
		}
		if err := httpapi.Decode(r, &input); err != nil {
			return err
		}
		updateSQL := `UPDATE dictionary_item SET name=$1,sort_no=$2,status=$3,version=version+1,updated_at=$4,updated_by=$5,updated_node=$6
			WHERE tenant_id=$7 AND id=$8 AND version=$9`
		args := []any{input.Name, input.SortNo, input.Status, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), input.Version}
		if api.Writer != nil && api.Store != nil && api.Store.Plain != nil && api.Store.Secure != nil {
			/* 字典数据存双库：主写库做乐观锁判断，另一库镜像同一 UPDATE */
			tx, err := api.Writer.Begin(r.Context(), p.TenantID)
			if err != nil {
				return err
			}
			defer tx.Rollback(r.Context())
			tx.MarkAggregate("dictionary_item", r.PathValue("id"))
			primary := tx.Secure()
			mirror := tx.Plain()
			if primary == nil {
				primary, mirror = tx.Plain(), nil
			}
			result, err := primary.Exec(r.Context(), updateSQL, args...)
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "记录已变更")
			}
			if mirror != nil {
				if _, err := mirror.Exec(r.Context(), updateSQL, args...); err != nil {
					return err
				}
			}
			auditTx := tx.Secure()
			if auditTx == nil {
				auditTx = tx.Plain()
			}
			if err := httpapi.Audit(r.Context(), auditTx, p, api.Config.NodeID, "update", "dictionary_item", r.PathValue("id"), nil,
				map[string]any{"name": input.Name, "sort_no": input.SortNo, "status": input.Status}, ""); err != nil {
				return err
			}
			if err := tx.Commit(r.Context()); err != nil {
				return err
			}
		} else {
			result, err := api.DB.Exec(r.Context(), updateSQL, args...)
			if err != nil {
				return err
			}
			if result.RowsAffected() == 0 {
				return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "记录已变更")
			}
		}
		dictOptionsInvalidateTenant(p.TenantID)
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": r.PathValue("id"), "version": input.Version + 1})
	})
}

var _ = json.Marshal

func maskPhone(value string) string {
	if len(value) == 11 {
		return value[:3] + "****" + value[7:]
	}
	if len(value) > 7 {
		return value[:3] + "****" + value[len(value)-4:]
	}
	return value
}

func maskIDCard(value string) string {
	if len(value) >= 14 {
		return value[:6] + "********" + value[len(value)-4:]
	}
	return value
}

func maskBankCard(value string) string {
	if len(value) > 4 {
		return "**** **** **** " + value[len(value)-4:]
	}
	return value
}

func listEmployees(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	selfOnly := !(p.Permissions["masterdata:read"] || p.Permissions["identity:read"] || p.Permissions["identity:write"])
	if selfOnly && p.EmployeeID == "" {
		return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "没有执行此操作的权限")
	}
	query := `SELECT e.id,e.employee_no,e.name,e.gender,e.position,e.workshop,e.production_line,e.hire_date,e.skill_grade,e.pay_mode,e.phone,e.status,e.version,
		u.id,u.login_name,u.status,
		COALESCE((SELECT json_agg(r.code ORDER BY r.code) FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id WHERE ur.tenant_id=e.tenant_id AND ur.user_id=u.id),'[]')
		FROM employee e
		LEFT JOIN user_account u ON u.tenant_id=e.tenant_id AND u.employee_id=e.id
		WHERE e.tenant_id=$1`
	args := []any{p.TenantID}
	if selfOnly {
		args = append(args, p.EmployeeID)
		query += fmt.Sprintf(" AND e.id=$%d", len(args))
	}
	query += " ORDER BY e.id"
	rows, err := api.DB.Query(r.Context(), query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	rowCrypto := dualwrite.NewRowCrypto(api.Crypto)
	items := []map[string]any{}
	for rows.Next() {
		var id, no, name, status string
		var position, workshop, line, hireDate, grade, mode, phone, gender *string
		var version int64
		var accountID, loginName, accountStatus *string
		var rolesBody []byte
		if err := rows.Scan(&id, &no, &name, &gender, &position, &workshop, &line, &hireDate, &grade, &mode, &phone, &status, &version, &accountID, &loginName, &accountStatus, &rolesBody); err != nil {
			return err
		}
		/* 密文库手机号列解密后再脱敏（迁移过渡期明文透传；本人或已解锁窗口内明文） */
		if phone != nil && rowCrypto.Enabled() && seccrypto.IsEncrypted(*phone) {
			plain, err := api.Crypto.DecryptField(r.Context(), p.TenantID, "employee", id, "phone", *phone)
			if err != nil {
				return fmt.Errorf("解密员工手机号失败: %w", err)
			}
			phone = &plain
		}
		revealUnlocked := validRevealToken(api.Config.JWTSecret, r.URL.Query().Get("reveal_token"), p.TenantID)
		if phone != nil && id != p.EmployeeID && !revealUnlocked {
			masked := maskPhone(*phone)
			phone = &masked
		}
		item := map[string]any{"id": id, "code": no, "name": name, "gender": gender, "position": position, "workshop": workshop, "production_line": line, "hire_date": hireDate, "skill_grade": grade, "pay_mode": mode, "phone": phone, "status": status, "version": version}
		if accountID != nil {
			account := map[string]any{"id": *accountID, "login_name": *loginName, "status": *accountStatus}
			if p.Permissions["identity:read"] {
				roles := []string{}
				_ = json.Unmarshal(rolesBody, &roles)
				account["roles"] = roles
			}
			item["account"] = account
			item["has_account"] = true
		} else {
			item["account"] = nil
			item["has_account"] = false
		}
		items = append(items, item)
	}
	return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "has_more": false})
}

func getEmployee(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	canSeeAll := p.Permissions["masterdata:read"] || p.Permissions["identity:read"] || p.Permissions["identity:write"]
	if !canSeeAll && p.EmployeeID != r.PathValue("id") {
		return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "没有执行此操作的权限")
	}
	var item map[string]any
	err := api.DB.QueryRow(r.Context(), "SELECT row_to_json(t) FROM (SELECT * FROM employee WHERE tenant_id=$1 AND id=$2) t", p.TenantID, r.PathValue("id")).Scan(&item)
	if err == pgx.ErrNoRows {
		return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "记录不存在")
	}
	if err != nil {
		return err
	}
	/* 密文库读出的敏感列解密回明文（迁移过渡期明文透传；本人直接可见明文） */
	if item, err = decryptEmployeeRow(r.Context(), api, p.TenantID, r.PathValue("id"), item); err != nil {
		return err
	}
	var skills []string
	if err := api.DB.QueryRow(r.Context(), "SELECT COALESCE(array_agg(process_id), '{}') FROM employee_skill WHERE tenant_id=$1 AND employee_id=$2", p.TenantID, r.PathValue("id")).Scan(&skills); err == nil {
		item["skill_process_ids"] = skills
	}
	var accountID, loginName, accountStatus *string
	_ = api.DB.QueryRow(r.Context(), "SELECT id,login_name,status FROM user_account WHERE tenant_id=$1 AND employee_id=$2", p.TenantID, r.PathValue("id")).Scan(&accountID, &loginName, &accountStatus)
	if accountID != nil {
		account := map[string]any{"id": *accountID, "login_name": *loginName, "status": *accountStatus}
		if p.Permissions["identity:read"] {
			roles := []string{}
			rows, err := api.DB.Query(r.Context(), `SELECT r.code FROM user_role ur JOIN role r ON r.tenant_id=ur.tenant_id AND r.id=ur.role_id WHERE ur.tenant_id=$1 AND ur.user_id=$2 ORDER BY r.code`, p.TenantID, *accountID)
			if err == nil {
				for rows.Next() {
					var code string
					if rows.Scan(&code) == nil {
						roles = append(roles, code)
					}
				}
				rows.Close()
			}
			account["roles"] = roles
		}
		item["account"] = account
	} else {
		item["account"] = nil
	}
	if item["id"] == p.EmployeeID {
		return httpapi.JSON(w, http.StatusOK, item)
	}
	/* 已解锁（5 分钟票据有效）：敏感列直接返回明文 */
	if validRevealToken(api.Config.JWTSecret, r.URL.Query().Get("reveal_token"), p.TenantID) {
		return httpapi.JSON(w, http.StatusOK, item)
	}
	/* 查看他人档案：敏感列一律 *** 占位（不返回脱敏明文，避免部分泄露），
	 * 解锁查看走 POST /api/v1/employees/{id}/reveal（管理员密码或授权码，记审计日志） */
	placeholderMasked(item)
	return httpapi.JSON(w, http.StatusOK, item)
}

/* employeeSensitiveColumns 员工档案加密敏感列（dualwrite.SensitiveColumns["employee"]）。 */
var employeeSensitiveColumns = []string{
	"phone", "id_card_no", "address", "birth_date", "nation", "native_place",
	"bank_card", "bank_name", "emergency_contact", "emergency_phone",
}

/* decryptEmployeeRow 解密密文库读出的员工行；加密服务未启用（明文库模式）时原样返回。 */
func decryptEmployeeRow(ctx context.Context, api *httpapi.API, tenantID, employeeID string, item map[string]any) (map[string]any, error) {
	rowCrypto := dualwrite.NewRowCrypto(api.Crypto)
	if !rowCrypto.Enabled() {
		return item, nil
	}
	dec, err := rowCrypto.FromSecure(ctx, tenantID, "employee", employeeID, item)
	if err != nil {
		return nil, fmt.Errorf("解密员工档案失败: %w", err)
	}
	return dec, nil
}

/* placeholderMasked 将已设置（非空）的敏感列替换为 *** 占位。 */
func placeholderMasked(item map[string]any) {
	for _, col := range employeeSensitiveColumns {
		if v, ok := item[col].(string); ok && v != "" {
			item[col] = "***"
		}
	}
}

var myProfileFields = map[string]string{
	"phone": "phone", "id_card_no": "id_card_no", "bank_card": "bank_card", "bank_name": "bank_name",
	"emergency_contact": "emergency_contact", "emergency_phone": "emergency_phone", "address": "address",
	"nation": "nation", "native_place": "native_place",
}

// selfEditableFields 本人在“员工与账号”页面可自行维护的字段。
var selfEditableFields = map[string]string{
	"phone": "phone", "address": "address", "emergency_contact": "emergency_contact",
	"emergency_phone": "emergency_phone", "native_place": "native_place", "nation": "nation",
}

func registerMyProfile(api *httpapi.API) {
	api.Handle("GET /api/v1/my/profile", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if p.EmployeeID == "" {
			return httpapi.Error(http.StatusBadRequest, "NO_EMPLOYEE_PROFILE", "当前账号未关联员工档案")
		}
		var item map[string]any
		err := api.DB.QueryRow(r.Context(), "SELECT row_to_json(t) FROM (SELECT * FROM employee WHERE tenant_id=$1 AND id=$2) t", p.TenantID, p.EmployeeID).Scan(&item)
		if err == pgx.ErrNoRows {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "员工档案不存在")
		}
		if err != nil {
			return err
		}
		if item, err = decryptEmployeeRow(r.Context(), api, p.TenantID, p.EmployeeID, item); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, item)
	})
	api.Handle("PATCH /api/v1/my/profile", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if p.EmployeeID == "" {
			return httpapi.Error(http.StatusBadRequest, "NO_EMPLOYEE_PROFILE", "当前账号未关联员工档案")
		}
		var input map[string]any
		if err := httpapi.Decode(r, &input); err != nil {
			return err
		}
		sets, args := []string{}, []any{}
		for field, column := range myProfileFields {
			if value, exists := input[field]; exists {
				args = append(args, value)
				sets = append(sets, fmt.Sprintf("%s=$%d", column, len(args)))
			}
		}
		if len(sets) == 0 {
			return httpapi.Error(http.StatusBadRequest, "NO_CHANGES", "没有可保存的字段")
		}
		if v, ok := input["phone"].(string); ok && v != "" && !regexp.MustCompile(`^1[3-9]\d{9}$`).MatchString(v) {
			return httpapi.Error(http.StatusBadRequest, "INVALID_PHONE", "联系电话须为 11 位手机号")
		}
		if v, ok := input["id_card_no"].(string); ok && v != "" && !regexp.MustCompile(`^\d{17}[\dXx]$`).MatchString(v) {
			return httpapi.Error(http.StatusBadRequest, "INVALID_ID_CARD", "身份证号须为 18 位")
		}
		args = append(args, platform.Now(), p.UserID, api.Config.NodeID)
		sets = append(sets, fmt.Sprintf("updated_at=$%d", len(args)-2), fmt.Sprintf("updated_by=$%d", len(args)-1), fmt.Sprintf("updated_node=$%d", len(args)), "version=version+1")
		args = append(args, p.TenantID, p.EmployeeID)
		result, err := api.DB.Exec(r.Context(), fmt.Sprintf("UPDATE employee SET %s WHERE tenant_id=$%d AND id=$%d", strings.Join(sets, ","), len(args)-1, len(args)), args...)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "员工档案不存在")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"id": p.EmployeeID})
	})
}

func syncEmployeeSkills(ctx context.Context, tx pgx.Tx, tenantID, employeeID string, raw any) error {
	ids, ok := raw.([]any)
	if !ok {
		return nil
	}
	if _, err := tx.Exec(ctx, "DELETE FROM employee_skill WHERE tenant_id=$1 AND employee_id=$2", tenantID, employeeID); err != nil {
		return err
	}
	for _, v := range ids {
		id, ok := v.(string)
		if !ok || id == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO employee_skill(tenant_id,id,employee_id,process_id,skill_level) VALUES($1,$2,$3,$4,'skilled')`, tenantID, platform.ID(), employeeID, id); err != nil {
			return err
		}
	}
	return nil
}

/* ===== 敏感信息查看授权（设计 2.4.3/6.4）：加密字段默认 *** 占位，持有效授权码经
 * /reveal 查看明文；授权码高熵随机串、SHA-256 哈希存储，明文仅此一次返回（生成时）；
 * 每次查看记审计日志（不含明文值）。 ===== */

var revealableTables = map[string]bool{"employee": true}

func registerSensitiveView(api *httpapi.API) {
	/* 生成授权码：租户管理员（identity:write）；可限定表与行，默认 30 分钟有效。 */
	api.Handle("POST /api/v1/sensitive-view-grants", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			TableName  string `json:"table_name"`
			RowID      string `json:"row_id"`
			TTLMinutes int    `json:"ttl_minutes"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		table := strings.TrimSpace(in.TableName)
		if table == "" {
			table = "employee"
		}
		if !revealableTables[table] {
			return httpapi.Error(http.StatusBadRequest, "TABLE_NOT_REVEALABLE", "该表不支持敏感信息授权查看（当前仅 employee）")
		}
		ttl := in.TTLMinutes
		if ttl == 0 {
			ttl = 30
		}
		if ttl < 1 || ttl > 720 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "有效期须为 1-720 分钟")
		}
		now := platform.Now()
		code, err := randomGrantCode()
		if err != nil {
			return err
		}
		id := platform.ID()
		expiresAt := now + int64(ttl)*int64(time.Minute/time.Microsecond)
		_, err = api.DB.Exec(r.Context(), `INSERT INTO sensitive_view_grant(id,tenant_id,table_name,row_id,code_hash,status,expires_at,created_by,created_at)
			VALUES($1,$2,$3,$4,$5,'active',$6,$7,$8)`,
			id, p.TenantID, table, strings.TrimSpace(in.RowID), sha256Hex(code), expiresAt, p.UserID, now)
		if err != nil {
			return err
		}
		if err := auditOnce(r.Context(), api, p, "sensitive_view_grant_create", table, strings.TrimSpace(in.RowID),
			nil, map[string]any{"grant_id": id, "table_name": table, "expires_at": expiresAt}, ""); err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{
			"grant_id": id, "grant_code": code, /* 明文仅此一次返回 */
			"table_name": table, "row_id": in.RowID, "expires_at": expiresAt,
		})
	})

	/* 敏感字段解锁：两种途径，均签发 5 分钟有效的查看票据（到期自动收回，恢复 ***）——
	 * 1) 租户管理员本人：输入自己的登录密码（二次确认）；
	 * 2) 其他角色：输入租户管理员签发的授权码（POST /sensitive-view-grants）。
	 * 票据经 HMAC-SHA256 签名（JWTSecret），无服务端状态，重启即失效。 */
	api.Handle("POST /api/v1/employees/{id}/reveal", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Password  string `json:"password"`
			GrantCode string `json:"grant_code"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		employeeID := r.PathValue("id")
		ctx := r.Context()
		grantID, authBy := "", ""
		switch {
		case strings.TrimSpace(in.Password) != "":
			if !p.Permissions["identity:write"] {
				return httpapi.Error(http.StatusForbidden, "PASSWORD_NOT_ALLOWED", "仅租户管理员可凭本人登录密码解锁查看")
			}
			ok, err := api.Auth.VerifyPassword(ctx, p.TenantID, p.UserID, in.Password)
			if err != nil {
				return err
			}
			if !ok {
				return httpapi.Error(http.StatusForbidden, "INVALID_PASSWORD", "登录密码不正确")
			}
			authBy = "admin_password"
		case strings.TrimSpace(in.GrantCode) != "":
			id, err := matchGrant(ctx, api, p.TenantID, "employee", employeeID, in.GrantCode)
			if err != nil {
				return err
			}
			grantID, authBy = id, "grant_code"
		case p.EmployeeID == employeeID:
			authBy = "self"
		default:
			return httpapi.Error(http.StatusBadRequest, "AUTH_REQUIRED", "请输入租户管理员登录密码或授权码")
		}
		/* 审计：记录谁、以何种方式解锁了哪行（绝不记录明文值） */
		if err := auditOnce(ctx, api, p, "sensitive_view_reveal", "employee", employeeID,
			nil, map[string]any{"grant_id": grantID, "auth": authBy, "fields": employeeSensitiveColumns}, ""); err != nil {
			return err
		}
		expiresAt := platform.Now() + revealTokenTTL
		return httpapi.JSON(w, http.StatusOK, map[string]any{
			"reveal_token": signRevealToken(api.Config.JWTSecret, p.TenantID, p.UserID, expiresAt),
			"expires_at":   expiresAt,
		})
	})
}

/* revealTokenTTL 敏感字段查看票据有效期：5 分钟，到期自动收回（恢复 *** 显示）。 */
const revealTokenTTL = int64((5 * time.Minute) / time.Microsecond)

/* signRevealToken 签发查看票据：payload=base64url(tenant|user|exp)，HMAC-SHA256 签名。 */
func signRevealToken(secret, tenantID, userID string, expiresAt int64) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%s|%s|%d", tenantID, userID, expiresAt)))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

/* validRevealToken 校验查看票据：签名正确、租户匹配、未过期。 */
func validRevealToken(secret, token, tenantID string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	expected := mac.Sum(nil)
	actual, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || subtle.ConstantTimeCompare(actual, expected) != 1 {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	segs := strings.Split(string(raw), "|")
	if len(segs) != 3 || segs[0] != tenantID {
		return false
	}
	exp, err := strconv.ParseInt(segs[2], 10, 64)
	return err == nil && exp > platform.Now()
}

/* matchGrant 在租户有效授权码中匹配明文码（SHA-256 哈希比对）；未命中或过期报 403。 */
func matchGrant(ctx context.Context, api *httpapi.API, tenantID, table, rowID, code string) (string, error) {
	rows, err := api.DB.Query(ctx, `SELECT id,code_hash FROM sensitive_view_grant
		WHERE tenant_id=$1 AND table_name=$2 AND status='active' AND expires_at>$3 AND (row_id='' OR row_id=$4)`,
		tenantID, table, platform.Now(), rowID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	want := sha256Hex(code)
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return "", err
		}
		if subtle.ConstantTimeCompare([]byte(hash), []byte(want)) == 1 {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", httpapi.Error(http.StatusForbidden, "GRANT_INVALID", "授权码无效或已过期")
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

/* auditOnce 非事务型 handler 里记一条审计日志（独立短事务）。 */
func auditOnce(ctx context.Context, api *httpapi.API, p auth.Principal, action, ownerType, ownerID string, before, after any, reason string) error {
	tx, err := api.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := httpapi.Audit(ctx, tx, p, api.Config.NodeID, action, ownerType, ownerID, before, after, reason); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

/* randomGrantCode 生成 12 位高熵授权码（字母+数字，去除易混淆字符）。 */
func randomGrantCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, 12)
	buf := make([]byte, len(out))
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out), nil
}
