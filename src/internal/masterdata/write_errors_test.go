package masterdata

import (
	"errors"
	"net"
	"net/http"
	"testing"

	"clothp/internal/httpapi"
	"clothp/internal/seccrypto"

	"github.com/jackc/pgx/v5/pgconn"
)

/* 归因口径必须稳定：可重试的瞬时故障不能被报成"数据重复"，真正的重复也不能被误导成可重试。 */
func TestWriteFailureClassification(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		wantCode  string
		wantStat  int
		retryable bool
	}{
		{"duplicate", &pgconn.PgError{Code: "23505", ConstraintName: "uk_employee_no"}, "DUPLICATE_CODE", http.StatusConflict, false},
		{"fk", &pgconn.PgError{Code: "23503"}, "INVALID_REFERENCE", http.StatusBadRequest, false},
		{"check", &pgconn.PgError{Code: "23514"}, "VALIDATION_ERROR", http.StatusBadRequest, false},
		{"serialization", &pgconn.PgError{Code: "40001"}, "TRANSIENT_DB_ERROR", http.StatusServiceUnavailable, true},
		{"deadlock", &pgconn.PgError{Code: "40P01"}, "TRANSIENT_DB_ERROR", http.StatusServiceUnavailable, true},
		{"lock_timeout", &pgconn.PgError{Code: "55P03"}, "TRANSIENT_DB_ERROR", http.StatusServiceUnavailable, true},
		{"conn_reset", &pgconn.PgError{Code: "08006"}, "TRANSIENT_DB_ERROR", http.StatusServiceUnavailable, true},
		{"too_many_conns", &pgconn.PgError{Code: "53300"}, "TRANSIENT_DB_ERROR", http.StatusServiceUnavailable, true},
		{"key_service", seccrypto.ErrKeyUnavailable, "KEY_UNAVAILABLE", http.StatusServiceUnavailable, true},
		{"network", &net.OpError{Op: "read", Err: errors.New("reset")}, "TRANSIENT_DB_ERROR", http.StatusServiceUnavailable, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := writeFailure(c.err, "secure")
			var appErr *httpapi.AppError
			if !errors.As(got, &appErr) {
				t.Fatalf("expected AppError, got %T (%v)", got, got)
			}
			if appErr.Code != c.wantCode || appErr.Status != c.wantStat || appErr.Retryable != c.retryable {
				t.Fatalf("got code=%s status=%d retryable=%v, want code=%s status=%d retryable=%v",
					appErr.Code, appErr.Status, appErr.Retryable, c.wantCode, c.wantStat, c.retryable)
			}
		})
	}

	/* 未知错误原样上抛（由统一异常处理按 500 记日志，避免掩盖缺陷） */
	unknown := errors.New("boom")
	if got := writeFailure(unknown, "plain"); got != unknown {
		t.Fatalf("unknown error should pass through, got %v", got)
	}
	if got := writeFailure(nil, "plain"); got != nil {
		t.Fatalf("nil error should stay nil, got %v", got)
	}
}

/* 幂等指纹：字段顺序无关、内容敏感（否则同键换内容会被误重放）。 */
func TestIdempotencyHash(t *testing.T) {
	a := idempotencyHash(map[string]any{"name": "张三", "phone": "13800000000", "qty": 1})
	b := idempotencyHash(map[string]any{"qty": 1, "phone": "13800000000", "name": "张三"})
	if a != b {
		t.Fatalf("hash must be order-independent: %s vs %s", a, b)
	}
	c := idempotencyHash(map[string]any{"name": "张三", "phone": "13800000001", "qty": 1})
	if a == c {
		t.Fatal("hash must change with payload")
	}
	/* 嵌套数组（skill_process_ids）也要参与指纹 */
	d := idempotencyHash(map[string]any{"name": "张三", "phone": "13800000000", "qty": 1, "skill_process_ids": []string{"p1"}})
	if a == d {
		t.Fatal("hash must include array fields")
	}
}
