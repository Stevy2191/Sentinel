package toolruns

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// 20 runs a minute per address, burst 20: the 21st at once is refused, one
// more is allowed once a token has refilled (one every 3 s), and addresses
// do not share a bucket.
func TestTargetLimiter(t *testing.T) {
	l := newTargetLimiter()
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= TargetRatePerMinute; i++ {
		if !l.allow("10.0.0.5", t0) {
			t.Fatalf("run %d refused", i)
		}
	}
	if l.allow("10.0.0.5", t0) {
		t.Error("the 21st run in the same instant was allowed")
	}
	if !l.allow("10.0.0.6", t0) {
		t.Error("another address was refused")
	}
	if !l.allow("10.0.0.5", t0.Add(4*time.Second)) {
		t.Error("refused 4 s later, after a token refilled")
	}
	if l.allow("10.0.0.5", t0.Add(4*time.Second)) {
		t.Error("two runs allowed on one refilled token")
	}
}

// Buckets idle for longer than targetIdleTTL are swept on a later call.
func TestTargetLimiterSweepsIdleBuckets(t *testing.T) {
	l := newTargetLimiter()
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	l.allow("10.0.0.5", t0)
	l.allow("10.0.0.6", t0.Add(targetIdleTTL+2*time.Minute))
	if _, ok := l.buckets["10.0.0.5"]; ok || len(l.buckets) != 1 {
		t.Errorf("buckets after the sweep: %d, want only 10.0.0.6", len(l.buckets))
	}
}

func TestPollTracker(t *testing.T) {
	p := newPollTracker()
	a := uuid.New()
	if _, ok := p.lastSeen(a); ok {
		t.Fatal("an agent that never polled has a last poll")
	}
	t0 := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	p.seen(a, t0)
	p.seen(a, t0.Add(time.Second))
	if got, ok := p.lastSeen(a); !ok || !got.Equal(t0.Add(time.Second)) {
		t.Errorf("lastSeen = %v, %v; want %v", got, ok, t0.Add(time.Second))
	}
}
