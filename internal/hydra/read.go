package hydra

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const firstPageSize = 100

// ClientRead retains every non-sensitive upstream field, including fields
// unknown to the pinned schema. It is ephemeral and never persisted locally.
type ClientRead struct {
	ClientID string                     `json:"client_id"`
	Fields   map[string]json.RawMessage `json:"fields"`
}

type ClientPage struct {
	Items         []ClientRead `json:"items"`
	NextPageToken string       `json:"next_page_token,omitempty"`
}

// ReadError contains only stable local codes, never an upstream body or URL.
type ReadError struct {
	Status int
	Code   string
}

func (e *ReadError) Error() string { return e.Code }

func readFailure(status int, code string) *ReadError { return &ReadError{Status: status, Code: code} }

func (a *HTTPAdapter) ListClients(ctx context.Context) ([]ClientRead, *ReadError) {
	page, err := a.ListClientPage(ctx, firstPageSize, "")
	return page.Items, err
}

func (a *HTTPAdapter) ListClientPage(ctx context.Context, pageSize int, pageToken string) (ClientPage, *ReadError) {
	if pageSize < 1 || pageSize > 1000 || !validPageToken(pageToken) {
		return ClientPage{}, readFailure(400, "invalid_page_token")
	}
	if err := a.ensureCompatible(ctx); err != nil {
		return ClientPage{}, err
	}
	result, err := a.readJSON(ctx, readClientList, pageToken, pageSize)
	if err != nil {
		return ClientPage{}, err
	}
	if trimmed := bytes.TrimSpace(result.data); len(trimmed) == 0 || trimmed[0] != '[' {
		return ClientPage{}, readFailure(502, "hydra_bad_response")
	}
	var records []json.RawMessage
	if json.Unmarshal(result.data, &records) != nil || len(records) > pageSize {
		return ClientPage{}, readFailure(502, "hydra_bad_response")
	}
	clients := make([]ClientRead, 0, len(records))
	for _, record := range records {
		client, err := decodeClient(record)
		if err != nil {
			return ClientPage{}, err
		}
		clients = append(clients, client)
	}
	next, err := nextClientPageToken(result.links, pageSize)
	if err != nil || (next == "" && len(result.links) == 0 && len(clients) == pageSize) || (next != "" && (len(clients) == 0 || next == pageToken)) {
		return ClientPage{}, readFailure(502, "hydra_bad_response")
	}
	return ClientPage{Items: clients, NextPageToken: next}, nil
}

