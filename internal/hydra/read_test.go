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

func TestReadClientsKeepsUnknownFieldsAndRedactsSecrets(t *testing.T) {
	list, err := os.ReadFile("testdata/client_list.json")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := os.ReadFile("testdata/client_detail.json")
	if err != nil {
		t.Fatal(err)
	}
	version := Version26
	listCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.URL.Path == "/openapi.json" {
			t.Errorf("non-whitelisted or credentialed request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			json.NewEncoder(w).Encode(map[string]string{"version": version})
		case "/admin/clients":
			listCalls++
			if r.URL.RawQuery != "page_size=100" {
				t.Errorf("unexpected list query: %s", r.URL.RawQuery)
			}
			w.Write(list)
		case "/admin/clients/fixture-web":
			w.Write(detail)
		case "/admin/clients/fixture-sparse":
			w.Write([]byte(`{"client_id":"fixture-sparse"}`))
		default:
			t.Errorf("unlisted path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	adapter, err := NewHTTPAdapter(server.URL, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	clients, readErr := adapter.ListClients(context.Background())
	if readErr != nil || len(clients) != 2 || clients[1].ClientID != "fixture-sparse" {
		t.Fatalf("list=%+v error=%v", clients, readErr)
	}
	if _, ok := clients[1].Fields["client_name"]; ok {
		t.Fatal("missing field was fabricated")
	}
	client, readErr := adapter.GetClient(context.Background(), "fixture-web")
	if readErr != nil || client.ClientID != "fixture-web" {
		t.Fatalf("detail=%+v error=%v", client, readErr)
	}
	if string(client.Fields["future_flag"]) == "" || !strings.Contains(string(client.Fields["future_flag"]), `"enabled":true`) {
		t.Fatal("unknown field not preserved")
	}
	serialized, _ := json.Marshal(client)
	for _, secret := range []string{"fixture-secret-never-print", "nested-sensitive", "registration-sensitive", "private-material"} {
		if strings.Contains(string(serialized), secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	if !strings.Contains(string(serialized), "public-material") {
		t.Fatal("public JWK field was lost")
	}
	if _, readErr := adapter.GetClient(context.Background(), "../escape"); readErr == nil || readErr.Status != 400 {
		t.Fatal("unsafe id accepted")
	}
	version = "v26.2.1"
	if _, readErr := adapter.ListClients(context.Background()); readErr == nil || readErr.Code != "unsupported_version" || listCalls != 1 {
		t.Fatalf("runtime version change not gated: error=%v listCalls=%d", readErr, listCalls)
	}
}

func TestReadErrorMappingDoesNotLeakUpstreamBody(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		status                  int
		contentType, body, code string
		wantStatus              int
	}{
		{"conflict", 409, "text/html", "<html>secret conflict</html>", "hydra_conflict", 409},
		{"denied", 403, "text/html", "<html>secret gateway</html>", "hydra_upstream_denied", 502},
		{"html", 200, "text/html", "<html>secret html</html>", "hydra_bad_response", 502},
		{"server_error", 500, "text/html", "<html>secret failure</html>", "hydra_bad_gateway", 502},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/version" {
					w.Header().Set("Content-Type", "application/json")
					w.Write([]byte(`{"version":"v26.2.0"}`))
					return
				}
				w.Header().Set("Content-Type", tt.contentType)
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()
			adapter, _ := NewHTTPAdapter(server.URL, time.Second)
			_, readErr := adapter.ListClients(context.Background())
			if readErr == nil || readErr.Status != tt.wantStatus || readErr.Code != tt.code || strings.Contains(readErr.Error(), "secret") {
				t.Fatalf("error=%v", readErr)
			}
		})
	}
	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()
		adapter, _ := NewHTTPAdapter(server.URL, 30*time.Millisecond)
		_, readErr := adapter.ListClients(context.Background())
		if readErr == nil || readErr.Status != 504 || readErr.Code != "hydra_timeout" {
			t.Fatalf("timeout=%v", readErr)
		}
	})
}

