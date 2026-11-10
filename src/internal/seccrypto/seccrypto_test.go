package seccrypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
)

func testDEK(t *testing.T) []byte {
	t.Helper()
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatal(err)
	}
	return dek
}

/* 设计 3.5：信封往返 + AAD 绑定租户/表/行/字段。 */
func TestEnvelopeRoundTrip(t *testing.T) {
	dek := testDEK(t)
	stored, err := Encrypt(dek, "t1", "employee", "e1", "phone", []byte("13800138000"))
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(stored) {
		t.Fatalf("expected encrypted envelope, got %q", stored)
	}
	plain, err := Decrypt(dek, "t1", "employee", "e1", "phone", stored)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "13800138000" {
		t.Fatalf("round trip mismatch: %q", plain)
	}
}

/* 设计 3.5/10.2：密文复制到他厂/行/字段必须 AAD 校验失败。 */
func TestEnvelopeAADBinding(t *testing.T) {
	dek := testDEK(t)
	stored, err := Encrypt(dek, "t1", "employee", "e1", "phone", []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ tenant, table, pk, col string }{
		{"t2", "employee", "e1", "phone"},
		{"t1", "tenant", "e1", "phone"},
		{"t1", "employee", "e2", "phone"},
		{"t1", "employee", "e1", "id_card_no"},
	} {
		if _, err := Decrypt(dek, tc.tenant, tc.table, tc.pk, tc.col, stored); err != ErrCiphertextInvalid {
			t.Fatalf("expected AAD failure for %+v, got %v", tc, err)
		}
	}
}

/* 设计 3.5：每次密文不同；空字符串与 NULL 语义区分。 */
func TestEnvelopeRandomizedAndEmptyDistinct(t *testing.T) {
	dek := testDEK(t)
	a, _ := Encrypt(dek, "t1", "employee", "e1", "phone", []byte("same"))
	b, _ := Encrypt(dek, "t1", "employee", "e1", "phone", []byte("same"))
	if a == b {
		t.Fatal("ciphertext should be randomized (random nonce)")
	}
	empty, err := Encrypt(dek, "t1", "employee", "e1", "phone", []byte(""))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Decrypt(dek, "t1", "employee", "e1", "phone", empty)
	if err != nil || len(plain) != 0 {
		t.Fatalf("empty string must be encryptable distinctly, got %q err=%v", plain, err)
	}
	if EnvelopeKeyVersion("not-encrypted") != 0 || EnvelopeKeyVersion(empty) != 1 {
		t.Fatal("version parse failed")
	}
}

/* 设计 3.6：盲索引确定性、域分隔、规范化。 */
func TestBlindIndex(t *testing.T) {
	key := testDEK(t)
	h1 := PhoneHash(key, "employee.phone", "138 0013-8000")
	h2 := PhoneHash(key, "employee.phone", "13800138000")
	if h1 != h2 {
		t.Fatal("normalization mismatch")
	}
	if PhoneHash(key, "employee.phone", "13800138000") != PhoneHash(key, "employee.phone", "13800138000") {
		t.Fatal("blind index must be deterministic")
	}
	if PhoneHash(key, "employee.phone", "13800138000") == PhoneHash(key, "tenant.contact_phone", "13800138000") {
		t.Fatal("domain separation failed")
	}
	if PhoneHash(key, "employee.phone", "13800138001") == h1 {
		t.Fatal("different phones must hash differently")
	}
}

/* 设计 3.2.1：local 适配器必须能解包既有 ensureTenantCryptoKey 写入的格式。 */
func TestLocalProviderCompatibility(t *testing.T) {
	secret := "test-jwt-secret"
	// 复刻 tenantreg.ensureTenantCryptoKey 的包装过程
	dek := []byte("0123456789abcdef0123456789abcdef")
	master := sha256.Sum256([]byte("yzws-tenant-crypto:v1:" + secret))
	block, _ := aes.NewCipher(master[:])
	gcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(nonce)
	wrapped := base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, dek, nil))

	p := NewLocalProvider(secret)
	ref := &KeyRef{TenantID: "t1", KeyVersion: 1, KeyWrapRef: "local-master:v1"}
	plain, err := p.Unwrap(context.Background(), ref, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(dek) {
		t.Fatal("local unwrap mismatch with legacy format")
	}
	// 回包兼容
	rewrapped, err := p.Wrap(context.Background(), ref, dek)
	if err != nil {
		t.Fatal(err)
	}
	back, err := p.Unwrap(context.Background(), ref, rewrapped)
	if err != nil || string(back) != string(dek) {
		t.Fatal("local wrap/unwrap round trip failed")
	}
}

/* 设计 3.2.1：local 仅迁移，禁止生成新密钥与轮换。 */
func TestLocalProviderMigrationOnly(t *testing.T) {
	p := NewLocalProvider("s")
	if _, err := p.GenerateTenantKeyPair(context.Background(), "t1", 1); !errors.Is(err, ErrProviderReadOnly) {
		t.Fatalf("expected ErrProviderReadOnly, got %v", err)
	}
	if _, err := p.Rotate(context.Background(), "t1", &KeyRef{KeyWrapRef: "local-master:v1"}); !errors.Is(err, ErrProviderReadOnly) {
		t.Fatalf("expected ErrProviderReadOnly on rotate, got %v", err)
	}
}

/* cloud_kms 预留：任何调用都不可用，不得伪装已接入。 */
func TestCloudKMSNotConfigured(t *testing.T) {
	p := NewCloudKMSProvider(CloudKMSSettings{})
	if _, err := p.Wrap(context.Background(), &KeyRef{}, nil); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("expected ErrKeyUnavailable, got %v", err)
	}
}
