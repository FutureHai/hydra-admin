package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestErrorMappingDoesNotExposeCause(t *testing.T) {
	for _, tc := range []struct {
		kind   ErrorKind
		status int
	}{
		{BadRequest, http.StatusBadRequest}, {Unauthenticated, http.StatusUnauthorized}, {Forbidden, http.StatusForbidden},
		{RateLimited, http.StatusTooManyRequests}, {Unavailable, http.StatusServiceUnavailable}, {AuditFailure, http.StatusInternalServerError},
	} {
		got := MapError(&AppError{Kind: tc.kind, Cause: errors.New("TEST_ONLY_SECRET"), RetryAfter: 1500 * time.Millisecond})
		if got.Status != tc.status || strings.Contains(got.Message, "TEST_ONLY_SECRET") || got.Code == "" {
			t.Fatalf("bad mapping for %s: %+v", tc.kind, got)
		}
		if tc.kind == RateLimited && got.RetryAfterSeconds != 2 {
			t.Fatalf("Retry-After rounded incorrectly: %+v", got)
		}
	}
	got := MapError(errors.New("TEST_ONLY_SECRET"))
	if got.Status != 500 || strings.Contains(got.Message, "TEST_ONLY_SECRET") {
		t.Fatalf("unknown error leaked: %+v", got)
	}
}
