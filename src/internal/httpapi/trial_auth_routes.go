package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

/**
 * 体验账号的用户名密码登录（演示版）
 * - 账号存进程内存，密码用随机盐 + SHA-256 存储，不保存明文
 * - 内置体验账号 demo / demo123456，方便立即进入用户体验页面
 * - 正式环境应改为数据库持久化 + bcrypt/argon2，并接统一登录态
 */
const trialAuthTicketTTL = 30 * time.Minute

var trialUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_\x{4e00}-\x{9fa5}]{2,20}$`)

type trialAccount struct {
	username  string
	salt      string
	hash      string
	createdAt time.Time
}

type trialAuthSession struct {
	username  string
	expiresAt time.Time
}

var (
	trialAuthMu       sync.Mutex
	trialAccounts     = make(map[string]*trialAccount)
	trialAuthSessions = make(map[string]trialAuthSession)
)

func trialPasswordHash(salt, password string) string {
	sum := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(sum[:])
}

func newTrialAccount(username, password string) (*trialAccount, error) {
	saltToken, err := newQRToken()
	if err != nil {
		return nil, err
	}
	return &trialAccount{
		username: username, salt: saltToken,
		hash: trialPasswordHash(saltToken, password), createdAt: time.Now(),
	}, nil
}

/* 调用方需持有 trialAuthMu */
func issueTrialAuthSession(username string) (map[string]any, error) {
	now := time.Now()
	for key, session := range trialAuthSessions {
		if now.After(session.expiresAt) {
			delete(trialAuthSessions, key)
		}
	}
	ticket, err := newQRToken()
	if err != nil {
		return nil, err
	}
	trialAuthSessions[ticket] = trialAuthSession{username: username, expiresAt: now.Add(trialAuthTicketTTL)}
	return map[string]any{
		"username": username, "ticket": ticket,
		"expires_in": int(trialAuthTicketTTL.Seconds()),
	}, nil
}

func validateTrialCredential(username, password string) error {
	if !trialUsernamePattern.MatchString(username) {
		return Error(http.StatusBadRequest, "INVALID_USERNAME", "用户名需为 2-20 位字母、数字、下划线或中文")
	}
	if utf8.RuneCountInString(password) < 6 {
		return Error(http.StatusBadRequest, "WEAK_PASSWORD", "密码至少 6 位")
	}
	return nil
}

func (a *API) RegisterTrialAuthRoutes() {
	/* 内置演示账号 */
	if account, err := newTrialAccount("demo", "demo123456"); err == nil {
		trialAuthMu.Lock()
		trialAccounts["demo"] = account
		trialAuthMu.Unlock()
	}

	a.Public("POST /api/v1/public/trial-auth/register", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		username := strings.TrimSpace(input.Username)
		if err := validateTrialCredential(username, input.Password); err != nil {
			return err
		}
		trialAuthMu.Lock()
		defer trialAuthMu.Unlock()
		if trialAccounts[username] != nil {
			return Error(http.StatusConflict, "USERNAME_TAKEN", "该用户名已被注册")
		}
		account, err := newTrialAccount(username, input.Password)
		if err != nil {
			return err
		}
		trialAccounts[username] = account
		result, err := issueTrialAuthSession(username)
		if err != nil {
			return err
		}
		return JSON(w, http.StatusOK, result)
	})

	a.Public("POST /api/v1/public/trial-auth/login", func(w http.ResponseWriter, r *http.Request) error {
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := Decode(r, &input); err != nil {
			return err
		}
		username := strings.TrimSpace(input.Username)
		trialAuthMu.Lock()
		defer trialAuthMu.Unlock()
		account := trialAccounts[username]
		if account == nil {
			return Error(http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码不正确")
		}
		expected := []byte(account.hash)
		actual := []byte(trialPasswordHash(account.salt, input.Password))
		if subtle.ConstantTimeCompare(expected, actual) != 1 {
			return Error(http.StatusUnauthorized, "INVALID_CREDENTIALS", "用户名或密码不正确")
		}
		result, err := issueTrialAuthSession(username)
		if err != nil {
			return err
		}
		return JSON(w, http.StatusOK, result)
	})

	a.Public("GET /api/v1/public/trial-auth/me", func(w http.ResponseWriter, r *http.Request) error {
		ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
		trialAuthMu.Lock()
		defer trialAuthMu.Unlock()
		session := trialAuthSessions[ticket]
		if session.username == "" || time.Now().After(session.expiresAt) {
			delete(trialAuthSessions, ticket)
			return Error(http.StatusUnauthorized, "SESSION_EXPIRED", "体验会话已失效，请重新登录")
		}
		return JSON(w, http.StatusOK, map[string]any{"username": session.username})
	})
}
