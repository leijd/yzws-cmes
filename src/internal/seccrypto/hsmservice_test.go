package seccrypto

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/* mockTenantHSM 模拟多租户 hsm-sign-service（真实 RSA PKCS#1 v1.5，按租户管理内存密钥对）。
 * tenants 为预开通的租户 ID；未开通租户 /pubkey、/unwrap 返回 404（对齐真实服务端）。
 * 返回的 addTenant 模拟 add-tenant.sh 生效（动态开通租户，供自动开通用例使用）。 */
func mockTenantHSM(t *testing.T, apiKey string, tenants ...string) (*httptest.Server, KeyProvider, func(string)) {
	t.Helper()
	keys := map[string]*rsa.PrivateKey{}
	addTenant := func(id string) {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		keys[id] = priv
	}
	for _, id := range tenants {
		addTenant(id)
	}
	authed := func(w http.ResponseWriter, r *http.Request) bool {
		if apiKey != "" && r.Header.Get("X-API-Key") != apiKey {
			http.Error(w, "未授权", http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /tenants", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		m := map[string]string{}
		for id := range keys {
			m[id] = "lic-t" + id
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(m)
	})
	mux.HandleFunc("GET /pubkey", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		tenantID := r.URL.Query().Get("tenant")
		priv, ok := keys[tenantID]
		if !ok {
			http.Error(w, "未知租户: "+tenantID, http.StatusNotFound)
			return
		}
		der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
		if err != nil {
			http.Error(w, "marshal fail", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Write(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	})
	mux.HandleFunc("POST /unwrap", func(w http.ResponseWriter, r *http.Request) {
		if !authed(w, r) {
			return
		}
		tenantID := r.Header.Get("X-Tenant-ID")
		priv, ok := keys[tenantID]
		if !ok {
			http.Error(w, "未知租户: "+tenantID, http.StatusNotFound)
			return
		}
		data, _ := io.ReadAll(r.Body)
		ct, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(data)))
		if err != nil {
			http.Error(w, "请求体须为 base64 密文", http.StatusBadRequest)
			return
		}
		pt, err := rsa.DecryptPKCS1v15(rand.Reader, priv, ct)
		if err != nil {
			http.Error(w, "unwrap 失败", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(pt)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("CLOTHP_TEST_HSM_API_KEY", apiKey)
	p, err := NewHSMServiceProvider(SoftHSM2Settings{
		ServerURL:  srv.URL,
		APIKeyEnv:  "CLOTHP_TEST_HSM_API_KEY",
		TokenLabel: "yzws-mes-test",
		TimeoutMS:  5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv, p, addTenant
}

/* 设计 3.2.1（远程多租户形态）：探测开通 → 本地公钥包装 → 服务端私钥解包，往返一致。 */
func TestHSMServiceWrapUnwrap(t *testing.T) {
	_, p, _ := mockTenantHSM(t, "k1", "t1")
	ctx := context.Background()
	if p.Provider() != "softhsm2" {
		t.Fatal("provider name must remain softhsm2")
	}
	ref, err := p.GenerateTenantKeyPair(ctx, "t1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if ref.KeyWrapRef != "softhsm2:yzws-mes-test:t1:1" {
		t.Fatalf("bad wrap ref %q", ref.KeyWrapRef)
	}
	wrapped, err := p.Wrap(ctx, ref, []byte("dek-material-0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base64.StdEncoding.DecodeString(wrapped); err != nil {
		t.Fatalf("WrappedKey 不是合法 base64: %v", err)
	}
	plain, err := p.Unwrap(ctx, ref, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "dek-material-0123456789abcdef" {
		t.Fatalf("round trip mismatch: %q", plain)
	}
	if err := p.HealthCheck(ctx); err != nil {
		t.Fatal(err)
	}
}

/* 未开通租户（服务端 404 未知租户）必须映射 ErrKeyUnavailable，不得静默成功。 */
func TestHSMServiceUnknownTenant(t *testing.T) {
	_, p, _ := mockTenantHSM(t, "k1", "t1")
	if _, err := p.GenerateTenantKeyPair(context.Background(), "t-unknown", 1); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("expected ErrKeyUnavailable for unknown tenant, got %v", err)
	}
}

/* 鉴权失败（401）与服务端错误（5xx）一律映射 ErrKeyUnavailable。 */
func TestHSMServiceUnavailable(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failing.Close()
	p, err := NewHSMServiceProvider(SoftHSM2Settings{ServerURL: failing.URL, TokenLabel: "yzws-mes-test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := p.HealthCheck(ctx); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("expected ErrKeyUnavailable, got %v", err)
	}

	/* 401：mock 期望 expected-key，客户端带 wrong-key */
	srv, _, _ := mockTenantHSM(t, "expected-key")
	t.Setenv("CLOTHP_TEST_HSM_API_KEY", "wrong-key")
	wrongKey, err := NewHSMServiceProvider(SoftHSM2Settings{
		ServerURL: srv.URL, APIKeyEnv: "CLOTHP_TEST_HSM_API_KEY", TokenLabel: "yzws-mes-test", TimeoutMS: 5000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongKey.GenerateTenantKeyPair(ctx, "t1", 1); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("expected ErrKeyUnavailable on auth failure, got %v", err)
	}
}

/* 多租户架构下轮换由运维在服务端改注册表完成，provider 只读。 */
func TestHSMServiceRotateReadOnly(t *testing.T) {
	_, p, _ := mockTenantHSM(t, "k1", "t1")
	_, err := p.Rotate(context.Background(), "t1", &KeyRef{TenantID: "t1", KeyVersion: 1})
	if !errors.Is(err, ErrProviderReadOnly) {
		t.Fatalf("expected ErrProviderReadOnly, got %v", err)
	}
}

/* NewProvider 按 server_url 分发到远程/本地形态。 */
func TestNewProviderRemoteDispatch(t *testing.T) {
	remote, err := NewProvider(KMSProviderConfig{Provider: "softhsm2", SoftHSM2: SoftHSM2Settings{
		ServerURL: "http://127.0.0.1:1", TokenLabel: "yzws-mes-test",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := remote.(*hsmServiceProvider); !ok {
		t.Fatalf("expected *hsmServiceProvider, got %T", remote)
	}
}

/* 自动开通：未开通租户（404）且配置 ssh_target 时，经 sshRunFn 执行开通脚本后轮询生效。
 * 与集成测试/生产共用同一条代码路径（脚本幂等）。 */
func TestHSMServiceAutoProvision(t *testing.T) {
	_, p, addTenant := mockTenantHSM(t, "k1") // 无预开通租户
	hp := p.(*hsmServiceProvider)
	hp.sshTarget = "root@test"
	hp.provisionScript = "/scripts/add-tenant.sh"

	var called string
	old := sshRunFn
	sshRunFn = func(target, script string, args ...string) ([]byte, error) {
		called = target + " " + script + " " + strings.Join(args, " ")
		addTenant(args[0]) // 模拟脚本生效（注册表热加载后公钥可取）
		return []byte("已生成密钥"), nil
	}
	defer func() { sshRunFn = old }()

	ref, err := p.GenerateTenantKeyPair(context.Background(), "t-new", 1)
	if err != nil {
		t.Fatal(err)
	}
	if called != "root@test /scripts/add-tenant.sh t-new" {
		t.Fatalf("ssh 调用不符预期: %q", called)
	}
	if ref.KeyWrapRef != "softhsm2:yzws-mes-test:t-new:1" {
		t.Fatalf("bad wrap ref %q", ref.KeyWrapRef)
	}
	/* 开通后应能完成包装/解包往返 */
	wrapped, err := p.Wrap(context.Background(), ref, []byte("dek-material"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := p.Unwrap(context.Background(), ref, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != "dek-material" {
		t.Fatalf("round trip mismatch: %q", plain)
	}
}

/* 5xx/网络错误重试（对标 client-example：隧道抖动常见），前两次 500 第三次成功。 */
func TestHSMServiceRetryOn5xx(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 3 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()
	p, err := NewHSMServiceProvider(SoftHSM2Settings{ServerURL: srv.URL, TokenLabel: "yzws-mes-test", TimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("expected 3 attempts (2 retries), got %d", n)
	}
}

/* 4xx（鉴权/参数/未知租户）立即失败不重试。 */
func TestHSMServiceNoRetryOn4xx(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		http.Error(w, "未授权", http.StatusUnauthorized)
	}))
	defer srv.Close()
	p, err := NewHSMServiceProvider(SoftHSM2Settings{ServerURL: srv.URL, TokenLabel: "yzws-mes-test", TimeoutMS: 5000})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.HealthCheck(context.Background()); !errors.Is(err, ErrKeyUnavailable) {
		t.Fatalf("expected ErrKeyUnavailable, got %v", err)
	}
	if n != 1 {
		t.Fatalf("4xx 不应重试, got %d requests", n)
	}
}
