package tech

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"clothp/internal/auth"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
)

/* profileField 用户资料模板字段定义（设计 4.5）。 */
type profileField struct {
	FieldCode    string `json:"field_code"`
	FieldName    string `json:"field_name"`
	Required     bool   `json:"required"`
	Encrypted    bool   `json:"encrypted"`
	VisibleLevel string `json:"visible_level"`
	DictType     string `json:"dict_type"`
	SortNo       int    `json:"sort_no"`
	Remark       string `json:"remark"`
}

/* defaultProfileFields 标准板内置字段（租户未保存自定义模板时返回）。 */
func defaultProfileFields() []profileField {
	return []profileField{
		{FieldCode: "name", FieldName: "姓名", Required: true, VisibleLevel: "L4_SELF", SortNo: 10},
		{FieldCode: "employee_no", FieldName: "工号", Required: true, VisibleLevel: "L4_SELF", SortNo: 20},
		{FieldCode: "workshop", FieldName: "车间", VisibleLevel: "L2_WORKSHOP", DictType: "workshop", SortNo: 30},
		{FieldCode: "line", FieldName: "产线", VisibleLevel: "L3_LINE", DictType: "line", SortNo: 40},
		{FieldCode: "team", FieldName: "班组", VisibleLevel: "L3_LINE", DictType: "team", SortNo: 50},
		{FieldCode: "position", FieldName: "岗位", VisibleLevel: "L4_SELF", DictType: "position", SortNo: 60},
		{FieldCode: "hire_date", FieldName: "入职日期", VisibleLevel: "L3_LINE", SortNo: 70},
		{FieldCode: "id_card_no", FieldName: "身份证号", Required: true, Encrypted: true, VisibleLevel: "L1_FACTORY", SortNo: 110},
		{FieldCode: "gender", FieldName: "性别", VisibleLevel: "L3_LINE", DictType: "gender", SortNo: 120},
		{FieldCode: "birth_date", FieldName: "出生日期", Encrypted: true, VisibleLevel: "L1_FACTORY", SortNo: 130},
		{FieldCode: "nation", FieldName: "民族", Encrypted: true, VisibleLevel: "L2_WORKSHOP", DictType: "nation", SortNo: 140},
		{FieldCode: "native_place", FieldName: "籍贯", Encrypted: true, VisibleLevel: "L1_FACTORY", SortNo: 150},
		{FieldCode: "phone", FieldName: "手机号", Required: true, Encrypted: true, VisibleLevel: "L4_SELF", SortNo: 210},
		{FieldCode: "address", FieldName: "现住址", Encrypted: true, VisibleLevel: "L2_WORKSHOP", SortNo: 220},
		{FieldCode: "emergency_contact", FieldName: "紧急联系人", Encrypted: true, VisibleLevel: "L3_LINE", SortNo: 230},
		{FieldCode: "emergency_phone", FieldName: "紧急联系电话", Encrypted: true, VisibleLevel: "L3_LINE", SortNo: 240},
		{FieldCode: "bank_card", FieldName: "银行卡号", Encrypted: true, VisibleLevel: "L1_FACTORY", SortNo: 310},
		{FieldCode: "bank_name", FieldName: "开户行", Encrypted: true, VisibleLevel: "L1_FACTORY", SortNo: 320},
		{FieldCode: "skill_level", FieldName: "技能等级", VisibleLevel: "L3_LINE", DictType: "skill_level", SortNo: 410},
		{FieldCode: "employment_type", FieldName: "用工形式", VisibleLevel: "L2_WORKSHOP", DictType: "employment_type", SortNo: 510},
	}
}

/* encryptedProfileFields 强制加密字段：无论前端如何传，保存时加密标记恒为 true（设计 4.5/4.15）。 */
var encryptedProfileFields = map[string]bool{
	"id_card_no": true, "birth_date": true, "phone": true, "address": true,
	"emergency_phone": true, "bank_card": true, "nation": true, "native_place": true,
}

