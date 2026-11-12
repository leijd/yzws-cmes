package dualwrite

import (
	"context"
	"fmt"

	"clothp/internal/datastore"
	"clothp/internal/seccrypto"
	"github.com/jackc/pgx/v5/pgxpool"
)

/* DBKeyStore 基于 datastore.Store 的 seccrypto.KeyStore 实现：从密钥表读取包装后的密钥材料。
 * tenant_crypto_key 两库同结构：运行期一律从主库 yzws-v1-sec 读取；
 * yzws-v1 仅作对账副本（由 datatool 转换/补偿保持同步），单库模式（未配置 secure）回退明文库。 */
type DBKeyStore struct {
	store *datastore.Store
}

func NewDBKeyStore(store *datastore.Store) *DBKeyStore {
	return &DBKeyStore{store: store}
}

/* readPool 密钥记录读取来源：默认主库（secure）；单库模式回退明文库。 */
func (s *DBKeyStore) readPool() *pgxpool.Pool {
	if s.store.Secure != nil {
		return s.store.Secure
	}
	return s.store.Plain
}

/* LoadTenantKey version=0 取当前 active 版本，否则取指定版本。 */
func (s *DBKeyStore) LoadTenantKey(ctx context.Context, tenantID string, keyType seccrypto.KeyType, version int64) (*seccrypto.TenantKeyRecord, error) {
	row := s.readPool().QueryRow(ctx, `SELECT tenant_id,key_version,wrap_key_type,key_provider,wrapped_key,key_wrap_ref,status
		FROM tenant_crypto_key
		WHERE tenant_id=$1 AND wrap_key_type=$2 AND (($3=0 AND status='active') OR key_version=$3)
		ORDER BY key_version DESC LIMIT 1`, tenantID, string(keyType), version)
	rec := &seccrypto.TenantKeyRecord{KeyType: keyType}
	var status string
	if err := row.Scan(&rec.TenantID, &rec.KeyVersion, &rec.KeyType, &rec.Provider, &rec.WrappedKey, &rec.KeyWrapRef, &status); err != nil {
		return nil, fmt.Errorf("load tenant key %s/%s v%d: %w", tenantID, keyType, version, err)
	}
	rec.Status = status
	return rec, nil
}