func TestClientListRejectsUnverifiedPaginationShape(t *testing.T) {
	for _, body := range []string{
		`{"items":[{"client_id":"one"}],"next_page_token":"unverified"}`,
		`[` + strings.Repeat(`{"client_id":"one"},`, 100) + `{"client_id":"last"}]`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/version" {
				w.Write([]byte(`{"version":"v26.2.0"}`))
				return
			}
			w.Write([]byte(body))
		}))
		adapter, _ := NewHTTPAdapter(server.URL, time.Second)
		_, readErr := adapter.ListClients(context.Background())
		server.Close()
		if readErr == nil || readErr.Code != "hydra_bad_response" {
			t.Fatalf("unverified pagination accepted: %v", readErr)
		}
	}
}

func TestClientCursorPaginationUsesOnlyAllowlistedRequests(t *testing.T) {
	queries := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("unsafe request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case "/admin/clients":
			queries = append(queries, r.URL.RawQuery)
			switch r.URL.Query().Get("page_token") {
			case "":
				w.Header().Set("Link", `</admin/clients?page_size=1&page_token=first>; rel="next"`)
				w.Write([]byte(`[{"client_id":"one","client_secret":"hidden"}]`))
			case "first":
				w.Header().Set("Link", `</admin/clients?page_size=1&page_token=first>; rel="first", </admin/clients?page_size=1&page_token=last>; rel="next"`)
				w.Write([]byte(`[{"client_id":"two"}]`))
			case "last":
				w.Header().Set("Link", `</admin/clients?page_size=1&page_token=first>; rel="first"`)
				w.Write([]byte(`[{"client_id":"three"}]`))
			default:
				t.Errorf("unexpected cursor")
			}
		default:
			t.Errorf("unlisted path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	adapter, _ := NewHTTPAdapter(server.URL, time.Second)
	var token string
	for index, wantID := range []string{"one", "two", "three"} {
		page, err := adapter.ListClientPage(context.Background(), 1, token)
		if err != nil || len(page.Items) != 1 || page.Items[0].ClientID != wantID {
			t.Fatalf("page %d: %#v, %v", index, page, err)
		}
		if _, ok := page.Items[0].Fields["client_secret"]; !ok && index == 0 {
			t.Fatal("expected redacted secret marker")
		}
		token = page.NextPageToken
	}
	if token != "" || strings.Join(queries, "|") != "page_size=1|page_size=1&page_token=first|page_size=1&page_token=last" {
		t.Fatalf("cursor or allowlist mismatch: %q %#v", token, queries)
	}
	if _, err := adapter.ListClientPage(context.Background(), 1, "bad\nvalue"); err == nil || err.Code != "invalid_page_token" {
		t.Fatal("invalid input cursor accepted")
	}
}

func TestClientCursorRejectsUntrustedNextLinks(t *testing.T) {
	for _, link := range []string{
		`broken-link`,
		`<https://elsewhere.invalid/admin/clients?page_size=1&page_token=x>; rel="next"`,
		`</admin/other?page_size=1&page_token=x>; rel="next"`,
		`</admin/clients?page_size=2&page_token=x>; rel="next"`,
		`</admin/clients?page_size=1&page_token=x&page_token=y>; rel="next"`,
		`</admin/clients?page_size=1&page_token=x>; rel="next"; rel="first"`,
		`</admin/clients?page_size=1&page_token=x>; rel="next", </admin/clients?page_size=1&page_token=y>; rel="next"`,
		`</admin/clients?page_size=1&page_token=x>; rel="next"`,
		"",
	} {
		t.Run(link, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/version" {
					w.Write([]byte(`{"version":"v26.2.0"}`))
					return
				}
				if link != "" {
					w.Header().Set("Link", link)
				}
				w.Write([]byte(`[{"client_id":"one"}]`))
			}))
			defer server.Close()
			adapter, _ := NewHTTPAdapter(server.URL, time.Second)
			cursor := ""
			if strings.Contains(link, `page_token=x>; rel="next"`) && strings.Count(link, `rel="next"`) == 1 && strings.HasPrefix(link, "</admin/clients?page_size=1") {
				cursor = "x"
			}
			_, err := adapter.ListClientPage(context.Background(), 1, cursor)
			if err == nil || err.Code != "hydra_bad_response" {
				t.Fatalf("unsafe Link accepted: %v", err)
			}
		})
	}
}
