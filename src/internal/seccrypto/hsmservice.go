package seccrypto

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

/* hsmServiceProvider 远程 SoftHSM2 提供方（设计 3.2.1 的远程形态），对接多租户
 * hsm-sign-service（softhsm-2.6-go/hsm-sign-service，多租户签名服务-使用说明.md）：
 *
 * - 租户路由：X-Tenant-ID 头；租户→密钥映射由服务端注册表（tenants.json）热加载，
 *   密钥生成/删除不进服务（只允许运维在 HSM 容器内执行，见 add-tenant.sh）。
 * - GenerateTenantKeyPair = 探测该租户密钥是否已开通（GET /pubkey?tenant=）：
 *   已开通返回 KeyRef；未开通且配置了 ssh_target 时自动经 SSH 执行 add-tenant.sh
 *   开通（密钥生成只允许运维脚本，HTTP 无此接口）并轮询生效；未配置则返回
 *   ErrKeyUnavailable（提示先开通租户密钥）。
 * - Wrap（包装 DEK/盲索引密钥）：本地用租户公钥做 RSA PKCS#1 v1.5——公钥公开，
 *   无需服务端参与，也无须把密钥材料发往服务器（测试环境 SoftHSM Botan2 后端
 *   OAEP 解密不可用，生产应切回 OAEP-SHA256）。
 * - Unwrap（解包）：POST /unwrap，由服务端用租户私钥在 HSM 内解密。
 * - Rotate：需运维在服务端轮换注册表后递增版本，本提供方不自动生成（ErrProviderReadOnly）。
 * - 网络抖动/5xx 自动重试（对标 client-example：隧道 RST 常见，4xx 不重试）；
 *   服务不可达、鉴权失败、未知租户一律返回 ErrKeyUnavailable，绝不回退 local。
 * - 客户端实现参考 softhsm-2.6-go/hsm-sign-service/client-example（X-API-Key、可选 CA、
 *   超时、多 IP 回退拨号），Windows 开发机经 SSH 隧道（http://127.0.0.1:8443）即可使用。
 */
type hsmServiceProvider struct {
	server          string
	apiKey          string
	tokenLabel      string
	sshTarget       string // 非空时租户未开通可自动经 SSH 执行 provisionScript（如 root@服务器IP）
	provisionScript string // 服务端开通脚本路径（默认 /app/HSM/softhsm2/scripts/add-tenant.sh）
	http            *http.Client

	mu       sync.Mutex
	pubCache map[string]*rsa.PublicKey // tenantID -> 公钥（本地包装用）
}

