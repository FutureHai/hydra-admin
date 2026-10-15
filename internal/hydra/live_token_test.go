package hydra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// Opt-in live integration uses one random, disposable Client and its own token.
// No token or Secret is printed, persisted, or passed as a command argument.
func TestLiveHydraTokenIntrospectAndRevoke(t *testing.T) {
	adminURL, publicURL := os.Getenv("HYDRA_ADMIN_TEST_URL"), os.Getenv("HYDRA_PUBLIC_TEST_URL")
	if adminURL == "" || publicURL == "" {
		t.Skip("set both Hydra test URLs")
	}
	a, err := NewHTTPAdapter(adminURL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ConfigurePublicRevoke(publicURL); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if version, result := a.Version(ctx); result != ProbeOK || version != Version26 {
		t.Fatal("test Hydra version mismatch")
	}
	if a.Ready(ctx) != ProbeOK || a.ClientListProbe(ctx) != ProbeOK {
		t.Fatal("test Admin API unavailable")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := "codex-token-test-" + hex.EncodeToString(random[:])
	if _, readErr := a.GetClient(ctx, id); readErr == nil || readErr.Status != 404 {
		t.Fatal("random test ID is not free")
	}
	created, createErr := a.CreateClient(ctx, map[string]json.RawMessage{
		"client_id": mustJSON(id), "client_name": mustJSON("Codex isolated token test"),
		"grant_types": mustJSON([]string{"client_credentials"}), "token_endpoint_auth_method": mustJSON("client_secret_basic"),
	})
	if createErr != nil {
		t.Fatalf("test Client create failed: %s", createErr.Code)
	}
	defer func() {
		if err := a.DeleteClient(context.Background(), id); err != nil {
			t.Errorf("test Client cleanup failed: %s", err.Code)
		}
	}()
	if created.Secret == "" {
		t.Fatal("test Client has no one-time secret")
	}
	secret := created.Secret
	created.Secret = ""
	public, err := NewHTTPAdapter(publicURL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	u := *public.base
	u.Path = strings.TrimRight(public.base.Path, "/") + "/oauth2/token"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(url.Values{"grant_type": {"client_credentials"}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.SetBasicAuth(id, secret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := public.client.Do(request)
	if err != nil {
		t.Fatal("test token request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("test token endpoint status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		t.Fatal("test token response invalid")
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(data, &tokenResponse) != nil || !validToken(tokenResponse.AccessToken) {
		t.Fatal("test token missing")
	}
	info, introErr := a.IntrospectToken(ctx, tokenResponse.AccessToken)
	if introErr != nil || !info.Active || info.ClientID != id {
		t.Fatal("live introspection did not confirm token owner")
	}
	if revokeErr := a.RevokeToken(ctx, tokenResponse.AccessToken, id, secret); revokeErr != nil {
		t.Fatalf("live revoke failed: %s", revokeErr.Code)
	}
	secret = ""
	info, introErr = a.IntrospectToken(ctx, tokenResponse.AccessToken)
	tokenResponse.AccessToken = ""
	if introErr != nil || info.Active {
		t.Fatal("token remained active after revoke")
	}
}
