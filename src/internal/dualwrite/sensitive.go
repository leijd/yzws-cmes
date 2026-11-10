package dualwrite

import (
	"context"
	"fmt"

	"clothp/internal/seccrypto"
)

/* SensitiveColumns 密文库敏感列映射（设计 4.15）。
 * 明文检索/关联字段（工号、姓名、编码、状态、组织 ID、phone_hash 盲索引等）不加密。
 * 身份证、银行卡等本版不提供等值查询，不建盲索引，列表仅脱敏展示。 */
var SensitiveColumns = map[string][]string{
	"tenant":       {"contact_name", "contact_phone", "contact_wechat", "contact_qq", "contact_alipay", "address"},
	"visitor_lead": {"contact_name", "contact_phone", "address", "wechat_unionid"},
	"employee":     {"phone", "id_card_no", "address", "birth_date", "nation", "native_place", "bank_card", "bank_name", "emergency_contact", "emergency_phone"},
	"customer":     {"contact_name", "phone", "address"},
	"supplier":     {"contact_name", "phone", "address"},
}

func sensitiveSet(table string) map[string]bool {
	set := map[string]bool{}
	for _, col := range SensitiveColumns[table] {
		set[col] = true
	}
	return set
}

/* RowCrypto 行级加解密助手：secure 写路径把敏感列替换为 enc:v1 信封，secure 读路径解密回明文。
 * 业务 DAO 按当前库类型处理：明文库原样读写，密文库经本助手转换（设计 3.5 字段类型调整）。 */
type RowCrypto struct {
	svc *seccrypto.Service
}

func NewRowCrypto(svc *seccrypto.Service) *RowCrypto {
	return &RowCrypto{svc: svc}
}

func (c *RowCrypto) Enabled() bool { return c.svc != nil }

/* IndexKey 取本厂盲索引密钥（手机号 hash 计算用）；密钥材料仅内存使用，不得落库或写日志。 */
func (c *RowCrypto) IndexKey(ctx context.Context, tenantID string) ([]byte, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("row crypto not configured")
	}
	return c.svc.Material(ctx, tenantID, seccrypto.KeyTypeIndex, 0)
}

/* ForSecure 把明文行转换为密文库写入值；非敏感列与 nil 原样保留。空字符串照常加密（与 NULL 区分）。 */
func (c *RowCrypto) ForSecure(ctx context.Context, tenantID, table, pk string, values map[string]any) (map[string]any, error) {
	if !c.Enabled() {
		return nil, fmt.Errorf("row crypto not configured")
	}
	sensitive := sensitiveSet(table)
	out := make(map[string]any, len(values))
	for col, v := range values {
		if !sensitive[col] || v == nil {
			out[col] = v
			continue
		}
		text, ok := v.(string)
		if !ok {
			/* NUMERIC/DATE 等先序列化为 UTF-8 字符串（设计 3.5）；非字符串类型暂不支持 */
			return nil, fmt.Errorf("sensitive column %s.%s expects string value, got %T", table, col, v)
		}
		stored, err := c.svc.EncryptField(ctx, tenantID, table, pk, col, []byte(text))
		if err != nil {
			return nil, fmt.Errorf("encrypt %s.%s: %w", table, col, err)
		}
		out[col] = stored
	}
	return out, nil
}

/* FromSecure 把密文库读出的行解密回明文（服务端解密后按权限返回明文或脱敏值，设计 10.2）。
 * 非加密值原样返回；明文/密文混存的迁移过渡期，明文值直接透传。 */
func (c *RowCrypto) FromSecure(ctx context.Context, tenantID, table, pk string, values map[string]any) (map[string]any, error) {
	if !c.Enabled() {
		return values, nil
	}
	sensitive := sensitiveSet(table)
	out := make(map[string]any, len(values))
	for col, v := range values {
		text, ok := v.(string)
		if !ok || !sensitive[col] || !seccrypto.IsEncrypted(text) {
			out[col] = v
			continue
		}
		plain, err := c.svc.DecryptField(ctx, tenantID, table, pk, col, text)
		if err != nil {
			return nil, fmt.Errorf("decrypt %s.%s: %w", table, col, err)
		}
		out[col] = plain
	}
	return out, nil
}
