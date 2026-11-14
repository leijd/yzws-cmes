package masterdata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"clothp/internal/httpapi"
	"clothp/internal/platform"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

/* 员工建档幂等（复用 001_core.sql 的 command_request，与报工/付款同一套机制）。
 *
 * 事故背景：建档原先是普通 POST，双击、网关超时重试、用户回退再点都会建出第二个员工
 * （第二次才因为工号唯一报 409），现场表现为"莫名其妙多一个人"，且两条记录都带审计。
 * 现在要求 Idempotency-Key：同一键 + 同一请求内容 → 直接重放首次响应（200, replayed=true）；
 * 同一键 + 不同内容 → 409，避免掩盖前端串键。
 *
 * command_request 属于"控制表"，不参与双库增量补偿（见 dualwrite.catchupExcluded），
 * 因此在双写事务里只写审计侧（优先密文库），不影响 plain/secure 一致性。
 */
const idempotentRetention = 30 * 24 * time.Hour

/* idempotencyHash 请求指纹：键排序后整体哈希，用于识别"同一幂等键换了内容"。 */
func idempotencyHash(input map[string]any) string {
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]any, 0, len(keys)*2)
	for _, k := range keys {
		pairs = append(pairs, k, input[k])
	}
	canonical, err := json.Marshal(pairs)
	if err != nil {
		/* 入参含不可序列化类型（不应发生）：退化为格式化文本，仍保持确定性 */
		canonical = fmt.Append(nil, pairs...)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

/* beginIdempotent 幂等预检 + 占位。
 * 返回 replayed=true 表示命中历史成功记录，调用方应直接返回 replay 而不落业务数据。 */
func beginIdempotent(ctx context.Context, tx pgx.Tx, tenantID, key, commandType, aggregateID, requestHash string) (replay map[string]any, replayed bool, err error) {
	var storedHash string
	var payload []byte
	err = tx.QueryRow(ctx,
		`SELECT request_hash,response_payload FROM command_request WHERE tenant_id=$1 AND idempotency_key=$2`,
		tenantID, key).Scan(&storedHash, &payload)
	if err == nil {
		if storedHash != requestHash {
			return nil, false, httpapi.Error(http.StatusConflict, "IDEMPOTENCY_REUSED_WITH_DIFFERENT_BODY",
				"同一幂等键的请求内容不一致，请为新请求使用新的 Idempotency-Key")
		}
		replay = map[string]any{}
		if len(payload) > 0 {
			if err := json.Unmarshal(payload, &replay); err != nil {
				return nil, false, err
			}
		}
		replay["replayed"] = true
		return replay, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, writeFailure(err, "command_request")
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO command_request(tenant_id,id,idempotency_key,command_type,aggregate_id,request_hash,status,expires_at,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,'processing',$7,$8)`,
		tenantID, platform.ID(), key, commandType, aggregateID, requestHash,
		time.Now().Add(idempotentRetention).UnixMicro(), platform.Now()); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			/* 同键并发：前一个请求还在处理，让客户端用同一键稍后重试 */
			return nil, false, httpapi.RetryableError(http.StatusConflict, "IDEMPOTENT_REQUEST_IN_PROGRESS",
				"相同请求正在处理中，请稍后用同一 Idempotency-Key 重试")
		}
		return nil, false, writeFailure(err, "command_request")
	}
	return nil, false, nil
}

/* completeIdempotent 记录成功响应，供重试时原样重放。 */
func completeIdempotent(ctx context.Context, tx pgx.Tx, tenantID, key string, response map[string]any) error {
	payload, err := json.Marshal(response)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`UPDATE command_request SET status='succeeded',response_payload=$1 WHERE tenant_id=$2 AND idempotency_key=$3`,
		payload, tenantID, key)
	return writeFailure(err, "command_request")
}
