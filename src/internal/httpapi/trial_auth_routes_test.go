package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTrialAuthTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	api := &API{mux: http.NewServeMux()}
	api.RegisterTrialAuthRoutes()
	server := httptest.NewServer(api.mux)
	t.Cleanup(server.Close)
	return server
}

func TestTrialAuthDemoLogin(t *testing.T) {
	server := newTrialAuthTestServer(t)

	status, result := postJSON(t, server.URL+"/api/v1/public/trial-auth/login", `{"username":"demo","password":"demo123456"}`)
	if status != http.StatusOK {
		t.Fatalf("demo login failed: %d %v", status, result)
	}
	if result["username"] != "demo" {
		t.Fatalf("username missing: %v", result)
	}
	ticket, _ := result["ticket"].(string)
	if len(ticket) < 20 {
		t.Fatalf("ticket missing: %v", result)
	}
	if status, me := getJSON(t, server.URL+"/api/v1/public/trial-auth/me?ticket="+ticket); status != http.StatusOK || me["username"] != "demo" {
		t.Fatalf("me should resolve the ticket: %d %v", status, me)
	}

	status, wrong := postJSON(t, server.URL+"/api/v1/public/trial-auth/login", `{"username":"demo","password":"wrongpass"}`)
	if status != http.StatusUnauthorized || wrong["code"] != "INVALID_CREDENTIALS" {
		t.Fatalf("wrong password should be 401: %d %v", status, wrong)
	}
	if status, unknown := postJSON(t, server.URL+"/api/v1/public/trial-auth/login", `{"username":"nobody","password":"demo123456"}`); status != http.StatusUnauthorized {
		t.Fatalf("unknown user should be 401: %d %v", status, unknown)
	}
	if status, expired := getJSON(t, server.URL+"/api/v1/public/trial-auth/me?ticket=missing"); status != http.StatusUnauthorized || expired["code"] != "SESSION_EXPIRED" {
		t.Fatalf("unknown ticket should be 401: %d %v", status, expired)
	}
}

func TestTrialAuthRegister(t *testing.T) {
	server := newTrialAuthTestServer(t)

	if status, invalid := postJSON(t, server.URL+"/api/v1/public/trial-auth/register", `{"username":"a","password":"abc123"}`); status != http.StatusBadRequest || invalid["code"] != "INVALID_USERNAME" {
		t.Fatalf("invalid username should be 400: %d %v", status, invalid)
	}
	if status, weak := postJSON(t, server.URL+"/api/v1/public/trial-auth/register", `{"username":"tester","password":"123"}`); status != http.StatusBadRequest || weak["code"] != "WEAK_PASSWORD" {
		t.Fatalf("weak password should be 400: %d %v", status, weak)
	}
	status, created := postJSON(t, server.URL+"/api/v1/public/trial-auth/register", `{"username":"tester","password":"abc123456"}`)
	if status != http.StatusOK || created["username"] != "tester" {
		t.Fatalf("register failed: %d %v", status, created)
	}
	if status, duplicate := postJSON(t, server.URL+"/api/v1/public/trial-auth/register", `{"username":"tester","password":"abc123456"}`); status != http.StatusConflict || duplicate["code"] != "USERNAME_TAKEN" {
		t.Fatalf("duplicate username should be 409: %d %v", status, duplicate)
	}
	if status, logged := postJSON(t, server.URL+"/api/v1/public/trial-auth/login", `{"username":"tester","password":"abc123456"}`); status != http.StatusOK {
		t.Fatalf("registered account should be able to login: %d %v", status, logged)
	}
	if status, demo := postJSON(t, server.URL+"/api/v1/public/trial-auth/register", `{"username":"demo","password":"abc123456"}`); status != http.StatusConflict {
		t.Fatalf("built-in demo account cannot be overwritten: %d %v", status, demo)
	}
}
