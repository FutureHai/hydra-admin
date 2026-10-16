package hydra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPinnedSpecAndVersionFixtures(t *testing.T) {
	registry := NewRegistry()
	if !registry.SpecValid() || !registry.Match(Version26) || registry.Match("v26.2.1") || registry.Match("26.2.0") {
		t.Fatal("registry match or pinned digest failed")
	}
	for _, tt := range []struct {
		name, fixture, wantStatus string
		wantList                  bool
	}{
		{"match", "version_match.json", "supported", true},
		{"mismatch", "version_mismatch.json", "unsupported_version", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			version, err := os.ReadFile("testdata/" + tt.fixture)
			if err != nil {
				t.Fatal(err)
			}
			listCalls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path == "/openapi.json" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("unsafe outbound request: %s %s", r.Method, r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/version":
					w.Write(version)
				case "/health/ready":
					w.Write([]byte(`{"status":"ok"}`))
				case "/admin/clients":
					listCalls++
					if r.URL.RawQuery != "page_size=1" {
						t.Errorf("query: %s", r.URL.RawQuery)
					}
					w.Write([]byte(`[{"client_id":"fixture-client","client_secret":"never-print"}]`))
				default:
					t.Errorf("unlisted path: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			adapter, err := NewHTTPAdapter(server.URL, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			report := Probe(context.Background(), adapter, registry)
			if report.Status != tt.wantStatus || (listCalls == 1) != tt.wantList {
				t.Fatalf("report=%+v listCalls=%d", report, listCalls)
			}
			for _, name := range businessNames {
				want := tt.wantList && (name == "client_list" || name == "client_detail" || name == "client_create" || name == "client_update" || name == "client_delete" || name == "token_introspect" || name == "pagination" || name == "scopes" || name == "audiences")
				if report.BusinessAllowed(name) != want {
					t.Fatalf("capability %s: got %v want %v", name, report.BusinessAllowed(name), want)
				}
			}
			if !report.Capabilities["audit_read"].Enabled || report.WriteAcceptance != "not_verified_in_this_deployment" {
				t.Fatal("local audit or write acceptance metadata missing")
			}
			if tt.wantList && (report.Capabilities["secret_rotate"].Reason != "rotation_operation_absent" || report.Capabilities["token_revoke"].Reason != "public_revoke_not_configured") {
				t.Fatal("disabled capability lacks a precise reason")
			}
			data, _ := json.Marshal(report)
			if strings.Contains(string(data), "never-print") || strings.Contains(string(data), server.URL) {
				t.Fatal("capability report leaked upstream data")
			}
		})
	}
}

func TestDeniedAndTransportRestrictions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			http.Error(w, "secret upstream body", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	adapter, err := NewHTTPAdapter(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	report := Probe(context.Background(), adapter, NewRegistry())
	data, _ := json.Marshal(report)
	if report.Status != string(ProbeDenied) || !strings.Contains(report.Diagnostic, "额外认证") || strings.Contains(string(data), "secret upstream body") {
		t.Fatalf("denied report: %s", data)
	}
	for _, address := range []string{
		"http://10.0.0.12:4445",
		"http://203.0.113.12:4445",
		"http://hydra.internal:4445",
		"https://hydra.example.test:4445",
	} {
		if _, err := NewHTTPAdapter(address, time.Second); err != nil {
			t.Fatalf("Admin URL %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{"http://10.0.0.12:4444", "http://hydra.internal:4444"} {
		if err := adapter.ConfigurePublicRevoke(address); err == nil {
			t.Fatalf("plaintext Public URL %q accepted", address)
		}
	}
}

func TestHTTPAdminHostnameIsReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":"v26.2.0"}`))
	}))
	defer server.Close()
	adapter, err := NewHTTPAdapter(strings.Replace(server.URL, "127.0.0.1", "localhost", 1), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	version, result := adapter.Version(context.Background())
	if result != ProbeOK || version != "v26.2.0" {
		t.Fatalf("hostname Admin probe failed: %s %s", result, version)
	}
}

func TestRedirectAndInvalidPayloadFailClosed(t *testing.T) {
	var targetCalls int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { targetCalls++ }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			http.Redirect(w, r, target.URL, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()
	adapter, _ := NewHTTPAdapter(server.URL, time.Second)
	report := Probe(context.Background(), adapter, NewRegistry())
	if targetCalls != 0 || report.VersionProbe != ProbeUnexpected || report.BusinessAllowed("client_list") {
		t.Fatal("redirect or invalid version enabled capability")
	}
}

func TestClientListRequiresArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case "/health/ready":
			w.Write([]byte(`{"status":"ok"}`))
		case "/admin/clients":
			w.Write([]byte(`null`))
		}
	}))
	defer server.Close()
	adapter, _ := NewHTTPAdapter(server.URL, time.Second)
	report := Probe(context.Background(), adapter, NewRegistry())
	if report.ClientListProbe != ProbeUnexpected || report.BusinessAllowed("client_list") {
		t.Fatalf("invalid list accepted: %+v", report)
	}
}
