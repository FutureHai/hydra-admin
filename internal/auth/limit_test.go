package auth

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginAndIPLimits(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLoginLimiter(time.Minute, 3, 2, 2*time.Minute)
	l.RecordFailure("192.0.2.1", "Admin", now)
	l.RecordFailure("192.0.2.1", "admin", now)
	if d := l.Check("192.0.2.2", "admin", now); d.Status != http.StatusUnauthorized || d.RetryAfter != 0 {
		t.Fatalf("username lockout: %+v", d)
	}
	l.RecordFailure("192.0.2.1", "other", now)
	if d := l.Check("192.0.2.1", "third", now); d.Status != http.StatusTooManyRequests || d.RetryAfter <= 0 {
		t.Fatalf("IP limit: %+v", d)
	}
	if d := l.Check("192.0.2.1", "admin", now.Add(2*time.Minute)); !d.Allowed {
		t.Fatalf("lockout not expired: %+v", d)
	}
	a := NewIPLimiter(2, 1)
	if !a.Allow("192.0.2.3", false, now).Allowed || !a.Allow("192.0.2.3", true, now).Allowed {
		t.Fatal("early IP limit")
	}
	if d := a.Allow("192.0.2.3", true, now); d.Status != http.StatusTooManyRequests {
		t.Fatalf("write IP limit: %+v", d)
	}
	if !a.Allow("192.0.2.3", true, now.Add(time.Minute)).Allowed {
		t.Fatal("IP window not reset")
	}
}

func TestConcurrentLoginReservation(t *testing.T) {
	l := NewLoginLimiter(time.Minute, 3, 100, time.Minute)
	now := time.Unix(1000, 0)
	var wg sync.WaitGroup
	var admitted atomic.Int32
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Begin("192.0.2.10", "admin", now).Allowed {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 3 {
		t.Fatalf("admitted %d concurrent attempts, want 3", admitted.Load())
	}
}