func registerProfileTemplate(api *httpapi.API) {
	/* 模板列表：未保存过自定义模板时返回内置标准板 */
	api.Handle("GET /api/v1/user-profile-templates", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		items, err := loadProfileTemplates(r.Context(), api, p)
		if err != nil {
			return err
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items})
	})
	api.Handle("GET /api/v1/user-profile-templates/{type}", "identity:read", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		templateType := strings.TrimSpace(r.PathValue("type"))
		if templateType != "standard" && templateType != "custom" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "模板类型必须是 standard 或 custom")
		}
		items, err := loadProfileTemplates(r.Context(), api, p)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item["template_type"] == templateType {
				return httpapi.JSON(w, http.StatusOK, item)
			}
		}
		return httpapi.Error(http.StatusNotFound, "NOT_FOUND", "模板不存在")
	})
	/* 保存自定义模板（厂管理账户可修改标准板字段集合并保存为自定义版本） */
	api.Handle("PUT /api/v1/user-profile-templates/{type}", "identity:write", func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		var in struct {
			Name   string         `json:"name"`
			Fields []profileField `json:"fields"`
			Remark string         `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		ctx := r.Context()
		templateType := strings.TrimSpace(r.PathValue("type"))
		if templateType != "standard" && templateType != "custom" {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "模板类型必须是 standard 或 custom")
		}
		if len(in.Fields) == 0 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "模板字段不能为空")
		}
		seen := map[string]bool{}
		for index := range in.Fields {
			field := &in.Fields[index]
			field.FieldCode = strings.TrimSpace(field.FieldCode)
			field.FieldName = strings.TrimSpace(field.FieldName)
			if field.FieldCode == "" || field.FieldName == "" {
				return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "字段编码与名称必填")
			}
			if seen[field.FieldCode] {
				return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "字段编码重复："+field.FieldCode)
			}
			seen[field.FieldCode] = true
			/* 敏感字段强制加密，防止前端误关 */
			if encryptedProfileFields[field.FieldCode] {
				field.Encrypted = true
			}
			if field.SortNo == 0 {
				field.SortNo = (index + 1) * 10
			}
		}
		fieldsJSON, _ := json.Marshal(in.Fields)
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = "自定义用户资料模板"
		}
		now := platform.Now()
		existingID := ""
		_ = api.DB.QueryRow(ctx, `SELECT id FROM user_profile_template WHERE tenant_id=$1 AND template_type=$2`, p.TenantID, templateType).Scan(&existingID)
		if existingID == "" {
			_, err := api.DB.Exec(ctx, `INSERT INTO user_profile_template(tenant_id,id,template_type,name,fields,is_active,applied_count,remark,
				created_at,created_by,created_node,updated_at,updated_by,updated_node,version)
				VALUES($1,$2,$3,$4,$5,TRUE,0,$6,$7,$8,$9,$7,$8,$9,1)`,
				p.TenantID, platform.ID(), templateType, name, fieldsJSON, in.Remark, now, p.UserID, api.Config.NodeID)
			if err != nil {
				return err
			}
		} else {
			if _, err := api.DB.Exec(ctx, `UPDATE user_profile_template SET name=$3,fields=$4,remark=$5,version=version+1,
				updated_at=$6,updated_by=$7,updated_node=$8 WHERE tenant_id=$1 AND id=$2`,
				p.TenantID, existingID, name, fieldsJSON, in.Remark, now, p.UserID, api.Config.NodeID); err != nil {
				return err
			}
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"saved": true, "template_type": templateType})
	})
}

/* loadProfileTemplates 读取模板；标准板缺失时用内置字段兜底，applied_count 取当前在职人数。 */
func loadProfileTemplates(ctx context.Context, api *httpapi.API, p auth.Principal) ([]map[string]any, error) {
	rows, err := api.DB.Query(ctx, `SELECT id,template_type,name,fields,is_active,remark,version,updated_at
		FROM user_profile_template WHERE tenant_id=$1 ORDER BY template_type`, p.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var applied int
	if err := api.DB.QueryRow(ctx, `SELECT COUNT(*) FROM employee WHERE tenant_id=$1 AND status='active'`, p.TenantID).Scan(&applied); err != nil {
		return nil, err
	}
	items := []map[string]any{}
	foundStandard := false
	for rows.Next() {
		var id, templateType, name, remark string
		var fields []byte
		var active bool
		var version, updatedAt int64
		if err := rows.Scan(&id, &templateType, &name, &fields, &active, &remark, &version, &updatedAt); err != nil {
			return nil, err
		}
		if templateType == "standard" {
			foundStandard = true
		}
		items = append(items, map[string]any{"id": id, "template_type": templateType, "name": name,
			"fields": json.RawMessage(fields), "is_active": active, "remark": remark, "version": version,
			"updated_at": updatedAt, "applied_count": applied})
	}
	if !foundStandard {
		fields, _ := json.Marshal(defaultProfileFields())
		items = append([]map[string]any{{"id": "", "template_type": "standard", "name": "标准用户资料模板",
			"fields": json.RawMessage(fields), "is_active": true, "remark": "系统内置字段", "version": 0,
			"updated_at": 0, "applied_count": applied, "builtin": true}}, items...)
	}
	return items, nil
}