func NewHSMServiceProvider(cfg SoftHSM2Settings) (KeyProvider, error) {
	server := strings.TrimRight(cfg.ServerURL, "/")
	if server == "" {
		return nil, fmt.Errorf("%w: crypto.softhsm2 server_url is empty", ErrKeyUnavailable)
	}
	timeout := time.Duration(cfg.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	transport := &http.Transport{
		TLSHandshakeTimeout: 5 * time.Second,
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     60 * time.Second,
		// 多 IP 回退拨号（参考 client-example）：解析全部地址，IPv4 优先逐个尝试
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			var v4, v6 []net.IPAddr
			for _, ip := range ips {
				if ip.IP.To4() != nil {
					v4 = append(v4, ip)
				} else {
					v6 = append(v6, ip)
				}
			}
			var lastErr error
			for _, ip := range append(v4, v6...) {
				d := net.Dialer{Timeout: 2 * time.Second}
				c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.IP.String(), port))
				if err == nil {
					return c, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
	if cfg.CACertFile != "" {
		pemBytes, err := os.ReadFile(cfg.CACertFile)
		if err != nil {
			return nil, fmt.Errorf("read hsm ca cert %s: %w", cfg.CACertFile, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("parse hsm ca cert %s failed", cfg.CACertFile)
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	apiKey := ""
	if cfg.APIKeyEnv != "" {
		apiKey = os.Getenv(cfg.APIKeyEnv)
	}
	provisionScript := cfg.ProvisionScript
	if provisionScript == "" {
		provisionScript = "/app/HSM/softhsm2/scripts/add-tenant.sh"
	}
	return &hsmServiceProvider{
		server:          server,
		apiKey:          apiKey,
		tokenLabel:      cfg.TokenLabel,
		sshTarget:       cfg.SSHTarget,
		provisionScript: provisionScript,
		http:            &http.Client{Timeout: timeout, Transport: transport},
		pubCache:        map[string]*rsa.PublicKey{},
	}, nil
}

func (p *hsmServiceProvider) Provider() string { return "softhsm2" }

func (p *hsmServiceProvider) ref(tenantID string, version int64) string {
	return WrapRef("softhsm2", version, p.tokenLabel, tenantID)
}

/* hsmHTTPError 服务端返回的非 200 响应，携带状态码以区分"未开通（404，可自动开通）"
 * 与其他错误；Unwrap 到 ErrKeyUnavailable，调用方用法不变。 */
type hsmHTTPError struct {
	path   string
	status int
	body   string
}

func (e *hsmHTTPError) Error() string {
	return fmt.Sprintf("hsm service %s returned %d: %s", e.path, e.status, e.body)
}
func (e *hsmHTTPError) Unwrap() error { return ErrKeyUnavailable }

/* do 发送带 X-API-Key 的请求，最多 3 次（对标 client-example 的稳定性设计）：
 * 网络错误（隧道 RST、超时）与 5xx/429 指数退避重试，4xx 立即失败不重试；
 * 最终失败统一映射 ErrKeyUnavailable。 */
func (p *hsmServiceProvider) do(ctx context.Context, method, path string, headers map[string]string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(200*(1<<(attempt-1)))*time.Millisecond +
				time.Duration(mrand.Intn(200))*time.Millisecond
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: hsm service %s: %v", ErrKeyUnavailable, path, ctx.Err())
			case <-time.After(backoff):
			}
		}
		respBody, retriable, err := p.doOnce(ctx, method, path, headers, body)
		if err == nil {
			return respBody, nil
		}
		lastErr = err
		if !retriable {
			return nil, err
		}
	}
	return nil, lastErr
}

func (p *hsmServiceProvider) doOnce(ctx context.Context, method, path string, headers map[string]string, body []byte) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.server+path, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("X-API-Key", p.apiKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.http.Do(req)
	if err != nil {
		// 网络层错误（RST、超时、DNS 抖动）→ 可重试
		return nil, true, fmt.Errorf("%w: hsm service %s: %v", ErrKeyUnavailable, path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusOK {
		return respBody, false, nil
	}
	herr := &hsmHTTPError{path: path, status: resp.StatusCode, body: strings.TrimSpace(string(respBody))}
	// 4xx（鉴权/参数/未知租户）重试无意义；5xx/429 可重试
	retriable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
	return nil, retriable, herr
}

/* tenantPub 取租户公钥（缓存）：GET /pubkey?tenant=<id>。 */
func (p *hsmServiceProvider) tenantPub(ctx context.Context, tenantID string) (*rsa.PublicKey, error) {
	p.mu.Lock()
	if pub, ok := p.pubCache[tenantID]; ok {
		p.mu.Unlock()
		return pub, nil
	}
	p.mu.Unlock()

	body, err := p.do(ctx, http.MethodGet, "/pubkey?tenant="+url.QueryEscape(tenantID), nil, nil)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(body)
	if block == nil {
		return nil, fmt.Errorf("%w: hsm service returned invalid PEM for tenant %q", ErrKeyUnavailable, tenantID)
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: parse tenant public key: %v", ErrKeyUnavailable, err)
	}
	pub, ok := parsed.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: tenant key for %q is not RSA", ErrKeyUnavailable, tenantID)
	}
	p.mu.Lock()
	p.pubCache[tenantID] = pub
	p.mu.Unlock()
	return pub, nil
}

/* sshRunFn 经 SSH 在 HSM 服务器上执行运维脚本（需免密）。包级变量便于测试替换。 */
var sshRunFn = func(target, script string, args ...string) ([]byte, error) {
	argv := append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", target, script}, args...)
	return exec.Command("ssh", argv...).CombinedOutput()
}

/* GenerateTenantKeyPair 多租户架构下密钥生成不进 HTTP 服务（只允许运维脚本在 HSM
 * 容器内执行）。本方法探测该租户密钥是否已开通：已开通返回 KeyRef（KeyWrapRef =
 * softhsm2:<token_label>:<tenantID>:<version>，第三段为路由用租户 ID）。
 * 未开通（服务端 404 未知租户）且配置了 ssh_target 时，自动经 SSH 执行
 * add-tenant.sh 开通并轮询生效（SoftHSM 新对象对 sign-svc 可见有秒级延迟）；
 * 未配置 ssh_target 则返回 ErrKeyUnavailable（提示先开通租户密钥）。 */
func (p *hsmServiceProvider) GenerateTenantKeyPair(ctx context.Context, tenantID string, keyVersion int64) (*KeyRef, error) {
	if _, err := p.tenantPub(ctx, tenantID); err != nil {
		var herr *hsmHTTPError
		if !errors.As(err, &herr) || herr.status != http.StatusNotFound || p.sshTarget == "" {
			return nil, err
		}
		if err := p.provisionTenant(ctx, tenantID); err != nil {
			return nil, err
		}
	}
	return &KeyRef{TenantID: tenantID, KeyVersion: keyVersion, KeyWrapRef: p.ref(tenantID, keyVersion)}, nil
}

/* provisionTenant 自动开通租户密钥：SSH 执行 add-tenant.sh（幂等），随后轮询
 * /pubkey?tenant= 直至公钥可取（最长约 25 秒；超时提示重启 sign-svc 刷新对象缓存）。 */
func (p *hsmServiceProvider) provisionTenant(ctx context.Context, tenantID string) error {
	out, err := sshRunFn(p.sshTarget, p.provisionScript, tenantID)
	if err != nil {
		return fmt.Errorf("%w: auto-provision tenant %s via ssh failed: %v: %s",
			ErrKeyUnavailable, tenantID, err, strings.TrimSpace(string(out)))
	}
	for i := 0; i < 25; i++ {
		if _, err := p.tenantPub(ctx, tenantID); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: waiting tenant key %s: %v", ErrKeyUnavailable, tenantID, ctx.Err())
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("%w: tenant %s provisioned but pubkey not visible after 25s (may need: docker compose restart sign-svc)",
		ErrKeyUnavailable, tenantID)
}

/* Wrap 本地用租户公钥包装密钥材料（PKCS#1 v1.5），返回 base64.Std 密文（与本地
 * PKCS#11 提供方落库格式一致）。公钥公开，包装不需服务端参与。
 * 测试环境 SoftHSM 2.6.1（Botan2 后端）OAEP 解密不可用，故用 v1.5，与服务端一致；
 * 生产（OpenSSL 后端/云 KMS）应随 wrap_algorithm 切回 OAEP-SHA256。 */
func (p *hsmServiceProvider) Wrap(ctx context.Context, ref *KeyRef, plaintext []byte) (string, error) {
	if ref == nil {
		return "", fmt.Errorf("%w: nil key ref", ErrKeyUnavailable)
	}
	pub, err := p.tenantPub(ctx, ref.TenantID)
	if err != nil {
		return "", err
	}
	ciphertext, err := rsa.EncryptPKCS1v15(rand.Reader, pub, plaintext)
	if err != nil {
		return "", fmt.Errorf("wrap with tenant public key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

/* Unwrap 由服务端在 HSM 内用租户私钥解密：POST /unwrap + X-Tenant-ID。 */
func (p *hsmServiceProvider) Unwrap(ctx context.Context, ref *KeyRef, wrapped string) ([]byte, error) {
	if ref == nil {
		return nil, fmt.Errorf("%w: nil key ref", ErrKeyUnavailable)
	}
	body, err := p.do(ctx, http.MethodPost, "/unwrap", map[string]string{"X-Tenant-ID": ref.TenantID}, []byte(wrapped))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (p *hsmServiceProvider) GetKeyMetadata(ctx context.Context, ref *KeyRef) (*KeyMetadata, error) {
	return &KeyMetadata{Provider: "softhsm2", Algorithm: "RSA-PKCS1v15", Version: ref.KeyVersion, Status: "active"}, nil
}

func (p *hsmServiceProvider) Rotate(ctx context.Context, tenantID string, current *KeyRef) (*KeyRef, error) {
	/* 轮换需运维在服务端生成新密钥并改注册表（热加载）后递增版本，本提供方不自动生成。 */
	return nil, fmt.Errorf("%w: rotate requires new key + registry update on hsm server (ops script)", ErrProviderReadOnly)
}

func (p *hsmServiceProvider) HealthCheck(ctx context.Context) error {
	body, err := p.do(ctx, http.MethodGet, "/healthz", nil, nil)
	if err != nil {
		return err
	}
	if !bytes.Equal(bytes.TrimSpace(body), []byte("ok")) {
		return fmt.Errorf("%w: hsm service healthz returned %q", ErrKeyUnavailable, strings.TrimSpace(string(body)))
	}
	return nil
}
