// Package datastore 双数据库提供方（设计文档 3.1/3.2/3.3）。
//
// 存储口径：plain=明文库 yzws-v1，secure=密文库 yzws-v1-sec；
// 默认 write_mode=both（2PC 双写）、read_source=secure（服务端解密后返回）；
// 可配置 plain_only / secure_only 单写降级（健康状态必须显示 degraded，恢复前须补数对账）。
//
// 本包只负责连接、迁移、版本一致性检查与健康检查；
// 双写 2PC、恢复任务与互转对账由后续 dualwrite / datatool 包实现，控制表结构见迁移 053。
package datastore

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"clothp/internal/config"
	"clothp/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Plain  *pgxpool.Pool
	Secure *pgxpool.Pool // nil 表示单库模式（plain_only）

	readSource string
	writeMode  string
}

/* Open 按配置打开所需数据库。both 模式必须能连接两库，否则拒绝启动（设计 3.2 启动校验）。 */
func Open(ctx context.Context, cfg config.Config) (*Store, error) {
	plain, err := database.Open(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}
	s := &Store{Plain: plain, readSource: cfg.DataStore.ReadSource, writeMode: cfg.DataStore.WriteMode}
	if cfg.SecureDatabase == nil {
		/* 未配置 secure 库：保持单库旧行为 */
		s.writeMode = "plain_only"
		s.readSource = "plain"
		return s, nil
	}
	secure, err := database.Open(ctx, *cfg.SecureDatabase)
	if err != nil {
		plain.Close()
		return nil, fmt.Errorf("connect secure database: %w", err)
	}
	s.Secure = secure
	if s.writeMode == "both" {
		if err := s.checkPreparedTransactions(ctx); err != nil {
			s.Close()
			return nil, err
		}
	}
	return s, nil
}

/* checkPreparedTransactions 2PC 部署要求：max_prepared_transactions>0 且应用账号可准备事务。 */
func (s *Store) checkPreparedTransactions(ctx context.Context) error {
	if s.Secure == nil {
		return nil
	}
	dbs := map[string]*pgxpool.Pool{"plain": s.Plain, "secure": s.Secure}
	for _, name := range []string{"plain", "secure"} {
		var text string
		if err := dbs[name].QueryRow(ctx, "SHOW max_prepared_transactions").Scan(&text); err != nil {
			return fmt.Errorf("check %s max_prepared_transactions: %w", name, err)
		}
		max, err := strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("parse %s max_prepared_transactions %q: %w", name, text, err)
		}
		if max <= 0 {
			return fmt.Errorf("%s database max_prepared_transactions=%d: 2PC dual-write requires it to be > 0", name, max)
		}
	}
	return nil
}

/* ReadPool 返回当前读取来源的连接池：read_source=secure 且密文库可用时读密文库，否则读明文库。 */
func (s *Store) ReadPool() *pgxpool.Pool {
	if s.readSource == "secure" && s.Secure != nil {
		return s.Secure
	}
	return s.Plain
}

func (s *Store) WriteMode() string   { return s.writeMode }
func (s *Store) ReadSource() string  { return s.readSource }
func (s *Store) DualEnabled() bool   { return s.Secure != nil && s.writeMode == "both" }

/* WritePools 返回本次写入需要落地的库（both=两库；单写=目标库）。 */
func (s *Store) WritePools() map[string]*pgxpool.Pool {
	pools := map[string]*pgxpool.Pool{"plain": s.Plain}
	if s.writeMode == "secure_only" && s.Secure != nil {
		return map[string]*pgxpool.Pool{"secure": s.Secure}
	}
	if s.writeMode == "both" && s.Secure != nil {
		pools["secure"] = s.Secure
	}
	return pools
}

/* Migrate 两库分别执行迁移；双库模式下校验两库迁移版本一致（checksum 由 database.Migrate 自身校验）。 */
func (s *Store) Migrate(ctx context.Context) error {
	if err := database.Migrate(ctx, s.Plain); err != nil {
		return err
	}
	if s.Secure == nil {
		return nil
	}
	if err := database.Migrate(ctx, s.Secure); err != nil {
		return err
	}
	return s.checkVersions(ctx)
}

func migrationVersions(ctx context.Context, db *pgxpool.Pool) (map[string]bool, error) {
	rows, err := db.Query(ctx, `SELECT version FROM schema_migration WHERE status='success'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	versions := map[string]bool{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		versions[v] = true
	}
	return versions, rows.Err()
}

/* checkVersions 双库必须拥有完全相同的成功迁移集合，否则拒绝提供业务写服务（设计 3.2）。 */
func (s *Store) checkVersions(ctx context.Context) error {
	if s.Secure == nil {
		return nil
	}
	plainVersions, err := migrationVersions(ctx, s.Plain)
	if err != nil {
		return fmt.Errorf("read plain migration versions: %w", err)
	}
	secureVersions, err := migrationVersions(ctx, s.Secure)
	if err != nil {
		return fmt.Errorf("read secure migration versions: %w", err)
	}
	var missing, extra []string
	for v := range plainVersions {
		if !secureVersions[v] {
			missing = append(missing, v)
		}
	}
	for v := range secureVersions {
		if !plainVersions[v] {
			extra = append(extra, v)
		}
	}
	if len(missing) > 0 || len(extra) > 0 {
		sort.Strings(missing)
		sort.Strings(extra)
		return fmt.Errorf("dual-store migration mismatch: secure missing %s; plain missing %s",
			strings.Join(missing, ","), strings.Join(extra, ","))
	}
	return nil
}

/* Health 双库健康状态：单写或任一库不可达时 status=degraded（设计 3.1，不得静默当作正常）。 */
type Health struct {
	Status string `json:"status"` // ok | degraded | down
	Plain  string `json:"plain"`  // ok | down
	Secure string `json:"secure"` // ok | down | disabled
	Detail string `json:"detail,omitempty"`
}

func (s *Store) Health(ctx context.Context) Health {
	h := Health{Status: "ok", Plain: "ok", Secure: "disabled"}
	if err := s.Plain.Ping(ctx); err != nil {
		h.Plain = "down"
	}
	if s.Secure == nil {
		if h.Plain == "down" {
			h.Status = "down"
			h.Detail = "plain database unreachable"
		}
		return h
	}
	if err := s.Secure.Ping(ctx); err != nil {
		h.Secure = "down"
	} else {
		h.Secure = "ok"
	}
	switch {
	case h.Plain == "down" && h.Secure == "down":
		h.Status = "down"
		h.Detail = "both databases unreachable"
	case h.Plain == "down" || h.Secure == "down":
		h.Status = "degraded"
		h.Detail = "single-store mode: one database unreachable, dual-write paused until recovery"
	default:
		if s.writeMode != "both" {
			h.Status = "degraded"
			h.Detail = "write_mode=" + s.writeMode + ": single-store degrade, reconcile required before restoring both"
		}
	}
	return h
}

func (s *Store) Close() {
	if s.Plain != nil {
		s.Plain.Close()
	}
	if s.Secure != nil {
		s.Secure.Close()
	}
}
