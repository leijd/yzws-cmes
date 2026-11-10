package seccrypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strings"
)

/* 手机号盲索引（设计 3.6）：
 *
 *	base64url(HMAC-SHA256(tenant_index_key, domain + "\x00" + normalized_phone))
 *
 * - 规范化：去空格/连字符，本版中国大陆手机号为 11 位数字（NormalizePhone 只规范化不强制位数，校验在业务层）。
 * - 不同用途使用不同域分隔字符串，避免跨用途碰撞。
 * - 不使用无密钥 SHA-256：防止对低熵手机号做字典枚举。
 * - 身份证、银行卡等本版不提供等值查询，不建盲索引。
 */

var phoneStripPattern = regexp.MustCompile(`[\s\-()+]`)

/* NormalizePhone 手机号规范化：去空格、连字符、括号与 +，仅保留数字。 */
func NormalizePhone(phone string) string {
	return phoneStripPattern.ReplaceAllString(strings.TrimSpace(phone), "")
}

/* BlindIndex 计算带密钥 HMAC 盲索引。domain 形如 "employee.phone"、"tenant.contact_phone"。 */
func BlindIndex(indexKey []byte, domain, normalizedValue string) string {
	mac := hmac.New(sha256.New, indexKey)
	mac.Write([]byte(domain))
	mac.Write([]byte{0})
	mac.Write([]byte(normalizedValue))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

/* PhoneHash 手机号盲索引的便捷封装。 */
func PhoneHash(indexKey []byte, domain, phone string) string {
	return BlindIndex(indexKey, domain, NormalizePhone(phone))
}
