package auth

import (
	"net/http"
	"testing"
	"time"
)

func TestLoginRotatesSessionAndConsumesNonce(t *testing.T) {
	now := time.Unix(1000, 0)
	hash, err := GeneratePasswordHash([]byte("example-password"), "bcrypt")
	if err != nil {
		t.Fatal(err)
	}
	sessions, _ := NewSessionStore(10, time.Hour)
	old, _ := sessions.Create("admin", now)
	pre := NewPreAuthStore()
	cookie, nonce, _ := pre.Issue(now)
	service := &LoginService{Username: "admin", PasswordHash: hash, PublicBaseURL: "https://admin.example.test", Sessions: sessions, PreAuth: pre, Limiter: NewLoginLimiter(time.Minute, 10, 5, time.Minute)}
	input := LoginInput{Username: "admin", Password: "example-password", SourceIP: "192.0.2.1", Origin: "https://admin.example.test", PreAuthCookie: cookie, PreAuthNonce: nonce, OldSessionID: old.ID}
	result := service.Attempt(input, now)
	if result.Status != http.StatusOK || result.Session.ID == old.ID {
		t.Fatalf("login failed to rotate: %+v", result)
	}
	if _, ok := sessions.Get(old.ID, now); ok {
		t.Fatal("old session still valid")
	}
	if service.Attempt(input, now).Status != http.StatusForbidden {
		t.Fatal("pre-auth nonce reused")
	}
	cookie, nonce, _ = pre.Issue(now)
	input.PreAuthCookie, input.PreAuthNonce = cookie, nonce
	input.Password = "wrong"
	if got := service.Attempt(input, now); got.Status != http.StatusUnauthorized || got.Message != InvalidCredentialsMessage {
		t.Fatalf("wrong password message: %+v", got)
	}
	cookie, nonce, _ = pre.Issue(now)
	input.PreAuthCookie, input.PreAuthNonce = cookie, nonce
	input.Username, input.Password = "missing", "example-password"
	if got := service.Attempt(input, now); got.Status != http.StatusUnauthorized || got.Message != InvalidCredentialsMessage {
		t.Fatalf("unknown user message: %+v", got)
	}
}
