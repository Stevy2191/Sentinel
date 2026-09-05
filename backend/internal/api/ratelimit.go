package api

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// This file holds two independent rate-limiting mechanisms:
//
//   - attemptLimiter: a failure-counting lockout for auth endpoints. It only
//     tracks failed attempts and clears on success, so a legitimate user who
//     mistypes a password once is never penalized. Used for login, MFA
//     verification, and registration, where the goal is stopping credential
//     guessing.
//   - RateLimiter: a token-bucket throttle keyed by IP or user. It caps request
//     rate regardless of success/failure. Used for endpoints where repetition
//     itself is the problem (e.g. report generation, which aggregates a time
//     range and renders a PDF on every call).
//
// Both are in-process, which is the right scope for a single-binary
// self-hosted deployment. Running several replicas behind a load balancer
// would multiply the effective limit by the replica count; a shared store
// (Postgres or Redis) would be needed for a hard global cap in that case.

// ---- attemptLimiter: failure-counting lockout for auth endpoints ----------

// attemptLimiter tracks failed attempts per key within a sliding window and
// locks a key out for a cooldown period once it exceeds maxAttempts.
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

// respondRateLimited writes a 429 with a Retry-After header. Used by the
// attemptLimiter call sites in auth_handler.go.
func respondRateLimited(c *gin.Context, retryAfter time.Duration) {
	c.Header("Retry-After", fmt.Sprintf("%d", int(retryAfter.Seconds())+1))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"success": false,
		"error":   gin.H{"code": http.StatusTooManyRequests, "message": "too many attempts, please try again later"},
	})
}

// ---- RateLimiter: token-bucket throttle for general endpoints ------------

// visitorTTL is how long an idle bucket is kept before being swept, bounding
// memory under a spray of distinct keys.
const visitorTTL = 15 * time.Minute

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter hands out a token bucket per key and expires idle ones.
type RateLimiter struct {
	every rate.Limit
	burst int

	mu       sync.Mutex
	visitors map[string]*visitor
}

// NewRateLimiter allows burst events immediately, refilling at n per period.
func NewRateLimiter(n int, period time.Duration, burst int) *RateLimiter {
	rl := &RateLimiter{
		every:    rate.Every(period / time.Duration(n)),
		burst:    burst,
		visitors: make(map[string]*visitor),
	}
	go rl.sweep()
	return rl
}

// allow reports whether key may proceed now.
func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	v, ok := rl.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rl.every, rl.burst)}
		rl.visitors[key] = v
	}
	v.lastSeen = time.Now()
	rl.mu.Unlock()
	return v.limiter.Allow()
}

// sweep drops buckets that have gone idle.
func (rl *RateLimiter) sweep() {
	ticker := time.NewTicker(visitorTTL)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-visitorTTL)
		rl.mu.Lock()
		for key, v := range rl.visitors {
			if v.lastSeen.Before(cutoff) {
				delete(rl.visitors, key)
			}
		}
		rl.mu.Unlock()
	}
}

// keyFunc derives the bucket key for a request.
type keyFunc func(*gin.Context) string

// Middleware rejects requests over the limit with 429 and a Retry-After hint.
func (rl *RateLimiter) Middleware(name string, key keyFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		k := key(c)
		if rl.allow(k) {
			c.Next()
			return
		}
		log.Printf("[ratelimit] %s: %s %s throttled (key=%s)", name, c.Request.Method, c.FullPath(), k)
		c.Header("Retry-After", "60")
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"success": false,
			"error":   "too many requests; please wait a moment and try again",
		})
	}
}

// ByIP keys on the client address, for endpoints reachable before sign-in.
func ByIP(c *gin.Context) string { return c.ClientIP() }

// ByUser keys on the authenticated user, falling back to IP when the request is
// rejected before AuthMiddleware populates the context.
func ByUser(c *gin.Context) string {
	if userID, _, _, ok := GetUserFromContext(c); ok {
		return userID.String()
	}
	return c.ClientIP()
}
