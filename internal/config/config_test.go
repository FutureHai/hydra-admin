package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validYAML = `server:
  bind_address: "127.0.0.1"
  port: 8085
  public_base_url: "http://127.0.0.1:8085"
  trusted_proxy_cidrs: ["127.0.0.1/32"]
  read_timeout: 15s
  read_header_timeout: 5s
  write_timeout: 30s
  idle_timeout: 60s
hydra:
  admin_base_url: "http://127.0.0.1:4445"
  public_api_base_url: ""
  timeout: 10s
  token_revoke_enabled: false
auth:
  username: "admin"
  password_hash: "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
  session_ttl: 8h
  session_cookie_name: "hydra_admin_session"
  max_sessions: 20
  login:
    window: 15m
    max_attempts_per_ip: 10
    max_attempts_per_username: 5
    lockout_duration: 15m
security:
  csrf_header_name: "X-CSRF-Token"
  cookie_secure: false
  cookie_same_site: "lax"
  api_rate_limit:
    read_per_minute_per_ip: 120
    write_per_minute_per_ip: 30
  request_body_max_bytes: 1048576
audit:
  path: "./logs/audit.jsonl"
  max_file_bytes: 1024
  max_archives: 2
  file_mode: "0600"
log:
  level: "info"
  format: "json"
  path: "./logs/app.jsonl"
  max_file_bytes: 1024
  max_archives: 2
  file_mode: "0600"
`

