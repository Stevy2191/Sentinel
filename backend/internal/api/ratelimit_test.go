package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRateLimiterAllowsBurstThenBlocks(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute, 3)

	for i := 0; i < 3; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("request %d within the burst should be allowed", i+1)
		}
	}
	if rl.allow("1.2.3.4") {
		t.Error("the request after the burst should be blocked")
	}
}

// Throttling one caller must not affect anyone else.
func TestRateLimiterIsolatesKeys(t *testing.T) {
	rl := NewRateLimiter(10, time.Minute, 2)

	rl.allow("attacker")
	rl.allow("attacker")
	if rl.allow("attacker") {
		t.Fatal("attacker should be throttled")
	}
	if !rl.allow("innocent") {
		t.Error("a different key must have its own bucket")
	}
}

func TestRateLimiterMiddlewareReturns429(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	rl := NewRateLimiter(10, time.Minute, 1)
	r.POST("/login", rl.Middleware("auth", ByIP), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	call := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		r.ServeHTTP(w, req)
		return w
	}

	if got := call().Code; got != http.StatusOK {
		t.Fatalf("first request = %d, want 200", got)
	}
	second := call()
	if second.Code != http.StatusTooManyRequests {
		t.Errorf("second request = %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("a 429 should tell the caller when to retry")
	}
	// The throttle response must use the standard envelope, not a bare string.
	if body := second.Body.String(); !contains(body, `"success":false`) {
		t.Errorf("429 body should use the error envelope, got %s", body)
	}
}

// The registry is written from concurrent requests.
func TestRateLimiterConcurrentAccess(t *testing.T) {
	rl := NewRateLimiter(1000, time.Minute, 1000)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rl.allow("shared")
			rl.allow(string(rune('a' + i%5)))
		}(i)
	}
	wg.Wait()
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
