package masterdata

import (
	"context"
	"errors"
	"net"
	"net/http"

	"clothp/internal/httpapi"
	"clothp/internal/seccrypto"

	"github.com/jackc/pgx/v5/pgconn"
)

/* writeFailure 把底层写库错误归因成对客户端有意义的错误。
 *
 * 事故背景：员工建档旧实现把"任何"写库失败都报成 409 DUPLICATE_OR_INVALID_REFERENCE，
 * 于是密钥服务抖动、连接被重置、锁等待超时、事务提交失败都会让现场以为"工号或手机号重复"，
 * 反复改数据重试，而真实原因是瞬时故障——原样重试即可成功。
 *
 * 归因口径：
 *   - 密钥服务不可用（KEY_UNAVAILABLE）           → 503 retryable
 *   - 序列化失败/死锁/锁超时/连接中断/连接池耗尽   → 503 retryable
 *   - 唯一键冲突（23505）                         → 409（真正重复，需改数据）
 *   - 外键/检查约束/字面量非法                     → 400（数据无效，需改数据）
 *   - 其余未知错误原样返回，由上层按 500 处理
 *
 * side 标记失败发生在哪一侧（plain/secure/dualwrite），便于定位双库问题。
 */
func writeFailure(err error, side string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, seccrypto.ErrKeyUnavailable) {
		return &httpapi.AppError{
			Status:    http.StatusServiceUnavailable,
			Code:      "KEY_UNAVAILABLE",
			Message:   "密钥服务暂不可用，未写入任何数据，请稍后原样重试",
			Retryable: true,
			Details:   map[string]any{"side": side, "cause": "key_service"},
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &httpapi.AppError{
			Status:    http.StatusServiceUnavailable,
			Code:      "WRITE_TIMEOUT",
			Message:   "写入超时，未提交任何数据，请稍后原样重试",
			Retryable: true,
			Details:   map[string]any{"side": side, "cause": "timeout"},
		}
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return &httpapi.AppError{
				Status:  http.StatusConflict,
				Code:    "DUPLICATE_CODE",
				Message: "编码或手机号重复，请更换后重试",
				Details: map[string]any{"side": side, "constraint": pgErr.ConstraintName},
			}
		case "23503": // foreign_key_violation
			return &httpapi.AppError{
				Status:  http.StatusBadRequest,
				Code:    "INVALID_REFERENCE",
				Message: "引用的数据不存在或已停用",
				Details: map[string]any{"side": side, "constraint": pgErr.ConstraintName},
			}
		case "23502", "23514", "22P02", "22003": // not_null / check / 非法字面量 / 数值越界
			return &httpapi.AppError{
				Status:  http.StatusBadRequest,
				Code:    "VALIDATION_ERROR",
				Message: "字段取值不合法，请检查后重试",
				Details: map[string]any{"side": side, "column": pgErr.ColumnName, "constraint": pgErr.ConstraintName},
			}
		case "40001", "40P01", "55P03", "55006", "57P01", "57014", "53300", "53400",
			"08000", "08001", "08003", "08004", "08006":
			/* 序列化失败/死锁/锁不可用/对象在用/连接被管理端关闭/语句取消/连接数耗尽/连接异常 */
			return &httpapi.AppError{
				Status:    http.StatusServiceUnavailable,
				Code:      "TRANSIENT_DB_ERROR",
				Message:   "数据库繁忙或连接中断，未提交任何数据，请稍后原样重试",
				Retryable: true,
				Details:   map[string]any{"side": side, "sqlstate": pgErr.Code},
			}
		}
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return &httpapi.AppError{
			Status:    http.StatusServiceUnavailable,
			Code:      "TRANSIENT_DB_ERROR",
			Message:   "数据库连接中断，未提交任何数据，请稍后原样重试",
			Retryable: true,
			Details:   map[string]any{"side": side, "cause": "network"},
		}
	}
	return err
}
