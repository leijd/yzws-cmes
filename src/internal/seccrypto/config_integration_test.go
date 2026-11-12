package seccrypto_test

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
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clothp/internal/config"
	"clothp/internal/seccrypto"
)

/* newMockHSMServer 模拟多租户 hsm-sign-service（真实 RSA PKCS#1 v1.5，按租户管理内存密钥对），
 * 使本测试验证的是 config.yaml 驱动的真实客户端代码路径与真实密码学算法。
 * tenants 为预开通租户；未开通租户 /pubkey、/unwrap 返回 404（对齐真实服务端）。 */
func newMockHSMServer(t *testing.T, apiKey string, tenants ...string) *httptest.Server {
	t.Helper()
	keys := map[string]*rsa.PrivateKey{}
	for _, id := range tenants {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		keys[id] = priv
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
	return srv
}

/* 内存 KeyStore/Issuer：按 tenant_crypto_key 语义存放发行的密钥记录。 */
type memKeyStore struct{ recs map[seccrypto.KeyType]*seccrypto.TenantKeyRecord }

func (m *memKeyStore) LoadTenantKey(ctx context.Context, tenantID string, keyType seccrypto.KeyType, version int64) (*seccrypto.TenantKeyRecord, error) {
	if rec, ok := m.recs[keyType]; ok {
		return rec, nil
	}
	return nil, fmt.Errorf("no active tenant key")
}

type memIssuer struct{ store *memKeyStore }

func (m *memIssuer) InsertKey(ctx context.Context, tenantID string, keyVersion int64, keyType seccrypto.KeyType, provider, wrapped, wrapRef, actorID string, now int64) error {
	m.store.recs[keyType] = &seccrypto.TenantKeyRecord{
		TenantID: tenantID, KeyVersion: keyVersion, KeyType: keyType,
		Provider: provider, WrappedKey: wrapped, KeyWrapRef: wrapRef, Status: "active",
	}
	return nil
}

/* 验证 config.yaml 的配置能正确处理信息项加密解密：
 * 真实加载仓库内 config.yaml，走与 cmd/server/main.go 相同的装配路径。
 * 默认 mock 模式（server_url 临时替换到本机模拟 HSM）；
 * CLOTHP_TEST_REAL_HSM=1 时直连 config.yaml 中的服务器（SSH 隧道地址），
 * 需环境中已设置与服务端一致的 CLOTHP_HSM_API_KEY，且该租户已用 add-tenant.sh 开通。 */
func TestConfigYAMLFieldEncryptionRoundTrip(t *testing.T) {
	tenantID := os.Getenv("CLOTHP_TEST_TENANT")
	if tenantID == "" {
		tenantID = "t1"
	}
	realHSM := os.Getenv("CLOTHP_TEST_REAL_HSM") == "1"
	var srv *httptest.Server
	if realHSM {
		if os.Getenv("CLOTHP_HSM_API_KEY") == "" {
			t.Skip("真实模式需要环境中已设置 CLOTHP_HSM_API_KEY")
		}
	} else {
		t.Setenv("CLOTHP_HSM_API_KEY", "integration-test-key")
		srv = newMockHSMServer(t, "integration-test-key", tenantID)
	}

	cfg, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml 加载失败: %v", err)
	}
	if cfg.Crypto.DataAlgorithm != "AES-256-GCM" || cfg.Crypto.KMSProvider != "softhsm2" {
		t.Fatalf("config.yaml 加密配置异常: data=%s provider=%s", cfg.Crypto.DataAlgorithm, cfg.Crypto.KMSProvider)
	}

	/* 控制台输出 config.yaml 的关键加密配置（api_key 只输出环境变量名与注入状态，不输出值） */
	scheme := strings.SplitN(cfg.Crypto.SoftHSM2.ServerURL, ":", 2)[0]
	apiKeySet := "未设置（服务端将拒绝请求）"
	if os.Getenv(cfg.Crypto.SoftHSM2.APIKeyEnv) != "" {
		apiKeySet = "已设置"
	}
	fmt.Printf("[config.yaml] kms_provider=%s  server_url=%s (协议 %s)  api_key_env=%s (%s)  ca_cert_file=%q\n",
		cfg.Crypto.KMSProvider, cfg.Crypto.SoftHSM2.ServerURL, scheme,
		cfg.Crypto.SoftHSM2.APIKeyEnv, apiKeySet, cfg.Crypto.SoftHSM2.CACertFile)
	fmt.Printf("[config.yaml] 字段算法=%s  密钥包装=%s  envelope=%s  测试租户=%s\n",
		cfg.Crypto.DataAlgorithm, cfg.Crypto.WrapAlgorithm, cfg.Crypto.EnvelopeVersion, tenantID)
	if srv != nil {
		fmt.Printf("[运行模式] mock：server_url 已临时替换为 %s，配置里的服务器地址不参与本次验证\n", srv.URL)
	} else {
		fmt.Printf("[运行模式] 真实服务器：直连 config.yaml 中的地址验证\n")
	}

	if srv != nil {
		cfg.Crypto.SoftHSM2.ServerURL = srv.URL
		cfg.Crypto.SoftHSM2.CACertFile = ""
	}

	/* 与 cmd/server/main.go 相同的 provider 装配；
	 * CLOTHP_TEST_SSH_TARGET（真实模式可选）经 ssh_target 启用生产同款自动开通能力 */
	sshTarget := os.Getenv("CLOTHP_TEST_SSH_TARGET")
	provider, err := seccrypto.NewProvider(seccrypto.KMSProviderConfig{
		Provider: cfg.Crypto.KMSProvider,
		SoftHSM2: seccrypto.SoftHSM2Settings{
			TokenLabel:     cfg.Crypto.SoftHSM2.TokenLabel,
			KeyLabelPrefix: cfg.Crypto.SoftHSM2.KeyLabelPrefix,
			ServerURL:      cfg.Crypto.SoftHSM2.ServerURL,
			APIKeyEnv:      cfg.Crypto.SoftHSM2.APIKeyEnv,
			CACertFile:     cfg.Crypto.SoftHSM2.CACertFile,
			TimeoutMS:      cfg.Crypto.SoftHSM2.TimeoutMS,
			SSHTarget:      sshTarget,
		},
	})
	if err != nil {
		t.Fatalf("按 config.yaml 装配密钥提供方失败: %v", err)
	}
	if provider.Provider() != "softhsm2" {
		t.Fatalf("provider=%q, want softhsm2（远程 HSM 模式）", provider.Provider())
	}
	ctx := context.Background()
	if err := provider.HealthCheck(ctx); err != nil {
		t.Fatalf("HSM 健康检查失败: %v", err)
	}

	/* 密钥发行前探测租户密钥是否已在服务端开通；ssh_target 非空时由 provider
	 * 自动经 SSH 执行 add-tenant.sh 开通并轮询生效（生产同款代码路径）。 */
	if _, err := provider.GenerateTenantKeyPair(ctx, tenantID, 1); err != nil {
		if realHSM && errors.Is(err, seccrypto.ErrKeyUnavailable) {
			hint := "请设置 CLOTHP_TEST_SSH_TARGET=root@服务器IP（需免密）启用自动开通，或手工执行 add-tenant.sh"
			if sshTarget != "" {
				hint = "自动开通已尝试但失败（检查 ssh 免密 / add-tenant.sh 输出），或执行 docker compose restart sign-svc 后重试"
			}
			t.Skipf("租户 %s 密钥不可用：%s\n（原始错误: %v）", tenantID, hint, err)
		}
		t.Fatalf("租户密钥探测失败: %v", err)
	}

	/* 完整业务闭环：包装 DEK/盲索引密钥（本地公钥 OAEP）落库 → 字段加解密 → 盲索引 */
	store := &memKeyStore{recs: map[seccrypto.KeyType]*seccrypto.TenantKeyRecord{}}
	svc := seccrypto.NewService(provider, seccrypto.NewLocalProvider(cfg.JWTSecret), store, 5*time.Minute)
	if err := svc.IssueKeys(ctx, &memIssuer{store}, tenantID, "integration-test", time.Now().Unix()); err != nil {
		t.Fatalf("密钥发行失败: %v", err)
	}

	/* 落库记录必须是 softhsm2 远程包装格式，且包装密文不含明文密钥材料 */
	rec := store.recs[seccrypto.KeyTypeDEK]
	if rec == nil || rec.Provider != "softhsm2" {
		t.Fatalf("DEK 落库 provider=%v, want softhsm2", rec)
	}
	if !strings.HasPrefix(rec.KeyWrapRef, "softhsm2:"+cfg.Crypto.SoftHSM2.TokenLabel+":") {
		t.Fatalf("KeyWrapRef=%q 不符合 softhsm2:<token>:<tenantID>:<version> 格式", rec.KeyWrapRef)
	}
	if _, err := base64.StdEncoding.DecodeString(rec.WrappedKey); err != nil {
		t.Fatalf("WrappedKey 不是合法 base64 包装密文: %v", err)
	}

	/* 信息项加密：密文必须是 enc:v1 信封且不含明文 */
	stored, err := svc.EncryptField(ctx, tenantID, "employee", "e1", "phone", []byte("13800138000"))
	if err != nil {
		t.Fatalf("字段加密失败: %v", err)
	}
	if !seccrypto.IsEncrypted(stored) {
		t.Fatalf("密文不是 enc:v1 信封: %q", stored)
	}
	if strings.Contains(stored, "13800138000") {
		t.Fatal("密文中泄漏明文")
	}

	/* 信息项解密：往返一致；AAD 绑定（换行必须失败） */
	plain, err := svc.DecryptField(ctx, tenantID, "employee", "e1", "phone", stored)
	if err != nil {
		t.Fatalf("字段解密失败: %v", err)
	}
	if plain != "13800138000" {
		t.Fatalf("解密往返不一致: %q", plain)
	}
	if _, err := svc.DecryptField(ctx, tenantID, "employee", "e2", "phone", stored); err == nil {
		t.Fatal("AAD 绑定失效：换主键后解密竟成功")
	}

	/* 盲索引：规范化后确定性一致 */
	h1, err := svc.PhoneHash(ctx, tenantID, "employee.phone", "138 0013-8000")
	if err != nil {
		t.Fatal(err)
	}
	h2, err := svc.PhoneHash(ctx, tenantID, "employee.phone", "13800138000")
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatal("手机号盲索引规范化不一致")
	}
}
