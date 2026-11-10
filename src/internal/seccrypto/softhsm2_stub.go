//go:build !softhsm2

package seccrypto

import (
	"context"
	"fmt"
)

/* 默认构建（无 cgo/无 PKCS#11 模块，如 Windows 开发机）不包含 SoftHSM2 适配器。
 * 测试环境（Linux + SoftHSM2）用 -tags softhsm2 构建启用真实 PKCS#11 实现（softhsm2.go）。
 * 未启用时一律返回 ErrKey_UNAVAILABLE，绝不回退 local（设计 3.2 启动校验）。 */
type softHSM2Provider struct {
	cfg SoftHSM2Settings
}

func NewSoftHSM2Provider(cfg SoftHSM2Settings) (KeyProvider, error) {
	return &softHSM2Provider{cfg: cfg}, nil
}

func (p *softHSM2Provider) Provider() string { return "softhsm2" }

func (p *softHSM2Provider) unavailable() error {
	return fmt.Errorf("%w: build without softhsm2 tag (requires cgo + PKCS#11 module)", ErrKeyUnavailable)
}

func (p *softHSM2Provider) GenerateTenantKeyPair(ctx context.Context, tenantID string, keyVersion int64) (*KeyRef, error) {
	return nil, p.unavailable()
}

func (p *softHSM2Provider) Wrap(ctx context.Context, ref *KeyRef, plaintext []byte) (string, error) {
	return "", p.unavailable()
}

func (p *softHSM2Provider) Unwrap(ctx context.Context, ref *KeyRef, wrapped string) ([]byte, error) {
	return nil, p.unavailable()
}

func (p *softHSM2Provider) GetKeyMetadata(ctx context.Context, ref *KeyRef) (*KeyMetadata, error) {
	return nil, p.unavailable()
}

func (p *softHSM2Provider) Rotate(ctx context.Context, tenantID string, current *KeyRef) (*KeyRef, error) {
	return nil, p.unavailable()
}

func (p *softHSM2Provider) HealthCheck(ctx context.Context) error {
	return p.unavailable()
}
