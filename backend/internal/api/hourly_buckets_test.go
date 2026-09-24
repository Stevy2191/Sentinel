package api

import (
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// computeHourlyUptimeBuckets always returns exactly 24 entries, oldest first,
// ending at the hour containing "now" - the shape both the authenticated
// history endpoint and the public status page rely on.
func TestComputeHourlyUptimeBucketsStatusPerHour(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 30, 0, 0, time.UTC)
	createdAt := time.Date(2026, 9, 24, 7, 30, 0, 0, time.UTC)

	checks := []models.Check{
		check("success", 10, time.Date(2026, 9, 24, 10, 5, 0, 0, time.UTC)),
		check("success", 10, time.Date(2026, 9, 24, 10, 35, 0, 0, time.UTC)),
		check("failed", 0, time.Date(2026, 9, 24, 9, 5, 0, 0, time.UTC)),
		check("failed", 0, time.Date(2026, 9, 24, 9, 35, 0, 0, time.UTC)),
		check("success", 10, time.Date(2026, 9, 24, 8, 5, 0, 0, time.UTC)),
		check("failed", 0, time.Date(2026, 9, 24, 8, 35, 0, 0, time.UTC)),
	}

	hourly := computeHourlyUptimeBuckets(checks, nil, nil, now, createdAt)
	if len(hourly) != 24 {
		t.Fatalf("want 24 entries, got %d", len(hourly))
	}

	// curHour = 12:00, so index 23 is 12:00 and index 21 is 10:00.
	cases := []struct {
		index    int
		hour     int
		status   string
		uptime   float64
		observed bool
	}{
		{21, 10, "up", 100, true},
		{20, 9, "down", 0, true},
		{19, 8, "partial", 50, true},
		{18, 7, "nodata", 0, true},  // ends at 08:00, after createdAt 07:30
		{17, 6, "nodata", 0, false}, // ends at 07:00, before createdAt 07:30
	}
	for _, c := range cases {
		e := hourly[c.index]
		if e["hour"] != c.hour {
			t.Errorf("index %d: hour = %v, want %v", c.index, e["hour"], c.hour)
		}
		if e["status"] != c.status {
			t.Errorf("index %d (hour %d): status = %v, want %v", c.index, c.hour, e["status"], c.status)
		}
		if e["uptime"] != c.uptime {
			t.Errorf("index %d (hour %d): uptime = %v, want %v", c.index, c.hour, e["uptime"], c.uptime)
		}
		if e["observed"] != c.observed {
			t.Errorf("index %d (hour %d): observed = %v, want %v", c.index, c.hour, e["observed"], c.observed)
		}
	}
}

// publicHourlyBuckets must leak nothing beyond the health signal: no exact
// downtime/maintenance clock times, no bucket_start, no internal "observed"
// flag - just enough to draw and colour a bar.
func TestPublicHourlyBucketsTrimsToHealthSignal(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 30, 0, 0, time.UTC)
	createdAt := now.Add(-48 * time.Hour)
	checks := []models.Check{
		check("failed", 0, time.Date(2026, 9, 24, 9, 5, 0, 0, time.UTC)),
	}
	hourly := computeHourlyUptimeBuckets(checks, nil, nil, now, createdAt)

	trimmed := publicHourlyBuckets(hourly)
	if len(trimmed) != len(hourly) {
		t.Fatalf("want %d entries, got %d", len(hourly), len(trimmed))
	}
	for i, e := range trimmed {
		if len(e) != 3 {
			t.Fatalf("entry %d has %d keys, want exactly 3 (hour, uptime, status): %v", i, len(e), e)
		}
		for _, key := range []string{"hour", "uptime", "status"} {
			if _, ok := e[key]; !ok {
				t.Errorf("entry %d missing key %q", i, key)
			}
		}
		for _, leaked := range []string{
			"bucket_start", "down_seconds", "maintenance_seconds",
			"down_start", "down_end", "maintenance_start", "maintenance_end", "observed",
		} {
			if _, ok := e[leaked]; ok {
				t.Errorf("entry %d leaked internal key %q", i, leaked)
			}
		}
	}
}
