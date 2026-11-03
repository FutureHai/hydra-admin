package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hydra-admin/internal/audit"
	"hydra-admin/internal/config"
	"hydra-admin/internal/hydra"
)

type failingAudit struct{}

func (failingAudit) Ready() error             { return nil }
func (failingAudit) Append(audit.Event) error { return errors.New("simulated disk failure") }

func TestClientWriteGuardsAndPostWriteAuditFailure(t *testing.T) {
	postCalls, deleteCalls, lifespanCalls := 0, 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case r.Method == "GET" && r.URL.Path == "/health/ready":
			w.Write([]byte(`{"status":"ok"}`))
		case r.Method == "GET" && r.URL.Path == "/admin/clients":
			w.Write([]byte(`[]`))
		case r.Method == "GET" && r.URL.Path == "/admin/clients/audit-test":
			w.Write([]byte(`{"client_id":"audit-test","grant_types":["client_credentials"],"client_credentials_grant_access_token_lifespan":"2h0m0s","authorization_code_grant_access_token_lifespan":"3h0m0s"}`))
		case r.Method == "PUT" && r.URL.Path == "/admin/clients/audit-test/lifespans":
			lifespanCalls++
			w.Write([]byte(`{"client_id":"audit-test","grant_types":["client_credentials"],"client_credentials_grant_access_token_lifespan":"4h0m0s","authorization_code_grant_access_token_lifespan":"3h0m0s"}`))
		case r.Method == "POST" && r.URL.Path == "/admin/clients":
			postCalls++
			w.WriteHeader(201)
			w.Write([]byte(`{"client_id":"audit-test","grant_types":["client_credentials"]}`))
		case r.Method == "DELETE" && r.URL.Path == "/admin/clients/audit-test":
			deleteCalls++
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	adapter, _ := hydra.NewHTTPAdapter(upstream.URL, time.Second)
	s, _ := testServer(t)
	s.Config.ResourceCatalog = config.ResourceCatalog{Audiences: []config.ResourceDefinition{{Audience: "https://api.example.test/orders", Scopes: []string{"orders.read"}}}}
	s.HydraAdapter = adapter
	s.HydraReport = hydra.Probe(context.Background(), adapter, hydra.NewRegistry())
	body := `{"fields":{"client_id":"audit-test","grant_types":["client_credentials"]}}`
	url := "/api/v1/clients"
	if got := request(t, s, "POST", url, body, nil, nil); got.Code != 401 || postCalls != 0 {
		t.Fatal("anonymous write reached Hydra")
	}
	nonce, preCookies := csrf(t, s, nil)
	login := request(t, s, "POST", "/api/v1/auth/login", `{"username":"admin","password":"test-only-password"}`, preCookies, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": nonce})
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
		t.Fatal("missing session")
	}
	cookies := []*http.Cookie{session}
	if got := request(t, s, "POST", url, body, cookies, map[string]string{"Origin": "http://127.0.0.1:8085"}); got.Code != 403 || postCalls != 0 {
		t.Fatal("missing CSRF reached Hydra")
	}
	token, _ := csrf(t, s, cookies)
	invalidAssignment := `{"fields":{"client_id":"audit-test","grant_types":["client_credentials"],"scope":"orders.read","audience":["https://api.example.test/other"]}}`
	if got := request(t, s, "POST", url, invalidAssignment, cookies, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": token}); got.Code != 422 || postCalls != 0 || !strings.Contains(got.Body.String(), `"resource_assignment_invalid"`) {
		t.Fatal("invalid resource assignment reached Hydra")
	}
	if got := request(t, s, "DELETE", "/api/v1/clients/audit-test", "", cookies, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": token}); got.Code != 400 || deleteCalls != 0 {
		t.Fatal("delete without typed ID reached Hydra")
	}
	client, readErr := adapter.GetClient(context.Background(), "audit-test")
	if readErr != nil {
		t.Fatal(readErr.Code)
	}
	lifespanURL := "/api/v1/clients/audit-test/lifespans"
	lifespanBody := `{"base_revision":"` + client.Revision() + `","changes":{"client_credentials_grant_access_token_lifespan":"4h"}}`
	if got := request(t, s, "PUT", lifespanURL, lifespanBody, cookies, map[string]string{"Origin": "http://127.0.0.1:8085"}); got.Code != 403 || lifespanCalls != 0 {
		t.Fatal("lifespan PUT without CSRF reached Hydra")
	}
	if got := request(t, s, "PUT", lifespanURL, lifespanBody, cookies, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": token}); got.Code != 200 || lifespanCalls != 1 {
		t.Fatalf("audited lifespan PUT failed: status=%d calls=%d", got.Code, lifespanCalls)
	}
	s.Audit = failingAudit{}
	response := request(t, s, "POST", url, body, cookies, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": token})
	if response.Code != 500 || postCalls != 1 || response.Header().Get("X-Request-ID") == "" || !strings.Contains(response.Body.String(), `"code":"audit_write_failed"`) || !strings.Contains(response.Body.String(), "可能已成功") {
		t.Fatalf("post-write audit boundary: status=%d calls=%d", response.Code, postCalls)
	}
}
