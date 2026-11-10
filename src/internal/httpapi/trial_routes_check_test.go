package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"clothp/internal/auth"
	"clothp/internal/sms"
)

func postJSON(t *testing.T, url string, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return resp.StatusCode, result
}

func TestTrialCodeFlowCheck(t *testing.T) {
	/* 强制演示模式，避免测试依赖外部短信网关 */
	t.Setenv("CLOTHP_SMS_WEBHOOK", "")
	t.Setenv("CLOTHP_SMS_TOKEN", "")
	api := &API{mux: http.NewServeMux()}
	api.RegisterTrialRoutes()
	server := httptest.NewServer(api.mux)
	defer server.Close()

	if status, _ := postJSON(t, server.URL+"/api/v1/auth/trial/send-code", `{"phone":"123"}`); status != 400 {
		t.Fatalf("bad phone should be 400, got %d", status)
	}
	status, sent := postJSON(t, server.URL+"/api/v1/auth/trial/send-code", `{"phone":"13800138000"}`)
	if status != 200 || sent["sms_sent"] != false {
		t.Fatalf("send-code failed: %d %v", status, sent)
	}
	code, _ := sent["demo_code"].(string)
	if len(code) != 6 {
		t.Fatalf("demo_code missing: %v", sent)
	}
	if status, _ := postJSON(t, server.URL+"/api/v1/auth/trial/send-code", `{"phone":"13800138000"}`); status != 429 {
		t.Fatalf("resend should be 429, got %d", status)
	}
	if status, _ := postJSON(t, server.URL+"/api/v1/auth/trial/verify", `{"phone":"13800138000","code":"000000"}`); status != 400 {
		t.Fatalf("wrong code should be 400, got %d", status)
	}
	status, verified := postJSON(t, server.URL+"/api/v1/auth/trial/verify", `{"phone":"13800138000","code":"`+code+`"}`)
	if status != 200 || verified["ticket"] == nil {
		t.Fatalf("verify failed: %d %v", status, verified)
	}
	if status, _ := postJSON(t, server.URL+"/api/v1/auth/trial/verify", `{"phone":"13800138000","code":"`+code+`"}`); status != 400 {
		t.Fatalf("code should be single-use, got %d", status)
	}
}

/** 体验主页“开始体验”必须持进入令牌或一次性短信 ticket，禁止仅凭手机号+厂编码进入。 */
func TestTrialEnterRequiresCredential(t *testing.T) {
	api := &API{mux: http.NewServeMux(), Auth: auth.NewService(nil, "test-secret", "test-node", time.Minute, time.Hour)}
	api.RegisterTrialRoutes()
	server := httptest.NewServer(api.mux)
	defer server.Close()

	status, result := postJSON(t, server.URL+"/api/v1/public/trial-enter",
		`{"phone":"13800138000","tenant_code":"YZWS001"}`)
	if status != 401 || result["code"] != "MISSING_CREDENTIAL" {
		t.Fatalf("trial-enter without credential should be 401 MISSING_CREDENTIAL, got %d %v", status, result)
	}
	status, result = postJSON(t, server.URL+"/api/v1/public/trial-enter",
		`{"entry_token":"invalid-token","tenant_code":"YZWS001"}`)
	if status != 401 || result["code"] != "INVALID_ENTRY_TOKEN" {
		t.Fatalf("trial-enter with invalid token should be 401 INVALID_ENTRY_TOKEN, got %d %v", status, result)
	}
}

/** 配置了真实服务商时：不返回 demo_code，验证码由网关发送且可用于校验。 */
func TestTrialCodeFlowWithSMSProvider(t *testing.T) {
	t.Setenv("CLOTHP_SMS_WEBHOOK", "")
	var mu sync.Mutex
	var gatewayCode string
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		mu.Lock()
		gatewayCode, _ = payload["code"].(string)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer gateway.Close()

	api := &API{mux: http.NewServeMux(), SMS: sms.NewRouter(sms.Settings{
		UsageFile: "-",
		Providers: []sms.Provider{{Name: "test-gateway", URL: gateway.URL}},
	})}
	api.RegisterTrialRoutes()
	server := httptest.NewServer(api.mux)
	defer server.Close()

	modeResp, err := http.Get(server.URL + "/api/v1/auth/trial/sms-mode")
	if err != nil {
		t.Fatal(err)
	}
	var mode map[string]any
	_ = json.NewDecoder(modeResp.Body).Decode(&mode)
	_ = modeResp.Body.Close()
	if mode["demo_mode"] != false {
		t.Fatalf("demo_mode should be false when providers configured: %v", mode)
	}

	status, sent := postJSON(t, server.URL+"/api/v1/auth/trial/send-code", `{"phone":"13900139000"}`)
	if status != 200 || sent["sms_sent"] != true || sent["demo_code"] != nil {
		t.Fatalf("real sms send failed: %d %v", status, sent)
	}
	mu.Lock()
	code := gatewayCode
	mu.Unlock()
	if len(code) != 6 {
		t.Fatalf("gateway did not receive code: %q", code)
	}
	status, verified := postJSON(t, server.URL+"/api/v1/auth/trial/verify", `{"phone":"13900139000","code":"`+code+`"}`)
	if status != 200 || verified["ticket"] == nil {
		t.Fatalf("verify with gateway code failed: %d %v", status, verified)
	}
}
