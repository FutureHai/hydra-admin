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
	fields := map[string]json.RawMessage{"client_id": mustJSON("test-id"), "grant_types": mustJSON([]string{"client_credentials"}), "metadata": mustJSON(map[string]any{"department": "billing", "priority": 2})}
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

func TestMetadataCreateOnlyAndJSONValidation(t *testing.T) {
	var metadataField *WritableField
	for i := range WritableSchema {
		if WritableSchema[i].Name == "metadata" {
			metadataField = &WritableSchema[i]
			break
		}
	}
	if metadataField == nil || metadataField.Kind != "json" || !contains(WritableNames(true), "metadata") || contains(WritableNames(false), "metadata") {
		t.Fatal("metadata must be an explicit create-only JSON field")
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
	if err := ValidateFields(map[string]json.RawMessage{"metadata": []byte(`{"department":"other"}`)}, false); err == nil || err.Status != http.StatusUnprocessableEntity {
		t.Fatal("metadata update bypassed create-only gate")
	}
}
