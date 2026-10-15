package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"
)

const PreAuthCookieName = "hydra_admin_pre_auth"
const preAuthTTL = 5 * time.Minute
const maxPreAuthEntries = 10000

type Session struct {
	ID         string
	Username   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	CSRFToken  string
	LastSeenAt time.Time
}

type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]Session
	max      int
	ttl      time.Duration
}

func NewSessionStore(max int, ttl time.Duration) (*SessionStore, error) {
	if max < 1 || ttl <= 0 {
		return nil, errors.New("invalid session limit or TTL")
	}
	return &SessionStore{sessions: make(map[string]Session), max: max, ttl: ttl}, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *SessionStore) Create(username string, now time.Time) (Session, error) {
	return s.Rotate("", username, now)
}

// Rotate invalidates any supplied old ID and creates a fresh session under one lock.
func (s *SessionStore) Rotate(oldID, username string, now time.Time) (Session, error) {
	id, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	session := Session{ID: id, Username: username, CreatedAt: now, ExpiresAt: now.Add(s.ttl), CSRFToken: csrf, LastSeenAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, oldID)
	s.cleanupLocked(now)
	for len(s.sessions) >= s.max {
		s.evictOldestLocked()
	}
	s.sessions[id] = session
	return session, nil
}

func (s *SessionStore) Get(id string, now time.Time) (Session, bool) {
	if id == "" {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[id]
	if !ok {
		return Session{}, false
	}
	if !now.Before(v.ExpiresAt) {
		delete(s.sessions, id)
		return Session{}, false
	}
	v.LastSeenAt = now
	s.sessions[id] = v
	return v, true
}

func (s *SessionStore) Delete(id string)      { s.mu.Lock(); delete(s.sessions, id); s.mu.Unlock() }
func (s *SessionStore) Len() int              { s.mu.Lock(); defer s.mu.Unlock(); return len(s.sessions) }
func (s *SessionStore) Cleanup(now time.Time) { s.mu.Lock(); s.cleanupLocked(now); s.mu.Unlock() }

func (s *SessionStore) cleanupLocked(now time.Time) {
	for id, v := range s.sessions {
		if !now.Before(v.ExpiresAt) {
			delete(s.sessions, id)
		}
	}
}

func (s *SessionStore) evictOldestLocked() {
	var oldestID string
	var oldest time.Time
	for id, v := range s.sessions {
		if oldestID == "" || v.CreatedAt.Before(oldest) || (v.CreatedAt.Equal(oldest) && id < oldestID) {
			oldestID, oldest = id, v.CreatedAt
		}
	}
	if oldestID != "" {
		delete(s.sessions, oldestID)
	}
}

func ValidSessionCSRF(session Session, token string) bool {
	if token == "" || len(token) != len(session.CSRFToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(session.CSRFToken)) == 1
}

func SessionCookie(name string, session Session, secure bool, ttl time.Duration) *http.Cookie {
	return &http.Cookie{Name: name, Value: session.ID, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds()), Expires: session.ExpiresAt}
}

func ExpiredCookie(name string, secure bool) *http.Cookie {
	return &http.Cookie{Name: name, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(0, 0)}
}

type preAuthEntry struct {
	nonce            string
	created, expires time.Time
}
type PreAuthStore struct {
	mu      sync.Mutex
	entries map[string]preAuthEntry
	max     int
}

func NewPreAuthStore() *PreAuthStore {
	return &PreAuthStore{entries: make(map[string]preAuthEntry), max: maxPreAuthEntries}
}

func (p *PreAuthStore) Issue(now time.Time) (cookieValue, nonce string, err error) {
	cookieValue, err = randomToken()
	if err != nil {
		return "", "", err
	}
	nonce, err = randomToken()
	if err != nil {
		return "", "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleanupLocked(now)
	for len(p.entries) >= p.max {
		p.evictOldestLocked()
	}
	p.entries[cookieValue] = preAuthEntry{nonce: nonce, created: now, expires: now.Add(preAuthTTL)}
	return cookieValue, nonce, nil
}

func (p *PreAuthStore) Consume(cookieValue, nonce string, now time.Time) bool {
	if cookieValue == "" || nonce == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.entries[cookieValue]
	if !ok {
		return false
	}
	if !now.Before(v.expires) {
		delete(p.entries, cookieValue)
		return false
	}
	if len(v.nonce) != len(nonce) || subtle.ConstantTimeCompare([]byte(v.nonce), []byte(nonce)) != 1 {
		return false
	}
	delete(p.entries, cookieValue)
	return true
}

func (p *PreAuthStore) Len() int              { p.mu.Lock(); defer p.mu.Unlock(); return len(p.entries) }
func (p *PreAuthStore) Cleanup(now time.Time) { p.mu.Lock(); p.cleanupLocked(now); p.mu.Unlock() }
func (p *PreAuthStore) cleanupLocked(now time.Time) {
	for id, v := range p.entries {
		if !now.Before(v.expires) {
			delete(p.entries, id)
		}
	}
}
func (p *PreAuthStore) evictOldestLocked() {
	var oldestID string
	var oldest time.Time
	for id, v := range p.entries {
		if oldestID == "" || v.created.Before(oldest) || (v.created.Equal(oldest) && id < oldestID) {
			oldestID, oldest = id, v.created
		}
	}
	if oldestID != "" {
		delete(p.entries, oldestID)
	}
}

func PreAuthCookie(value string, secure bool, now time.Time) *http.Cookie {
	return &http.Cookie{Name: PreAuthCookieName, Value: value, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: int(preAuthTTL.Seconds()), Expires: now.Add(preAuthTTL)}
}
