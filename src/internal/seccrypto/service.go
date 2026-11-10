package seccrypto

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"
)

/* Service 业务加密服务：按厂加载密钥材料（DEK/盲索引密钥），提供字段信封加解密与盲索引。
 *
 * - 密钥材料从 KeyStore 读取包装密文后用对应 KeyProvider 解包，仅在内存短期缓存（ttl 默认 5 分钟）。
 * - 提供方不可用返回 ErrKeyUnavailable，绝不回退 local 解包新数据；local 仅用于 key_ref=local-master:v1 的旧记录迁移。
 * - 本服务不直接依赖 pgx：KeyStore 由调用方（如 dualwrite 仓库）实现。
 */

/* KeyType 密钥材料类型。 */
type KeyType string

const (
	KeyTypeDEK    KeyType = "dek"    // 数据加密密钥（AES-256-GCM 字段加密）
	KeyTypeIndex  KeyType = "index"  // 手机号盲索引 HMAC 密钥
)

/* TenantKeyRecord 库存密钥记录（tenant_crypto_key 行）。 */
type TenantKeyRecord struct {
	TenantID    string
	KeyVersion  int64
	KeyType     KeyType
	Provider    string // local | softhsm2 | cloud_kms
	WrappedKey  string
	KeyWrapRef  string
	Status      string // active | rotated | revoked
}

/* KeyStore 由调用方实现：从库中读取指定版本（version=0 表示当前 active）的密钥记录。 */
type KeyStore interface {
	LoadTenantKey(ctx context.Context, tenantID string, keyType KeyType, version int64) (*TenantKeyRecord, error)
}

type Service struct {
	provider KeyProvider
	local    KeyProvider // 仅解包 local-master:v1 旧记录
	store    KeyStore
	ttl      time.Duration

	mu       sync.Mutex
	material map[string]cachedMaterial
}

type cachedMaterial struct {
	key       []byte
	expiresAt time.Time
}

func NewService(provider, local KeyProvider, store KeyStore, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Service{provider: provider, local: local, store: store, ttl: ttl, material: map[string]cachedMaterial{}}
}

var errNoActiveKey = fmt.Errorf("no active tenant key")

/* unwrapKey 解包密钥材料：local-master 引用走 local 兼容适配器，其余走当前提供方。 */
func (s *Service) unwrapKey(ctx context.Context, rec *TenantKeyRecord) ([]byte, error) {
	if rec.KeyWrapRef == "local-master:v1" && s.local != nil {
		return s.local.Unwrap(ctx, &KeyRef{TenantID: rec.TenantID, KeyVersion: rec.KeyVersion, KeyWrapRef: rec.KeyWrapRef}, rec.WrappedKey)
	}
	if s.provider == nil {
		return nil, ErrKeyUnavailable
	}
	return s.provider.Unwrap(ctx, &KeyRef{TenantID: rec.TenantID, KeyVersion: rec.KeyVersion, KeyLabel: rec.KeyWrapRef, KeyWrapRef: rec.KeyWrapRef}, rec.WrappedKey)
}

/* Material 返回指定类型/版本的密钥材料（带短期缓存）。 */
func (s *Service) Material(ctx context.Context, tenantID string, keyType KeyType, version int64) ([]byte, error) {
	cacheKey := fmt.Sprintf("%s:%s:%d", tenantID, keyType, version)
	s.mu.Lock()
	if hit, ok := s.material[cacheKey]; ok && time.Now().Before(hit.expiresAt) {
		s.mu.Unlock()
		return hit.key, nil
	}
	s.mu.Unlock()

	rec, err := s.store.LoadTenantKey(ctx, tenantID, keyType, version)
	if err != nil {
		return nil, err
	}
	key, err := s.unwrapKey(ctx, rec)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.material[cacheKey] = cachedMaterial{key: key, expiresAt: time.Now().Add(s.ttl)}
	s.mu.Unlock()
	return key, nil
}

/* ActiveVersion 返回当前 active 版本号（不存在返回 0）。 */
func (s *Service) ActiveVersion(ctx context.Context, tenantID string, keyType KeyType) (int64, error) {
	rec, err := s.store.LoadTenantKey(ctx, tenantID, keyType, 0)
	if err != nil {
		if err == errNoActiveKey {
			return 0, nil
		}
		return 0, err
	}
	return rec.KeyVersion, nil
}

/* EncryptField 加密一个业务字段，返回 enc:v1 信封。 */
func (s *Service) EncryptField(ctx context.Context, tenantID, table, primaryKey, column string, plaintext []byte) (string, error) {
	dek, err := s.Material(ctx, tenantID, KeyTypeDEK, 0)
	if err != nil {
		return "", err
	}
	return Encrypt(dek, tenantID, table, primaryKey, column, plaintext)
}

