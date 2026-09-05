package api

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// attemptLimiter tracks failed attempts per key within a sliding window and
// locks a key out for a cooldown period once it exceeds maxAttempts.
//
// This is intentionally in-process rather than backed by Redis or the
// database: Sentinel runs as a single backend instance, so a mutex-protected
// map is sufficient and avoids adding an external dependency. If Sentinel
// ever moves to multiple backend replicas, this needs to move to a shared
// store (e.g. Postgres or Redis) or attackers can simply round-robin across
// instances to reset their attempt count.
type attemptLimiter struct {
	mu          sync.Mutex
	maxAttempts int
	window      time.Duration
	lockout     time.Duration
	entries     map[string]*attemptEntry
}

type attemptEntry struct {
	count       int
	windowFrom  time.Time
	lockedUntil time.Time
}

// newAttemptLimiter returns a limiter that locks a key out for lockout once it
// racks up maxAttempts failures within window. It starts a background
// goroutine that periodically evicts stale entries.
func newAttemptLimiter(maxAttempts int, window, lockout time.Duration) *attemptLimiter {
	l := &attemptLimiter{
		maxAttempts: maxAttempts,
		window:      window,
		lockout:     lockout,
		entries:     make(map[string]*attemptEntry),
	}
	go l.cleanupLoop()
	return l
}

// Allowed reports whether key may currently attempt again, and if not, how
// long until it may retry.
func (l *attemptLimiter) Allowed(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	if !ok {
		return true, 0
	}
	now := time.Now()
	if now.Before(e.lockedUntil) {
		return false, e.lockedUntil.Sub(now)
	}
	return true, 0
}

// RecordFailure increments key's failure count within the current window,
// locking it out once maxAttempts is reached.
func (l *attemptLimiter) RecordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	e, ok := l.entries[key]
	if !ok || now.Sub(e.windowFrom) > l.window {
		e = &attemptEntry{windowFrom: now}
		l.entries[key] = e
	}
	e.count++
	if e.count >= l.maxAttempts {
		e.lockedUntil = now.Add(l.lockout)
	}
}

// RecordSuccess clears any tracked failures for key, so a legitimate login
// isn't penalized by a few earlier mistyped passwords.
func (l *attemptLimiter) RecordSuccess(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// cleanupLoop periodically evicts stale entries so the map doesn't grow
// unboundedly over the life of the process.
func (l *attemptLimiter) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for k, e := range l.entries {
			if now.After(e.lockedUntil) && now.Sub(e.windowFrom) > l.window {
				delete(l.entries, k)
			}
		}
		l.mu.Unlock()
	}
}

// Shared limiters for the auth endpoints. Tuned to be generous for a
// legitimate user who mistypes a password or TOTP code a couple of times,
// while making automated guessing impractical.
var (
	// loginUserLimiter keys on IP+username: stops one attacker grinding a
	// single account from one IP.
	loginUserLimiter = newAttemptLimiter(10, 15*time.Minute, 15*time.Minute)
	// loginIPLimiter keys on IP alone: stops one attacker spraying many
	// usernames (username enumeration / credential stuffing) from one IP.
	loginIPLimiter = newAttemptLimiter(30, 15*time.Minute, 15*time.Minute)
	// mfaLimiter keys on IP+userID: bounds TOTP/backup-code guessing once a
	// password has already been supplied correctly.
	mfaLimiter = newAttemptLimiter(8, 15*time.Minute, 15*time.Minute)
	// registerLimiter keys on IP alone: slows automated mass account
	// creation. Generous enough not to interfere with the one-time first-admin
	// bootstrap flow.
	registerLimiter = newAttemptLimiter(5, time.Hour, time.Hour)
)

// respondRateLimited writes a 429 with a Retry-After header.
func respondRateLimited(c *gin.Context, retryAfter time.Duration) {
	c.Header("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"error":   gin.H{"code": http.StatusTooManyRequests, "message": "too many attempts, please try again later"},
	})
}
