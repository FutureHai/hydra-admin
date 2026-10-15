package hydra

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// ClientWriteAdapter is intentionally separate from read probes: check never
// performs a mutation and a read-only fixture cannot accidentally gain writes.
type ClientWriteAdapter interface {
	CreateClient(context.Context, map[string]json.RawMessage) (CreateResult, *ReadError)
	PatchClient(context.Context, string, string, map[string]json.RawMessage) (ClientRead, *ReadError)
	DeleteClient(context.Context, string) *ReadError
}

type CreateResult struct {
	Client ClientRead `json:"client"`
	Secret string     `json:"client_secret,omitempty"`
}

type WritableField struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
	Help     string   `json:"help,omitempty"`
}

// This is the reviewed, deliberately narrow subset of oAuth2Client in the
// pinned v26.2.0 schema. No arbitrary JSON, secret, JWK, or ID update path.
var WritableSchema = []WritableField{
	{Name: "client_id", Kind: "text", Required: true},
	{Name: "client_name", Kind: "text"},
	{Name: "grant_types", Kind: "array", Required: true, Options: []string{"authorization_code", "implicit", "client_credentials", "refresh_token", "urn:ietf:params:oauth:grant-type:device_code"}, Help: "token / id_token 响应需要 implicit；包含 code 的响应需要 authorization_code。"},
	{Name: "response_types", Kind: "array", Options: []string{"code", "code id_token", "id_token", "token id_token", "token", "token id_token code"}, Help: "每个选项是一个完整的 response_type；组合类型中的空格属于该选项。"},
	{Name: "scope", Kind: "text", Help: "可输入多个 Scope，按 Enter 添加；提交时用空格分隔。"},
	{Name: "audience", Kind: "array", Help: "可输入多个目标资源；Hydra 将 Audience 与 Scope 分别保存。"},
	{Name: "redirect_uris", Kind: "array"},
	{Name: "post_logout_redirect_uris", Kind: "array"},
	{Name: "allowed_cors_origins", Kind: "array"},
	{Name: "token_endpoint_auth_method", Kind: "select", Options: []string{"client_secret_basic", "client_secret_post", "none"}},
	{Name: "access_token_strategy", Kind: "select", Options: []string{"opaque", "jwt"}, Help: "可选；留空沿用 Hydra 全局策略。选择 jwt 会覆盖此 Client 的 Access Token 格式，请确认下游支持。"},
	{Name: "owner", Kind: "text"},
	{Name: "contacts", Kind: "array"},
	{Name: "client_uri", Kind: "text"},
	{Name: "logo_uri", Kind: "text"},
	{Name: "policy_uri", Kind: "text"},
	{Name: "tos_uri", Kind: "text"},
}

func createOnlyField(name string) bool {
	return name == "client_id" || name == "access_token_strategy"
}

func WritableNames(create bool) []string {
	names := make([]string, 0, len(WritableSchema))
	for _, field := range WritableSchema {
		if create || !createOnlyField(field.Name) {
			names = append(names, field.Name)
		}
	}
	return names
}

