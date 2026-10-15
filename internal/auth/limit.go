package auth

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const maxLimiterEntries = 10000

type Decision struct {
	Allowed    bool
	Status     int
	RetryAfter time.Duration
}

func allowed() Decision { return Decision{Allowed: true} }

type loginBucket struct {
	count                            int
	windowEnd, lockedUntil, lastSeen time.Time
}
type LoginLimiter struct {
	mu                         sync.Mutex
	ip, users                  map[string]loginBucket
	window, lockout            time.Duration
	ipMax, userMax, maxEntries int
}

func NewLoginLimiter(window time.Duration, ipMax, userMax int, lockout time.Duration) *LoginLimiter {
	return &LoginLimiter{ip: map[string]loginBucket{}, users: map[string]loginBucket{}, window: window, lockout: lockout, ipMax: ipMax, userMax: userMax, maxEntries: maxLimiterEntries}
}

func normalizedIP(raw string) string {
	if ip := net.ParseIP(raw); ip != nil {
		return ip.String()
	}
	return "invalid"
}
func normalizedUser(raw string) string {
	if len(raw) > 256 {
		return "oversize"
	}
	return strings.ToLower(raw)
}

func (l *LoginLimiter) Check(ip, username string, now time.Time) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(now)
	ipEntry := l.ip[normalizedIP(ip)]
	if now.Before(ipEntry.windowEnd) && ipEntry.count >= l.ipMax {
		return Decision{Status: http.StatusTooManyRequests, RetryAfter: ipEntry.windowEnd.Sub(now)}
	}
	userEntry := l.users[normalizedUser(username)]
	if now.Before(userEntry.lockedUntil) {
		return Decision{Status: http.StatusUnauthorized}
	}
	return allowed()
}

// Begin reserves one login attempt under a single lock so concurrent requests
// cannot all pass a limit before recording their failures.
func (l *LoginLimiter) Begin(ip, username string, now time.Time) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(now)
	ipKey, userKey := normalizedIP(ip), normalizedUser(username)
	ipEntry := l.ip[ipKey]
	if now.Before(ipEntry.windowEnd) && ipEntry.count >= l.ipMax {
		return Decision{Status: http.StatusTooManyRequests, RetryAfter: ipEntry.windowEnd.Sub(now)}
	}
	userEntry := l.users[userKey]
	if now.Before(userEntry.lockedUntil) {
		return Decision{Status: http.StatusUnauthorized}
	}
	if !now.Before(ipEntry.windowEnd) {
		ipEntry.count, ipEntry.windowEnd = 0, now.Add(l.window)
	}
	if !now.Before(userEntry.windowEnd) {
		userEntry.count, userEntry.windowEnd = 0, now.Add(l.window)
	}
	ipEntry.count++
	ipEntry.lastSeen = now
	userEntry.count++
	userEntry.lastSeen = now
	if userEntry.count >= l.userMax {
		userEntry.lockedUntil = now.Add(l.lockout)
	}
	l.ip[ipKey], l.users[userKey] = ipEntry, userEntry
	l.trimLocked(l.ip)
	l.trimLocked(l.users)
	return allowed()
}

func (l *LoginLimiter) RecordFailure(ip, username string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked(now)
	ipKey, userKey := normalizedIP(ip), normalizedUser(username)
	ipEntry := l.ip[ipKey]
	if !now.Before(ipEntry.windowEnd) {
		ipEntry.count = 0
		ipEntry.windowEnd = now.Add(l.window)
	}
	ipEntry.count++
	ipEntry.lastSeen = now
	l.ip[ipKey] = ipEntry
	userEntry := l.users[userKey]
	if !now.Before(userEntry.windowEnd) {
		userEntry.count = 0
		userEntry.windowEnd = now.Add(l.window)
	}
	userEntry.count++
	userEntry.lastSeen = now
	if userEntry.count >= l.userMax {
		userEntry.lockedUntil = now.Add(l.lockout)
	}
	l.users[userKey] = userEntry
	l.trimLocked(l.ip)
	l.trimLocked(l.users)
}

func (l *LoginLimiter) RecordSuccess(username string) {
	l.mu.Lock()
	delete(l.users, normalizedUser(username))
	l.mu.Unlock()
}

func (l *LoginLimiter) cleanupLocked(now time.Time) {
	for key, v := range l.ip {
		if !now.Before(v.windowEnd) {
			delete(l.ip, key)
		}
	}
	for key, v := range l.users {
		if !now.Before(v.windowEnd) && !now.Before(v.lockedUntil) {
			delete(l.users, key)
		}
	}
}

func (l *LoginLimiter) trimLocked(m map[string]loginBucket) {
	for len(m) > l.maxEntries {
		var key string
		var oldest time.Time
		for k, v := range m {
			if key == "" || v.lastSeen.Before(oldest) {
				key, oldest = k, v.lastSeen
			}
		}
		delete(m, key)
	}
}

type apiBucket struct {
	read, write   int
	end, lastSeen time.Time
}
type IPLimiter struct {
	mu                            sync.Mutex
	entries                       map[string]apiBucket
	readMax, writeMax, maxEntries int
}

func NewIPLimiter(readPerMinute, writePerMinute int) *IPLimiter {
	return &IPLimiter{entries: map[string]apiBucket{}, readMax: readPerMinute, writeMax: writePerMinute, maxEntries: maxLimiterEntries}
}

func (l *IPLimiter) Allow(ip string, write bool, now time.Time) Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	for key, v := range l.entries {
		if !now.Before(v.end) {
			delete(l.entries, key)
		}
	}
	key := normalizedIP(ip)
	v := l.entries[key]
	if !now.Before(v.end) {
		v = apiBucket{end: now.Add(time.Minute)}
	}
	if v.read >= l.readMax || (write && v.write >= l.writeMax) {
		return Decision{Status: http.StatusTooManyRequests, RetryAfter: v.end.Sub(now)}
	}
	v.read++
	if write {
		v.write++
	}
	v.lastSeen = now
	l.entries[key] = v
	for len(l.entries) > l.maxEntries {
		var oldestKey string
		var oldest time.Time
		for k, item := range l.entries {
			if oldestKey == "" || item.lastSeen.Before(oldest) {
				oldestKey, oldest = k, item.lastSeen
			}
		}
		delete(l.entries, oldestKey)
	}
	return allowed()
}
