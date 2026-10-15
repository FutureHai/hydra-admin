package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"hydra-admin/assets"
)

// In a fresh checkout the placeholder-only build must pass tests; after the
// web build this also exercises the real embedded service lifecycle.
func TestServeCompletesInFlightRequestOnSIGTERM(t *testing.T) {
	if os.Getenv("HYDRA_ADMIN_SERVE_HELPER") == "1" {
		os.Exit(runServe(os.Getenv("HYDRA_ADMIN_TEST_CONFIG"), os.Stderr))
	}
	if _, err := fs.Stat(assets.Dist, "dist/index.html"); err != nil {
		t.Skip("production web assets not built")
	}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	hydra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			fmt.Fprint(w, `{"version":"v26.2.0"}`)
		case "/health/ready":
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/admin/clients":
			if r.URL.Query().Get("page_size") == "10" {
				started <- struct{}{}
				<-release
			}
			fmt.Fprint(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer hydra.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	root := t.TempDir()
	template, err := os.ReadFile("../../tests/fixtures/smoke-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	config := strings.Replace(string(template), "port: 8085", fmt.Sprintf("port: %d", port), 1)
	config = strings.Replace(config, "http://127.0.0.1:8085", fmt.Sprintf("http://127.0.0.1:%d", port), 1)
	config = strings.Replace(config, "http://127.0.0.1:4445", hydra.URL, 1)
	path := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestServeCompletesInFlightRequestOnSIGTERM$")
	cmd.Env = append(os.Environ(), "HYDRA_ADMIN_SERVE_HELPER=1", "HYDRA_ADMIN_TEST_CONFIG="+path)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	ready := false
	for i := 0; i < 80; i++ {
		response, err := client.Get(base + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("service did not start: %s", output.String())
	}
	response, err := client.Get(base + "/api/v1/auth/csrf")
	if err != nil {
		t.Fatal(err)
	}
	var csrf struct {
		Token string `json:"csrf_token"`
	}
	if err := json.NewDecoder(response.Body).Decode(&csrf); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	request, _ := http.NewRequest("POST", base+"/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"test-only-password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", base)
	request.Header.Set("X-CSRF-Token", csrf.Token)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("login status %d", response.StatusCode)
	}
	type result struct {
		status int
		err    error
	}
	done := make(chan result, 1)
	go func() {
		response, err := client.Get(base + "/api/v1/clients")
		if err != nil {
			done <- result{err: err}
			return
		}
		_, _ = io.Copy(io.Discard, response.Body)
		response.Body.Close()
		done <- result{status: response.StatusCode}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request never reached Hydra")
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	select {
	case got := <-done:
		if got.err != nil || got.status != 200 {
			t.Fatalf("in-flight request lost: %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request did not complete")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("graceful exit: %v: %s", err, output.String())
	}
	for _, name := range []string{"audit.jsonl", "hydra-admin.log"} {
		info, err := os.Stat(filepath.Join(root, "logs", name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode: %v %v", name, info, err)
		}
	}
	audit, err := os.ReadFile(filepath.Join(root, "logs", "audit.jsonl"))
	if err != nil || !bytes.Contains(audit, []byte(`"action":"login"`)) || bytes.Contains(audit, []byte("test-only-password")) {
		t.Fatal("audit missing or leaked test password")
	}
}
