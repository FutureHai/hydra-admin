package auth

import (
	"crypto/subtle"
	"net/http"
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
	Sessions                              *SessionStore
	PreAuth                               *PreAuthStore
	Limiter                               *LoginLimiter
}

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
	passwordOK := VerifyPassword(s.PasswordHash, []byte(input.Password))
	if !usernameOK || !passwordOK {
		return LoginOutcome{Status: http.StatusUnauthorized, Message: InvalidCredentialsMessage}
	}
	session, err := s.Sessions.Rotate(input.OldSessionID, s.Username, now)
	if err != nil {
		return LoginOutcome{Status: http.StatusInternalServerError, Message: "登录服务暂不可用"}
	}
	s.Limiter.RecordSuccess(input.Username)
	return LoginOutcome{Status: http.StatusOK, Session: session}
}
