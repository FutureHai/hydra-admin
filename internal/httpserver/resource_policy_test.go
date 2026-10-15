package httpserver

import (
	"encoding/json"
	"testing"

	"hydra-admin/internal/config"
)

func TestResourceAssignmentPreservesOldUnmatchedValues(t *testing.T) {
	catalog := config.ResourceCatalog{
		GlobalScopes: []string{"openid"},
		Audiences: []config.ResourceDefinition{
			{Audience: "https://api.example.test/orders", Scopes: []string{"orders.read", "orders.write"}},
			{Audience: "https://api.example.test/billing", Scopes: []string{"billing.read"}},
		},
	}
	previous := map[string]json.RawMessage{
		"scope":    json.RawMessage(`"orders.read old.permission"`),
		"audience": json.RawMessage(`["https://api.example.test/orders","https://old.example.test"]`),
	}
	valid := []map[string]json.RawMessage{
		{"scope": json.RawMessage(`"orders.read orders.write old.permission"`)},
		{"audience": json.RawMessage(`["https://api.example.test/orders","https://api.example.test/billing","https://old.example.test"]`), "scope": json.RawMessage(`"orders.read billing.read old.permission"`)},
		{"scope": json.RawMessage(`"orders.read"`), "audience": json.RawMessage(`["https://api.example.test/orders"]`)},
	}
	for _, fields := range valid {
		if err := validateResourceAssignment(catalog, fields, previous); err != nil {
			t.Fatalf("valid assignment rejected: %v", err)
		}
	}
	invalid := []map[string]json.RawMessage{
		{"scope": json.RawMessage(`"orders.read billing.read"`)},
		{"scope": json.RawMessage(`"orders.read new.permission"`)},
		{"audience": json.RawMessage(`["https://api.example.test/orders","https://new.example.test"]`)},
		{"audience": json.RawMessage(`["https://api.example.test/billing"]`)},
	}
	for _, fields := range invalid {
		if err := validateResourceAssignment(catalog, fields, previous); err == nil {
			t.Fatal("invalid assignment accepted")
		}
	}
}
