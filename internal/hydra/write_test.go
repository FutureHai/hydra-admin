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

func TestWriteWhitelistPatchIsolationAndNoRetry(t *testing.T) {
	version := Version26
	postCalls, patchCalls, deleteCalls := 0, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("forwarded browser credential")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/version":
			json.NewEncoder(w).Encode(map[string]string{"version": version})
		case r.Method == "GET" && r.URL.Path == "/admin/clients/test-id":
			w.Write([]byte(`{"client_id":"test-id","client_name":"before","grant_types":["client_credentials"],"future_field":{"keep":true},"client_secret":"upstream-secret"}`))
		case r.Method == "POST" && r.URL.Path == "/admin/clients":
			postCalls++
			var body map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body["client_secret"]) == 0 {
				t.Error("create omitted generated secret")
			}
			if string(body["metadata"]) != `{"department":"billing","priority":2}` {
				t.Errorf("create metadata was not forwarded as an object: %s", body["metadata"])
			}
			if string(body["client_credentials_grant_access_token_lifespan"]) != `"2h30m"` || string(body["skip_consent"]) != `true` || string(body["backchannel_logout_uri"]) != `"https://example.invalid/logout"` || string(body["backchannel_logout_session_required"]) != `true` {
				t.Error("advanced Client create fields were not forwarded with their JSON types")
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"client_id":"test-id","client_name":"before","grant_types":["client_credentials"],"metadata":{"department":"billing","priority":2,"api_key":"redact-me"}}`))
		case r.Method == "PATCH" && r.URL.Path == "/admin/clients/test-id":
			patchCalls++
			var operations []map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&operations) != nil || len(operations) != 1 || string(operations[0]["path"]) != `"/client_name"` {
				t.Error("PATCH contained extra or wrong path")
			}
			if _, ok := operations[0]["client_secret"]; ok {
				t.Error("PATCH included secret")
			}
			w.Write([]byte(`{"client_id":"test-id","client_name":"after","grant_types":["client_credentials"],"future_field":{"keep":true}}`))
		case r.Method == "DELETE" && r.URL.Path == "/admin/clients/test-id":
			deleteCalls++
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected method/path %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, _ := NewHTTPAdapter(server.URL, time.Second)
	ctx := context.Background()
	fields := map[string]json.RawMessage{
		"client_id": mustJSON("test-id"), "grant_types": mustJSON([]string{"client_credentials"}),
		"metadata": mustJSON(map[string]any{"department": "billing", "priority": 2}),
		"client_credentials_grant_access_token_lifespan": mustJSON("2h30m"),
		"skip_consent": mustJSON(true), "backchannel_logout_uri": mustJSON("https://example.invalid/logout"),
		"backchannel_logout_session_required": mustJSON(true),
	}
	created, createErr := a.CreateClient(ctx, fields)
	if createErr != nil || postCalls != 1 {
		t.Fatal("create failed or retried")
	}
	if string(created.Client.Fields["metadata"]) != `{"api_key":"[redacted]","department":"billing","priority":2}` {
		t.Fatalf("create response did not preserve and redact metadata: %s", created.Client.Fields["metadata"])
	}
	if _, err := a.CreateClient(ctx, map[string]json.RawMessage{"client_secret": mustJSON("forbidden")}); err == nil || postCalls != 1 {
		t.Fatal("invalid create reached upstream")
	}
	before, err := a.GetClient(ctx, "test-id")
	if err != nil {
		t.Fatal(err.Code)
	}
	if _, err := a.PatchClient(ctx, "test-id", before.Revision(), map[string]json.RawMessage{"client_secret": mustJSON("forbidden")}); err == nil || patchCalls != 0 {
		t.Fatal("secret update reached upstream")
	}
	updated, err := a.PatchClient(ctx, "test-id", before.Revision(), map[string]json.RawMessage{"client_name": mustJSON("after")})
	if err != nil || patchCalls != 1 || string(updated.Fields["future_field"]) != `{"keep":true}` || strings.Contains(string(updated.Fields["client_secret"]), "upstream-secret") {
		t.Fatal("PATCH failed to preserve or redact fields")
	}
	if err := a.DeleteClient(ctx, "test-id"); err != nil || deleteCalls != 1 {
		t.Fatal("delete failed or retried")
	}
	if _, err := a.writeJSON(ctx, http.MethodPut, "test-id", []byte(`{"client_name":"replacement"}`), http.StatusOK); err == nil || err.Status != 400 || patchCalls != 1 {
		t.Fatal("replacement PUT reached Hydra")
	}
	version = "v26.2.1"
	if err := a.DeleteClient(ctx, "test-id"); err == nil || err.Code != "unsupported_version" || deleteCalls != 1 {
		t.Fatal("version mismatch allowed deletion")
	}
}

func TestMetadataPatchReplacesOnlyMetadata(t *testing.T) {
	patchCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/admin/clients/metadata-test":
			w.Write([]byte(`{"client_id":"metadata-test","metadata":{"department":"old","keep":"nested"},"future_field":{"keep":true}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/admin/clients/metadata-test":
			patchCalls++
			var operations []struct {
				Op    string          `json:"op"`
				Path  string          `json:"path"`
				Value json.RawMessage `json:"value"`
			}
			if json.NewDecoder(r.Body).Decode(&operations) != nil || len(operations) != 1 || operations[0].Op != "replace" || operations[0].Path != "/metadata" || string(operations[0].Value) != `{"department":"new","keep":"nested"}` {
				t.Errorf("unexpected metadata patch: %+v", operations)
			}
			w.Write([]byte(`{"client_id":"metadata-test","metadata":{"department":"new","keep":"nested"},"future_field":{"keep":true}}`))
		default:
			t.Errorf("unexpected method/path %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter, _ := NewHTTPAdapter(server.URL, time.Second)
	current, err := adapter.GetClient(context.Background(), "metadata-test")
	if err != nil {
		t.Fatal(err.Code)
	}
	updated, err := adapter.PatchClient(context.Background(), "metadata-test", current.Revision(), map[string]json.RawMessage{
		"metadata": []byte(`{"department":"new","keep":"nested"}`),
	})
	if err != nil || patchCalls != 1 || string(updated.Fields["future_field"]) != `{"keep":true}` {
		t.Fatalf("metadata patch failed or altered unselected fields: error=%v calls=%d", err, patchCalls)
	}
}

func TestVersion26ClientCreateSchema(t *testing.T) {
	options := make(map[string][]string)
	for _, field := range WritableSchema {
		options[field.Name] = field.Options
	}
	if !contains(options["grant_types"], "implicit") || !contains(options["grant_types"], "urn:ietf:params:oauth:grant-type:device_code") {
		t.Fatal("supported grant types missing from create schema")
	}
	for _, responseType := range []string{"code", "code id_token", "id_token", "token id_token", "token", "token id_token code"} {
		if !contains(options["response_types"], responseType) {
			t.Fatalf("supported response type %q missing from create schema", responseType)
		}
	}
	fields := map[string]json.RawMessage{
		"client_id":                  mustJSON("implicit-test"),
		"grant_types":                mustJSON([]string{"implicit"}),
		"response_types":             mustJSON([]string{"token"}),
		"redirect_uris":              mustJSON([]string{"https://example.invalid/callback"}),
		"post_logout_redirect_uris":  mustJSON([]string{"https://example.invalid/logout"}),
		"token_endpoint_auth_method": mustJSON("none"),
	}
	if err := ValidateFields(fields, true); err != nil {
		t.Fatalf("supported implicit client rejected: %s", err.Code)
	}
	fields["response_types"] = mustJSON([]string{"unsupported"})
	if err := ValidateFields(fields, true); err == nil || err.Status != http.StatusUnprocessableEntity {
		t.Fatal("unsupported response type accepted")
	}
}

func TestAccessTokenStrategyCreateOnly(t *testing.T) {
	var strategyField *WritableField
	for i := range WritableSchema {
		if WritableSchema[i].Name == "access_token_strategy" {
			strategyField = &WritableSchema[i]
			break
		}
	}
	if strategyField == nil || strategyField.Kind != "select" || len(strategyField.Options) != 2 || !contains(strategyField.Options, "opaque") || !contains(strategyField.Options, "jwt") {
		t.Fatal("access token strategy is not an explicit two-option create field")
	}
	if contains(WritableNames(false), "access_token_strategy") || !contains(WritableNames(true), "access_token_strategy") {
		t.Fatal("access token strategy leaked into editable fields or is absent from create")
	}
	fields := map[string]json.RawMessage{
		"client_id":   mustJSON("strategy-test"),
		"grant_types": mustJSON([]string{"client_credentials"}),
	}
	if err := ValidateFields(fields, true); err != nil {
		t.Fatal("omitted strategy must inherit the Hydra global setting")
	}
	for _, value := range []string{"opaque", "jwt"} {
		fields["access_token_strategy"] = mustJSON(value)
		if err := ValidateFields(fields, true); err != nil {
			t.Fatalf("supported strategy %s rejected: %s", value, err.Code)
		}
	}
	fields["access_token_strategy"] = mustJSON("unsupported")
	if err := ValidateFields(fields, true); err == nil || err.Status != http.StatusUnprocessableEntity {
		t.Fatal("unknown access token strategy accepted")
	}
	if err := ValidateFields(map[string]json.RawMessage{"access_token_strategy": mustJSON("jwt")}, false); err == nil || err.Status != http.StatusUnprocessableEntity {
		t.Fatal("strategy update bypassed create-only gate")
	}
}

func TestMetadataCreateAndUpdateJSONValidation(t *testing.T) {
	var metadataField *WritableField
	for i := range WritableSchema {
		if WritableSchema[i].Name == "metadata" {
			metadataField = &WritableSchema[i]
			break
		}
	}
	if metadataField == nil || metadataField.Kind != "json" || !contains(WritableNames(true), "metadata") || !contains(WritableNames(false), "metadata") {
		t.Fatal("metadata must be an explicit create and update JSON field")
	}
	fields := map[string]json.RawMessage{
		"client_id":   mustJSON("metadata-test"),
		"grant_types": mustJSON([]string{"client_credentials"}),
		"metadata":    []byte(`{"department":"billing","priority":2,"labels":["a","b"]}`),
	}
	for _, valid := range []string{`{"department":"billing","priority":2,"labels":["a","b"]}`, `["business","labels"]`, `"business-data"`, `42`, `false`, `null`} {
		fields["metadata"] = []byte(valid)
		if err := ValidateFields(fields, true); err != nil {
			t.Fatalf("valid metadata %s rejected: %s", valid, err.Code)
		}
	}
	for _, invalid := range []string{`{invalid}`, `[1,`} {
		fields["metadata"] = []byte(invalid)
		if err := ValidateFields(fields, true); err == nil || err.Status != http.StatusUnprocessableEntity {
			t.Fatalf("invalid metadata %s accepted", invalid)
		}
	}
	if err := ValidateFields(map[string]json.RawMessage{"metadata": []byte(`{"department":"other"}`)}, false); err != nil {
		t.Fatalf("valid metadata update rejected: %s", err.Code)
	}
	if err := ValidateFields(map[string]json.RawMessage{"metadata": []byte(`{invalid}`)}, false); err == nil || err.Status != http.StatusUnprocessableEntity {
		t.Fatal("invalid metadata update accepted")
	}
}

func TestAdvancedClientCreateValidation(t *testing.T) {
	base := map[string]json.RawMessage{"client_id": mustJSON("advanced-test"), "grant_types": mustJSON([]string{"client_credentials"})}
	for _, name := range clientLifespanFields {
		var field *WritableField
		for i := range WritableSchema {
			if WritableSchema[i].Name == name {
				field = &WritableSchema[i]
				break
			}
		}
		if field == nil || field.Kind != "duration" || !contains(WritableNames(true), name) || contains(WritableNames(false), name) {
			t.Fatalf("lifespan %s is not create-only duration", name)
		}
		fields := map[string]json.RawMessage{"client_id": base["client_id"], "grant_types": base["grant_types"], name: mustJSON("1h30m")}
		if err := ValidateFields(fields, true); err != nil {
			t.Fatalf("valid lifespan %s rejected: %s", name, err.Code)
		}
	}
	for _, bad := range []string{`""`, `"0s"`, `"30d"`, `"-1h"`, `"1.5h"`, `"999999999999999999h"`, `30`, `null`} {
		fields := map[string]json.RawMessage{"client_id": base["client_id"], "grant_types": base["grant_types"], "client_credentials_grant_access_token_lifespan": []byte(bad)}
		if err := ValidateFields(fields, true); err == nil || err.Status != http.StatusUnprocessableEntity {
			t.Fatalf("invalid lifespan %s accepted", bad)
		}
	}
	for _, bad := range []string{`"true"`, `1`, `null`} {
		fields := map[string]json.RawMessage{"client_id": base["client_id"], "grant_types": base["grant_types"], "skip_consent": []byte(bad)}
		if err := ValidateFields(fields, true); err == nil || err.Status != http.StatusUnprocessableEntity {
			t.Fatalf("invalid boolean %s accepted", bad)
		}
	}
	for _, good := range []string{`true`, `false`} {
		fields := map[string]json.RawMessage{"client_id": base["client_id"], "grant_types": base["grant_types"], "skip_consent": []byte(good)}
		if err := ValidateFields(fields, true); err != nil {
			t.Fatalf("valid boolean %s rejected: %s", good, err.Code)
		}
	}
	for _, bad := range []string{`"relative/path"`, `"file:///tmp/logout"`, `"https://user:pass@example.invalid/logout"`, `"https://example.invalid/logout#fragment"`} {
		fields := map[string]json.RawMessage{"client_id": base["client_id"], "grant_types": base["grant_types"], "backchannel_logout_uri": []byte(bad)}
		if err := ValidateFields(fields, true); err == nil || err.Status != http.StatusUnprocessableEntity {
			t.Fatalf("invalid logout URI %s accepted", bad)
		}
	}
	fields := map[string]json.RawMessage{"client_id": base["client_id"], "grant_types": base["grant_types"], "backchannel_logout_session_required": mustJSON(true)}
	if err := ValidateFields(fields, true); err == nil || err.Status != http.StatusUnprocessableEntity {
		t.Fatal("session-required accepted without logout URI")
	}
	fields["backchannel_logout_uri"] = mustJSON("https://example.invalid/logout")
	if err := ValidateFields(fields, true); err != nil {
		t.Fatalf("valid backchannel logout rejected: %s", err.Code)
	}
	if err := ValidateFields(map[string]json.RawMessage{"skip_consent": mustJSON(true)}, false); err != nil {
		t.Fatalf("consent override PATCH rejected: %s", err.Code)
	}
	if err := ValidateFields(map[string]json.RawMessage{"client_credentials_grant_access_token_lifespan": mustJSON("2h")}, false); err == nil || err.Status != http.StatusUnprocessableEntity {
		t.Fatal("lifespan bypassed the dedicated update operation")
	}
}

func TestLifespanUpdatePreservesExistingAndBlocksArbitraryPUT(t *testing.T) {
	putCalls := 0
	current := map[string]any{
		"client_id": "lifespan-test", "grant_types": []string{"client_credentials"},
		"client_credentials_grant_access_token_lifespan": "2h0m0s",
		"authorization_code_grant_access_token_lifespan": "3h0m0s",
		"jwt_bearer_grant_access_token_lifespan":         "5h0m0s",
		"future_field":                                   map[string]any{"keep": true}, "client_secret": "never-forward",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/version":
			w.Write([]byte(`{"version":"v26.2.0"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/admin/clients/lifespan-test":
			json.NewEncoder(w).Encode(current)
		case r.Method == http.MethodPut && r.URL.Path == "/admin/clients/lifespan-test/lifespans":
			putCalls++
			var body map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 3 || string(body["client_credentials_grant_access_token_lifespan"]) != `"4h"` || string(body["authorization_code_grant_access_token_lifespan"]) != `"3h0m0s"` || string(body["jwt_bearer_grant_access_token_lifespan"]) != `"5h0m0s"` {
				t.Errorf("lifespan PUT omitted an existing field or forwarded a Client field: %v", body)
			}
			current["client_credentials_grant_access_token_lifespan"] = "4h0m0s"
			json.NewEncoder(w).Encode(current)
		default:
			t.Errorf("unexpected method/path %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter, _ := NewHTTPAdapter(server.URL, time.Second)
	baseline, err := adapter.GetClient(context.Background(), "lifespan-test")
	if err != nil {
		t.Fatal(err.Code)
	}
	if err := ValidateLifespanChanges(map[string]json.RawMessage{"jwt_bearer_grant_access_token_lifespan": mustJSON("4h")}); err == nil {
		t.Fatal("non-editable JWT bearer lifespan was accepted")
	}
	if err := ValidateLifespanChanges(map[string]json.RawMessage{"client_credentials_grant_access_token_lifespan": []byte("null")}); err != nil {
		t.Fatal("null reset was rejected")
	}
	if _, err := adapter.SetClientLifespans(context.Background(), "lifespan-test", strings.Repeat("0", 64), map[string]json.RawMessage{"client_credentials_grant_access_token_lifespan": mustJSON("4h")}); err == nil || err.Code != "client_changed" || putCalls != 0 {
		t.Fatal("stale revision reached Hydra")
	}
	updated, err := adapter.SetClientLifespans(context.Background(), "lifespan-test", baseline.Revision(), map[string]json.RawMessage{"client_credentials_grant_access_token_lifespan": mustJSON("4h")})
	if err != nil || putCalls != 1 || string(updated.Fields["future_field"]) != `{"keep":true}` {
		t.Fatalf("lifespan update failed or changed an unrelated field: error=%v calls=%d", err, putCalls)
	}
	if _, err := adapter.writeJSON(context.Background(), http.MethodPut, "lifespan-test", []byte(`{}`), http.StatusOK); err == nil || err.Status != http.StatusBadRequest || putCalls != 1 {
		t.Fatal("general Client replacement PUT was allowed")
	}
}
