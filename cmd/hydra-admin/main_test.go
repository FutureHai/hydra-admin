package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionAndHashPasswordArguments(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"version"}, &out, &errOut); code != 0 {
		t.Fatalf("version exit %d: %s", code, errOut.String())
	}
	for _, part := range []string{"hydra-admin", "commit:", "built:", "go:"} {
		if !strings.Contains(out.String(), part) {
			t.Fatalf("version missing %s", part)
		}
	}
	out.Reset()
	errOut.Reset()
	if code := run([]string{"hash-password", "--password", "plaintext"}, &out, &errOut); code != 2 || out.Len() != 0 {
		t.Fatal("password CLI flag accepted")
	}
}

func TestCheckReadOnlyCapabilityReport(t *testing.T) {
	upstreamVersion := "v26.2.0"
	listCalls := 0
	hydra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("check sent %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			fmt.Fprintf(w, `{"version":%q}`, upstreamVersion)
		case "/health/ready":
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/admin/clients":
			listCalls++
			fmt.Fprint(w, `[{"client_id":"test","client_secret":"do-not-leak"}]`)
		default:
			t.Errorf("unexpected probe path: %s", r.URL.Path)
		}
	}))
	defer hydra.Close()
	example, err := os.ReadFile("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(example), "https://<ADMIN_CONSOLE_FQDN>", "http://127.0.0.1:8085", 1)
	text = strings.Replace(text, "cookie_secure: true", "cookie_secure: false", 1)
	text = strings.Replace(text, "http://127.0.0.1:4445", hydra.URL, 1)
	text = strings.Replace(text, "$argon2id$v=19$m=65536,t=3,p=2$REPLACE_WITH_BASE64_SALT$REPLACE_WITH_BASE64_HASH", "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", 1)
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"check", "--config", path}, &out, &errOut); code != 0 {
		t.Fatalf("check exit %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `"status": "supported"`) || !strings.Contains(out.String(), `"client_list_probe": "ok"`) || strings.Contains(out.String(), "do-not-leak") {
		t.Fatalf("unexpected report: %s", out.String())
	}
	if listCalls != 1 {
		t.Fatalf("expected one read-only list probe, got %d", listCalls)
	}
	upstreamVersion = "v26.2.1"
	out.Reset()
	errOut.Reset()
	if code := run([]string{"check", "--config", path}, &out, &errOut); code != 1 || !strings.Contains(out.String(), `"status": "unsupported_version"`) || listCalls != 1 {
		t.Fatalf("mismatched check: exit=%d output=%s listCalls=%d", code, out.String(), listCalls)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "logs", "audit.jsonl")); !os.IsNotExist(err) {
		t.Fatal("check should not create log file")
	}
}
