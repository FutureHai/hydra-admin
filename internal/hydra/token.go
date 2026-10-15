package hydra

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const maxTokenBytes = 16 * 1024

type TokenAdapter interface {
	IntrospectToken(context.Context, string) (TokenInfo, *ReadError)
	RevokeToken(context.Context, string, string, string) *ReadError
	PublicRevokeConfigured() bool
}

// Only these RFC 7662 fields cross the console API boundary. Hydra ext,
// username, token_use, and all unknown fields are always discarded.
type TokenInfo struct {
	Active    bool     `json:"active"`
	ClientID  string   `json:"client_id,omitempty"`
	Subject   string   `json:"sub,omitempty"`
	Scope     string   `json:"scope,omitempty"`
	Audience  []string `json:"aud,omitempty"`
	ExpiresAt *int64   `json:"exp,omitempty"`
	IssuedAt  *int64   `json:"iat,omitempty"`
	Issuer    string   `json:"iss,omitempty"`
}

func (a *HTTPAdapter) PublicRevokeConfigured() bool { return a.public != nil }

func validToken(token string) bool {
	return token != "" && len(token) <= maxTokenBytes && !strings.ContainsAny(token, "\x00\r\n")
}

func (a *HTTPAdapter) IntrospectToken(ctx context.Context, token string) (TokenInfo, *ReadError) {
	if !validToken(token) {
		return TokenInfo{}, readFailure(422, "validation_failed")
	}
	if !NewRegistry().TokenIntrospectValid() {
		return TokenInfo{}, readFailure(409, "unsupported_version")
	}
	if err := a.ensureCompatible(ctx); err != nil {
		return TokenInfo{}, err
	}
	form := url.Values{"token": {token}}
	u := *a.base
	u.Path = strings.TrimRight(a.base.Path, "/") + "/admin/oauth2/introspect"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return TokenInfo{}, readFailure(502, "hydra_bad_gateway")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return TokenInfo{}, tokenNetworkError(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return TokenInfo{}, tokenStatusError(response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return TokenInfo{}, readFailure(502, "hydra_bad_response")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxClientReadBytes+1))
	if err != nil || len(data) > maxClientReadBytes {
		return TokenInfo{}, readFailure(502, "hydra_bad_response")
	}
	return decodeTokenInfo(data)
}

func decodeTokenInfo(data []byte) (TokenInfo, *ReadError) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return TokenInfo{}, readFailure(502, "hydra_bad_response")
	}
	var info TokenInfo
	if len(raw["active"]) == 0 || json.Unmarshal(raw["active"], &info.Active) != nil {
		return TokenInfo{}, readFailure(502, "hydra_bad_response")
	}
	if !info.Active {
		return info, nil
	}
	for _, entry := range []struct {
		key    string
		target *string
	}{
		{"client_id", &info.ClientID}, {"sub", &info.Subject}, {"scope", &info.Scope}, {"iss", &info.Issuer},
	} {
		if value, ok := raw[entry.key]; ok && !bytes.Equal(value, []byte("null")) {
			if json.Unmarshal(value, entry.target) != nil || len(*entry.target) > 4096 {
				return TokenInfo{}, readFailure(502, "hydra_bad_response")
			}
		}
	}
	if value, ok := raw["aud"]; ok && !bytes.Equal(value, []byte("null")) {
		if json.Unmarshal(value, &info.Audience) != nil || len(info.Audience) > 64 {
			return TokenInfo{}, readFailure(502, "hydra_bad_response")
		}
		for _, item := range info.Audience {
			if len(item) > 4096 {
				return TokenInfo{}, readFailure(502, "hydra_bad_response")
			}
		}
	}
	for _, entry := range []struct {
		key    string
		target **int64
	}{{"exp", &info.ExpiresAt}, {"iat", &info.IssuedAt}} {
		if value, ok := raw[entry.key]; ok && !bytes.Equal(value, []byte("null")) {
			var number int64
			if json.Unmarshal(value, &number) != nil {
				return TokenInfo{}, readFailure(502, "hydra_bad_response")
			}
			*entry.target = &number
		}
	}
	return info, nil
}

func (a *HTTPAdapter) RevokeToken(ctx context.Context, token, clientID, secret string) *ReadError {
	if a.public == nil {
		return readFailure(503, "revoke_not_configured")
	}
	if !NewRegistry().TokenRevokeValid() {
		return readFailure(409, "unsupported_version")
	}
	if !validToken(token) || !validClientID(clientID) || secret == "" || len(secret) > 4096 || strings.ContainsAny(secret, "\x00\r\n") {
		return readFailure(422, "validation_failed")
	}
	// Admin introspection establishes the token-owning Client. A missing or
	// inactive token never reaches Public revoke, which could otherwise return
	// 200 even for an unknown token.
	info, err := a.IntrospectToken(ctx, token)
	if err != nil {
		return err
	}
	if !info.Active || info.ClientID == "" || info.ClientID != clientID {
		return readFailure(409, "token_owner_unconfirmed")
	}
	client, clientErr := a.GetClient(ctx, clientID)
	if clientErr != nil {
		return clientErr
	}
	var authMethod string
	if raw, exists := client.Fields["token_endpoint_auth_method"]; exists && json.Unmarshal(raw, &authMethod) != nil {
		return readFailure(502, "hydra_bad_response")
	}
	if authMethod != "" && authMethod != "client_secret_basic" {
		return readFailure(409, "client_auth_method_unsupported")
	}
	form := url.Values{"token": {token}}
	u := *a.public.base
	u.Path = strings.TrimRight(a.public.base.Path, "/") + "/oauth2/revoke"
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(form.Encode()))
	if requestErr != nil {
		return readFailure(502, "hydra_bad_gateway")
	}
	request.SetBasicAuth(clientID, secret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, doErr := a.public.client.Do(request)
	if doErr != nil {
		return tokenNetworkError(doErr)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return tokenStatusError(response.StatusCode)
	}
	data, readErr := io.ReadAll(io.LimitReader(response.Body, 65537))
	if readErr != nil || len(data) > 65536 || len(bytes.TrimSpace(data)) != 0 {
		return readFailure(502, "hydra_bad_response")
	}
	return nil
}

func tokenNetworkError(err error) *ReadError {
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
		return readFailure(504, "hydra_timeout")
	}
	return readFailure(502, "hydra_bad_gateway")
}

func tokenStatusError(status int) *ReadError {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return readFailure(422, "hydra_validation_failed")
	case http.StatusUnauthorized, http.StatusForbidden:
		return readFailure(502, "hydra_upstream_denied")
	case http.StatusConflict:
		return readFailure(409, "hydra_conflict")
	default:
		return readFailure(502, "hydra_bad_gateway")
	}
}
