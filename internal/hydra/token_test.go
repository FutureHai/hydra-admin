package hydra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTokenWhitelistOwnershipAndPublicGate(t *testing.T) {
	const token = "fixture-sensitive-token"
	version := Version26
	authMethod := "client_secret_basic"
	introCalls, revokeCalls := 0, 0
	admin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/version":
			json.NewEncoder(w).Encode(map[string]string{"version": version})
		case r.Method == "GET" && r.URL.Path == "/admin/clients/owner":
			json.NewEncoder(w).Encode(map[string]string{"client_id": "owner", "token_endpoint_auth_method": authMethod})
		case r.Method == "POST" && r.URL.Path == "/admin/oauth2/introspect":
			introCalls++
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.ParseForm() != nil || r.PostForm.Get("token") != token {
				t.Error("unsafe introspection request")
			}
			w.Write([]byte(`{"active":true,"client_id":"owner","scope":"read","ext":{"access_token":"fixture-sensitive-token"},"unknown":"fixture-sensitive-token"}`))
		default:
			t.Errorf("unexpected Admin request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer admin.Close()
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		revokeCalls++
		id, secret, ok := r.BasicAuth()
		if !ok || id != "owner" || secret != "fixture-secret" || r.Method != "POST" || r.URL.Path != "/oauth2/revoke" || r.ParseForm() != nil || r.PostForm.Get("token") != token || r.PostForm.Get("client_secret") != "" {
			t.Error("unsafe Public revoke request")
		}
		w.WriteHeader(200)
	}))
	defer public.Close()
	a, _ := NewHTTPAdapter(admin.URL, time.Second)
	ctx := context.Background()
	info, err := a.IntrospectToken(ctx, token)
	if err != nil || !info.Active || info.ClientID != "owner" || info.Scope != "read" {
		t.Fatal("introspection fixture failed")
	}
	encoded, _ := json.Marshal(info)
	if strings.Contains(string(encoded), token) || strings.Contains(string(encoded), "ext") || strings.Contains(string(encoded), "unknown") {
		t.Fatal("whitelist leaked upstream field")
	}
	if err := a.RevokeToken(ctx, token, "owner", "fixture-secret"); err == nil || err.Code != "revoke_not_configured" || revokeCalls != 0 {
		t.Fatal("unconfigured Public URL used")
	}
	if err := a.ConfigurePublicRevoke(public.URL); err != nil {
		t.Fatal(err)
	}
	if err := a.RevokeToken(ctx, token, "other", "fixture-secret"); err == nil || err.Code != "token_owner_unconfirmed" || revokeCalls != 0 {
		t.Fatal("wrong Client revoked token")
	}
	if err := a.RevokeToken(ctx, token, "owner", "fixture-secret"); err != nil || revokeCalls != 1 {
		t.Fatal("revoke failed or retried")
	}
	authMethod = "none"
	if err := a.RevokeToken(ctx, token, "owner", "fixture-secret"); err == nil || err.Code != "client_auth_method_unsupported" || revokeCalls != 1 {
		t.Fatal("unsupported Client auth method reached Public API")
	}
	version = "v26.2.1"
	if _, err := a.IntrospectToken(ctx, token); err == nil || err.Code != "unsupported_version" || introCalls != 4 {
		t.Fatal("mismatched version reached introspection")
	}
}

func TestInactiveIntrospectionDropsAdditionalFields(t *testing.T) {
	info, err := decodeTokenInfo([]byte(`{"active":false,"client_id":"should-not-return","ext":{"token":"hidden"}}`))
	if err != nil || info.Active || info.ClientID != "" {
		t.Fatal("inactive response leaked additional fields")
	}
}
