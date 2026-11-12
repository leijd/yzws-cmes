// Package seccrypto 字段级加密与密钥适配（设计文档 3.2.1/3.4/3.5/3.6）。
//
// 信封加密：每厂 RSA-3072 密钥对保护随机 DEK（数据密钥），业务字段由 DEK 做 AES-256-GCM；
// 密文编码 enc:v1:<key_version>:<base64url(nonce||ciphertext||tag)>，AAD 绑定租户/表/行/字段/版本。
// 手机号等值查询使用 HMAC-SHA256 盲索引（tenant_index_key + 域分隔串），不保存明文索引。
//
// KeyProvider 屏蔽 SoftHSM2（PKCS#11）与云端 KMS 差异；local 兼容适配器只用于解包旧数据并迁移，
// 禁止用于新密钥。SoftHSM2 不可用不得回退 local，涉及密文的读写返回 KEY_UNAVAILABLE。
package seccrypto

import (
	"context"
	"errors"
	"fmt"
)

/* ErrKeyUnavailable 密钥提供方不可用（模块缺失、token 不可用、云凭据失效等）。不得自动降级。 */
var ErrKeyUnavailable = errors.New("KEY_UNAVAILABLE: key provider unavailable")

/* ErrProviderReadOnly local 兼容适配器只用于迁移旧数据，禁止生成新密钥。 */
var ErrProviderReadOnly = errors.New("local key provider is migration-only and cannot generate new keys")

/* KeyProvider 统一密钥适配接口（设计 3.2.1）。业务加密服务只依赖本接口。 */
type KeyProvider interface {
	Provider() string // softhsm2 | cloud_kms | local

	/* GenerateTenantKeyPair 为本厂生成 RSA-3072 密钥对，返回不可导出私钥引用（HSM token 内 / 云 KMS）。 */
	GenerateTenantKeyPair(ctx context.Context, tenantID string, keyVersion int64) (*KeyRef, error)

	/* Wrap 使用本厂公钥包装 DEK/盲索引密钥，返回可落库的包装密文。 */
	Wrap(ctx context.Context, ref *KeyRef, plaintext []byte) (string, error)

	/* Unwrap 使用私钥引用解包，返回明文密钥材料（仅存在于内存，不得落库/写日志）。 */
	Unwrap(ctx context.Context, ref *KeyRef, wrapped string) ([]byte, error)

	GetKeyMetadata(ctx context.Context, ref *KeyRef) (*KeyMetadata, error)

	/* Rotate 创建新密钥对与引用；旧版本置 rotated，仍承担历史解密，不得删除。 */
	Rotate(ctx context.Context, tenantID string, current *KeyRef) (*KeyRef, error)

	/* HealthCheck 检查模块/token/云凭据并做一次实际包装/解包自检。 */
	HealthCheck(ctx context.Context) error
}

/* KeyRef 密钥引用：数据库只保存 KeyWrapRef，永不保存私钥/PIN/云 Secret。 */
type KeyRef struct {
	TenantID   string
	KeyVersion int64
	KeyLabel   string // HSM key label 或云 key_id
	KeyWrapRef string // softhsm2:<token_label>:<key_label>:<version> | cloud_kms:<provider>:<key_id>:<version> | local-master:v1
}

/* KeyMetadata 密钥元数据。 */
type KeyMetadata struct {
	Provider  string `json:"provider"`
	Algorithm string `json:"algorithm"` // RSA-3072-OAEP-SHA256
	Version   int64  `json:"version"`
	Status    string `json:"status"` // active | rotated | revoked
}

/* NewProvider 按配置创建密钥提供方。
 * kms_provider=softhsm2 时：server_url 非空走远程模式（HTTP 调用 hsm-sign-service，
 * 参考 softhsm-2.6-go/client-example，无本机 PKCS#11 库也可用）；否则加载本地 PKCS#11 模块
 * （模块路径与 PIN 只从指定环境变量注入）。
 * cloud_kms 为预留占位（返回未接入错误，不在测试环境伪装已接入）；local 仅迁移旧数据。 */
func NewProvider(cfg KMSProviderConfig) (KeyProvider, error) {
	switch cfg.Provider {
	case "softhsm2":
		if cfg.SoftHSM2.ServerURL != "" {
			return NewHSMServiceProvider(cfg.SoftHSM2)
		}
		return NewSoftHSM2Provider(cfg.SoftHSM2)
	case "cloud_kms":
		return NewCloudKMSProvider(cfg.CloudKMS), nil
	case "local":
		if cfg.JWTSecret == "" {
			return nil, fmt.Errorf("local key provider requires jwt secret for migration unwrap")
		}
		return NewLocalProvider(cfg.JWTSecret), nil
	default:
		return nil, fmt.Errorf("unsupported kms_provider %q", cfg.Provider)
	}
}

/* KMSProviderConfig 密钥提供方配置（对应 config.CryptoConfig，避免 config 包反向依赖）。 */
type KMSProviderConfig struct {
	Provider  string
	JWTSecret string
	SoftHSM2  SoftHSM2Settings
	CloudKMS  CloudKMSSettings
}

type SoftHSM2Settings struct {
	PKCS11ModuleEnv string // 环境变量名（模块路径只从环境注入；本地 PKCS#11 模式用）
	TokenLabel      string // 落库 KeyWrapRef 的 token 标识（两种模式都用）
	PINEnv          string // 环境变量名（本地 PKCS#11 模式用）
	KeyLabelPrefix  string
	/* 远程模式（server_url 非空时生效）：HTTP 调用 hsm-sign-service，
	 * 客户端实现参考 softhsm-2.6-go/client-example，无需本机 PKCS#11 库。 */
	ServerURL  string
	APIKeyEnv  string // 环境变量名，X-API-Key 只从环境注入
	CACertFile string // 自签 TLS 的服务端 CA 证书（可选）
	TimeoutMS  int    // 远程调用超时（默认 10000）
	/* 租户密钥自动开通（可选）：未开通的租户（服务端 404）经 SSH 执行开通脚本。
	 * 密钥生成只允许运维脚本（HTTP 无此接口），故需免密 SSH 到 HSM 服务器；
	 * 留空则未开通租户直接报 KEY_UNAVAILABLE。 */
	SSHTarget       string // 如 root@服务器IP；生产后端与 HSM 同机可用 root@127.0.0.1
	ProvisionScript string // 服务端开通脚本路径（默认 /app/HSM/softhsm2/scripts/add-tenant.sh）
}

type CloudKMSSettings struct {
	Provider    string
	Region      string
	KeyIDPrefix string
}

/* WrapRef 生成落库引用。 */
func WrapRef(provider string, version int64, parts ...string) string {
	ref := provider
	for _, p := range parts {
		ref += ":" + p
	}
	return fmt.Sprintf("%s:%d", ref, version)
}
