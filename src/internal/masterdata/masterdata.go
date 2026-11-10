package masterdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

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
	api.Handle("GET /api/v1/employees/next-no", "", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		if !p.Permissions["masterdata:write"] && !p.Permissions["identity:write"] {
			return httpapi.Error(http.StatusForbidden, "FORBIDDEN", "没有执行此操作的权限")
		}
		employeeNo, err := nextEmployeeNo(r.Context(), api, p.TenantID)
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

// nextEmployeeNo 生成 {租户标识}-{租户内序号}，序号按前缀（即租户）递增。
func nextEmployeeNo(ctx context.Context, api *httpapi.API, tenantID string) (string, error) {
	prefix, err := tenantPrefix(ctx, api, tenantID)
	if err != nil {
		return "", err
	}
	var seq int64
	if err := api.DB.QueryRow(ctx, `SELECT COALESCE(MAX((regexp_match(employee_no, '-([0-9]{1,6})$'))[1]::bigint),0)+1 FROM employee WHERE upper(employee_no) LIKE $1 || '-%'`, prefix).Scan(&seq); err != nil {
		return "", err
	}
	if seq > 999999 {
		return "", httpapi.Error(http.StatusConflict, "EMPLOYEE_NO_EXHAUSTED", "该租户工号序号已用尽")
	}
	return fmt.Sprintf("%s-%06d", prefix, seq), nil
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
		generated, err := nextEmployeeNo(r.Context(), api, p.TenantID)
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

func createWithInput(api *httpapi.API, w http.ResponseWriter, r *http.Request, p auth.Principal, definition entity, input map[string]any) error {
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
	sets, args := []string{}, []any{}
	for field, column := range allowed {
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
	args = append(args, p.TenantID, employeeID, int64(version))
	tx, err := api.DB.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	result, err := tx.Exec(r.Context(), fmt.Sprintf("UPDATE employee SET %s WHERE tenant_id=$%d AND id=$%d AND version=$%d", strings.Join(sets, ","), len(args)-2, len(args)-1, len(args)), args...)
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
	return httpapi.JSON(w, http.StatusOK, map[string]any{"id": employeeID, "version": int64(version) + 1})
}

func registerDictionaries(api *httpapi.API) {
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
		if input.Code == "" || input.Name == "" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "编码和名称必填")
		}
		id, now := platform.ID(), platform.Now()
		_, err := api.DB.Exec(r.Context(), `INSERT INTO dictionary_item(tenant_id,id,dictionary_type,code,name,parent_id,sort_no,created_at,created_by,created_node,updated_at,updated_by,updated_node)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$8,$9,$10)`, p.TenantID, id, r.PathValue("type"), input.Code, input.Name, input.ParentID, input.SortNo, now, p.UserID, api.Config.NodeID)
		if err != nil {
			return httpapi.Error(http.StatusConflict, "DUPLICATE_CODE", "字典编码已存在")
		}
		return httpapi.JSON(w, http.StatusCreated, map[string]any{"id": id, "version": 1, "status": "active"})
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
		result, err := api.DB.Exec(r.Context(), `UPDATE dictionary_item SET name=$1,sort_no=$2,status=$3,version=version+1,updated_at=$4,updated_by=$5,updated_node=$6
			WHERE tenant_id=$7 AND id=$8 AND version=$9`, input.Name, input.SortNo, input.Status, platform.Now(), p.UserID, api.Config.NodeID, p.TenantID, r.PathValue("id"), input.Version)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "VERSION_CONFLICT", "记录已变更")
		}
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
		if phone != nil && id != p.EmployeeID {
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
	isSelf := item["id"] == p.EmployeeID
	if isSelf {
		return httpapi.JSON(w, http.StatusOK, item)
	}
	if v, ok := item["phone"].(string); ok && v != "" {
		item["phone"] = maskPhone(v)
	}
	if v, ok := item["id_card_no"].(string); ok && v != "" {
		item["id_card_no"] = maskIDCard(v)
	}
	if !p.Permissions["payroll:pay"] {
		if v, ok := item["bank_card"].(string); ok && v != "" {
			item["bank_card"] = maskBankCard(v)
		}
	}
	return httpapi.JSON(w, http.StatusOK, item)
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
