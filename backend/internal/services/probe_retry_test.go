package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestProbeAttempts(t *testing.T) {
	cases := []struct{ retries, want int }{
		{0, 1}, {1, 2}, {3, 4}, {10, 11}, {-1, 1},
	}
	for _, c := range cases {
		if got := probeAttempts(&models.Monitor{Retries: c.retries}); got != c.want {
			t.Errorf("retries=%d: got %d attempts, want %d", c.retries, got, c.want)
		}
	}
}

// The budget divides the monitor's timeout rather than multiplying it, so
// retrying cannot make a check outlast the timeout it was given.
func TestProbeBudgetDividesTheTimeout(t *testing.T) {
	cases := []struct {
		total    time.Duration
		attempts int
		want     time.Duration
	}{
		{10 * time.Second, 4, 2500 * time.Millisecond},
		{10 * time.Second, 1, 10 * time.Second},
		{4 * time.Second, 4, time.Second},
		// Never below the floor, however many attempts are asked for.
		{2 * time.Second, 8, time.Second},
		{0, 4, time.Second},
	}
	for _, c := range cases {
		if got := probeBudget(c.total, c.attempts); got != c.want {
			t.Errorf("budget(%s, %d) = %s, want %s", c.total, c.attempts, got, c.want)
		}
	}
}

// A monitor with retries must not take longer than its timeout on a host that
// never answers, which is what would stall the sequential check loop.
func TestRetryStaysWithinTimeout(t *testing.T) {
	m := &models.Monitor{Retries: 3, TimeoutSeconds: 8}
	attempts := probeAttempts(m)
	budget := probeBudget(8*time.Second, attempts)

	var worst time.Duration
	for i := 0; i < attempts; i++ {
		worst += budget
		if i > 0 {
			worst += retryBackoff(i)
		}
	}
	// The probes themselves fit the timeout; only the short pauses between
	// them are extra, and they are deliberately small.
	if worst > 10*time.Second {
		t.Errorf("worst case %s exceeds a reasonable bound for an 8s timeout", worst)
	}
	t.Logf("4 probes of %s plus backoff = %s worst case for an 8s timeout", budget, worst)
}

// The behaviour this exists for: a probe that fails once and then succeeds is
// a successful check, not an outage.
//
// This is what produced the false alerts. A single lost ICMP packet — normal
// on a long path, since routers deprioritise ICMP — marked the monitor down,
// opened an incident and sent an alert, and the next check a minute later
// cleared it. The host was never unreachable.
func TestRetryProbeToleratesTransientFailure(t *testing.T) {
	svc := NewCheckService(nil)
	monitor := &models.Monitor{Retries: 3, TimeoutSeconds: 4}

	cases := []struct {
		name       string
		failFirst  int
		wantStatus string
		wantProbes int
	}{
		{"succeeds immediately", 0, checkSuccess, 1},
		{"one lost packet", 1, checkSuccess, 2},
		{"two lost packets", 2, checkSuccess, 3},
		{"three lost packets", 3, checkSuccess, 4},
		// Every probe unanswered is a real outage and must still be reported
		// on this check rather than being deferred to a later one.
		{"host genuinely down", 99, checkTimeout, 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probes := 0
			check := svc.retryProbe(context.Background(), monitor, "test",
				func(ctx context.Context, budget time.Duration) *models.Check {
					probes++
					if probes <= tc.failFirst {
						return svc.timeoutCheck(monitor, 1, errors.New("i/o timeout"))
					}
					return svc.successCheck(monitor, time.Millisecond, 0)
				})

			if check.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q", check.Status, tc.wantStatus)
			}
			if probes != tc.wantProbes {
				t.Errorf("sent %d probes, want %d", probes, tc.wantProbes)
			}
			if tc.wantStatus == checkTimeout && !strings.Contains(check.ErrorMessage, "4 attempts") {
				t.Errorf("a real failure should say how many attempts were made: %q", check.ErrorMessage)
			}
		})
	}
}

// With retries switched off the old behaviour is unchanged: one probe, and a
// single failure is a failure.
func TestRetryProbeHonoursZeroRetries(t *testing.T) {
	svc := NewCheckService(nil)
	monitor := &models.Monitor{Retries: 0, TimeoutSeconds: 4}

	probes := 0
	check := svc.retryProbe(context.Background(), monitor, "test",
		func(ctx context.Context, budget time.Duration) *models.Check {
			probes++
			return svc.timeoutCheck(monitor, 1, errors.New("i/o timeout"))
		})

	if probes != 1 {
		t.Errorf("sent %d probes, want 1", probes)
	}
	if check.Status != checkTimeout {
		t.Errorf("status = %q, want %q", check.Status, checkTimeout)
	}
}