func ValidateFields(fields map[string]json.RawMessage, create bool) *ReadError {
	if len(fields) == 0 || len(fields) > len(WritableSchema) {
		return readFailure(422, "validation_failed")
	}
	allowed := make(map[string]WritableField, len(WritableSchema))
	for _, f := range WritableSchema {
		if create || !createOnlyField(f.Name) {
			allowed[f.Name] = f
		}
	}
	for name, raw := range fields {
		f, ok := allowed[name]
		if !ok || len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return readFailure(422, "validation_failed")
		}
		switch f.Kind {
		case "text", "select":
			var s string
			if json.Unmarshal(raw, &s) != nil || len(s) > 2048 || strings.ContainsAny(s, "\x00\r\n") {
				return readFailure(422, "validation_failed")
			}
			if name == "client_id" && !validClientID(s) {
				return readFailure(422, "validation_failed")
			}
			if f.Kind == "select" && !contains(f.Options, s) {
				return readFailure(422, "validation_failed")
			}
		case "array":
			var v []string
			if json.Unmarshal(raw, &v) != nil || len(v) > 64 || (f.Required && len(v) == 0) {
				return readFailure(422, "validation_failed")
			}
			for _, s := range v {
				if s == "" || len(s) > 2048 || strings.ContainsAny(s, "\x00\r\n") || len(f.Options) > 0 && !contains(f.Options, s) {
					return readFailure(422, "validation_failed")
				}
			}
		default:
			return readFailure(422, "validation_failed")
		}
	}
	if create {
		for _, f := range WritableSchema {
			if f.Required {
				if _, ok := fields[f.Name]; !ok {
					return readFailure(422, "validation_failed")
				}
			}
		}
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Revision covers the full redacted read model, including unknown fields.
// It is a conflict detector, not an ETag or an atomic upstream condition.
func (c ClientRead) Revision() string {
	data, _ := json.Marshal(c.Fields)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (a *HTTPAdapter) CreateClient(ctx context.Context, fields map[string]json.RawMessage) (CreateResult, *ReadError) {
	if err := ValidateFields(fields, true); err != nil {
		return CreateResult{}, err
	}
	if err := a.ensureCompatible(ctx); err != nil {
		return CreateResult{}, err
	}
	body := make(map[string]json.RawMessage, len(fields)+1)
	for k, v := range fields {
		body[k] = v
	}
	var authMethod string
	_ = json.Unmarshal(fields["token_endpoint_auth_method"], &authMethod)
	secret := ""
	if authMethod != "none" {
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return CreateResult{}, readFailure(500, "internal_error")
		}
		secret = base64.RawURLEncoding.EncodeToString(random[:])
		body["client_secret"], _ = json.Marshal(secret)
	}
	encoded, _ := json.Marshal(body)
	data, err := a.writeJSON(ctx, http.MethodPost, "", encoded, http.StatusCreated)
	if err != nil {
		return CreateResult{}, err
	}
	client, err := decodeClient(data)
	if err != nil {
		return CreateResult{}, err
	}
	var id string
	_ = json.Unmarshal(fields["client_id"], &id)
	if client.ClientID != id {
		return CreateResult{}, readFailure(502, "hydra_bad_response")
	}
	return CreateResult{Client: client, Secret: secret}, nil
}

func (a *HTTPAdapter) PatchClient(ctx context.Context, id, baseline string, changes map[string]json.RawMessage) (ClientRead, *ReadError) {
	if !validClientID(id) || len(baseline) != 64 {
		return ClientRead{}, readFailure(400, "bad_request")
	}
	if err := ValidateFields(changes, false); err != nil {
		return ClientRead{}, err
	}
	// GetClient repeats the version gate and fetches the latest full read model.
	current, err := a.GetClient(ctx, id)
	if err != nil {
		return ClientRead{}, err
	}
	if current.Revision() != baseline {
		return ClientRead{}, readFailure(409, "client_changed")
	}
	patch := make([]map[string]any, 0, len(changes))
	for _, f := range WritableSchema {
		if createOnlyField(f.Name) {
			continue
		}
		value, ok := changes[f.Name]
		if !ok {
			continue
		}
		var decoded any
		if json.Unmarshal(value, &decoded) != nil {
			return ClientRead{}, readFailure(422, "validation_failed")
		}
		if old, exists := current.Fields[f.Name]; exists && bytes.Equal(old, value) {
			continue
		}
		op := "add"
		if _, exists := current.Fields[f.Name]; exists {
			op = "replace"
		}
		patch = append(patch, map[string]any{"op": op, "path": "/" + f.Name, "value": decoded})
	}
	if len(patch) == 0 {
		return ClientRead{}, readFailure(422, "no_changes")
	}
	encoded, _ := json.Marshal(patch)
	data, err := a.writeJSON(ctx, http.MethodPatch, id, encoded, http.StatusOK)
	if err != nil {
		return ClientRead{}, err
	}
	updated, err := decodeClient(data)
	if err != nil {
		return ClientRead{}, err
	}
	if updated.ClientID != id {
		return ClientRead{}, readFailure(502, "hydra_bad_response")
	}
	return updated, nil
}

func (a *HTTPAdapter) DeleteClient(ctx context.Context, id string) *ReadError {
	if !validClientID(id) {
		return readFailure(400, "invalid_client_id")
	}
	if err := a.ensureCompatible(ctx); err != nil {
		return err
	}
	_, err := a.writeJSON(ctx, http.MethodDelete, id, nil, http.StatusNoContent)
	return err
}
