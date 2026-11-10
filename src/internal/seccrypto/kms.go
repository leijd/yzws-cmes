package seccrypto

import (
	"context"
	"fmt"
)

/* cloudKMSProvider 生产预留云端 KMS 适配器（设计 3.2.1）。
 *
 * 本版只定义与 SoftHSM2 相同的 KeyProvider 契约，不在测试环境伪装已接入：
 * 任何调用都返回 ErrKeyUnavailable（CLOUD_KMS_NOT_CONFIGURED），
 * 业务加密服务依赖接口而非具体提供方，接入云厂商 SDK 时无需修改业务代码。
 */
type cloudKMSProvider struct {
	cfg CloudKMSSettings
}

func NewCloudKMSProvider(cfg CloudKMSSettings) KeyProvider {
	return &cloudKMSProvider{cfg: cfg}
}

func (p *cloudKMSProvider) Provider() string { return "cloud_kms" }

func (p *cloudKMSProvider) notConfigured() error {
	if p.cfg.Provider == "" {
		return fmt.Errorf("%w: cloud_kms provider not configured", ErrKeyUnavailable)
	}
	return fmt.Errorf("%w: cloud_kms provider %q adapter not implemented in this release", ErrKeyUnavailable, p.cfg.Provider)
}

func (p *cloudKMSProvider) GenerateTenantKeyPair(ctx context.Context, tenantID string, keyVersion int64) (*KeyRef, error) {
	return nil, p.notConfigured()
}

func (p *cloudKMSProvider) Wrap(ctx context.Context, ref *KeyRef, plaintext []byte) (string, error) {
	return "", p.notConfigured()
}

func (p *cloudKMSProvider) Unwrap(ctx context.Context, ref *KeyRef, wrapped string) ([]byte, error) {
	return nil, p.notConfigured()
}

func (p *cloudKMSProvider) GetKeyMetadata(ctx context.Context, ref *KeyRef) (*KeyMetadata, error) {
	return nil, p.notConfigured()
}

func (p *cloudKMSProvider) Rotate(ctx context.Context, tenantID string, current *KeyRef) (*KeyRef, error) {
	return nil, p.notConfigured()
}

func (p *cloudKMSProvider) HealthCheck(ctx context.Context) error {
	return p.notConfigured()
}
