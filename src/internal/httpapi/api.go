package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"clothp/internal/auth"
	"clothp/internal/config"
	"clothp/internal/platform"
	"clothp/internal/sms"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HandlerFunc func(http.ResponseWriter, *http.Request, auth.Principal) error

type API struct {
	DB     *pgxpool.Pool
	Auth   *auth.Service
	Config config.Config
	SMS    *sms.Router
	mux    *http.ServeMux
}

type AppError struct {
	Status    int
	Code      string
	Message   string
	Retryable bool
	Details   any
}

func (e *AppError) Error() string { return e.Message }
func Error(status int, code, message string) *AppError {
	return &AppError{Status: status, Code: code, Message: message}
}

func New(db *pgxpool.Pool, authService *auth.Service, cfg config.Config) *API {
	api := &API{DB: db, Auth: authService, Config: cfg, SMS: sms.NewRouter(cfg.SMS), mux: http.NewServeMux()}
	api.Public("GET /api/v1/system/health/live", func(w http.ResponseWriter, r *http.Request) error {
		return JSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	api.Public("GET /api/v1/system/health/ready", func(w http.ResponseWriter, r *http.Request) error {
		if err := db.Ping(r.Context()); err != nil {
			return Error(http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE", "数据库不可用")
		}
		return JSON(w, http.StatusOK, map[string]any{"status": "ok", "mode": cfg.Mode})
	})
	return api
}

func (a *API) Public(pattern string, handler func(http.ResponseWriter, *http.Request) error) {
	a.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			WriteError(w, r, err)
		}
	})
}

func (a *API) Handle(pattern, permission string, handler HandlerFunc) {
	a.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		principal, err := a.Auth.AuthenticateRequest(r)
		if err == nil && principal.PasswordChangeRequired {
			switch r.URL.Path {
			case "/api/v1/auth/password/change", "/api/v1/me", "/api/v1/auth/logout":
			default:
				err = Error(http.StatusForbidden, "PASSWORD_CHANGE_REQUIRED", "请先设置新密码再进入业务系统")
			}
		}
		if err == nil && permission != "" && !principal.Permissions[permission] {
			err = Error(http.StatusForbidden, "FORBIDDEN", "没有执行此操作的权限")
		}
		if err == nil {
			err = handler(w, r, principal)
		}
		if err != nil {
			WriteError(w, r, err)
		}
	})
}

func (a *API) PlatformHandle(pattern string, handler func(http.ResponseWriter, *http.Request, auth.PlatformPrincipal) error) {
	a.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		principal, err := a.Auth.AuthenticatePlatformRequest(r)
		if err == nil && principal.Role == "assistant" {
			// 辅助人员按路由权限点放行；assistants.manage（平台人员管理）仅限超管
			if perm := platformRoutePermission(r); perm != "" {
				if perm == platformPermAssistantsManage || !principal.Permissions[perm] {
					err = Error(http.StatusForbidden, "FORBIDDEN", "平台辅助人员没有执行此操作的权限")
				}
			}
		}
		if err == nil {
			err = handler(w, r, principal)
		}
		if err != nil {
			if err == auth.ErrUnauthenticated {
				err = Error(http.StatusUnauthorized, "PLATFORM_UNAUTHENTICATED", "平台管理员会话已失效")
			}
			WriteError(w, r, err)
		}
	})
}

const platformPermAssistantsManage = "assistants.manage"

// platformRoutePermission 平台接口 → 辅助人员权限点映射（按路径前缀 + 请求方法）。
func platformRoutePermission(r *http.Request) string {
	path, write := r.URL.Path, r.Method != http.MethodGet
	pick := func(read, writePerm string) string {
		if write {
			return writePerm
		}
		return read
	}
	switch {
	case strings.HasPrefix(path, "/api/v1/platform/assistants"):
		return platformPermAssistantsManage
	case strings.HasPrefix(path, "/api/v1/platform/auth/"):
		return "" // 登录/登出/自身会话所有人可用
	case strings.HasPrefix(path, "/api/v1/platform/tenant-registrations"):
		return pick("registrations.read", "registrations.write")
	case strings.HasPrefix(path, "/api/v1/platform/trial-extensions"):
		return pick("registrations.read", "registrations.write")
	case strings.HasPrefix(path, "/api/v1/platform/settings"):
		return pick("settings.read", "settings.write")
	case strings.HasPrefix(path, "/api/v1/platform/audit-logs"):
		return "audit.read"
	case strings.HasPrefix(path, "/api/v1/platform/modules"):
		return pick("modules.read", "modules.write")
	case strings.HasPrefix(path, "/api/v1/platform/role-templates"),
		strings.HasPrefix(path, "/api/v1/platform/permissions"):
		return pick("role_templates.read", "role_templates.write")
	case strings.HasPrefix(path, "/api/v1/platform/statistics"):
		return "tenants.read"
	case strings.HasPrefix(path, "/api/v1/platform/tenants"):
		return pick("tenants.read", "tenants.write")
	}
	return ""
}

func (a *API) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = "req-" + platform.ID()
		}
		w.Header().Set("X-Request-Id", requestID)
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key,If-Match,X-Request-Id")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic %s: %v\n%s", requestID, recovered, debug.Stack())
				WriteError(recorder, r, Error(http.StatusInternalServerError, "INTERNAL_ERROR", "服务处理失败"))
			}
			if !strings.HasPrefix(r.URL.Path, "/api/v1/system/health/") {
				log.Printf("http request %s method=%s path=%s status=%d duration=%s", requestID, r.Method, r.URL.Path, recorder.status, time.Since(started).Round(time.Millisecond))
			}
		}()
		a.mux.ServeHTTP(recorder, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, requestID)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey{}).(string)
	return value
}

func Decode(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return Error(http.StatusBadRequest, "INVALID_REQUEST", "请求参数格式不正确: "+err.Error())
	}
	return nil
}

func JSON(w http.ResponseWriter, status int, value any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(value)
}

func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, auth.ErrUnauthenticated) {
		err = Error(http.StatusUnauthorized, "UNAUTHENTICATED", "登录状态已失效，请重新登录")
	}
	var appError *AppError
	if !errors.As(err, &appError) {
		log.Printf("request exception %s error=%v", RequestID(r.Context()), err)
		appError = Error(http.StatusInternalServerError, "INTERNAL_ERROR", "服务处理失败")
	}
	log.Printf("http error request=%s method=%s path=%s status=%d code=%s message=%q details=%v", RequestID(r.Context()), r.Method, r.URL.Path, appError.Status, appError.Code, appError.Message, appError.Details)
	_ = JSON(w, appError.Status, map[string]any{
		"code": appError.Code, "message": appError.Message, "request_id": RequestID(r.Context()),
		"retryable": appError.Retryable, "details": appError.Details,
	})
}

func IdempotencyKey(r *http.Request) (string, error) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		return "", Error(http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED", "写请求必须提供 Idempotency-Key")
	}
	if len(key) > 200 {
		return "", Error(http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY", "幂等键过长")
	}
	return key, nil
}

func Page(r *http.Request) (int, string) {
	limit := 30
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var parsed int
		if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil && parsed >= 1 && parsed <= 100 {
			limit = parsed
		}
	}
	return limit, r.URL.Query().Get("cursor")
}
