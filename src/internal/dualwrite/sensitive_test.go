package dualwrite

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"clothp/internal/seccrypto"
)

/* mockKeyStore：用 local 提供方包装固定 DEK/盲索引密钥，模拟 tenant_crypto_key 记录。 */
type mockKeyStore struct {
	dekWrapped string
	idxWrapped string
}

func (m *mockKeyStore) LoadTenantKey(ctx context.Context, tenantID string, keyType seccrypto.KeyType, version int64) (*seccrypto.TenantKeyRecord, error) {
	rec := &seccrypto.TenantKeyRecord{TenantID: tenantID, KeyVersion: 1, KeyType: keyType, Provider: "local", KeyWrapRef: "local-master:v1", Status: "active"}
	if keyType == seccrypto.KeyTypeDEK {
		rec.WrappedKey = m.dekWrapped
	} else {
		rec.WrappedKey = m.idxWrapped
	}
	return rec, nil
}

func newMockService(t *testing.T) *seccrypto.Service {
	t.Helper()
	dek := make([]byte, 32)
	idx := make([]byte, 32)
	_, _ = rand.Read(dek)
	_, _ = rand.Read(idx)
	local := seccrypto.NewLocalProvider("test-secret")
	wrapDEK, err := local.Wrap(context.Background(), &seccrypto.KeyRef{KeyWrapRef: "local-master:v1"}, dek)
	if err != nil {
		t.Fatal(err)
	}
	wrapIdx, err := local.Wrap(context.Background(), &seccrypto.KeyRef{KeyWrapRef: "local-master:v1"}, idx)
	if err != nil {
		t.Fatal(err)
	}
	return seccrypto.NewService(local, local, &mockKeyStore{dekWrapped: wrapDEK, idxWrapped: wrapIdx}, 0)
}

/* 设计 4.15：secure 写路径敏感列加密、非敏感列原样；secure 读路径解密还原。 */
func TestRowCryptoRoundTrip(t *testing.T) {
	c := NewRowCrypto(newMockService(t))
	row := map[string]any{
		"employee_no": "EMP001", // 非敏感：工号明文
		"name":        "张三",    // 非敏感
		"phone":       "13800138000",
		"id_card_no":  "440301199001011234",
		"bank_card":   nil, // NULL 不加密
	}
	secure, err := c.ForSecure(context.Background(), "t1", "employee", "EMP001", row)
	if err != nil {
		t.Fatal(err)
	}
	if secure["employee_no"] != "EMP001" || secure["name"] != "张三" {
		t.Fatal("non-sensitive columns must stay plaintext")
	}
	if secure["phone"] == row["phone"] {
		t.Fatal("phone must be encrypted")
	}
	if v := secure["bank_card"]; v != nil {
		t.Fatal("NULL must remain NULL")
	}

	back, err := c.FromSecure(context.Background(), "t1", "employee", "EMP001", secure)
	if err != nil {
		t.Fatal(err)
	}
	if back["phone"] != "13800138000" || back["id_card_no"] != "440301199001011234" {
		t.Fatalf("round trip mismatch: %+v", back)
	}
}

/* 设计 10.2：AAD 绑定主键，篡改行主键或租户后无法解密。 */
func TestRowCryptoAADBinding(t *testing.T) {
	c := NewRowCrypto(newMockService(t))
	secure, err := c.ForSecure(context.Background(), "t1", "employee", "e1", map[string]any{"phone": "13800138000"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FromSecure(context.Background(), "t1", "employee", "e2", secure); err == nil {
		t.Fatal("decrypt with different primary key must fail (AAD)")
	}
	if _, err := c.FromSecure(context.Background(), "t2", "employee", "e1", secure); err == nil {
		t.Fatal("decrypt with different tenant must fail (AAD)")
	}
}

/* 盲索引经 Service 计算：同号同域稳定，跨域/跨租户不同。 */
func TestRowCryptoPhoneHash(t *testing.T) {
	svc := newMockService(t)
	h1, err := svc.PhoneHash(context.Background(), "t1", "employee.phone", "138 0013-8000")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := svc.PhoneHash(context.Background(), "t1", "employee.phone", "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatal("normalized phone hash must be stable")
	}
	h3, _ := svc.PhoneHash(context.Background(), "t1", "tenant.contact_phone", "13800138000")
	if h1 == h3 {
		t.Fatal("domain separation failed")
	}
	/* 另一厂使用独立密钥材料（真实环境每厂密钥不同，此处用独立 service 实例模拟） */
	svc2 := newMockService(t)
	h4, _ := svc2.PhoneHash(context.Background(), "t2", "employee.phone", "13800138000")
	if h1 == h4 {
		t.Fatal("per-tenant index key required")
	}
}

/* 提供方不可用时加密必须失败（KEY_UNAVAILABLE），不得回退明文。 */
func TestRowCryptoKeyUnavailable(t *testing.T) {
	svc := seccrypto.NewService(nil, nil, &mockKeyStore{}, 0) // provider=nil
	c := NewRowCrypto(svc)
	if _, err := c.ForSecure(context.Background(), "t1", "employee", "e1", map[string]any{"phone": "x"}); !errors.Is(err, seccrypto.ErrKeyUnavailable) {
		t.Fatalf("expected KEY_UNAVAILABLE, got %v", err)
	}
}
