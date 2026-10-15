package auth

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestSessionLifecycleAndConcurrency(t *testing.T) {
	now := time.Unix(1000, 0)
	s, err := NewSessionStore(2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := s.Create("admin", now)
	second, _ := s.Create("admin", now.Add(time.Second))
	third, _ := s.Create("admin", now.Add(2*time.Second))
	if _, ok := s.Get(first.ID, now.Add(3*time.Second)); ok {
		t.Fatal("oldest session was not evicted")
	}
	if _, ok := s.Get(second.ID, now.Add(3*time.Second)); !ok {
		t.Fatal("newer session was evicted")
	}
	if !ValidSessionCSRF(third, third.CSRFToken) || ValidSessionCSRF(third, "wrong") {
		t.Fatal("CSRF check failed")
	}
	rotated, err := s.Rotate(second.ID, "admin", now.Add(4*time.Second))
	if err != nil || rotated.ID == second.ID || rotated.CSRFToken == second.CSRFToken {
		t.Fatal("session rotation failed")
	}
	if _, ok := s.Get(second.ID, now.Add(4*time.Second)); ok {
		t.Fatal("old ID still valid")
	}
	if _, ok := s.Get(rotated.ID, now.Add(2*time.Hour)); ok {
		t.Fatal("expired session valid")
	}
	cookie := SessionCookie("hydra_admin_session", rotated, true, time.Hour)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatal("unsafe session cookie")
	}

	parallel, _ := NewSessionStore(8, time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, e := parallel.Create("admin", now.Add(time.Duration(i)*time.Second))
			if e != nil {
				t.Error(e)
				return
			}
			parallel.Get(v.ID, now.Add(time.Minute))
		}(i)
	}
	wg.Wait()
	if parallel.Len() > 8 {
		t.Fatalf("session cap exceeded: %d", parallel.Len())
	}
}

func TestPreAuthOneTimeExpiryCapAndConcurrency(t *testing.T) {
	now := time.Unix(1000, 0)
	p := NewPreAuthStore()
	p.max = 2
	oldCookie, oldNonce, err := p.Issue(now)
	if err != nil {
		t.Fatal(err)
	}
	p.Issue(now.Add(time.Second))
	cookie, nonce, _ := p.Issue(now.Add(2 * time.Second))
	if p.Len() != 2 || p.Consume(oldCookie, oldNonce, now.Add(3*time.Second)) {
		t.Fatal("pre-auth cap did not evict oldest")
	}
	var wg sync.WaitGroup
	twins := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); twins <- p.Consume(cookie, nonce, now.Add(3*time.Second)) }()
	}
	wg.Wait()
	close(twins)
	successes := 0
	for ok := range twins {
		if ok {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("nonce consumed %d times", successes)
	}
	expiredCookie, expiredNonce, _ := p.Issue(now)
	if p.Consume(expiredCookie, expiredNonce, now.Add(5*time.Minute)) {
		t.Fatal("expired nonce valid")
	}
	c := PreAuthCookie(cookie, true, now)
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 300 {
		t.Fatal("unsafe pre-auth cookie")
	}
	parallel := NewPreAuthStore()
	parallel.max = 8
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := parallel.Issue(now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if parallel.Len() > 8 {
		t.Fatalf("pre-auth cap exceeded: %d", parallel.Len())
	}
}
