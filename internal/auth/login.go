package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"sync"
	"time"
)

const InvalidCredentialsMessage = "用户名或密码错误"

type LoginInput struct {
	Username, Password, SourceIP string
	PreAuthCookie, PreAuthNonce  string
	OldSessionID                 string
	Origin, Referer              string
}

type LoginOutcome struct {
	Status     int
	Message    string
	RetryAfter time.Duration
	Session    Session
}

type LoginService struct {
	Username, PasswordHash, PublicBaseURL string
	PasswordFile                          string
	Sessions                              *SessionStore
	PreAuth                               *PreAuthStore
	Limiter                               *LoginLimiter
	mu                                    sync.Mutex
}

var ErrCurrentPasswordInvalid = errors.New("current password is invalid")
var ErrPasswordUnchanged = errors.New("new password must differ from current password")

func (s *LoginService) Attempt(input LoginInput, now time.Time) LoginOutcome {
	if !ValidateOrigin(input.Origin, input.Referer, s.PublicBaseURL) {
		return LoginOutcome{Status: http.StatusForbidden, Message: "请求来源校验失败"}
	}
	if !s.PreAuth.Consume(input.PreAuthCookie, input.PreAuthNonce, now) {
		return LoginOutcome{Status: http.StatusForbidden, Message: "CSRF 校验失败"}
	}
	decision := s.Limiter.Begin(input.SourceIP, input.Username, now)
	if !decision.Allowed {
		if decision.Status == http.StatusTooManyRequests {
			return LoginOutcome{Status: decision.Status, Message: "请求过于频繁", RetryAfter: decision.RetryAfter}
		}
		return LoginOutcome{Status: http.StatusUnauthorized, Message: InvalidCredentialsMessage}
	}
	usernameOK := subtle.ConstantTimeCompare([]byte(input.Username), []byte(s.Username)) == 1
	s.mu.Lock()
	defer s.mu.Unlock()
	passwordOK := VerifyPassword(s.PasswordHash, []byte(input.Password))
	if !usernameOK || !passwordOK {
		return LoginOutcome{Status: http.StatusUnauthorized, Message: InvalidCredentialsMessage}
	}
	session, err := s.Sessions.Rotate(input.OldSessionID, s.Username, now)
	if err != nil {
		return LoginOutcome{Status: http.StatusInternalServerError, Message: "登录服务暂不可用"}
	}
	s.Limiter.RecordSuccess(input.SourceIP, input.Username)
	return LoginOutcome{Status: http.StatusOK, Session: session}
}

// ChangePassword serializes credential changes with login session creation.
func (s *LoginService) ChangePassword(current, next string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !VerifyPassword(s.PasswordHash, []byte(current)) {
		return ErrCurrentPasswordInvalid
	}
	if subtle.ConstantTimeCompare([]byte(current), []byte(next)) == 1 {
		return ErrPasswordUnchanged
	}
	hash, err := GeneratePasswordHash([]byte(next), "argon2id")
	if err != nil {
		return err
	}
	if err := SavePasswordHash(s.PasswordFile, hash); err != nil {
		return err
	}
	s.PasswordHash = hash
	s.Sessions.DeleteAll()
	return nil
}