func validPageToken(token string) bool {
	if len(token) > 2048 || !utf8.ValidString(token) {
		return false
	}
	for _, r := range token {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// The upstream Link is parsed as data. Only its opaque cursor can cross the
// adapter boundary; the next request still uses the fixed, allowlisted path.
func nextClientPageToken(links []string, pageSize int) (string, *ReadError) {
	var next string
	if len(links) > 4 {
		return "", readFailure(502, "hydra_bad_response")
	}
	for _, header := range links {
		if header == "" || len(header) > 8192 {
			return "", readFailure(502, "hydra_bad_response")
		}
		for _, item := range strings.Split(header, ",") {
			parts := strings.Split(strings.TrimSpace(item), ";")
			if len(parts) < 2 {
				return "", readFailure(502, "hydra_bad_response")
			}
			relation := ""
			for _, param := range parts[1:] {
				param = strings.TrimSpace(param)
				if strings.HasPrefix(param, "rel=") {
					if relation != "" {
						return "", readFailure(502, "hydra_bad_response")
					}
					relation = strings.Trim(strings.TrimPrefix(param, "rel="), `"`)
				}
			}
			if relation == "" {
				return "", readFailure(502, "hydra_bad_response")
			}
			if relation != "next" {
				continue
			}
			target := strings.TrimSpace(parts[0])
			if next != "" || len(target) < 3 || target[0] != '<' || target[len(target)-1] != '>' {
				return "", readFailure(502, "hydra_bad_response")
			}
			parsed, err := url.ParseRequestURI(target[1 : len(target)-1])
			if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Fragment != "" || parsed.Path != "/admin/clients" {
				return "", readFailure(502, "hydra_bad_response")
			}
			query, err := url.ParseQuery(parsed.RawQuery)
			if err != nil || len(query) != 2 || len(query["page_size"]) != 1 || query.Get("page_size") != strconv.Itoa(pageSize) || len(query["page_token"]) != 1 || query.Get("page_token") == "" || !validPageToken(query.Get("page_token")) {
				return "", readFailure(502, "hydra_bad_response")
			}
			next = query.Get("page_token")
		}
	}
	return next, nil
}

func (a *HTTPAdapter) GetClient(ctx context.Context, id string) (ClientRead, *ReadError) {
	if !validClientID(id) {
		return ClientRead{}, readFailure(400, "invalid_client_id")
	}
	if err := a.ensureCompatible(ctx); err != nil {
		return ClientRead{}, err
	}
	result, err := a.readJSON(ctx, readClientDetail, id, 0)
	if err != nil {
		return ClientRead{}, err
	}
	client, err := decodeClient(result.data)
	if err != nil {
		return ClientRead{}, err
	}
	if client.ClientID != id {
		return ClientRead{}, readFailure(502, "hydra_bad_response")
	}
	return client, nil
}

func (a *HTTPAdapter) ensureCompatible(ctx context.Context) *ReadError {
	result, err := a.readJSON(ctx, readVersion, "", 0)
	if err != nil {
		return err
	}
	var value struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(result.data, &value) != nil || !safeVersion(value.Version) {
		return readFailure(502, "hydra_bad_response")
	}
	if !NewRegistry().Match(value.Version) || a.Tag() != value.Version || a.SpecSHA256() != SpecSHA26 {
		return readFailure(409, "unsupported_version")
	}
	return nil
}

func validClientID(id string) bool {
	if id == "" || len(id) > 512 || id == "." || id == ".." || strings.ContainsAny(id, "/\\") {
		return false
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func decodeClient(data []byte) (ClientRead, *ReadError) {
	var original map[string]json.RawMessage
	if json.Unmarshal(data, &original) != nil || original == nil {
		return ClientRead{}, readFailure(502, "hydra_bad_response")
	}
	var id string
	if json.Unmarshal(original["client_id"], &id) != nil || !validClientID(id) {
		return ClientRead{}, readFailure(502, "hydra_bad_response")
	}
	for _, name := range []string{"client_name", "scope", "owner", "token_endpoint_auth_method", "jwks_uri", "client_uri"} {
		if raw, ok := original[name]; ok && !bytes.Equal(raw, []byte("null")) {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return ClientRead{}, readFailure(502, "hydra_bad_response")
			}
		}
	}
	for _, name := range []string{"grant_types", "response_types", "redirect_uris", "allowed_cors_origins", "audience", "post_logout_redirect_uris"} {
		if raw, ok := original[name]; ok && !bytes.Equal(raw, []byte("null")) {
			var value []string
			if json.Unmarshal(raw, &value) != nil {
				return ClientRead{}, readFailure(502, "hydra_bad_response")
			}
		}
	}
	fields := make(map[string]json.RawMessage, len(original))
	for name, raw := range original {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return ClientRead{}, readFailure(502, "hydra_bad_response")
		}
		cleaned := sanitizeClientValue(name, value, false)
		encoded, err := json.Marshal(cleaned)
		if err != nil {
			return ClientRead{}, readFailure(502, "hydra_bad_response")
		}
		fields[name] = encoded
	}
	return ClientRead{ClientID: id, Fields: fields}, nil
}

func sanitizeClientValue(name string, value any, inJWK bool) any {
	lower := strings.ToLower(name)
	if sensitiveClientKey(lower) || (inJWK && privateJWKKey(lower)) {
		return "[redacted]"
	}
	if lower == "jwks" || lower == "keys" {
		inJWK = true
	}
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = sanitizeClientValue(key, child, inJWK)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = sanitizeClientValue("", child, inJWK)
		}
		return out
	default:
		return value
	}
}

func sensitiveClientKey(key string) bool {
	if key == "client_secret" || key == "registration_access_token" || key == "authorization" || key == "cookie" || strings.Contains(key, "private_key") {
		return true
	}
	for _, suffix := range []string{"secret", "password", "token", "credential", "api_key", "apikey"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func privateJWKKey(key string) bool {
	switch key {
	case "d", "p", "q", "dp", "dq", "qi", "k":
		return true
	}
	return false
}
