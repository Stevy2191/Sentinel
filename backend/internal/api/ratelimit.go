package api

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// Rate limiting for endpoints where repetition is either an attack or a
// resource problem:
//
//   - Login and registration: unlimited attempts let an attacker brute-force a
//     password at network speed. Keyed by client IP.
//   - Report generation: each call aggregates a time range and renders a PDF, so
//     a loop here is a cheap way to exhaust CPU and disk. Keyed by user.
//
// The limiter is in-process, which is the right scope for a single-binary
// self-hosted deployment. Running several replicas behind a load balancer would
// multiply the effective limit by the replica count; a shared store would be
// needed for a hard global cap.

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
