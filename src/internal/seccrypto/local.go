package seccrypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

/* localProvider 旧数据迁移兼容适配器（设计 3.2.1 表"旧数据迁移"行）。
 *
 * 兼容既有 tenant_crypto_key 记录（tenantreg.ensureTenantCryptoKey 生成）：
 *
 *	KEK   = SHA256("yzws-tenant-crypto:v1:" + JWTSecret)
 *	存库  = base64.StdEncoding(nonce || AES-256-GCM(KEK).Seal(DEK))
 *	ref   = local-master:v1
 *
 * 仅允许 Wrap/Unwrap 既有引用用于读取旧数据并迁移到新提供方；GenerateTenantKeyPair/Rotate 一律拒绝。
 */
type localProvider struct {
	masterKey [32]byte
}

func NewLocalProvider(jwtSecret string) KeyProvider {
	return &localProvider{masterKey: sha256.Sum256([]byte("yzws-tenant-crypto:v1:" + jwtSecret))}
}

func (p *localProvider) Provider() string { return "local" }

func (p *localProvider) GenerateTenantKeyPair(ctx context.Context, tenantID string, keyVersion int64) (*KeyRef, error) {
	return nil, fmt.Errorf("%w: local-master", ErrProviderReadOnly)
}

func (p *localProvider) Wrap(ctx context.Context, ref *KeyRef, plaintext []byte) (string, error) {
	if ref.KeyWrapRef != "local-master:v1" {
		return "", fmt.Errorf("local provider cannot wrap ref %q", ref.KeyWrapRef)
	}
	block, err := aes.NewCipher(p.masterKey[:])
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
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, plaintext, nil)), nil
}

func (p *localProvider) Unwrap(ctx context.Context, ref *KeyRef, wrapped string) ([]byte, error) {
	if ref.KeyWrapRef != "local-master:v1" {
		return nil, fmt.Errorf("local provider cannot unwrap ref %q", ref.KeyWrapRef)
	}
	raw, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, fmt.Errorf("decode wrapped key: %w", err)
	}
	block, err := aes.NewCipher(p.masterKey[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize()+gcm.Overhead() {
		return nil, fmt.Errorf("wrapped key too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("unwrap tenant key: %w", err)
	}
	return plaintext, nil
}

func (p *localProvider) GetKeyMetadata(ctx context.Context, ref *KeyRef) (*KeyMetadata, error) {
	return &KeyMetadata{Provider: "local", Algorithm: "AES-256-GCM(local-master)", Version: ref.KeyVersion, Status: "active"}, nil
}

func (p *localProvider) Rotate(ctx context.Context, tenantID string, current *KeyRef) (*KeyRef, error) {
	return nil, fmt.Errorf("%w: local-master", ErrProviderReadOnly)
}

func (p *localProvider) HealthCheck(ctx context.Context) error {
	/* 自检：包装再解包一段已知明文 */
	probe := []byte("clothp-local-healthcheck")
	ref := &KeyRef{KeyWrapRef: "local-master:v1"}
	wrapped, err := p.Wrap(ctx, ref, probe)
	if err != nil {
		return err
	}
	plain, err := p.Unwrap(ctx, ref, wrapped)
	if err != nil {
		return err
	}
	if string(plain) != string(probe) {
		return fmt.Errorf("local provider self-check mismatch")
	}
	return nil
}
