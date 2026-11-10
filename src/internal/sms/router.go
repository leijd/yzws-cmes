// Package sms 提供多服务商短信路由：按实时使用情况自动选择服务商，失败时自动切换。
package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

/**
 * 所有服务商都通过统一的 Webhook 契约接入：
 *   POST { "phone": "...", "code": "123456", "expires_in": 180, ...extra }
 *   Header: Authorization: Bearer <token>（可选）
 *   返回 2xx 表示发送成功，其它状态码或超时视为失败。
 * 所以「换服务商」= 换一条 provider 配置（或换它背后的适配层），业务代码无需改动。
 */
type Provider struct {
	Name       string         `yaml:"name" json:"name"`
	URL        string         `yaml:"url" json:"url"`
	Token      string         `yaml:"token" json:"-"`
	Weight     int            `yaml:"weight" json:"weight"`
	DailyLimit int            `yaml:"daily_limit" json:"daily_limit"` // 0 表示不限
	TimeoutMS  int            `yaml:"timeout_ms" json:"timeout_ms"`
	Enabled    *bool          `yaml:"enabled" json:"-"` // nil = 启用；false = 停用
	Extra      map[string]any `yaml:"extra" json:"-"`   // 适配层需要的附加字段（模板 ID、签名等）
}

type Settings struct {
	Providers        []Provider `yaml:"providers"`
	CooldownSeconds  int        `yaml:"cooldown_seconds"`  // 连续失败后的熔断冷却时间
	FailureThreshold int        `yaml:"failure_threshold"` // 连续失败多少次触发熔断
	UsageFile        string     `yaml:"usage_file"`        // 用量持久化文件；"-" 表示不持久化
}

