package datatool

import (
	"bytes"
	"context"
	"fmt"
	"log"

	"clothp/internal/datastore"
	"clothp/internal/platform"
	"clothp/internal/seccrypto"

	"github.com/jackc/pgx/v5/pgxpool"
)

/* RewrapLocalMaster 用新的 local-master 口令重包 tenant_crypto_key（key_wrap_ref='local-master:v1'），
 * 用于轮换 CLOTHP_JWT_SECRET。
 *
 * 背景：JWT 签名密钥同时是租户 DEK/盲索引密钥的 local-master 包装口令
 * （sha256("yzws-tenant-crypto:v1:"+secret)）。直接改配置改口令，存量密文将无法解包
 * （KEY_UNAVAILABLE），因此必须先用旧口令解包、新口令重包并写回。
 *
 * 安全约束：只处理 local-master:v1 包装的记录（HSM/KMS 包装的记录不受影响，跳过）；
 * 每行重包后立即用新口令回解并逐字节校验，不一致则跳过该行；dry_run 只统计不写库。
 * 两库都执行（明文库的 tenant_crypto_key 是对账副本，需保持一致）。
 * 完成后所有 access/refresh 令牌失效（签名密钥已变），需重新登录。
 */
func RewrapLocalMaster(ctx context.Context, store *datastore.Store, oldSecret, newSecret string, dryRun bool) (*Result, error) {
	if store.Plain == nil {
		return nil, fmt.Errorf("plain database required")
	}
	if oldSecret == "" || newSecret == "" {
		return nil, fmt.Errorf("old and new secret both required")
	}
	if oldSecret == newSecret {
		return nil, fmt.Errorf("new secret equals current secret, nothing to do")
	}
	oldProvider := seccrypto.NewLocalProvider(oldSecret)
	newProvider := seccrypto.NewLocalProvider(newSecret)
	result := &Result{JobID: platform.ID(), Direction: "rewrap-local-master", DryRun: dryRun, Tables: map[string]int{}}

	for _, target := range []struct {
		name string
		db   *pgxpool.Pool
	}{
		{"secure", store.Secure},
		{"plain", store.Plain},
	} {
		if target.db == nil {
			continue
		}
		rows, err := target.db.Query(ctx, `SELECT tenant_id,key_version,wrap_key_type,wrapped_key
			FROM tenant_crypto_key WHERE key_wrap_ref='local-master:v1' ORDER BY tenant_id,wrap_key_type,key_version`)
		if err != nil {
			return result, fmt.Errorf("load %s keys: %w", target.name, err)
		}
		type keyRow struct {
			tenantID string
			version  int64
			keyType  string
			wrapped  string
		}
		var keys []keyRow
		for rows.Next() {
			var k keyRow
			if err := rows.Scan(&k.tenantID, &k.version, &k.keyType, &k.wrapped); err != nil {
				rows.Close()
				return result, err
			}
			keys = append(keys, k)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return result, err
		}
		for _, k := range keys {
			ref := &seccrypto.KeyRef{TenantID: k.tenantID, KeyVersion: k.version, KeyWrapRef: "local-master:v1"}
			material, err := oldProvider.Unwrap(ctx, ref, k.wrapped)
			if err != nil {
				log.Printf("rewrap-local-master: %s %s/%s v%d unwrap with current secret failed: %v (skipped)",
					target.name, k.tenantID, k.keyType, k.version, err)
				result.Failed++
				continue
			}
			rewrapped, err := newProvider.Wrap(ctx, ref, material)
			if err != nil {
				log.Printf("rewrap-local-master: %s %s/%s v%d wrap failed: %v (skipped)",
					target.name, k.tenantID, k.keyType, k.version, err)
				result.Failed++
				continue
			}
			check, err := newProvider.Unwrap(ctx, ref, rewrapped)
			if err != nil || !bytes.Equal(check, material) {
				log.Printf("rewrap-local-master: %s %s/%s v%d round-trip verify failed (skipped)",
					target.name, k.tenantID, k.keyType, k.version)
				result.Failed++
				continue
			}
			result.Tables[target.name+".rewrapped"]++
			if dryRun {
				continue
			}
			if _, err := target.db.Exec(ctx, `UPDATE tenant_crypto_key SET wrapped_key=$1
				WHERE tenant_id=$2 AND key_version=$3 AND wrap_key_type=$4 AND key_wrap_ref='local-master:v1'`,
				rewrapped, k.tenantID, k.version, k.keyType); err != nil {
				return result, fmt.Errorf("update %s %s/%s v%d: %w", target.name, k.tenantID, k.keyType, k.version, err)
			}
		}
	}
	return result, nil
}
