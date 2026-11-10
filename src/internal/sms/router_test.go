package sms

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type testServer struct {
	server *httptest.Server
	hits   atomic.Int32
	last   map[string]any
}

func newTestServer(t *testing.T, status int, onRequest func(*http.Request, map[string]any)) *testServer {
	t.Helper()
	ts := &testServer{}
	ts.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.hits.Add(1)
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		ts.last = payload
		if onRequest != nil {
			onRequest(r, payload)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(ts.server.Close)
	return ts
}

func sendN(t *testing.T, r *Router, n int) []string {
	t.Helper()
	used := make([]string, 0, n)
	for i := 0; i < n; i++ {
		provider, err := r.Send("13800138000", "123456", 180)
		if err != nil {
			t.Fatalf("send %d failed: %v", i, err)
		}
		used = append(used, provider)
	}
	return used
}

func TestRouterHonoursDailyQuota(t *testing.T) {
	t.Setenv("CLOTHP_SMS_WEBHOOK", "")
	a := newTestServer(t, http.StatusOK, nil)
	b := newTestServer(t, http.StatusOK, nil)
	r := NewRouter(Settings{
		UsageFile: "-", FailureThreshold: 3, CooldownSeconds: 60,
		Providers: []Provider{
			{Name: "a", URL: a.server.URL, Weight: 1000, DailyLimit: 2},
			{Name: "b", URL: b.server.URL, Weight: 100},
		},
	})
	sendN(t, r, 6)
	if a.hits.Load() != 2 {
		t.Fatalf("provider a should be used exactly 2 times (daily_limit), got %d", a.hits.Load())
	}
	if b.hits.Load() != 4 {
		t.Fatalf("provider b should take the remaining 4 sends, got %d", b.hits.Load())
	}
	status := r.Status()
	if status[0].EffectiveWeight != 0 || status[0].SentToday != 2 {
		t.Fatalf("provider a should be excluded after quota exhausted: %+v", status[0])
	}
}

func TestRouterFailsOverAndOpensCircuit(t *testing.T) {
	t.Setenv("CLOTHP_SMS_WEBHOOK", "")
	bad := newTestServer(t, http.StatusInternalServerError, nil)
	good := newTestServer(t, http.StatusOK, nil)
	r := NewRouter(Settings{
		UsageFile: "-", FailureThreshold: 2, CooldownSeconds: 60,
		Providers: []Provider{
			{Name: "bad", URL: bad.server.URL, Weight: 10000},
			{Name: "good", URL: good.server.URL, Weight: 100},
		},
	})
	used := sendN(t, r, 5)
	for _, name := range used {
		if name != "good" {
			t.Fatalf("all sends should finally succeed via good provider, got %v", used)
		}
	}
	if good.hits.Load() != 5 {
		t.Fatalf("good provider should receive 5 sends, got %d", good.hits.Load())
	}
	if bad.hits.Load() != 2 {
		t.Fatalf("bad provider should be tried twice then熔断, got %d", bad.hits.Load())
	}
	if !r.Status()[0].CircuitOpen {
		t.Fatalf("bad provider should be circuit-open: %+v", r.Status()[0])
	}
}

func TestRouterAllProvidersFail(t *testing.T) {
	t.Setenv("CLOTHP_SMS_WEBHOOK", "")
	a := newTestServer(t, http.StatusBadGateway, nil)
	b := newTestServer(t, http.StatusBadGateway, nil)
	r := NewRouter(Settings{
		UsageFile: "-",
		Providers: []Provider{
			{Name: "a", URL: a.server.URL},
			{Name: "b", URL: b.server.URL},
		},
	})
	if _, err := r.Send("13800138000", "123456", 180); err == nil {
		t.Fatal("expected error when all providers fail")
	}
	if a.hits.Load() != 1 || b.hits.Load() != 1 {
		t.Fatalf("each provider should be tried once, got a=%d b=%d", a.hits.Load(), b.hits.Load())
	}
}

func TestRouterDemoModeWhenNotConfigured(t *testing.T) {
	t.Setenv("CLOTHP_SMS_WEBHOOK", "")
	r := NewRouter(Settings{UsageFile: "-"})
	if r.Configured() {
		t.Fatal("no provider should mean demo mode")
	}
	if _, err := r.Send("13800138000", "123456", 180); err == nil {
		t.Fatal("Send should fail when not configured")
	}
}

func TestRouterLegacyEnvProvider(t *testing.T) {
	server := newTestServer(t, http.StatusOK, nil)
	t.Setenv("CLOTHP_SMS_WEBHOOK", server.server.URL)
	t.Setenv("CLOTHP_SMS_TOKEN", "")
	r := NewRouter(Settings{UsageFile: "-"})
	if !r.Configured() {
		t.Fatal("legacy env webhook should be picked up")
	}
	if name, err := r.Send("13800138000", "123456", 180); err != nil || name != "legacy-webhook" {
		t.Fatalf("legacy send failed: name=%s err=%v", name, err)
	}
}

func TestRouterExtraPayloadAndToken(t *testing.T) {
	t.Setenv("CLOTHP_SMS_WEBHOOK", "")
	var gotAuth string
	a := newTestServer(t, http.StatusOK, func(r *http.Request, payload map[string]any) {
		gotAuth = r.Header.Get("Authorization")
	})
	r := NewRouter(Settings{
		UsageFile: "-",
		Providers: []Provider{{
			Name: "adapter", URL: a.server.URL, Token: "secret-token",
			Extra: map[string]any{"template_code": "SMS_001", "phone": "should-be-overridden"},
		}},
	})
	if _, err := r.Send("13800138000", "654321", 180); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret-token" {
		t.Fatalf("token header missing: %q", gotAuth)
	}
	if a.last["template_code"] != "SMS_001" || a.last["phone"] != "13800138000" || a.last["code"] != "654321" || a.last["expires_in"] != float64(180) {
		t.Fatalf("unexpected payload: %+v", a.last)
	}
}

func TestRouterDisabledProviderAndUsagePersistence(t *testing.T) {
	t.Setenv("CLOTHP_SMS_WEBHOOK", "")
	a := newTestServer(t, http.StatusOK, nil)
	disabled := newTestServer(t, http.StatusOK, nil)
	enabled := false
	usageFile := filepath.Join(t.TempDir(), "sms_usage.json")
	settings := Settings{
		UsageFile: usageFile,
		Providers: []Provider{
			{Name: "a", URL: a.server.URL, Weight: 100},
			{Name: "disabled", URL: disabled.server.URL, Weight: 100, Enabled: &enabled},
		},
	}
	r1 := NewRouter(settings)
	sendN(t, r1, 2)
	if disabled.hits.Load() != 0 {
		t.Fatal("disabled provider should never be used")
	}
	r2 := NewRouter(settings)
	if r2.Status()[0].SentToday != 2 {
		t.Fatalf("usage should survive restart, got %+v", r2.Status()[0])
	}
}
