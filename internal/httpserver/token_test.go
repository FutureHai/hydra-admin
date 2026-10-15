package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"hydra-admin/internal/hydra"
)

func TestTokenHTTPGuardsWhitelistAndAuditBoundary(t *testing.T) {
	introCalls, revokeCalls := 0, 0
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case r.Method == "GET" && r.URL.Path == "/health/ready":
			w.Write([]byte(`{"status":"ok"}`))
		case r.Method == "GET" && r.URL.Path == "/admin/clients":
			w.Write([]byte(`[]`))
		case r.Method == "GET" && r.URL.Path == "/admin/clients/token-owner":
			w.Write([]byte(`{"client_id":"token-owner","token_endpoint_auth_method":"client_secret_basic"}`))
		case r.Method == "POST" && r.URL.Path == "/admin/oauth2/introspect":
			introCalls++
			w.Write([]byte(`{"active":true,"client_id":"token-owner","scope":"read","ext":{"token":"fixture-sensitive-token"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer admin.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { revokeCalls++; w.WriteHeader(200) }))
	defer public.Close()
	a, _ := hydra.NewHTTPAdapter(admin.URL, time.Second)
	if err := a.ConfigurePublicRevoke(public.URL); err != nil {
		t.Fatal(err)
	}
	s, auditPath := testServer(t)
	s.HydraAdapter = a
	s.HydraReport = hydra.Probe(context.Background(), a, hydra.NewRegistry())
	if !s.HydraReport.BusinessAllowed("token_introspect") || !s.HydraReport.BusinessAllowed("token_revoke") || s.HydraReport.BusinessAllowed("secret_rotate") {
		t.Fatal("wrong Token capabilities")
	}
	body := `{"token":"fixture-sensitive-token"}`
	if got := request(t, s, "POST", "/api/v1/tokens/introspect", body, nil, nil); got.Code != 401 || introCalls != 0 {
		t.Fatal("anonymous introspection reached Hydra")
	}
	nonce, pre := csrf(t, s, nil)
	login := request(t, s, "POST", "/api/v1/auth/login", `{"username":"admin","password":"test-only-password"}`, pre, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": nonce})
	if login.Code != 200 {
		t.Fatal("login failed")
	}
	var session *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == s.Config.Auth.SessionCookieName {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("session missing")
	}
	cookies := []*http.Cookie{session}
	if got := request(t, s, "POST", "/api/v1/tokens/introspect", body, cookies, map[string]string{"Origin": "http://127.0.0.1:8085"}); got.Code != 403 || introCalls != 0 {
		t.Fatal("missing CSRF reached Hydra")
	}
	token, _ := csrf(t, s, cookies)
	headers := map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": token}
	rotation := request(t, s, "POST", "/api/v1/clients/token-owner/secret/rotate", "", cookies, headers)
	if rotation.Code != 503 || !strings.Contains(rotation.Body.String(), `"code":"rotation_operation_absent"`) {
		t.Fatal("unconfirmed Secret rotation was not explicitly disabled")
	}
	read := request(t, s, "POST", "/api/v1/tokens/introspect", body, cookies, headers)
	if read.Code != 200 || introCalls != 1 || strings.Contains(read.Body.String(), "fixture-sensitive-token") || strings.Contains(read.Body.String(), "ext") || !strings.Contains(read.Body.String(), `"client_id":"token-owner"`) {
		t.Fatal("introspection response not whitelisted")
	}
	badConfirm := `{"token":"fixture-sensitive-token","client_id":"token-owner","client_secret":"fixture-secret","confirm_client_id":"other"}`
	if got := request(t, s, "POST", "/api/v1/tokens/revoke", badConfirm, cookies, headers); got.Code != 400 || revokeCalls != 0 {
		t.Fatal("wrong confirmation reached Public API")
	}
	good := strings.Replace(badConfirm, `"confirm_client_id":"other"`, `"confirm_client_id":"token-owner"`, 1)
	accepted := request(t, s, "POST", "/api/v1/tokens/revoke", good, cookies, headers)
	if accepted.Code != 200 || revokeCalls != 1 || !strings.Contains(accepted.Body.String(), `"accepted":true`) {
		t.Fatal("revoke flow failed")
	}
	auditData, err := os.ReadFile(auditPath)
	if err != nil || strings.Contains(string(auditData), "fixture-sensitive-token") || strings.Contains(string(auditData), "fixture-secret") {
		t.Fatal("token or secret leaked to audit")
	}
	s.Audit = failingAudit{}
	auditFailure := request(t, s, "POST", "/api/v1/tokens/revoke", good, cookies, headers)
	if auditFailure.Code != 500 || revokeCalls != 2 || !strings.Contains(auditFailure.Body.String(), `"code":"audit_write_failed"`) {
		t.Fatal("post-revoke audit failure boundary failed")
	}
}
