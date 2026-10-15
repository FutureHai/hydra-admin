package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestRecursiveRedactionAndQuery(t *testing.T) {
	type nested struct {
		PasswordHash string `json:"password_hash"`
		Link         string `json:"link"`
	}
	secret := "TEST_ONLY_SECRET"
	value := map[string]any{
		"client-secret": secret,
		"nested":        []any{map[string]any{"Access_Token": secret}, nested{PasswordHash: secret, Link: "https://example.test/path?code=" + secret + "&safe=ok"}},
	}
	clean := RedactAny(value)
	encoded, err := json.Marshal(clean)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatal("recursive secret leaked")
	}
	if got := SanitizeString("/callback?csrf=" + secret + "&page_token=" + secret + "&safe=ok"); strings.Contains(got, secret) || !strings.Contains(got, "safe=ok") {
		t.Fatalf("query sanitization: %q", got)
	}
	for _, key := range []string{"password", "password_hash", "client-secret", "Authorization", "set-cookie", "access_token", "refresh-token", "id_token", "token", "page_token", "csrf", "code", "assertion"} {
		if !IsSensitiveKey(key) {
			t.Fatalf("unrecognized secret key %s", key)
		}
	}
}

func TestSlogHandlerRedactsAttrsGroupsAndWithAttrs(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(NewRedactingHandler(slog.NewJSONHandler(&out, nil)))
	secret := "TEST_ONLY_SECRET"
	logger.With("authorization", secret).Info("request", "payload", map[string]any{"refresh_token": secret, "url": "/cb?code=" + secret})
	logger.WithGroup("password").Info("group", "anything", secret)
	logger.LogAttrs(context.Background(), slog.LevelInfo, "group record", slog.Group("nested", slog.String("set_cookie", secret)))
	if strings.Contains(out.String(), secret) {
		t.Fatalf("slog leaked secret: %s", out.String())
	}
	if !strings.Contains(out.String(), Redacted) {
		t.Fatal("missing redaction marker")
	}
}
