package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func getJSON(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	result := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func TestLoginQRConfirmFlow(t *testing.T) {
	api := &API{mux: http.NewServeMux()}
	api.RegisterLoginQRRoutes()
	server := httptest.NewServer(api.mux)
	defer server.Close()

	if status, _ := postJSON(t, server.URL+"/api/v1/public/login-qr/create", `{"app":"unknown"}`); status != 400 {
		t.Fatalf("unknown app should be 400, got %d", status)
	}
	status, created := postJSON(t, server.URL+"/api/v1/public/login-qr/create", `{"app":"zhisuan"}`)
	if status != 200 || created["app_name"] != "一智算" {
		t.Fatalf("create failed: %d %v", status, created)
	}
	ticket, _ := created["ticket"].(string)
	if len(ticket) < 20 {
		t.Fatalf("ticket missing: %v", created)
	}

	if status, state := getJSON(t, server.URL+"/api/v1/public/login-qr/status?ticket="+ticket); status != 200 || state["status"] != "pending" {
		t.Fatalf("initial state should be pending: %d %v", status, state)
	}
	if status, state := postJSON(t, server.URL+"/api/v1/public/login-qr/scan", `{"ticket":"`+ticket+`"}`); status != 200 || state["status"] != "scanned" {
		t.Fatalf("scan failed: %d %v", status, state)
	}
	if status, state := postJSON(t, server.URL+"/api/v1/public/login-qr/confirm", `{"ticket":"`+ticket+`"}`); status != 200 || state["status"] != "confirmed" {
		t.Fatalf("confirm failed: %d %v", status, state)
	}
	if status, state := getJSON(t, server.URL+"/api/v1/public/login-qr/status?ticket="+ticket); status != 200 || state["status"] != "confirmed" {
		t.Fatalf("PC poll should see confirmed: %d %v", status, state)
	}
	if status, _ := postJSON(t, server.URL+"/api/v1/public/login-qr/confirm", `{"ticket":"`+ticket+`"}`); status != 409 {
		t.Fatalf("duplicate confirm should be rejected, got %d", status)
	}
	if status, _ := getJSON(t, server.URL+"/api/v1/public/login-qr/status?ticket=missing"); status != 404 {
		t.Fatalf("unknown ticket should be 404, got %d", status)
	}
}

func TestLoginQRCancelFlow(t *testing.T) {
	api := &API{mux: http.NewServeMux()}
	api.RegisterLoginQRRoutes()
	server := httptest.NewServer(api.mux)
	defer server.Close()

	_, created := postJSON(t, server.URL+"/api/v1/public/login-qr/create", `{"app":"couhe"}`)
	ticket, _ := created["ticket"].(string)
	if status, state := postJSON(t, server.URL+"/api/v1/public/login-qr/cancel", `{"ticket":"`+ticket+`"}`); status != 200 || state["status"] != "cancelled" {
		t.Fatalf("cancel failed: %d %v", status, state)
	}
	if status, _ := postJSON(t, server.URL+"/api/v1/public/login-qr/confirm", `{"ticket":"`+ticket+`"}`); status != 409 {
		t.Fatalf("cancelled ticket cannot be confirmed, got %d", status)
	}
}
