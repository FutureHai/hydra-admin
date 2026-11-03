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

// Opt in explicitly against a disposable v26.2.0 Hydra Admin API. The test
// owns exactly one random ID and never modifies or deletes an existing Client.
func TestLiveHydraClientCRUD(t *testing.T) {
	base := os.Getenv("HYDRA_ADMIN_TEST_URL")
	if base == "" {
		t.Skip("set HYDRA_ADMIN_TEST_URL for isolated Hydra write test")
	}
	adapter, err := NewHTTPAdapter(base, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if version, result := adapter.Version(ctx); result != ProbeOK || version != Version26 {
		t.Fatal("test Hydra version mismatch")
	}
	if adapter.Ready(ctx) != ProbeOK || adapter.ClientListProbe(ctx) != ProbeOK {
		t.Fatal("test Hydra Admin API unavailable")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := "codex-write-test-" + hex.EncodeToString(random[:])
	if _, err := adapter.GetClient(ctx, id); err == nil || err.Status != http.StatusNotFound {
		t.Fatal("generated ID already exists or lookup failed")
	}
	fields := map[string]json.RawMessage{
		"client_id": mustJSON(id), "client_name": mustJSON("Codex isolated CRUD test"),
		"grant_types": mustJSON([]string{"client_credentials"}), "scope": mustJSON("read:test"),
		"audience":                   mustJSON([]string{"https://example.invalid/test"}),
		"token_endpoint_auth_method": mustJSON("client_secret_basic"),
	}
	created, writeErr := adapter.CreateClient(ctx, fields)
	if writeErr != nil {
		t.Fatalf("create failed: %s", writeErr.Code)
	}
	deleted := false
	defer func() {
		if !deleted {
			if err := adapter.DeleteClient(context.Background(), id); err != nil {
				t.Errorf("cleanup failed for isolated test client %s: %s", id, err.Code)
			}
		}
	}()
	if created.Client.ClientID != id || created.Secret == "" {
		t.Fatal("create response lacked ID or one-time secret")
	}
	created.Secret = ""
	before, readErr := adapter.GetClient(ctx, id)
	if readErr != nil {
		t.Fatalf("read after create: %s", readErr.Code)
	}
	// Configure a field excluded from the writable UI schema on this test ID.
	setup := []byte(`[{"op":"add","path":"/metadata","value":{"future_field":"keep-me"}}]`)
	if _, err := adapter.writeJSON(ctx, http.MethodPatch, id, setup, http.StatusOK); err != nil {
		t.Fatalf("metadata setup: %s", err.Code)
	}
	before, readErr = adapter.GetClient(ctx, id)
	if readErr != nil {
		t.Fatalf("read metadata: %s", readErr.Code)
	}
	changed, patchErr := adapter.PatchClient(ctx, id, before.Revision(), map[string]json.RawMessage{"client_name": mustJSON("Codex patched")})
	if patchErr != nil {
		t.Fatalf("patch failed: %s", patchErr.Code)
	}
	if string(changed.Fields["metadata"]) != string(before.Fields["metadata"]) || string(changed.Fields["scope"]) != string(before.Fields["scope"]) || string(changed.Fields["audience"]) != string(before.Fields["audience"]) {
		t.Fatal("PATCH changed an unselected field")
	}
	if string(changed.Fields["client_name"]) != `"Codex patched"` {
		t.Fatal("PATCH did not update name")
	}
	// A second edit starts from a fresh detail read, as the UI does after a
	// successful PATCH. Hydra can normalize fields in the PATCH response.
	beforeMetadataEdit, readErr := adapter.GetClient(ctx, id)
	if readErr != nil {
		t.Fatalf("read before metadata edit: %s", readErr.Code)
	}
	metadataChanged, patchErr := adapter.PatchClient(ctx, id, beforeMetadataEdit.Revision(), map[string]json.RawMessage{
		"metadata": mustJSON(map[string]any{"future_field": "keep-me", "department": "billing"}),
	})
	if patchErr != nil || string(metadataChanged.Fields["metadata"]) != `{"department":"billing","future_field":"keep-me"}` || string(metadataChanged.Fields["client_name"]) != `"Codex patched"` {
		t.Fatalf("metadata JSON Patch failed or changed another field: %v", patchErr)
	}
	if err := adapter.DeleteClient(ctx, id); err != nil {
		t.Fatalf("delete failed: %s", err.Code)
	}
	deleted = true
	if _, err := adapter.GetClient(ctx, id); err == nil || err.Status != 404 {
		t.Fatal("delete did not remove isolated test client")
	}
}

func mustJSON(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

// Opt in against a disposable v26.2.0 instance to confirm that the tagged
// Client schema accepts the advanced creation overrides and returns them.
func TestLiveHydraAdvancedClientCreate(t *testing.T) {
	base := os.Getenv("HYDRA_ADMIN_TEST_URL")
	if base == "" {
		t.Skip("set HYDRA_ADMIN_TEST_URL for isolated Hydra write test")
	}
	adapter, err := NewHTTPAdapter(base, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if version, result := adapter.Version(ctx); result != ProbeOK || version != Version26 {
		t.Fatal("test Hydra version mismatch")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := "codex-advanced-test-" + hex.EncodeToString(random[:])
	if _, err := adapter.GetClient(ctx, id); err == nil || err.Status != http.StatusNotFound {
		t.Fatal("generated ID already exists or lookup failed")
	}
	fields := map[string]json.RawMessage{
		"client_id": mustJSON(id), "grant_types": mustJSON([]string{
			"authorization_code", "implicit", "client_credentials", "refresh_token", "urn:ietf:params:oauth:grant-type:device_code",
		}),
		"response_types":                                 mustJSON([]string{"code", "token", "id_token"}),
		"redirect_uris":                                  mustJSON([]string{"https://example.invalid/callback"}),
		"token_endpoint_auth_method":                     mustJSON("client_secret_basic"),
		"client_credentials_grant_access_token_lifespan": mustJSON("2h30m"),
		"skip_consent":                                   mustJSON(true), "skip_logout_consent": mustJSON(false),
		"frontchannel_logout_uri":              mustJSON("https://example.invalid/front-logout"),
		"frontchannel_logout_session_required": mustJSON(true),
		"backchannel_logout_uri":               mustJSON("https://example.invalid/logout"),
		"backchannel_logout_session_required":  mustJSON(true),
	}
	for _, name := range clientLifespanFields {
		if name != "client_credentials_grant_access_token_lifespan" {
			fields[name] = mustJSON("1h30m")
		}
	}
	created, writeErr := adapter.CreateClient(ctx, fields)
	if writeErr != nil {
		t.Fatalf("advanced create failed: %s", writeErr.Code)
	}
	if created.Client.ClientID != id {
		t.Fatal("advanced create returned a different Client ID")
	}
	defer func() {
		if err := adapter.DeleteClient(context.Background(), id); err != nil {
			t.Errorf("advanced test client cleanup failed: %s", err.Code)
		}
	}()
	secret := created.Secret
	created.Secret = ""
	actual, readErr := adapter.GetClient(ctx, id)
	if readErr != nil {
		t.Fatalf("advanced Client read failed: %s", readErr.Code)
	}
	for _, name := range append(append([]string{}, clientLifespanFields...), "skip_consent", "skip_logout_consent", "frontchannel_logout_uri", "frontchannel_logout_session_required", "backchannel_logout_uri", "backchannel_logout_session_required") {
		if contains(clientLifespanFields, name) {
			var lifespan string
			if json.Unmarshal(actual.Fields[name], &lifespan) != nil {
				t.Error("Hydra returned an invalid Client lifespan")
				continue
			}
			var expected string
			_ = json.Unmarshal(fields[name], &expected)
			actualDuration, actualErr := time.ParseDuration(lifespan)
			expectedDuration, expectedErr := time.ParseDuration(expected)
			if actualErr != nil || expectedErr != nil || actualDuration != expectedDuration {
				t.Errorf("Client lifespan %s did not round-trip semantically", name)
			}
			continue
		}
		if string(actual.Fields[name]) != string(fields[name]) {
			t.Errorf("advanced field %s did not round-trip: got %s", name, actual.Fields[name])
		}
	}
	if publicURL := os.Getenv("HYDRA_PUBLIC_TEST_URL"); publicURL != "" {
		if secret == "" {
			t.Fatal("advanced test Client has no one-time secret")
		}
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
		secret = ""
		if err != nil {
			t.Fatal("advanced Client token request failed")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("advanced Client token endpoint returned %d", response.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
		if err != nil || len(data) > 65536 {
			t.Fatal("advanced Client token response invalid")
		}
		var tokenResponse struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int64  `json:"expires_in"`
		}
		if json.Unmarshal(data, &tokenResponse) != nil || tokenResponse.AccessToken == "" || tokenResponse.ExpiresIn < 8990 || tokenResponse.ExpiresIn > 9001 {
			t.Fatal("Client-specific lifespan was not applied to the issued token")
		}
		tokenResponse.AccessToken = ""
	}
	updated, updateErr := adapter.SetClientLifespans(ctx, id, actual.Revision(), map[string]json.RawMessage{
		"client_credentials_grant_access_token_lifespan": mustJSON("3h"),
	})
	if updateErr != nil {
		t.Fatalf("live lifespan update failed: %s", updateErr.Code)
	}
	for _, name := range clientLifespanFields {
		var value string
		if json.Unmarshal(updated.Fields[name], &value) != nil {
			t.Errorf("lifespan %s disappeared after updating another field", name)
			continue
		}
		expected := 90 * time.Minute
		if name == "client_credentials_grant_access_token_lifespan" {
			expected = 3 * time.Hour
		}
		if duration, err := time.ParseDuration(value); err != nil || duration != expected {
			t.Errorf("lifespan %s changed unexpectedly", name)
		}
	}
	latest, readErr := adapter.GetClient(ctx, id)
	if readErr != nil {
		t.Fatalf("read before lifespan reset: %s", readErr.Code)
	}
	reset, resetErr := adapter.SetClientLifespans(ctx, id, latest.Revision(), map[string]json.RawMessage{
		"client_credentials_grant_access_token_lifespan": []byte("null"),
	})
	if resetErr != nil || string(reset.Fields["client_credentials_grant_access_token_lifespan"]) != "null" {
		t.Fatalf("live lifespan reset failed: %v", resetErr)
	}
	latest, readErr = adapter.GetClient(ctx, id)
	if readErr != nil {
		t.Fatalf("read before advanced PATCH: %s", readErr.Code)
	}
	patched, patchErr := adapter.PatchClient(ctx, id, latest.Revision(), map[string]json.RawMessage{
		"skip_consent": mustJSON(false), "frontchannel_logout_uri": mustJSON("https://example.invalid/updated-front-logout"),
	})
	if patchErr != nil || string(patched.Fields["skip_consent"]) != "false" || string(patched.Fields["frontchannel_logout_uri"]) != `"https://example.invalid/updated-front-logout"` {
		t.Fatalf("live advanced PATCH failed: %v", patchErr)
	}
}

// Opt-in coverage for the creation fields omitted from the first UI schema.
func TestLiveHydraImplicitClientCreate(t *testing.T) {
	base := os.Getenv("HYDRA_ADMIN_TEST_URL")
	if base == "" {
		t.Skip("set HYDRA_ADMIN_TEST_URL for isolated Hydra write test")
	}
	adapter, err := NewHTTPAdapter(base, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if version, result := adapter.Version(ctx); result != ProbeOK || version != Version26 {
		t.Fatal("test Hydra version mismatch")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := "codex-implicit-test-" + hex.EncodeToString(random[:])
	if _, err := adapter.GetClient(ctx, id); err == nil || err.Status != http.StatusNotFound {
		t.Fatal("generated ID already exists or lookup failed")
	}
	fields := map[string]json.RawMessage{
		"client_id": mustJSON(id), "client_name": mustJSON("Codex isolated implicit test"),
		"grant_types":                mustJSON([]string{"implicit", "authorization_code"}),
		"response_types":             mustJSON([]string{"token", "code"}),
		"redirect_uris":              mustJSON([]string{"https://example.invalid/callback"}),
		"post_logout_redirect_uris":  mustJSON([]string{"https://example.invalid/logout"}),
		"token_endpoint_auth_method": mustJSON("none"),
		"owner":                      mustJSON("codex-test"),
		"contacts":                   mustJSON([]string{"admin@example.invalid"}),
		"client_uri":                 mustJSON("https://example.invalid/app"),
		"logo_uri":                   mustJSON("https://example.invalid/logo.png"),
		"policy_uri":                 mustJSON("https://example.invalid/privacy"),
		"tos_uri":                    mustJSON("https://example.invalid/terms"),
	}
	created, writeErr := adapter.CreateClient(ctx, fields)
	if writeErr != nil {
		t.Fatalf("implicit client create failed: %s", writeErr.Code)
	}
	defer func() {
		if err := adapter.DeleteClient(context.Background(), id); err != nil {
			t.Errorf("cleanup failed for isolated test client %s: %s", id, err.Code)
		}
	}()
	if created.Client.ClientID != id || created.Secret != "" {
		t.Fatal("public implicit client returned unexpected ID or secret")
	}
	client, readErr := adapter.GetClient(ctx, id)
	if readErr != nil {
		t.Fatalf("read after create: %s", readErr.Code)
	}
	for name, want := range fields {
		if name == "client_id" {
			continue
		}
		if string(client.Fields[name]) != string(want) {
			t.Errorf("field %s did not round-trip", name)
		}
	}
	updated, patchErr := adapter.PatchClient(ctx, id, client.Revision(), map[string]json.RawMessage{
		"owner":                     mustJSON("codex-test-updated"),
		"post_logout_redirect_uris": mustJSON([]string{"https://example.invalid/logout-updated"}),
	})
	if patchErr != nil {
		t.Fatalf("implicit client PATCH failed: %s", patchErr.Code)
	}
	if string(updated.Fields["owner"]) != `"codex-test-updated"` || string(updated.Fields["post_logout_redirect_uris"]) != `["https://example.invalid/logout-updated"]` {
		t.Fatal("new writable fields did not patch")
	}
	for _, name := range []string{"grant_types", "response_types", "redirect_uris", "client_uri", "contacts"} {
		if string(updated.Fields[name]) != string(client.Fields[name]) {
			t.Errorf("PATCH changed unselected field %s", name)
		}
	}
}

// Opt-in v26.2.0 proof: a per-Client JWT override round-trips and mints a JWT.
// The test owns one random Client and never prints its Secret or Token.
func TestLiveHydraClientJWTStrategy(t *testing.T) {
	adminURL, publicURL := os.Getenv("HYDRA_ADMIN_TEST_URL"), os.Getenv("HYDRA_PUBLIC_TEST_URL")
	if adminURL == "" || publicURL == "" {
		t.Skip("set both Hydra test URLs for isolated JWT strategy test")
	}
	adapter, err := NewHTTPAdapter(adminURL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if version, result := adapter.Version(ctx); result != ProbeOK || version != Version26 || adapter.Ready(ctx) != ProbeOK {
		t.Fatal("test Hydra version mismatch or Admin API unavailable")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := "codex-jwt-strategy-test-" + hex.EncodeToString(random[:])
	if _, readErr := adapter.GetClient(ctx, id); readErr == nil || readErr.Status != http.StatusNotFound {
		t.Fatal("random test ID is not free")
	}
	created, writeErr := adapter.CreateClient(ctx, map[string]json.RawMessage{
		"client_id": mustJSON(id), "grant_types": mustJSON([]string{"client_credentials"}),
		"token_endpoint_auth_method": mustJSON("client_secret_basic"),
		"access_token_strategy":      mustJSON("jwt"),
	})
	if writeErr != nil {
		t.Fatalf("JWT Client create failed: %s", writeErr.Code)
	}
	defer func() {
		if err := adapter.DeleteClient(context.Background(), id); err != nil {
			t.Errorf("isolated JWT Client cleanup failed: %s", err.Code)
		}
	}()
	if created.Secret == "" {
		t.Fatal("test Client has no one-time secret")
	}
	secret := created.Secret
	created.Secret = ""
	client, readErr := adapter.GetClient(ctx, id)
	if readErr != nil || string(client.Fields["access_token_strategy"]) != `"jwt"` {
		t.Fatal("JWT strategy did not round-trip")
	}
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
	if response.StatusCode != http.StatusOK {
		t.Fatalf("test token endpoint status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		t.Fatal("test token response invalid")
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(data, &tokenResponse) != nil || strings.Count(tokenResponse.AccessToken, ".") != 2 {
		t.Fatal("per-Client JWT strategy did not mint a compact JWT")
	}
	tokenResponse.AccessToken = ""
}

// Opt-in proof of the real Link cursor. Owns and removes one isolated Client.
func TestLiveHydraClientCursorPagination(t *testing.T) {
	base := os.Getenv("HYDRA_ADMIN_TEST_URL")
	if base == "" {
		t.Skip("set HYDRA_ADMIN_TEST_URL for isolated Hydra pagination test")
	}
	adapter, err := NewHTTPAdapter(base, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if version, result := adapter.Version(ctx); result != ProbeOK || version != Version26 || adapter.Ready(ctx) != ProbeOK {
		t.Fatal("test Hydra version mismatch or Admin API unavailable")
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	id := "codex-pagination-test-" + hex.EncodeToString(random[:])
	if _, readErr := adapter.GetClient(ctx, id); readErr == nil || readErr.Status != http.StatusNotFound {
		t.Fatal("random test ID is not free")
	}
	created, writeErr := adapter.CreateClient(ctx, map[string]json.RawMessage{
		"client_id": mustJSON(id), "grant_types": mustJSON([]string{"client_credentials"}),
		"token_endpoint_auth_method": mustJSON("client_secret_basic"),
	})
	if writeErr != nil {
		t.Fatalf("isolated pagination Client create failed: %s", writeErr.Code)
	}
	created.Secret = ""
	defer func() {
		if err := adapter.DeleteClient(context.Background(), id); err != nil {
			t.Errorf("isolated pagination Client cleanup failed: %s", err.Code)
		}
	}()
	seenTest, sawNext, sawEnd := false, false, false
	token := ""
	seenTokens := map[string]bool{}
	for range 20 {
		page, readErr := adapter.ListClientPage(ctx, 1, token)
		if readErr != nil || len(page.Items) != 1 {
			t.Fatalf("live cursor page failed: %v", readErr)
		}
		if page.Items[0].ClientID == id {
			seenTest = true
		}
		if page.NextPageToken == "" {
			sawEnd = true
			break
		}
		if seenTokens[page.NextPageToken] {
			t.Fatal("Hydra repeated a cursor")
		}
		seenTokens[page.NextPageToken] = true
		sawNext = true
		token = page.NextPageToken
	}
	if !seenTest || !sawNext || !sawEnd {
		t.Fatal("live Client pagination did not traverse first, next and last pages")
	}
}
