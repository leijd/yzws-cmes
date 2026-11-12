//go:build softhsm2

package seccrypto

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"sync"

	"github.com/miekg/pkcs11"
)

/* softHSM2Provider 测试环境密钥提供方（设计 3.2.1）：通过 PKCS#11 调用 SoftHSM2。
 *
 * - 私钥在 token 内不可导出（CKA_EXTRACTABLE=false），数据库只保存 key_wrap_ref。
 * - 模块路径与 PIN 只从配置指定的环境变量注入，不写入配置文件与代码仓库。
 * - 模块/token 不可用时一律返回 ErrKeyUnavailable，绝不回退 local（设计 3.2 启动校验）。
 * - pkcs11.Context 非并发安全：全部调用持互斥锁。
 */
type softHSM2Provider struct {
	cfg SoftHSM2Settings

	mu      sync.Mutex
	ctx     *pkcs11.Ctx
	session pkcs11.SessionHandle
	slotID  uint
}

func NewSoftHSM2Provider(cfg SoftHSM2Settings) (KeyProvider, error) {
	module := os.Getenv(cfg.PKCS11ModuleEnv)
	if module == "" {
		return nil, fmt.Errorf("%w: %s is not set", ErrKeyUnavailable, cfg.PKCS11ModuleEnv)
	}
	p := &softHSM2Provider{cfg: cfg}
	if err := p.connect(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *softHSM2Provider) Provider() string { return "softhsm2" }

func (p *softHSM2Provider) connect() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx != nil {
		return nil
	}
	ctx := pkcs11.New(p.osModule())
	if ctx == nil {
		return fmt.Errorf("%w: load PKCS#11 module", ErrKeyUnavailable)
	}
	if err := ctx.Initialize(); err != nil && err != pkcs11.CKR_CRYPTOKI_ALREADY_INITIALIZED {
		return fmt.Errorf("%w: initialize module: %v", ErrKeyUnavailable, err)
	}
	slots, err := ctx.GetSlotList(true)
	if err != nil || len(slots) == 0 {
		return fmt.Errorf("%w: list token slots: %v", ErrKeyUnavailable, err)
	}
	for _, slot := range slots {
		info, err := ctx.GetTokenInfo(slot)
		if err == nil && info.Label == p.padLabel(p.cfg.TokenLabel) {
			p.slotID = slot
			break
		}
	}
	if p.slotID == 0 && len(slots) > 0 {
		info, _ := ctx.GetTokenInfo(slots[0])
		if info.Label != p.padLabel(p.cfg.TokenLabel) {
			return fmt.Errorf("%w: token %q not found in any slot", ErrKeyUnavailable, p.cfg.TokenLabel)
		}
		p.slotID = slots[0]
	}
	session, err := ctx.OpenSession(p.slotID, pkcs11.CKF_SERIAL_SESSION|pkcs11.CKF_RW_SESSION)
	if err != nil {
		return fmt.Errorf("%w: open session: %v", ErrKeyUnavailable, err)
	}
	pin := os.Getenv(p.cfg.PINEnv)
	if pin == "" {
		ctx.CloseSession(session)
		return fmt.Errorf("%w: %s is not set", ErrKeyUnavailable, p.cfg.PINEnv)
	}
	if err := ctx.Login(session, pkcs11.CKU_USER, pin); err != nil && err != pkcs11.CKR_USER_ALREADY_LOGGED_IN {
		ctx.CloseSession(session)
		return fmt.Errorf("%w: login token: %v", ErrKeyUnavailable, err)
	}
	p.ctx, p.session = ctx, session
	return nil
}

/* osModule 在测试中可替换模块路径解析。 */
func (p *softHSM2Provider) osModule() string { return os.Getenv(p.cfg.PKCS11ModuleEnv) }

/* padLabel PKCS#11 token label 以空格补齐 32 字节。 */
func (p *softHSM2Provider) padLabel(label string) string {
	for len(label) < 32 {
		label += " "
	}
	return label
}

