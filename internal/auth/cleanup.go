package auth

import (
	"context"
	"time"
)

// StartCleanup removes expired sessions and pre-auth entries every minute until ctx is canceled.
func StartCleanup(ctx context.Context, sessions *SessionStore, preAuth *PreAuthStore) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				sessions.Cleanup(now)
				preAuth.Cleanup(now)
			case <-ctx.Done():
				return
			}
		}
	}()
	return done
}