func loadText(t *testing.T, body string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestStrictYAMLAndValidation(t *testing.T) {
	cfg, err := loadText(t, validYAML)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(cfg.Audit.Path) || cfg.Server.ReadTimeout.Value().Seconds() != 15 {
		t.Fatal("config not normalized")
	}
	if err := cfg.CheckWritablePaths(); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, old, new string }{
		{"unknown top level", "server:\n", "surprise: true\nserver:\n"},
		{"unknown nested", "  port: 8085", "  port: 8085\n  surprise: true"},
		{"empty user", `username: "admin"`, `username: ""`},
		{"empty hash", `password_hash: "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"`, `password_hash: ""`},
		{"duration without unit", "read_timeout: 15s", "read_timeout: 15"},
		{"invalid duration", "read_timeout: 15s", "read_timeout: bad"},
		{"invalid CIDR", `"127.0.0.1/32"`, `"127.0.0.1/99"`},
		{"relative escape", `path: "./logs/audit.jsonl"`, `path: "../audit.jsonl"`},
		{"cookie conflict", "cookie_secure: false", "cookie_secure: true"},
		{"cross-host HTTP Hydra", `admin_base_url: "http://127.0.0.1:4445"`, `admin_base_url: "http://192.0.2.1:4445"`},
		{"userinfo Hydra", `admin_base_url: "http://127.0.0.1:4445"`, `admin_base_url: "https://user:pass@example.test"`},
		{"fragment Hydra", `admin_base_url: "http://127.0.0.1:4445"`, `admin_base_url: "https://example.test/#part"`},
		{"bad scheme", `admin_base_url: "http://127.0.0.1:4445"`, `admin_base_url: "ftp://example.test"`},
		{"revoke no public URL", "token_revoke_enabled: false", "token_revoke_enabled: true"},
		{"preauth cookie collision", `session_cookie_name: "hydra_admin_session"`, `session_cookie_name: "hydra_admin_pre_auth"`},
		{"unknown log field", `format: "json"`, `format: "json"\n  secret: foo`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadText(t, strings.Replace(validYAML, tc.old, tc.new, 1)); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	if _, err := loadText(t, validYAML+"---\n{}\n"); err == nil {
		t.Fatal("multiple YAML documents accepted")
	}
}

func TestProductionURLAndCookie(t *testing.T) {
	text := strings.ReplaceAll(validYAML, `public_base_url: "http://127.0.0.1:8085"`, `public_base_url: "https://admin.example.test"`)
	if _, err := loadText(t, text); err == nil {
		t.Fatal("HTTPS with insecure cookie accepted")
	}
	text = strings.Replace(text, "cookie_secure: false", "cookie_secure: true", 1)
	if _, err := loadText(t, text); err != nil {
		t.Fatal(err)
	}
}

func TestInsecureHTTPProxyRequiresExplicitTrustedHost(t *testing.T) {
	proxyConfig := strings.NewReplacer(
		`bind_address: "127.0.0.1"`, `bind_address: "10.0.0.12"`,
		`public_base_url: "http://127.0.0.1:8085"`, `public_base_url: "http://admin.example.test"`,
		`"127.0.0.1/32"`, `"10.0.0.11/32"`,
		"  cookie_secure: false", "  cookie_secure: false\n  allow_insecure_http: true",
	).Replace(validYAML)
	if cfg, err := loadText(t, proxyConfig); err != nil || !cfg.Security.AllowInsecureHTTP || !cfg.HTTPProxyOnly() {
		t.Fatalf("trusted HTTP proxy configuration rejected: %v", err)
	}
	for _, tc := range []struct{ name, body string }{
		{"missing opt-in", strings.Replace(proxyConfig, "  allow_insecure_http: true\n", "", 1)},
		{"missing proxy", strings.Replace(proxyConfig, `"10.0.0.11/32"`, `"127.0.0.1/32"`, 1)},
		{"broad proxy range", strings.Replace(proxyConfig, `"10.0.0.11/32"`, `"10.0.0.0/24"`, 1)},
		{"secure cookie over HTTP", strings.Replace(proxyConfig, "cookie_secure: false", "cookie_secure: true", 1)},
		{"loopback bind", strings.Replace(proxyConfig, `bind_address: "10.0.0.12"`, `bind_address: "127.0.0.1"`, 1)},
		{"HTTPS with HTTP opt-in", strings.Replace(proxyConfig, `public_base_url: "http://admin.example.test"`, `public_base_url: "https://admin.example.test"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadText(t, tc.body); err == nil {
				t.Fatal("unsafe HTTP proxy configuration accepted")
			}
		})
	}
}

func TestInsecureHTTPDirectRequiresMatchingPrivateAddress(t *testing.T) {
	directConfig := strings.NewReplacer(
		`bind_address: "127.0.0.1"`, `bind_address: "10.0.0.12"`,
		`public_base_url: "http://127.0.0.1:8085"`, `public_base_url: "http://10.0.0.12:8085"`,
		"  cookie_secure: false", "  cookie_secure: false\n  allow_insecure_http: true",
	).Replace(validYAML)
	if cfg, err := loadText(t, directConfig); err != nil || !cfg.Security.AllowInsecureHTTP || cfg.HTTPProxyOnly() {
		t.Fatalf("private HTTP direct configuration rejected: %v", err)
	}
	for _, tc := range []struct{ name, body string }{
		{"missing opt-in", strings.Replace(directConfig, "  allow_insecure_http: true\n", "", 1)},
		{"public IP", strings.ReplaceAll(directConfig, "10.0.0.12", "203.0.113.12")},
		{"different private IP", strings.Replace(directConfig, `public_base_url: "http://10.0.0.12:8085"`, `public_base_url: "http://10.0.0.13:8085"`, 1)},
		{"different port", strings.Replace(directConfig, `public_base_url: "http://10.0.0.12:8085"`, `public_base_url: "http://10.0.0.12:8086"`, 1)},
		{"secure cookie over HTTP", strings.Replace(directConfig, "cookie_secure: false", "cookie_secure: true", 1)},
		{"remote trusted proxy", strings.Replace(directConfig, `"127.0.0.1/32"`, `"10.0.0.11/32"`, 1)},
		{"wildcard bind", strings.Replace(directConfig, `bind_address: "10.0.0.12"`, `bind_address: "0.0.0.0"`, 1)},
		{"domain instead of IP", strings.Replace(directConfig, `public_base_url: "http://10.0.0.12:8085"`, `public_base_url: "http://admin.example.test:8085"`, 1)},
		{"old mode flag", strings.Replace(directConfig, "  allow_insecure_http: true", "  allow_insecure_http_direct: true", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := loadText(t, tc.body); err == nil {
				t.Fatal("unsafe HTTP direct configuration accepted")
			}
		})
	}
}

func TestNamedHydraReplicas(t *testing.T) {
	const single = `  admin_base_url: "http://127.0.0.1:4445"`
	multiple := `  admin_replicas:
    - name: hydra-a
      admin_base_url: "http://127.0.0.1:4445"
    - name: hydra-b
      admin_base_url: "https://hydra-b.internal.example:4445"`
	cfg, err := loadText(t, strings.Replace(validYAML, single, multiple, 1))
	if err != nil || len(cfg.ConfiguredReplicas()) != 2 || cfg.ConfiguredReplicas()[1].Name != "hydra-b" {
		t.Fatalf("valid replicas rejected: %v", err)
	}
	for _, text := range []string{
		strings.Replace(validYAML, single, single+"\n"+multiple, 1),
		strings.Replace(validYAML, single, strings.Replace(multiple, "hydra-b", "hydra-a", 1), 1),
		strings.Replace(validYAML, single, strings.Replace(multiple, "https://hydra-b.internal.example:4445", "http://127.0.0.1:4445", 1), 1),
		strings.Replace(validYAML, single, strings.Replace(multiple, "https://hydra-b.internal.example:4445", "http://192.0.2.2:4445", 1), 1),
		strings.Replace(validYAML, single, strings.Replace(multiple, "name: hydra-b", "name: bad/name", 1), 1),
	} {
		if _, err := loadText(t, text); err == nil {
			t.Fatal("invalid replica configuration accepted")
		}
	}
}

func TestResourceCatalogValidation(t *testing.T) {
	const catalog = `resource_catalog:
  global_scopes: ["openid", "offline"]
  audiences:
    - audience: "https://api.example.test/orders"
      scopes: ["orders.read", "orders.write"]
    - audience: "https://api.example.test/billing"
      scopes: ["billing.read"]
`
	cfg, err := loadText(t, validYAML+catalog)
	if err != nil || !cfg.ResourceCatalog.Enabled() || len(cfg.ResourceCatalog.Audiences) != 2 {
		t.Fatalf("valid resource catalog rejected: %v", err)
	}
	for _, body := range []string{
		strings.Replace(catalog, "billing.read", "orders.read", 1),
		strings.Replace(catalog, "https://api.example.test/billing", "https://api.example.test/orders", 1),
		strings.Replace(catalog, "https://api.example.test/billing", "https://api.example.test/bad url", 1),
		strings.Replace(catalog, "billing.read", "billing read", 1),
		strings.Replace(catalog, "scopes: [\"billing.read\"]", "scopes: []", 1),
		catalog + "  unknown: true\n",
	} {
		if _, err := loadText(t, validYAML+body); err == nil {
			t.Fatal("invalid resource catalog accepted")
		}
	}
}