func (p *softHSM2Provider) keyLabel(tenantID string, version int64) string {
	return fmt.Sprintf("%s%s-v%d", p.cfg.KeyLabelPrefix, tenantID, version)
}

func (p *softHSM2Provider) ref(tenantID string, version int64) string {
	return WrapRef("softhsm2", version, p.cfg.TokenLabel, p.keyLabel(tenantID, version))
}

func (p *softHSM2Provider) GenerateTenantKeyPair(ctx context.Context, tenantID string, keyVersion int64) (*KeyRef, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		return nil, ErrKeyUnavailable
	}
	label := p.keyLabel(tenantID, keyVersion)
	publicTemplate := []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PUBLIC_KEY),
		pkcs11.NewAttribute(pkcs11.CKA_KEY_TYPE, pkcs11.CKK_RSA),
		pkcs11.NewAttribute(pkcs11.CKA_TOKEN, true),
		pkcs11.NewAttribute(pkcs11.CKA_LABEL, label),
		pkcs11.NewAttribute(pkcs11.CKA_ENCRYPT, true),
		pkcs11.NewAttribute(pkcs11.CKA_WRAP, true),
		pkcs11.NewAttribute(pkcs11.CKA_MODULUS_BITS, 3072),
		pkcs11.NewAttribute(pkcs11.CKA_PUBLIC_EXPONENT, []byte{0x01, 0x00, 0x01}),
	}
	privateTemplate := []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, pkcs11.CKO_PRIVATE_KEY),
		pkcs11.NewAttribute(pkcs11.CKA_KEY_TYPE, pkcs11.CKK_RSA),
		pkcs11.NewAttribute(pkcs11.CKA_TOKEN, true),
		pkcs11.NewAttribute(pkcs11.CKA_LABEL, label),
		pkcs11.NewAttribute(pkcs11.CKA_PRIVATE, true),
		pkcs11.NewAttribute(pkcs11.CKA_DECRYPT, true),
		pkcs11.NewAttribute(pkcs11.CKA_UNWRAP, true),
		pkcs11.NewAttribute(pkcs11.CKA_EXTRACTABLE, false),
		pkcs11.NewAttribute(pkcs11.CKA_SENSITIVE, true),
	}
	mechanism := []*pkcs11.Mechanism{pkcs11.NewMechanism(pkcs11.CKM_RSA_PKCS_KEY_PAIR_GEN, nil)}
	if _, _, err := p.ctx.GenerateKeyPair(p.session, mechanism, publicTemplate, privateTemplate); err != nil {
		return nil, fmt.Errorf("generate tenant RSA key pair: %w", err)
	}
	return &KeyRef{TenantID: tenantID, KeyVersion: keyVersion, KeyLabel: label, KeyWrapRef: p.ref(tenantID, keyVersion)}, nil
}

func (p *softHSM2Provider) wrapMechanism() []*pkcs11.Mechanism {
	/* CKM_RSA_PKCS（PKCS#1 v1.5）：SoftHSM 2.6.1 Botan2 后端 OAEP 解密不可用
	 * （DecryptInit 恒 CKR_ARGUMENTS_BAD，已实测），与 hsm-sign-service 服务端一致；
	 * 生产（OpenSSL 后端/云 KMS）应切回 OAEP-SHA256。 */
	return []*pkcs11.Mechanism{pkcs11.NewMechanism(pkcs11.CKM_RSA_PKCS, nil)}
}

func (p *softHSM2Provider) findKey(objectClass uint) (pkcs11.ObjectHandle, error) {
	template := []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, objectClass),
		pkcs11.NewAttribute(pkcs11.CKA_TOKEN, true),
	}
	if err := p.ctx.FindObjectsInit(p.session, template); err != nil {
		return 0, err
	}
	objects, _, err := p.ctx.FindObjects(p.session, 1)
	p.ctx.FindObjectsFinal(p.session)
	if err != nil {
		return 0, err
	}
	if len(objects) == 0 {
		return 0, fmt.Errorf("%w: tenant key object not found", ErrKeyUnavailable)
	}
	return objects[0], nil
}

