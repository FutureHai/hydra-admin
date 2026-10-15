package httpserver

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"hydra-admin/internal/config"
	"hydra-admin/internal/hydra"
)

func TestClientReadThroughAuthenticatedAPI(t *testing.T) {
	list, err := os.ReadFile("../hydra/testdata/client_list.json")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := os.ReadFile("../hydra/testdata/client_detail.json")
	if err != nil {
		t.Fatal(err)
	}
	upstreamVersion := hydra.Version26
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("write sent upstream: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			json.NewEncoder(w).Encode(map[string]string{"version": upstreamVersion})
		case "/health/ready":
			w.Write([]byte(`{"status":"ok"}`))
		case "/admin/clients":
			if r.URL.Query().Get("page_token") == "second" {
				if r.URL.Query().Get("page_size") != "10" {
					t.Errorf("unexpected page size: %s", r.URL.RawQuery)
				}
				w.Write([]byte(`[{"client_id":"last"}]`))
			} else {
				if r.URL.Query().Get("page_size") == "10" {
					w.Header().Set("Link", `</admin/clients?page_size=10&page_token=second>; rel="next"`)
				}
				w.Write(list)
			}
		case "/admin/clients/fixture-web":
			w.Write(detail)
		case "/admin/clients/fixture-sparse":
			w.Write([]byte(`{"client_id":"fixture-sparse"}`))
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
	s.Config.ResourceCatalog = config.ResourceCatalog{Audiences: []config.ResourceDefinition{{Audience: "https://api.example.test/orders", Scopes: []string{"orders.read", "orders.write"}}}}
	s.HydraAdapter = adapter
	s.HydraReport = hydra.Probe(context.Background(), adapter, hydra.NewRegistry())
	if !s.HydraReport.BusinessAllowed("client_list") || !s.HydraReport.BusinessAllowed("client_detail") {
		t.Fatal("read capabilities did not activate")
	}
	if got := request(t, s, "GET", "/api/v1/clients", "", nil, nil); got.Code != 401 {
		t.Fatalf("anonymous list: %d", got.Code)
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
	cookies := []*http.Cookie{session}
	listResponse := request(t, s, "GET", "/api/v1/clients", "", cookies, nil)
	if listResponse.Code != 200 || !strings.Contains(listResponse.Body.String(), `"fixture-sparse"`) || strings.Contains(listResponse.Body.String(), "fixture-secret-never-print") || strings.Contains(listResponse.Body.String(), `"total"`) || !strings.Contains(listResponse.Body.String(), `"page_size":10`) || !strings.Contains(listResponse.Body.String(), `"next_page_token":"second"`) || !strings.Contains(listResponse.Body.String(), `"pagination_supported":true`) || !strings.Contains(listResponse.Body.String(), `"orders.write"`) {
		t.Fatalf("list response: %d %s", listResponse.Code, listResponse.Body.String())
	}
	secondPage := request(t, s, "GET", "/api/v1/clients?page_token=second", "", cookies, nil)
	if secondPage.Code != 200 || !strings.Contains(secondPage.Body.String(), `"client_id":"last"`) || !strings.Contains(secondPage.Body.String(), `"next_page_token":""`) {
		t.Fatalf("next page: %d %s", secondPage.Code, secondPage.Body.String())
	}
	for _, query := range []string{"page_token=", "page_token=a&page_token=b", "page_size=100", "path=/admin/other", "page_token=%0A"} {
		if got := request(t, s, "GET", "/api/v1/clients?"+query, "", cookies, nil); got.Code != 400 {
			t.Fatalf("invalid paging query accepted: %s => %d", query, got.Code)
		}
	}
	detailResponse := request(t, s, "GET", "/api/v1/clients/fixture-web", "", cookies, nil)
	if detailResponse.Code != 200 || !strings.Contains(detailResponse.Body.String(), `"future_flag"`) || strings.Contains(detailResponse.Body.String(), "private-material") || !strings.Contains(detailResponse.Body.String(), `"client_name"`) || !strings.Contains(detailResponse.Body.String(), `"orders.write"`) {
		t.Fatalf("detail response: %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	missing := request(t, s, "GET", "/api/v1/clients/fixture-sparse", "", cookies, nil)
	if missing.Code != 200 || strings.Contains(missing.Body.String(), `"client_name":"`) {
		t.Fatalf("missing field fabricated: %d %s", missing.Code, missing.Body.String())
	}
	upstreamVersion = "v26.2.1"
	unsupported := request(t, s, "GET", "/api/v1/clients", "", cookies, nil)
	if unsupported.Code != 409 || !strings.Contains(unsupported.Body.String(), `"code":"unsupported_version"`) {
		t.Fatalf("runtime mismatch: %d %s", unsupported.Code, unsupported.Body.String())
	}
}

func TestClientReadPublicErrorsHaveStableCodes(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   string
	}{
		{502, "hydra_bad_gateway"}, {504, "hydra_timeout"}, {409, "hydra_conflict"},
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set("request_id", "fixture-request")
		(&Server{}).failHydraRead(c, &hydra.ReadError{Status: tt.status, Code: tt.code})
		if w.Code != tt.status || !strings.Contains(w.Body.String(), `"code":"`+tt.code+`"`) || strings.Contains(w.Body.String(), "<html>") {
			t.Fatalf("public error for %s: %d %s", tt.code, w.Code, w.Body.String())
		}
	}
}
