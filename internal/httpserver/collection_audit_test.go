package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"hydra-admin/internal/audit"
	"hydra-admin/internal/hydra"
)

func TestCollectionAndAuditAuthenticatedReadOnly(t *testing.T) {
	version := hydra.Version26
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected Hydra write: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			json.NewEncoder(w).Encode(map[string]string{"version": version})
		case "/health/ready":
			w.Write([]byte(`{"status":"ok"}`))
		case "/admin/clients":
			w.Write([]byte(`[{"client_id":"a","scope":"read write","audience":["api-a"]},{"client_id":"b","scope":"read","audience":["api-a","api-b"]},{"client_id":"c"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	adapter, err := hydra.NewHTTPAdapter(upstream.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := testServer(t)
	s.HydraAdapter = adapter
	s.HydraReport = hydra.Probe(context.Background(), adapter, hydra.NewRegistry())
	if !s.HydraReport.BusinessAllowed("scopes") || !s.HydraReport.BusinessAllowed("audiences") {
		t.Fatal("aggregation capability closed")
	}
	if got := request(t, s, "GET", "/api/v1/scopes", "", nil, nil); got.Code != 401 {
		t.Fatal("anonymous aggregation accepted")
	}
	if got := request(t, s, "GET", "/api/v1/audit", "", nil, nil); got.Code != 401 {
		t.Fatal("anonymous audit accepted")
	}
	nonce, preCookies := csrf(t, s, nil)
	login := request(t, s, "POST", "/api/v1/auth/login", `{"username":"admin","password":"test-only-password"}`, preCookies, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": nonce})
	if login.Code != 200 {
		t.Fatalf("login: %d", login.Code)
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
	for _, tc := range []struct{ route, want string }{
		{"/api/v1/scopes", `"value":"read","client_count":2`},
		{"/api/v1/audiences", `"value":"api-a","client_count":2`},
	} {
		got := request(t, s, "GET", tc.route, "", cookies, nil)
		if got.Code != 200 || !strings.Contains(got.Body.String(), tc.want) || !strings.Contains(got.Body.String(), `"complete":false`) || !strings.Contains(got.Body.String(), "可能不完整") {
			t.Fatalf("aggregation %s: %d %s", tc.route, got.Code, got.Body.String())
		}
	}
	if got := request(t, s, "GET", "/api/v1/scopes?path=/tmp/x", "", cookies, nil); got.Code != 400 {
		t.Fatal("collection query accepted")
	}
	eventTime := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	if err := s.auditReader.Append(audit.Event{Timestamp: eventTime, Action: "client.update", TargetType: "client", TargetID: "a", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	filter := url.Values{"action": {"client.update"}, "client_id": {"a"}, "from": {"2026-10-13T00:00:00Z"}, "to": {"2026-10-13T02:00:00Z"}}
	got := request(t, s, "GET", "/api/v1/audit?"+filter.Encode(), "", cookies, nil)
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"action":"client.update"`) || strings.Contains(got.Body.String(), `"action":"login"`) {
		t.Fatalf("audit filter: %d %s", got.Code, got.Body.String())
	}
	for _, raw := range []string{"path=/tmp/secret", "limit=10000", "action=a&action=b", "from=bad", "from=2026-10-14T00%3A00%3A00Z&to=2026-10-13T00%3A00%3A00Z"} {
		if got := request(t, s, "GET", "/api/v1/audit?"+raw, "", cookies, nil); got.Code != 400 {
			t.Fatalf("invalid audit query accepted: %s", raw)
		}
	}
	version = "v26.2.1"
	if got := request(t, s, "GET", "/api/v1/scopes", "", cookies, nil); got.Code != 409 {
		t.Fatal("runtime version drift did not close aggregation")
	}
	if got := request(t, s, "GET", "/api/v1/audit", "", cookies, nil); got.Code != 200 {
		t.Fatal("Hydra mismatch incorrectly closed local audit")
	}
}