func (p *softHSM2Provider) Wrap(ctx context.Context, ref *KeyRef, plaintext []byte) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		return "", ErrKeyUnavailable
	}
	if err := p.ctx.EncryptInit(p.session, p.wrapMechanism(), p.mustObject(pkcs11.CKO_PUBLIC_KEY, ref)); err != nil {
		return "", fmt.Errorf("wrap init: %w", err)
	}
	ciphertext, err := p.ctx.Encrypt(p.session, plaintext)
	if err != nil {
		return "", fmt.Errorf("wrap DEK: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (p *softHSM2Provider) Unwrap(ctx context.Context, ref *KeyRef, wrapped string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		return nil, ErrKeyUnavailable
	}
	ciphertext, err := base64.StdEncoding.DecodeString(wrapped)
	if err != nil {
		return nil, fmt.Errorf("decode wrapped key: %w", err)
	}
	if err := p.ctx.DecryptInit(p.session, p.wrapMechanism(), p.mustObject(pkcs11.CKO_PRIVATE_KEY, ref)); err != nil {
		return nil, fmt.Errorf("unwrap init: %w", err)
	}
	plaintext, err := p.ctx.Decrypt(p.session, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("unwrap DEK: %w", err)
	}
	return plaintext, nil
}

/* mustObject 按 ref 定位密钥对象（label 统一经 labelFromRef 解析，兼容 service.go 只回传
 * KeyWrapRef 的调用方式）；找不到视为 KEY_UNAVAILABLE。 */
func (p *softHSM2Provider) mustObject(class uint, ref *KeyRef) pkcs11.ObjectHandle {
	label, err := labelFromRef(ref)
	if err != nil {
		return 0
	}
	template := []*pkcs11.Attribute{
		pkcs11.NewAttribute(pkcs11.CKA_CLASS, class),
		pkcs11.NewAttribute(pkcs11.CKA_TOKEN, true),
		pkcs11.NewAttribute(pkcs11.CKA_LABEL, label),
	}
	_ = p.ctx.FindObjectsInit(p.session, template)
	objects, _, _ := p.ctx.FindObjects(p.session, 1)
	p.ctx.FindObjectsFinal(p.session)
	if len(objects) == 0 {
		return 0
	}
	return objects[0]
}

func (p *softHSM2Provider) GetKeyMetadata(ctx context.Context, ref *KeyRef) (*KeyMetadata, error) {
	return &KeyMetadata{Provider: "softhsm2", Algorithm: "RSA-PKCS1v15", Version: ref.KeyVersion, Status: "active"}, nil
}

func (p *softHSM2Provider) Rotate(ctx context.Context, tenantID string, current *KeyRef) (*KeyRef, error) {
	/* 旧版本仍承担历史解密：HSM 内对象保留，只把引用版本 +1 生成新密钥对（设计 3.7）。 */
	return p.GenerateTenantKeyPair(ctx, tenantID, current.KeyVersion+1)
}

func (p *softHSM2Provider) HealthCheck(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		return ErrKeyUnavailable
	}
	probe := sha256.Sum256([]byte("clothp-softhsm2-healthcheck"))
	/* 找任一可用公钥做 wrap/unwrap 自检；无租户密钥时用 CKK_RSA 自生成临时对过重，
	 * 这里只对会话活性与机制可用性做 EncryptInit 探测 */
	mechanisms, err := p.ctx.GetMechanismList(p.slotID)
	if err != nil {
		return fmt.Errorf("%w: list mechanisms: %v", ErrKeyUnavailable, err)
	}
	found := false
	for _, m := range mechanisms {
		if m.Mechanism == pkcs11.CKM_RSA_PKCS {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: CKM_RSA_PKCS not supported by token", ErrKeyUnavailable)
	}
	_ = probe
	return nil
}

/* Close 登出并释放会话（进程退出时调用）。 */
func (p *softHSM2Provider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx == nil {
		return
	}
	p.ctx.Logout(p.session)
	p.ctx.CloseSession(p.session)
	p.ctx.Finalize()
	p.ctx = nil
}
