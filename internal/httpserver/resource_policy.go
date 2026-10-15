package httpserver

import (
	"encoding/json"
	"errors"
	"strings"

	"hydra-admin/internal/config"
)

// validateResourceAssignment keeps new Client values inside the configured
// catalog while allowing legacy values to survive unrelated edits. Hydra still
// stores scope and audience independently; resource servers must authorize tokens.
func validateResourceAssignment(catalog config.ResourceCatalog, changes, previous map[string]json.RawMessage) error {
	if !catalog.Enabled() {
		return nil
	}
	scopeRaw, audienceRaw := changes["scope"], changes["audience"]
	if scopeRaw == nil {
		scopeRaw = previous["scope"]
	}
	if audienceRaw == nil {
		audienceRaw = previous["audience"]
	}
	scopes, err := decodeScopeSet(scopeRaw)
	if err != nil {
		return err
	}
	audiences, err := decodeAudienceSet(audienceRaw)
	if err != nil {
		return err
	}
	oldScopes, _ := decodeScopeSet(previous["scope"])
	oldAudiences, _ := decodeAudienceSet(previous["audience"])
	knownAudiences := make(map[string]bool, len(catalog.Audiences))
	owner := make(map[string]string)
	for _, global := range catalog.GlobalScopes {
		owner[global] = ""
	}
	for _, resource := range catalog.Audiences {
		knownAudiences[resource.Audience] = true
		for _, scope := range resource.Scopes {
			owner[scope] = resource.Audience
		}
	}
	for audience := range audiences {
		if !knownAudiences[audience] && !oldAudiences[audience] {
			return errors.New("audience is not in resource catalog")
		}
	}
	for scope := range scopes {
		resource, known := owner[scope]
		if !known {
			if !oldScopes[scope] {
				return errors.New("scope is not in resource catalog")
			}
			continue
		}
		if resource != "" && !audiences[resource] && (!oldScopes[scope] || oldAudiences[resource]) {
			return errors.New("scope requires its resource audience")
		}
	}
	return nil
}

func decodeScopeSet(raw json.RawMessage) (map[string]bool, error) {
	values := map[string]bool{}
	if len(raw) == 0 || string(raw) == "null" {
		return values, nil
	}
	var scope string
	if err := json.Unmarshal(raw, &scope); err != nil {
		return nil, err
	}
	for _, value := range strings.Fields(scope) {
		values[value] = true
	}
	return values, nil
}

func decodeAudienceSet(raw json.RawMessage) (map[string]bool, error) {
	values := map[string]bool{}
	if len(raw) == 0 || string(raw) == "null" {
		return values, nil
	}
	var audience []string
	if err := json.Unmarshal(raw, &audience); err != nil {
		return nil, err
	}
	for _, value := range audience {
		values[value] = true
	}
	return values, nil
}