/* DecryptField 解密字段信封；keyVersion=0 时从信封解析版本选择对应 DEK。 */
func (s *Service) DecryptField(ctx context.Context, tenantID, table, primaryKey, column, stored string) (string, error) {
	version := EnvelopeKeyVersion(stored)
	if version == 0 {
		return stored, nil // 明文/NULL 原样返回（迁移过渡期）
	}
	dek, err := s.Material(ctx, tenantID, KeyTypeDEK, version)
	if err != nil {
		return "", err
	}
	plaintext, err := Decrypt(dek, tenantID, table, primaryKey, column, stored)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

/* PhoneHash 计算手机号盲索引（自动规范化）。 */
func (s *Service) PhoneHash(ctx context.Context, tenantID, domain, phone string) (string, error) {
	indexKey, err := s.Material(ctx, tenantID, KeyTypeIndex, 0)
	if err != nil {
		return "", err
	}
	return PhoneHash(indexKey, domain, phone), nil
}

/* BlindIndexForPlatform 平台级盲索引（登录标识安全统计等，设计 4.9）：使用 platform 主体盲索引密钥。 */
func (s *Service) BlindIndexForPlatform(ctx context.Context, domain, value string) (string, error) {
	indexKey, err := s.Material(ctx, "platform", KeyTypeIndex, 0)
	if err != nil {
		return "", err
	}
	return BlindIndex(indexKey, domain, NormalizePhone(value)), nil
}

/* IssueKeys 发行首版密钥材料：随机 DEK + 独立盲索引密钥。
 * 当前提供方为 SoftHSM2/云 KMS 时用其包装；提供方为 local（过渡阶段，尚未接入 HSM）或不可用时，
 * 用 local 兼容适配器按既有 local-master:v1 格式包装（与 ensureTenantCryptoKey 生成的旧记录同格式），
 * 迁移到 HSM 后再按设计 3.7 轮换。tenantID="platform" 表示平台级密钥主体（设计 3.4）。 */
func (s *Service) IssueKeys(ctx context.Context, issuer Issuer, tenantID, actorID string, now int64) error {
	dek := make([]byte, 32)
	indexKey := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return err
	}
	if _, err := rand.Read(indexKey); err != nil {
		return err
	}
	legacy := s.provider == nil || s.provider.Provider() == "local"
	wrap := func(material []byte, keyType KeyType) error {
		if legacy {
			if s.local == nil {
				return ErrKeyUnavailable
			}
			wrapped, err := s.local.Wrap(ctx, &KeyRef{TenantID: tenantID, KeyVersion: 1, KeyWrapRef: "local-master:v1"}, material)
			if err != nil {
				return err
			}
			return issuer.InsertKey(ctx, tenantID, 1, keyType, "local", wrapped, "local-master:v1", actorID, now)
		}
		ref, err := s.provider.GenerateTenantKeyPair(ctx, tenantID, 1)
		if err != nil {
			return err
		}
		wrapped, err := s.provider.Wrap(ctx, ref, material)
		if err != nil {
			return err
		}
		return issuer.InsertKey(ctx, tenantID, ref.KeyVersion, keyType, s.provider.Provider(), wrapped, ref.KeyWrapRef, actorID, now)
	}
	if err := wrap(dek, KeyTypeDEK); err != nil {
		return err
	}
	if err := wrap(indexKey, KeyTypeIndex); err != nil {
		return err
	}
	s.Invalidate(tenantID)
	return nil
}

/* EnsureKeys 指定主体已有 active DEK 则跳过，否则发行（转换/回填前的幂等准备）。 */
func (s *Service) EnsureKeys(ctx context.Context, issuer Issuer, tenantID, actorID string, now int64) error {
	rec, err := s.store.LoadTenantKey(ctx, tenantID, KeyTypeDEK, 0)
	if err == nil && rec != nil && rec.Status == "active" {
		if _, errIdx := s.store.LoadTenantKey(ctx, tenantID, KeyTypeIndex, 0); errIdx == nil {
			return nil
		}
	}
	return s.IssueKeys(ctx, issuer, tenantID, actorID, now)
}

/* Issuer 由调用方实现：在同一业务事务内插入 tenant_crypto_key 行。 */
type Issuer interface {
	InsertKey(ctx context.Context, tenantID string, keyVersion int64, keyType KeyType, provider, wrapped, wrapRef, actorID string, now int64) error
}

/* Invalidate 清掉某厂缓存（密钥轮换/发行后调用）。 */
func (s *Service) Invalidate(tenantID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k := range s.material {
		if len(k) > len(tenantID) && k[:len(tenantID)] == tenantID && k[len(tenantID)] == ':' {
			delete(s.material, k)
		}
	}
}
