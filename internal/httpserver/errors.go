package httpserver

import (
	"errors"
	"net/http"
	"time"
)

type ErrorKind string

const (
	BadRequest      ErrorKind = "bad_request"
	Unauthenticated ErrorKind = "unauthenticated"
	Forbidden       ErrorKind = "forbidden"
	RateLimited     ErrorKind = "rate_limited"
	Unavailable     ErrorKind = "unavailable"
	AuditFailure    ErrorKind = "audit_write_failed"
)

type AppError struct {
	Kind       ErrorKind
	Cause      error
	RetryAfter time.Duration
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Kind)
}
func (e *AppError) Unwrap() error { return e.Cause }

type PublicError struct {
	Status            int    `json:"-"`
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"-"`
}

// MapError never includes upstream or internal error text in a public response.
func MapError(err error) PublicError {
	var app *AppError
	if !errors.As(err, &app) {
		return PublicError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "服务暂不可用"}
	}
	switch app.Kind {
	case BadRequest:
		return PublicError{Status: http.StatusBadRequest, Code: "bad_request", Message: "请求参数无效"}
	case Unauthenticated:
		return PublicError{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: "用户名或密码错误"}
	case Forbidden:
		return PublicError{Status: http.StatusForbidden, Code: "forbidden", Message: "请求未获授权"}
	case RateLimited:
		seconds := int((app.RetryAfter + time.Second - 1) / time.Second)
		if seconds < 1 {
			seconds = 1
		}
		return PublicError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "请求过于频繁", RetryAfterSeconds: seconds}
	case Unavailable:
		return PublicError{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "所需能力不可用"}
	case AuditFailure:
		return PublicError{Status: http.StatusInternalServerError, Code: "audit_write_failed", Message: "审计写入失败"}
	default:
		return PublicError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "服务暂不可用"}
	}
}
