package production

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
	"github.com/jackc/pgx/v5"
)

func registerCustomFields(api *httpapi.API) {
	api.Handle("GET /api/v1/custom-field-definitions", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		args := []any{p.TenantID}
		where := "tenant_id=$1"
		if owner := strings.TrimSpace(r.URL.Query().Get("owner_type")); owner != "" {
			args = append(args, owner)
			where += " AND owner_type=$2"
		}
		rows, err := api.DB.Query(r.Context(), `SELECT id,owner_type,field_key,label,data_type,required,options::text,display_scope::text,COALESCE(permission_code,''),sort_no,status,version FROM custom_field_definition WHERE `+where+` ORDER BY owner_type,sort_no,field_key`, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, owner, key, label, dataType, options, display, permission, status string
			var required bool
			var sortNo int
			var version int64
			if err := rows.Scan(&id, &owner, &key, &label, &dataType, &required, &options, &display, &permission, &sortNo, &status, &version); err != nil {
				return err
			}
			items = append(items, map[string]any{"id": id, "owner_type": owner, "field_key": key, "label": label, "data_type": dataType, "required": required, "options": options, "display_scope": display, "permission_code": permission, "sort_no": sortNo, "status": status, "version": version})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("POST /api/v1/custom-field-definitions", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			OwnerType      string          `json:"owner_type"`
			FieldKey       string          `json:"field_key"`
			Label          string          `json:"label"`
			DataType       string          `json:"data_type"`
			Required       bool            `json:"required"`
			Options        json.RawMessage `json:"options"`
			DisplayScope   json.RawMessage `json:"display_scope"`
			PermissionCode *string         `json:"permission_code"`
			SortNo         int             `json:"sort_no"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		in.FieldKey = strings.TrimSpace(in.FieldKey)
		in.Label = strings.TrimSpace(in.Label)
		in.OwnerType = strings.TrimSpace(in.OwnerType)
		if in.OwnerType == "" || in.FieldKey == "" || in.Label == "" {
			return httpapi.Error(400, "VALIDATION_ERROR", "对象类型、字段键和名称必填")
		}
		if in.DataType != "text" && in.DataType != "number" && in.DataType != "date" && in.DataType != "bool" && in.DataType != "select" {
			return httpapi.Error(400, "VALIDATION_ERROR", "字段类型只支持 text/number/date/bool/select")
		}
		options := in.Options
		if len(options) == 0 {
			options = json.RawMessage(`[]`)
		}
		display := in.DisplayScope
		if len(display) == 0 {
			display = json.RawMessage(`{}`)
		}
		id := platform.ID()
		if _, err := api.DB.Exec(r.Context(), `INSERT INTO custom_field_definition(tenant_id,id,owner_type,field_key,label,data_type,required,options,display_scope,permission_code,sort_no,status,version,created_at,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'active',1,$12,$12)`, p.TenantID, id, in.OwnerType, in.FieldKey, in.Label, in.DataType, in.Required, options, display, in.PermissionCode, in.SortNo, platform.Now()); err != nil {
			if isUniqueViolation(err) {
				return httpapi.Error(409, "DUPLICATE_FIELD", "同一对象下的字段键已存在")
			}
			return err
		}
		return httpapi.JSON(w, 201, map[string]any{"id": id, "version": 1})
	})

	api.Handle("PATCH /api/v1/custom-field-definitions/{id}", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Label        *string         `json:"label"`
			Options      json.RawMessage `json:"options"`
			DisplayScope json.RawMessage `json:"display_scope"`
			SortNo       *int            `json:"sort_no"`
			Status       *string         `json:"status"`
			Version      int64           `json:"version"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		var current map[string]any
		if err := tx.QueryRow(r.Context(), `SELECT row_to_json(t) FROM (SELECT * FROM custom_field_definition WHERE tenant_id=$1 AND id=$2 FOR UPDATE) t`, p.TenantID, r.PathValue("id")).Scan(&current); err != nil {
			return httpapi.Error(404, "NOT_FOUND", "字段定义不存在")
		}
		sets, args := []string{}, []any{}
		if in.Label != nil {
			args = append(args, strings.TrimSpace(*in.Label))
			sets = append(sets, "label=$"+strconv.Itoa(len(args)))
		}
		if len(in.Options) > 0 {
			args = append(args, in.Options)
			sets = append(sets, "options=$"+strconv.Itoa(len(args)))
		}
		if len(in.DisplayScope) > 0 {
			args = append(args, in.DisplayScope)
			sets = append(sets, "display_scope=$"+strconv.Itoa(len(args)))
		}
		if in.SortNo != nil {
			args = append(args, *in.SortNo)
			sets = append(sets, "sort_no=$"+strconv.Itoa(len(args)))
		}
		if in.Status != nil {
			args = append(args, *in.Status)
			sets = append(sets, "status=$"+strconv.Itoa(len(args)))
		}
		if len(sets) == 0 {
			return httpapi.Error(400, "NO_CHANGES", "没有可保存的字段")
		}
		args = append(args, platform.Now())
		sets = append(sets, "updated_at=$"+strconv.Itoa(len(args)), "version=version+1")
		args = append(args, p.TenantID, r.PathValue("id"), in.Version)
		result, err := tx.Exec(r.Context(), `UPDATE custom_field_definition SET `+strings.Join(sets, ",")+` WHERE tenant_id=$`+strconv.Itoa(len(args)-2)+` AND id=$`+strconv.Itoa(len(args)-1)+` AND version=$`+strconv.Itoa(len(args)), args...)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			return httpapi.Error(409, "VERSION_CONFLICT", "字段定义已变更，请刷新后重试")
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"id": r.PathValue("id"), "version": in.Version + 1})
	})

	api.Handle("GET /api/v1/custom-field-values/{ownerType}/{ownerId}", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		rows, err := api.DB.Query(r.Context(), `SELECT d.id,d.field_key,d.label,d.data_type,d.required,d.options::text,COALESCE(v.value_text,''),COALESCE(v.value_number::text,''),COALESCE(v.value_date,0),COALESCE(v.value_bool,false)
			FROM custom_field_definition d
			LEFT JOIN custom_field_value v ON v.tenant_id=d.tenant_id AND v.definition_id=d.id AND v.owner_id=$3
			WHERE d.tenant_id=$1 AND d.owner_type=$2 AND d.status='active' ORDER BY d.sort_no,d.field_key`, p.TenantID, r.PathValue("ownerType"), r.PathValue("ownerId"))
		if err != nil {
			return err
		}
		defer rows.Close()
		items := []map[string]any{}
		for rows.Next() {
			var id, key, label, dataType, options, text, number string
			var date int64
			var required, boolValue bool
			if err := rows.Scan(&id, &key, &label, &dataType, &required, &options, &text, &number, &date, &boolValue); err != nil {
				return err
			}
			var value any = nil
			switch dataType {
			case "number":
				if number != "" {
					value = number
				}
			case "date":
				if date != 0 {
					value = date
				}
			case "bool":
				value = boolValue
			default:
				if text != "" {
					value = text
				}
			}
			items = append(items, map[string]any{"definition_id": id, "field_key": key, "label": label, "data_type": dataType, "required": required, "options": options, "value": value})
		}
		return httpapi.JSON(w, 200, map[string]any{"items": items})
	})

	api.Handle("PUT /api/v1/custom-field-values/{ownerType}/{ownerId}", "settings:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Values []struct {
				FieldKey string `json:"field_key"`
				Value    any    `json:"value"`
			} `json:"values"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		tx, err := api.DB.Begin(r.Context())
		if err != nil {
			return err
		}
		defer tx.Rollback(r.Context())
		if _, err := tx.Exec(r.Context(), `DELETE FROM custom_field_value WHERE tenant_id=$1 AND owner_id=$2 AND definition_id IN (SELECT id FROM custom_field_definition WHERE tenant_id=$1 AND owner_type=$3)`, p.TenantID, r.PathValue("ownerId"), r.PathValue("ownerType")); err != nil {
			return err
		}
		now := platform.Now()
		saved := 0
		for _, item := range in.Values {
			var definitionID, dataType string
			if err := tx.QueryRow(r.Context(), "SELECT id,data_type FROM custom_field_definition WHERE tenant_id=$1 AND owner_type=$2 AND field_key=$3 AND status='active'", p.TenantID, r.PathValue("ownerType"), item.FieldKey).Scan(&definitionID, &dataType); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				return err
			}
			var text *string
			var number *string
			var date *int64
			var boolValue *bool
			switch dataType {
			case "number":
				if v, ok := item.Value.(string); ok && strings.TrimSpace(v) != "" {
					number = &v
				} else if v, ok := item.Value.(float64); ok {
					s := jsonNumberString(v)
					number = &s
				}
			case "date":
				if v, ok := item.Value.(float64); ok && v != 0 {
					d := int64(v)
					date = &d
				}
			case "bool":
				if v, ok := item.Value.(bool); ok {
					boolValue = &v
				}
			default:
				if v, ok := item.Value.(string); ok && v != "" {
					text = &v
				}
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO custom_field_value(tenant_id,id,definition_id,owner_id,value_text,value_number,value_date,value_bool,version,created_at,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9)`, p.TenantID, platform.ID(), definitionID, r.PathValue("ownerId"), text, number, date, boolValue, now); err != nil {
				return err
			}
			saved++
		}
		if err := tx.Commit(r.Context()); err != nil {
			return err
		}
		return httpapi.JSON(w, 200, map[string]any{"saved": saved})
	})

	api.Handle("GET /api/v1/audit-logs/{id}", "settings:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var item map[string]any
		if err := api.DB.QueryRow(r.Context(), `SELECT row_to_json(t) FROM (SELECT id,request_id,actor_type,actor_id,action,owner_type,owner_id,before_values,after_values,reason,ip,device_info,created_node,created_at FROM audit_log WHERE tenant_id=$1 AND id=$2) t`, p.TenantID, r.PathValue("id")).Scan(&item); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return httpapi.Error(404, "NOT_FOUND", "审计记录不存在")
			}
			return err
		}
		return httpapi.JSON(w, 200, item)
	})
}

func jsonNumberString(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
