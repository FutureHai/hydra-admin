package httpserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loginForPasswordTest(t *testing.T, s *Server, password string) *http.Cookie {
	t.Helper()
	nonce, cookies := csrf(t, s, nil)
	response := request(t, s, http.MethodPost, "/api/v1/auth/login", `{"username":"admin","password":"`+password+`"}`, cookies,
		map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": nonce})
	if response.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == s.Config.Auth.SessionCookieName {
			return cookie
		}
	}
	t.Fatal("session cookie missing")
	return nil
}

func TestChangePasswordRequiresCurrentPasswordAndExpiresAllSessions(t *testing.T) {
	s, auditPath := testServer(t)
	if got := request(t, s, "POST", "/api/v1/auth/password", `{}`, nil, nil); got.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous password change accepted: %d", got.Code)
	}
	first := loginForPasswordTest(t, s, "test-only-password")
	second := loginForPasswordTest(t, s, "test-only-password")
	csrfToken, _ := csrf(t, s, []*http.Cookie{first})
	headers := map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": csrfToken}
	valid := `{"current_password":"test-only-password","new_password":"new-admin-password-2026","confirm_password":"new-admin-password-2026"}`
	if got := request(t, s, "POST", "/api/v1/auth/password", valid, []*http.Cookie{first}, nil); got.Code != http.StatusForbidden {
		t.Fatalf("password change without CSRF accepted: %d", got.Code)
	}
	if got := request(t, s, "POST", "/api/v1/auth/password", strings.Replace(valid, "test-only-password", "incorrect-password", 1), []*http.Cookie{first}, headers); got.Code != http.StatusForbidden || !strings.Contains(got.Body.String(), "current_password_incorrect") {
		t.Fatalf("wrong current password: %d %s", got.Code, got.Body.String())
	}
	if got := request(t, s, "POST", "/api/v1/auth/password", strings.Replace(valid, `"confirm_password":"new-admin-password-2026"`, `"confirm_password":"different"`, 1), []*http.Cookie{first}, headers); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("password mismatch accepted: %d", got.Code)
	}
	if got := request(t, s, "POST", "/api/v1/auth/password", strings.Replace(valid, "new-admin-password-2026", "short", 2), []*http.Cookie{first}, headers); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("short password accepted: %d", got.Code)
	}
	for _, cookie := range []*http.Cookie{first, second} {
		if got := request(t, s, "GET", "/api/v1/auth/session", "", []*http.Cookie{cookie}, nil); got.Code != http.StatusOK {
			t.Fatalf("failed change expired a session: %d", got.Code)
		}
	}
	changed := request(t, s, "POST", "/api/v1/auth/password", valid, []*http.Cookie{first}, headers)
	if changed.Code != http.StatusNoContent {
		t.Fatalf("password change failed: %d %s", changed.Code, changed.Body.String())
	}
	if s.Sessions.Len() != 0 {
		t.Fatal("password change retained old sessions")
	}
	for _, cookie := range []*http.Cookie{first, second} {
		if got := request(t, s, "GET", "/api/v1/auth/session", "", []*http.Cookie{cookie}, nil); got.Code != http.StatusUnauthorized {
			t.Fatalf("old session remained valid: %d", got.Code)
		}
	}
	nonce, cookies := csrf(t, s, nil)
	oldLogin := request(t, s, "POST", "/api/v1/auth/login", `{"username":"admin","password":"test-only-password"}`, cookies,
		map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": nonce})
	if oldLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old password still worked: %d", oldLogin.Code)
	}
	loginForPasswordTest(t, s, "new-admin-password-2026")
	restarted, err := New(s.Config, s.web, s.auditReader, s.Logger)
	if err != nil {
		t.Fatalf("restart failed to load changed password: %v", err)
	}
	loginForPasswordTest(t, restarted, "new-admin-password-2026")
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"action":"password.change"`) < 3 || strings.Contains(string(data), "new-admin-password-2026") || strings.Contains(string(data), "test-only-password") {
		t.Fatal("password audit missing or leaked plaintext")
	}
}

func TestPasswordPersistenceFailureKeepsOldCredentialAndSession(t *testing.T) {
	s, _ := testServer(t)
	cookie := loginForPasswordTest(t, s, "test-only-password")
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s.Login.PasswordFile = filepath.Join(parent, "password.hash")
	csrfToken, _ := csrf(t, s, []*http.Cookie{cookie})
	response := request(t, s, "POST", "/api/v1/auth/password", `{"current_password":"test-only-password","new_password":"new-admin-password-2026","confirm_password":"new-admin-password-2026"}`,
		[]*http.Cookie{cookie}, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": csrfToken})
	if response.Code != http.StatusInternalServerError || s.Sessions.Len() != 1 {
		t.Fatalf("failed persistence changed login state: %d", response.Code)
	}
	if got := request(t, s, "GET", "/api/v1/auth/session", "", []*http.Cookie{cookie}, nil); got.Code != http.StatusOK {
		t.Fatal("failed persistence invalidated session")
	}
}

func TestPasswordChangeAuditFailureStillEndsSession(t *testing.T) {
	s, _ := testServer(t)
	cookie := loginForPasswordTest(t, s, "test-only-password")
	csrfToken, _ := csrf(t, s, []*http.Cookie{cookie})
	s.Audit = failingAudit{}
	response := request(t, s, "POST", "/api/v1/auth/password", `{"current_password":"test-only-password","new_password":"new-admin-password-2026","confirm_password":"new-admin-password-2026"}`,
		[]*http.Cookie{cookie}, map[string]string{"Origin": "http://127.0.0.1:8085", "X-CSRF-Token": csrfToken})
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "password_changed_audit_failed") {
		t.Fatalf("audit failure did not report committed change: %d %s", response.Code, response.Body.String())
	}
	if s.Sessions.Len() != 0 {
		t.Fatal("audit failure left a session active after password change")
	}
	if _, err := os.Stat(s.Config.Auth.PasswordHashFile); err != nil {
		t.Fatalf("changed password was not persisted: %v", err)
	}
}
