package tenantreg

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"clothp/internal/dualwrite"
	"clothp/internal/httpapi"
	"clothp/internal/platform"
)

/* 门户「注册」入口：短信验证后按手机号列出本人提交过的所有注册申请（体验/付费、待审/已审批），
 * 供注册页面打开第一家、切换多版、回填信息，并在未审批前修改。
 * 短信验证码一次性校验通过后签发短期 lookup token，后续修改不再需要重复验证手机号。 */

type registrationLookupToken struct {
	phone     string
	ids       map[string]bool
	expiresAt time.Time
}

var (
	regLookupMu     sync.Mutex
	regLookupTokens = map[string]*registrationLookupToken{}
)

func registerRegistrationLookup(api *httpapi.API) {
	api.Public("POST /api/v1/public/registration-lookup", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			Phone   string `json:"phone"`
			SmsCode string `json:"sms_code"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		phone := strings.TrimSpace(in.Phone)
		if !phonePattern.MatchString(phone) {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "请输入正确的 11 位手机号")
		}
		if err := peekRegCode(phone, strings.TrimSpace(in.SmsCode)); err != nil {
			return err
		}
		rows, err := api.DB.Query(r.Context(), `SELECT t.id,t.code,t.name,COALESCE(t.short_code,''),
			COALESCE(t.contact_name,''),COALESCE(t.contact_phone,''),COALESCE(t.contact_wechat,''),
			COALESCE(t.contact_qq,''),COALESCE(t.contact_alipay,''),COALESCE(t.address,''),
			COALESCE(t.region,''),COALESCE(t.scale,''),COALESCE(t.remark,''),
			t.registration_status,COALESCE(t.customer_type,'paid'),t.status,t.created_at
			FROM tenant t ORDER BY t.created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		rowCrypto := dualwrite.NewRowCrypto(api.Crypto)
		items := []map[string]any{}
		ids := map[string]bool{}
		for rows.Next() {
			var id, code, name, shortCode, contactName, contactPhone, contactWechat, contactQQ, contactAlipay, address, region, scale, remark, regStatus, customerType, status string
			var created int64
			if err := rows.Scan(&id, &code, &name, &shortCode, &contactName, &contactPhone, &contactWechat, &contactQQ,
				&contactAlipay, &address, &region, &scale, &remark, &regStatus, &customerType, &status, &created); err != nil {
				return err
			}
			dec, err := rowCrypto.FromSecure(r.Context(), id, "tenant", id, map[string]any{
				"contact_name": contactName, "contact_phone": contactPhone,
				"contact_wechat": contactWechat, "contact_qq": contactQQ,
				"contact_alipay": contactAlipay, "address": address,
			})
			if err != nil {
				return err
			}
			contactPhone, _ = dec["contact_phone"].(string)
			if strings.TrimSpace(contactPhone) != phone {
				continue
			}
			contactName, _ = dec["contact_name"].(string)
			contactWechat, _ = dec["contact_wechat"].(string)
			contactQQ, _ = dec["contact_qq"].(string)
			contactAlipay, _ = dec["contact_alipay"].(string)
			address, _ = dec["address"].(string)
			ids[id] = true
			items = append(items, map[string]any{
				"id": id, "code": code, "name": name, "short_code": shortCode,
				"contact_name": contactName, "contact_phone": contactPhone,
				"contact_wechat": contactWechat, "contact_qq": contactQQ, "contact_alipay": contactAlipay,
				"address": address, "region": region, "scale": scale, "remark": remark,
				"registration_status": regStatus, "customer_type": customerType,
				"status": status, "created_at": created,
			})
		}
		token := platform.ID()
		regLookupMu.Lock()
		now := time.Now()
		for key, old := range regLookupTokens {
			if now.After(old.expiresAt) {
				delete(regLookupTokens, key)
			}
		}
		regLookupTokens[token] = &registrationLookupToken{phone: phone, ids: ids, expiresAt: now.Add(15 * time.Minute)}
		regLookupMu.Unlock()
		return httpapi.JSON(w, http.StatusOK, map[string]any{"items": items, "update_token": token})
	})

	api.Public("POST /api/v1/public/registration-update", func(w http.ResponseWriter, r *http.Request) error {
		var in struct {
			UpdateToken   string `json:"update_token"`
			ID            string `json:"id"`
			Name          string `json:"name"`
			Region        string `json:"region"`
			Scale         string `json:"scale"`
			ContactName   string `json:"contact_name"`
			ContactWechat string `json:"contact_wechat"`
			ContactQQ     string `json:"contact_qq"`
			ContactAlipay string `json:"contact_alipay"`
			Address       string `json:"address"`
			Remark        string `json:"remark"`
		}
		if err := httpapi.Decode(r, &in); err != nil {
			return err
		}
		regLookupMu.Lock()
		session := regLookupTokens[strings.TrimSpace(in.UpdateToken)]
		valid := session != nil && time.Now().Before(session.expiresAt) && session.ids[strings.TrimSpace(in.ID)]
		regLookupMu.Unlock()
		if !valid {
			return httpapi.Error(http.StatusUnauthorized, "INVALID_TOKEN", "验证已失效，请重新用手机号与短信验证码查询")
		}
		name := strings.TrimSpace(in.Name)
		if name != "" && len(name) < 2 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "制衣厂名称至少 2 个字符")
		}
		scale := strings.TrimSpace(in.Scale)
		if scale != "" && !scaleSet[scale] {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "厂规模不合法")
		}
		if len(strings.TrimSpace(in.ContactWechat)) > 64 || len(strings.TrimSpace(in.ContactQQ)) > 32 || len(strings.TrimSpace(in.ContactAlipay)) > 64 {
			return httpapi.Error(http.StatusBadRequest, "VALIDATION_ERROR", "微信号/QQ/支付宝账号过长")
		}
		tag, err := api.DB.Exec(r.Context(), `UPDATE tenant SET
			name=CASE WHEN $1<>'' THEN $1 ELSE name END,
			region=NULLIF($2,''),scale=NULLIF($3,''),
			contact_name=CASE WHEN $4<>'' THEN $4 ELSE contact_name END,
			address=NULLIF($5,''),remark=NULLIF($6,''),
			contact_wechat=NULLIF($7,''),contact_qq=NULLIF($8,''),contact_alipay=NULLIF($9,''),version=version+1
			WHERE id=$10 AND registration_status='pending'`,
			name, strings.TrimSpace(in.Region), scale, strings.TrimSpace(in.ContactName),
			strings.TrimSpace(in.Address), strings.TrimSpace(in.Remark),
			strings.TrimSpace(in.ContactWechat), strings.TrimSpace(in.ContactQQ), strings.TrimSpace(in.ContactAlipay),
			strings.TrimSpace(in.ID))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return httpapi.Error(http.StatusConflict, "NOT_PENDING", "该申请已审批处理，注册信息不可再修改")
		}
		return httpapi.JSON(w, http.StatusOK, map[string]any{"message": "注册信息已更新，平台管理员审批时将看到最新内容"})
	})
}
