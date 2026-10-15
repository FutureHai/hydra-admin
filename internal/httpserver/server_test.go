package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"hydra-admin/internal/audit"
	"hydra-admin/internal/auth"
	"hydra-admin/internal/config"
	"hydra-admin/internal/hydra"
)

func testServer(t *testing.T) (*Server, string) {
	t.Helper()
	temp := t.TempDir()
	configText, err := os.ReadFile("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.GeneratePasswordHash([]byte("test-only-password"), "bcrypt")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(configText), "https://<ADMIN_CONSOLE_FQDN>", "http://127.0.0.1:8085", 1)
	text = strings.Replace(text, "cookie_secure: true", "cookie_secure: false", 1)
	text = strings.Replace(text, "$argon2id$v=19$m=65536,t=3,p=2$REPLACE_WITH_BASE64_SALT$REPLACE_WITH_BASE64_HASH", hash, 1)
	path := filepath.Join(temp, "config.yaml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := audit.NewRotatingWriter(cfg.Audit.Path, cfg.Audit.MaxFileBytes, cfg.Audit.MaxArchives, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	web := fstest.MapFS{
		"index.html":              &fstest.MapFile{Data: []byte(`<html><head><meta name="csp-nonce" content="__CSP_NONCE__"></head><body>app</body></html>`)},
		"assets/app-Abc12345.css": &fstest.MapFile{Data: []byte("body{color:blue}")},
	}
	var _ fs.FS = web
	s, err := New(cfg, web, audit.NewRecorder(w), slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return s, cfg.Audit.Path
}

func request(t *testing.T, s *Server, method, target, body string, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	for _, c := range cookies {
		r.AddCookie(c)
	}
	for name, value := range headers {
		r.Header.Set(name, value)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func csrf(t *testing.T, s *Server, cookies []*http.Cookie) (string, []*http.Cookie) {
	t.Helper()
	w := request(t, s, http.MethodGet, "/api/v1/auth/csrf", "", cookies, nil)
	if w.Code != 200 {
		t.Fatalf("csrf: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Token string `json:"csrf_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Token == "" {
		t.Fatalf("csrf response: %v", err)
	}
	return body.Token, w.Result().Cookies()
}

func TestLoginLogoutSessionAndCSRFEndToEnd(t *testing.T) {
	s, auditPath := testServer(t)
	if got := request(t, s, "GET", "/healthz", "", nil, nil); got.Code != 200 || strings.Contains(got.Body.String(), "Hydra") {
		t.Fatal("health leaked details")
	}
	if got := request(t, s, "GET", "/api/v1/auth/session", "", nil, nil); got.Code != 401 || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("anonymous session not rejected")
	}
	input := `{"username":"admin","password":"test-only-password"}`
	nonce, preCookies := csrf(t, s, nil)
	if got := request(t, s, "POST", "/api/v1/auth/login", input, preCookies, map[string]string{"X-CSRF-Token": nonce}); got.Code != 403 {
		t.Fatalf("missing Origin accepted: %d", got.Code)
	}
	nonce, preCookies = csrf(t, s, nil)
	if got := request(t, s, "POST", "/api/v1/auth/login", input, preCookies, map[string]string{"Origin": "http://127.0.0.1:8085"}); got.Code != 403 {
		t.Fatalf("missing CSRF accepted: %d", got.Code)
	}
	nonce, preCookies = csrf(t, s, nil)
	login := request(t, s, "POST", "/api/v1/auth/login", input, preCookies, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": nonce})
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == s.Config.Auth.SessionCookieName {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("missing secure session cookie attributes")
	}
	if got := request(t, s, "GET", "/api/v1/auth/session", "", []*http.Cookie{sessionCookie}, nil); got.Code != 200 || !strings.Contains(got.Body.String(), `"username":"admin"`) {
		t.Fatal("session check failed")
	}
	sessionCSRF, _ := csrf(t, s, []*http.Cookie{sessionCookie})
	if sessionCSRF == nonce {
		t.Fatal("pre-auth CSRF reused as session CSRF")
	}
	if got := request(t, s, "POST", "/api/v1/auth/logout", "", []*http.Cookie{sessionCookie}, map[string]string{"Origin": "http://127.0.0.1:8085"}); got.Code != 403 {
		t.Fatalf("logout without CSRF: %d", got.Code)
	}
	if got := request(t, s, "POST", "/api/v1/auth/logout", "", []*http.Cookie{sessionCookie}, map[string]string{"Origin": "http://other.test", "X-CSRF-Token": sessionCSRF}); got.Code != 403 {
		t.Fatalf("logout with bad Origin: %d", got.Code)
	}
	if got := request(t, s, "GET", "/api/v1/auth/session", "", []*http.Cookie{sessionCookie}, nil); got.Code != 200 {
		t.Fatal("rejected logout changed session")
	}
	logout := request(t, s, "POST", "/api/v1/auth/logout", "", []*http.Cookie{sessionCookie}, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": sessionCSRF})
	if logout.Code != 204 {
		t.Fatalf("logout: %d %s", logout.Code, logout.Body.String())
	}
	if got := request(t, s, "GET", "/api/v1/auth/session", "", []*http.Cookie{sessionCookie}, nil); got.Code != 401 {
		t.Fatal("logged-out session remained valid")
	}
	auditData, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(auditData), "test-only-password") || !strings.Contains(string(auditData), `"action":"login"`) || !strings.Contains(string(auditData), `"action":"logout"`) {
		t.Fatalf("audit invalid: %s", auditData)
	}
}

func TestUnsupportedVersionKeepsLoginAndStatusButClosesBusiness(t *testing.T) {
	s, _ := testServer(t)
	s.HydraReport = hydra.Report{Status: "unsupported_version", SupportedVersion: hydra.Version26, SpecSHA256: hydra.SpecSHA26,
		Capabilities: map[string]hydra.Capability{"client_list": {Reason: "unsupported_version"}, "token_revoke": {Reason: "unsupported_version"}}}
	if got := request(t, s, "GET", "/api/v1/system/status", "", nil, nil); got.Code != 401 {
		t.Fatal("anonymous status access accepted")
	}
	nonce, preCookies := csrf(t, s, nil)
	login := request(t, s, "POST", "/api/v1/auth/login", `{"username":"admin","password":"test-only-password"}`, preCookies,
		map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": nonce})
	if login.Code != 200 {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	var session *http.Cookie
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == s.Config.Auth.SessionCookieName {
			session = cookie
		}
	}
	if session == nil {
		t.Fatal("session cookie missing")
	}
	status := request(t, s, "GET", "/api/v1/system/status", "", []*http.Cookie{session}, nil)
	if status.Code != 200 || !strings.Contains(status.Body.String(), `"status":"unsupported_version"`) {
		t.Fatalf("status: %d %s", status.Code, status.Body.String())
	}
	for _, target := range []string{"/api/v1/clients", "/api/v1/clients/example", "/api/v1/tokens/introspect", "/api/v1/tokens/revoke", "/api/v1/scopes", "/api/v1/audiences"} {
		got := request(t, s, "GET", target, "", []*http.Cookie{session}, nil)
		if got.Code != 409 || !strings.Contains(got.Body.String(), `"code":"unsupported_version"`) {
			t.Fatalf("business route %s: %d %s", target, got.Code, got.Body.String())
		}
	}
	sessionCSRF, _ := csrf(t, s, []*http.Cookie{session})
	write := request(t, s, "POST", "/api/v1/tokens/introspect", "", []*http.Cookie{session},
		map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": sessionCSRF})
	if write.Code != 409 || !strings.Contains(write.Body.String(), `"code":"unsupported_version"`) {
		t.Fatalf("business write not closed: %d %s", write.Code, write.Body.String())
	}
}

func TestClusterStatusAndClientGateUseLiveReplicaReport(t *testing.T) {
	s, _ := testServer(t)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case "/health/ready":
			w.Write([]byte(`{"status":"ok"}`))
		case "/admin/clients":
			w.Write([]byte(`[]`))
		}
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/version" {
			w.Write([]byte(`{"version":"v26.2.1"}`))
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer bad.Close()
	cluster, err := hydra.NewClusterAdapter([]hydra.AdminReplica{{Name: "good", URL: good.URL}, {Name: "bad", URL: bad.URL}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	s.HydraAdapter = cluster
	s.HydraReport = hydra.Report{Status: "unsupported_version"}
	if !cluster.Report(context.Background()).BusinessAllowed("client_list") {
		t.Fatal("healthy replica was not enabled")
	}
	nonce, preCookies := csrf(t, s, nil)
	login := request(t, s, "POST", "/api/v1/auth/login", `{"username":"admin","password":"test-only-password"}`, preCookies,
		map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": nonce})
	if login.Code != http.StatusOK {
		t.Fatalf("login failed: %d", login.Code)
	}
	cookies := login.Result().Cookies()
	status := request(t, s, "GET", "/api/v1/system/status", "", cookies, nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"name":"bad"`) || !strings.Contains(status.Body.String(), `"eligible":false`) || strings.Contains(status.Body.String(), good.URL) {
		t.Fatalf("cluster status invalid: %d %s", status.Code, status.Body.String())
	}
	list := request(t, s, "GET", "/api/v1/clients", "", cookies, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("live cluster gate rejected Client list: %d %s", list.Code, list.Body.String())
	}
}

func TestStaticSPAHeadersCSPAndBodyLimit(t *testing.T) {
	s, _ := testServer(t)
	root := request(t, s, "GET", "/", "", nil, nil)
	if root.Code != 200 || strings.Contains(root.Body.String(), "__CSP_NONCE__") || root.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("dynamic index invalid")
	}
	for _, name := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "X-Frame-Options", "X-Request-ID"} {
		if root.Header().Get(name) == "" {
			t.Fatalf("missing %s", name)
		}
	}
	nonce := strings.Split(strings.Split(root.Body.String(), `content="`)[1], `"`)[0]
	csp := root.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "'nonce-"+nonce+"'") || strings.Contains(csp, "unsafe-eval") || !strings.Contains(csp, "style-src-attr 'unsafe-inline'") {
		t.Fatalf("CSP invalid: %s", csp)
	}
	if got := request(t, s, "GET", "/clients", "", nil, nil); got.Code != 200 || !strings.Contains(got.Body.String(), "app") {
		t.Fatal("SPA fallback failed")
	}
	if got := request(t, s, "GET", "/api/v1/unknown", "", nil, nil); got.Code != 404 || strings.Contains(got.Body.String(), "<html") {
		t.Fatal("API fallback returned HTML")
	}
	if got := request(t, s, "POST", "/clients", "", nil, nil); got.Code != 404 {
		t.Fatal("POST SPA fallback accepted")
	}
	if got := request(t, s, "GET", "/assets/app-Abc12345.css", "", nil, nil); got.Code != 200 || !strings.Contains(got.Header().Get("Cache-Control"), "immutable") {
		t.Fatal("static asset cache invalid")
	}
	if got := request(t, s, "POST", "/api/v1/auth/login", strings.Repeat("x", 1048577), nil, nil); got.Code != 413 {
		t.Fatalf("body limit: %d", got.Code)
	}
}

func TestInvalidPreAuthLoginIsIPLimited(t *testing.T) {
	s, _ := testServer(t)
	input := `{"username":"admin","password":"test-only-password"}`
	for i := 0; i < s.Config.Security.APIRateLimit.WritePerMinutePerIP; i++ {
		got := request(t, s, "POST", "/api/v1/auth/login", input, nil, nil)
		if got.Code != 403 {
			t.Fatalf("invalid pre-auth request %d: %d", i, got.Code)
		}
	}
	got := request(t, s, "POST", "/api/v1/auth/login", input, nil, nil)
	if got.Code != 429 || got.Header().Get("Retry-After") == "" {
		t.Fatalf("unlimited invalid pre-auth attempts: %d", got.Code)
	}
}

func TestForgedForwardedHeadersCannotEvadeRateLimit(t *testing.T) {
	for _, tc := range []struct {
		name, remote, forwarded string
		additionalTrusted       string
	}{
		{"untrusted direct peer", "203.0.113.10:50000", "198.51.100.%d", ""},
		{"single edge proxy", "127.0.0.1:50000", "198.51.100.9", ""},
		{"two trusted proxy hops with forged left prefix", "127.0.0.1:50000", "192.0.2.%d, 198.51.100.9, 10.0.0.2", "10.0.0.0/8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := testServer(t)
			if tc.additionalTrusted != "" {
				ranges, err := auth.ParseTrustedCIDRs([]string{"127.0.0.1/32", tc.additionalTrusted})
				if err != nil {
					t.Fatal(err)
				}
				s.proxies = ranges
			}
			attempt := func(i int) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"test-only-password"}`))
				r.RemoteAddr = tc.remote
				forwarded := tc.forwarded
				if strings.Contains(forwarded, "%d") {
					forwarded = fmt.Sprintf(forwarded, i%200+1)
				}
				r.Header.Set("X-Forwarded-For", forwarded)
				r.Header.Set("X-Forwarded-Proto", "https")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, r)
				return w
			}
			for i := 0; i < s.Config.Security.APIRateLimit.WritePerMinutePerIP; i++ {
				if got := attempt(i); got.Code != 403 {
					t.Fatalf("attempt %d: %d", i, got.Code)
				}
			}
			if got := attempt(100); got.Code != 429 {
				t.Fatalf("forged header evaded rate limit: %d", got.Code)
			}
		})
	}
}
