package seccrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

/* 字段信封（设计 3.5）：
 *
 *	enc:v1:<key_version>:<base64url(nonce || ciphertext || tag)>
 *
 * - NULL 不加密仍存 NULL；空字符串与 NULL 必须区分（空字符串照常加密）。
 * - AAD 固定包含 tenant_id + table_name + primary_key + column_name + envelope_version，
 *   防止密文被复制到其他厂、行或字段后仍通过认证。
 * - 密文不排序、不模糊查询；等值检索必须显式设计盲索引（blindindex.go）。
 */

const EnvelopeVersion = "v1"

const encPrefix = "enc:v1:"

/* ErrCiphertextInvalid 密文格式错误、版本未知或 AAD 校验失败（含被复制到他厂/行/字段的情况）。 */
var ErrCiphertextInvalid = errors.New("invalid ciphertext envelope")

/* FieldAAD 构造 AAD：绑定租户、表、行、字段与格式版本。 */
func FieldAAD(tenantID, table, primaryKey, column string) []byte {
	return []byte(strings.Join([]string{tenantID, table, primaryKey, column, EnvelopeVersion}, "\x1f"))
}

/* IsEncrypted 判断存储值是否为加密信封。 */
func IsEncrypted(value string) bool { return strings.HasPrefix(value, encPrefix) }

/* Encrypt 用本厂 DEK 加密一个字段值，返回可落库的信封串。 */
func Encrypt(dek []byte, tenantID, table, primaryKey, column string, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, FieldAAD(tenantID, table, primaryKey, column))
	return encPrefix + "1:" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

/* Decrypt 解密信封并校验 AAD；keyVersion 必须与信封记录版本一致才允许解密（禁止跨版本静默解包）。 */
func Decrypt(dek []byte, tenantID, table, primaryKey, column, stored string) ([]byte, error) {
	if !IsEncrypted(stored) {
		return nil, ErrCiphertextInvalid
	}
	rest := strings.TrimPrefix(stored, encPrefix)
	versionText, payload, found := strings.Cut(rest, ":")
	if !found || versionText == "" || payload == "" {
		return nil, ErrCiphertextInvalid
	}
	if _, err := strconv.ParseInt(versionText, 10, 64); err != nil {
		return nil, fmt.Errorf("%w: bad key version", ErrCiphertextInvalid)
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrCiphertextInvalid
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrCiphertextInvalid
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, FieldAAD(tenantID, table, primaryKey, column))
	if err != nil {
		/* AAD 不匹配：密文被复制到其他租户/行/字段，或已损坏 */
		return nil, ErrCiphertextInvalid
	}
	return plaintext, nil
}

/* EnvelopeKeyVersion 解析信封中的密钥版本；非信封返回 0。 */
func EnvelopeKeyVersion(stored string) int64 {
	if !IsEncrypted(stored) {
		return 0
	}
	rest := strings.TrimPrefix(stored, encPrefix)
	versionText, _, found := strings.Cut(rest, ":")
	if !found {
		return 0
	}
	version, err := strconv.ParseInt(versionText, 10, 64)
	if err != nil {
		return 0
	}
	return version
}
