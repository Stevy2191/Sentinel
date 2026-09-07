package api

import (
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func check(status string, ms int, ts time.Time) models.Check {
	return models.Check{Status: status, ResponseTimeMs: ms, Timestamp: ts}
}

// The strip is drawn left-to-right oldest-first, but the query returns newest
// first. Getting this backwards would silently show history in reverse.
func TestBuildRecentChecksReversesToOldestFirst(t *testing.T) {
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	newestFirst := []models.Check{
		check("success", 30, base.Add(2*time.Minute)),
		check("failed", 0, base.Add(time.Minute)),
		check("success", 10, base),
	}

	out, _ := buildRecentChecks(newestFirst)

	if len(out) != 3 {
		t.Fatalf("want 3 entries, got %d", len(out))
	}
	want := []string{
		base.Format(time.RFC3339),
		base.Add(time.Minute).Format(time.RFC3339),
		base.Add(2 * time.Minute).Format(time.RFC3339),
	}
	for i, w := range want {
		if got := out[i]["timestamp"]; got != w {
			t.Errorf("entry %d: timestamp = %v, want %v", i, got, w)
		}
	}
}

// The percentage beside the strip must describe the same sample the strip
// draws, not a time window.
func TestBuildRecentChecksPassRateCoversExactlyTheseChecks(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		checks []models.Check
		want   float64
	}{
		{"all passing", []models.Check{
			check("success", 5, now), check("success", 5, now),
		}, 100},
		{"one of four failed", []models.Check{
			check("failed", 0, now), check("success", 5, now),
			check("success", 5, now), check("success", 5, now),
		}, 75},
		{"a timeout counts against it", []models.Check{
			check("timeout", 0, now), check("success", 5, now),
		}, 50},
		{"all failing", []models.Check{
			check("failed", 0, now), check("timeout", 0, now),
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, pct := buildRecentChecks(tc.checks)
			if pct == nil {
				t.Fatal("pass rate should not be nil when checks exist")
			}
			if *pct != tc.want {
				t.Errorf("pass rate = %v, want %v", *pct, tc.want)
			}
		})
	}
}

// A monitor that has never run has no pass rate. Reporting 0% would paint a
// brand new monitor as completely down.
func TestBuildRecentChecksNilRateWhenNoChecks(t *testing.T) {
	out, pct := buildRecentChecks(nil)
	if len(out) != 0 {
		t.Errorf("want no entries, got %d", len(out))
	}
	if pct != nil {
		t.Errorf("pass rate = %v, want nil", *pct)
	}
}

// The sample is defined by count, not by age: checks from months ago still
// belong in the strip if they are the most recent ones there are.
func TestBuildRecentChecksIgnoresCheckAge(t *testing.T) {
	old := time.Now().Add(-90 * 24 * time.Hour)
	out, pct := buildRecentChecks([]models.Check{
		check("success", 12, old.Add(time.Hour)),
		check("success", 12, old),
	})
	if len(out) != 2 {
		t.Fatalf("want 2 entries regardless of age, got %d", len(out))
	}
	if pct == nil || *pct != 100 {
		t.Errorf("pass rate = %v, want 100", pct)
	}
}
