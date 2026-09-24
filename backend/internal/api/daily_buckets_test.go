package api

import (
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// computeDailyUptimeBuckets always returns exactly 90 entries, oldest first,
// ending at the UTC calendar day containing "now" - the public status page's
// lighter-weight, day-granularity counterpart to computeHourlyUptimeBuckets.
func TestComputeDailyUptimeBucketsStatusPerDay(t *testing.T) {
	now := time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC)

	checks := []models.Check{
		check("success", 10, time.Date(2026, 9, 22, 1, 0, 0, 0, time.UTC)),
		check("success", 10, time.Date(2026, 9, 22, 13, 0, 0, 0, time.UTC)),
		check("failed", 0, time.Date(2026, 9, 21, 1, 0, 0, 0, time.UTC)),
		check("failed", 0, time.Date(2026, 9, 21, 13, 0, 0, 0, time.UTC)),
		check("success", 10, time.Date(2026, 9, 20, 1, 0, 0, 0, time.UTC)),
		check("failed", 0, time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC)),
	}

	daily := computeDailyUptimeBuckets(checks, now)
	if len(daily) != 90 {
		t.Fatalf("want 90 entries, got %d", len(daily))
	}

	// curDay = Sep 24, so index 89 is Sep 24 and index 87 is Sep 22.
	cases := []struct {
		index  int
		date   string
		status string
		uptime float64
	}{
		{87, "2026-09-22", "up", 100},
		{86, "2026-09-21", "down", 0},
		{85, "2026-09-20", "partial", 50},
		{84, "2026-09-19", "nodata", 0},
	}
	for _, c := range cases {
		e := daily[c.index]
		if e["date"] != c.date {
			t.Errorf("index %d: date = %v, want %v", c.index, e["date"], c.date)
		}
		if e["status"] != c.status {
			t.Errorf("index %d (%s): status = %v, want %v", c.index, c.date, e["status"], c.status)
		}
		if e["uptime"] != c.uptime {
			t.Errorf("index %d (%s): uptime = %v, want %v", c.index, c.date, e["uptime"], c.uptime)
		}
	}

	if daily[89]["date"] != "2026-09-24" {
		t.Errorf("last entry date = %v, want today (2026-09-24)", daily[89]["date"])
	}
	if daily[0]["date"] != "2026-06-27" {
		t.Errorf("first entry date = %v, want 89 days before today (2026-06-27)", daily[0]["date"])
	}
}

// The public shape leaks nothing beyond the health signal: exactly date,
// uptime, and status - no incident- or maintenance-derived detail, since this
// function (unlike computeHourlyUptimeBuckets) never had it to begin with.
func TestComputeDailyUptimeBucketsShapeIsMinimal(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	daily := computeDailyUptimeBuckets(nil, now)
	if len(daily) != 90 {
		t.Fatalf("want 90 entries, got %d", len(daily))
	}
	for i, e := range daily {
		if len(e) != 3 {
			t.Fatalf("entry %d has %d keys, want exactly 3 (date, uptime, status): %v", i, len(e), e)
		}
		for _, key := range []string{"date", "uptime", "status"} {
			if _, ok := e[key]; !ok {
				t.Errorf("entry %d missing key %q", i, key)
			}
		}
	}
}

// A day with no checks at all - before the monitor existed, or simply quiet -
// reads as "nodata" rather than a false 0% (which would look like a full-day
// outage instead of an absence of information).
func TestComputeDailyUptimeBucketsNoChecksIsNodataNotZero(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	daily := computeDailyUptimeBuckets(nil, now)
	last := daily[len(daily)-1]
	if last["status"] != "nodata" {
		t.Errorf("status = %v, want nodata", last["status"])
	}
	if last["uptime"] != 0.0 {
		t.Errorf("uptime = %v, want 0", last["uptime"])
	}
}