type ProviderStatus struct {
	Name                string `json:"name"`
	Weight              int    `json:"weight"`
	DailyLimit          int    `json:"daily_limit"`
	SentToday           int    `json:"sent_today"`
	FailedToday         int    `json:"failed_today"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	CircuitOpen         bool   `json:"circuit_open"`
	EffectiveWeight     int    `json:"effective_weight"`
}

type usageRecord struct {
	Date   string `json:"date"`
	Sent   int    `json:"sent"`
	Failed int    `json:"failed"`
}

type providerState struct {
	provider            Provider
	day                 string
	sent                int
	failed              int
	consecutiveFailures int
	circuitOpenUntil    time.Time
	currentWeight       int // 平滑加权轮询的当前权重
}

type Router struct {
	mu        sync.Mutex
	states    []*providerState
	cooldown  time.Duration
	threshold int
	usageFile string
	now       func() time.Time
}

/** NewRouter 构建路由；兼容旧的 CLOTHP_SMS_WEBHOOK 单服务商环境变量。 */
func NewRouter(settings Settings) *Router {
	cooldown := time.Duration(settings.CooldownSeconds) * time.Second
	if cooldown <= 0 {
		cooldown = 60 * time.Second
	}
	threshold := settings.FailureThreshold
	if threshold <= 0 {
		threshold = 3
	}
	providers := append([]Provider(nil), settings.Providers...)
	if len(providers) == 0 {
		if webhook := strings.TrimSpace(os.Getenv("CLOTHP_SMS_WEBHOOK")); webhook != "" {
			providers = append(providers, Provider{
				Name: "legacy-webhook", URL: webhook,
				Token: strings.TrimSpace(os.Getenv("CLOTHP_SMS_TOKEN")),
			})
		}
	}
	// 内置腾讯云服务商：TENCENT_* 环境变量配齐即自动启用（无需改配置文件）
	if env := TencentFromEnv(); env != nil {
		providers = append(providers, env.Provider())
		log.Printf("[sms] tencent-cloud provider enabled from environment (region=%s)", env.Region)
	}
	day := time.Now().Format("2006-01-02")
	states := make([]*providerState, 0, len(providers))
	for _, p := range providers {
		if strings.TrimSpace(p.URL) == "" || (p.Enabled != nil && !*p.Enabled) {
			continue
		}
		if strings.TrimSpace(p.Name) == "" {
			p.Name = p.URL
		}
		if p.Weight <= 0 {
			p.Weight = 100
		}
		if p.TimeoutMS <= 0 {
			p.TimeoutMS = 5000
		}
		states = append(states, &providerState{provider: p, day: day})
	}
	r := &Router{
		states: states, cooldown: cooldown, threshold: threshold,
		usageFile: settings.UsageFile, now: time.Now,
	}
	r.loadUsage()
	return r
}

/** Configured 为 false 时上层进入演示模式（不真发短信）。 */
func (r *Router) Configured() bool { return r != nil && len(r.states) > 0 }

func maskPhone(phone string) string {
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}

/**
 * Send 自动选路发送一条短信，返回实际使用的服务商名称。
 * 选路依据：有效权重 = 配置权重 × 健康系数(连续失败) × 剩余日配额比例，
 * 再用平滑加权轮询分配；熔断中或已达日配额的服务商自动剔除。
 * 某服务商失败时，同一条短信会自动切换下一个服务商重试，不会重复生成验证码。
 */
func (r *Router) Send(phone, code string, expiresIn int) (string, error) {
	if !r.Configured() {
		return "", errors.New("no sms provider configured")
	}
	tried := make(map[int]bool)
	var lastErr error
	for attempt := 0; attempt < len(r.states); attempt++ {
		idx := r.selectProvider(tried)
		if idx < 0 {
			break
		}
		state := r.states[idx]
		name := state.provider.Name
		err := r.sendOnce(state.provider, phone, code, expiresIn)
		r.record(state, err == nil)
		if err == nil {
			log.Printf("[sms] sent provider=%s phone=%s", name, maskPhone(phone))
			return name, nil
		}
		lastErr = err
		tried[idx] = true
		log.Printf("[sms] provider=%s failed: %v; trying next provider", name, err)
	}
	if lastErr == nil {
		lastErr = errors.New("all sms providers unavailable (quota exhausted or circuit open)")
	}
	return "", lastErr
}

/** 平滑加权轮询（SWRR）：按有效权重分配，且避免一直命中同一个服务商。 */
func (r *Router) selectProvider(exclude map[int]bool) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rolloverDayLocked()

	type candidate struct{ idx, weight int }
	candidates := make([]candidate, 0, len(r.states))
	total := 0
	for i, state := range r.states {
		if exclude[i] {
			continue
		}
		weight := r.effectiveWeightLocked(state)
		if weight <= 0 {
			continue
		}
		total += weight
		candidates = append(candidates, candidate{i, weight})
	}
	if len(candidates) == 0 {
		return -1
	}
	for _, c := range candidates {
		r.states[c.idx].currentWeight += c.weight
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if r.states[c.idx].currentWeight > r.states[best.idx].currentWeight {
			best = c
		}
	}
	r.states[best.idx].currentWeight -= total
	return best.idx
}

func (r *Router) effectiveWeightLocked(state *providerState) int {
	now := r.now()
	/* 熔断冷却结束 → 半开试探，重置失败计数 */
	if !state.circuitOpenUntil.IsZero() && !state.circuitOpenUntil.After(now) {
		state.circuitOpenUntil = time.Time{}
		state.consecutiveFailures = 0
	}
	if state.circuitOpenUntil.After(now) {
		return 0
	}
	if state.provider.DailyLimit > 0 && state.sent >= state.provider.DailyLimit {
		return 0
	}
	health := 1.0 / (1.0 + float64(state.consecutiveFailures))
	quota := 1.0
	if state.provider.DailyLimit > 0 {
		quota = float64(state.provider.DailyLimit-state.sent) / float64(state.provider.DailyLimit)
	}
	weight := int(float64(state.provider.Weight) * health * quota)
	if weight < 1 {
		weight = 1
	}
	return weight
}

func (r *Router) record(state *providerState, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rolloverDayLocked()
	if success {
		state.sent++
		state.consecutiveFailures = 0
		state.circuitOpenUntil = time.Time{}
	} else {
		state.failed++
		state.consecutiveFailures++
		if state.consecutiveFailures >= r.threshold {
			state.circuitOpenUntil = r.now().Add(r.cooldown)
		}
	}
	r.saveUsageLocked()
}

func (r *Router) rolloverDayLocked() {
	day := r.now().Format("2006-01-02")
	for _, state := range r.states {
		if state.day == day {
			continue
		}
		state.day = day
		state.sent, state.failed = 0, 0
		state.consecutiveFailures = 0
		state.circuitOpenUntil = time.Time{}
		state.currentWeight = 0
	}
}

func (r *Router) sendOnce(p Provider, phone, code string, expiresIn int) error {
	/* 内置腾讯云服务商走专属实现（签名 + SendSms API） */
	if p.URL == tencentURLScheme {
		return sendTencent(p, phone, code)
	}
	payload := make(map[string]any, len(p.Extra)+3)
	for key, value := range p.Extra {
		payload[key] = value
	}
	/* 保留字段不允许被 extra 覆盖 */
	payload["phone"] = phone
	payload["code"] = code
	payload["expires_in"] = expiresIn
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(p.TimeoutMS)*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(p.Token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return nil
}

/** Status 返回各服务商的实时用量与健康状态，供运维查看选路依据。 */
func (r *Router) Status() []ProviderStatus {
	if r == nil {
		return []ProviderStatus{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rolloverDayLocked()
	result := make([]ProviderStatus, 0, len(r.states))
	for _, state := range r.states {
		result = append(result, ProviderStatus{
			Name:                state.provider.Name,
			Weight:              state.provider.Weight,
			DailyLimit:          state.provider.DailyLimit,
			SentToday:           state.sent,
			FailedToday:         state.failed,
			ConsecutiveFailures: state.consecutiveFailures,
			CircuitOpen:         state.circuitOpenUntil.After(r.now()),
			EffectiveWeight:     r.effectiveWeightLocked(state),
		})
	}
	return result
}

/* ===== 按天用量持久化：重启后日配额仍然有效 ===== */
func (r *Router) loadUsage() {
	if r.usageFile == "" || r.usageFile == "-" {
		return
	}
	body, err := os.ReadFile(r.usageFile)
	if err != nil {
		return
	}
	var data map[string]usageRecord
	if err := json.Unmarshal(body, &data); err != nil {
		log.Printf("[sms] read usage file failed: %v", err)
		return
	}
	day := r.now().Format("2006-01-02")
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, state := range r.states {
		if record, ok := data[state.provider.Name]; ok && record.Date == day {
			state.sent, state.failed = record.Sent, record.Failed
		}
	}
}

func (r *Router) saveUsageLocked() {
	if r.usageFile == "" || r.usageFile == "-" {
		return
	}
	data := make(map[string]usageRecord, len(r.states))
	for _, state := range r.states {
		data[state.provider.Name] = usageRecord{Date: state.day, Sent: state.sent, Failed: state.failed}
	}
	body, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(r.usageFile), 0o755); err != nil {
		log.Printf("[sms] create usage dir failed: %v", err)
		return
	}
	tmp := r.usageFile + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		log.Printf("[sms] write usage file failed: %v", err)
		return
	}
	if err := os.Rename(tmp, r.usageFile); err != nil {
		log.Printf("[sms] rename usage file failed: %v", err)
	}
}
