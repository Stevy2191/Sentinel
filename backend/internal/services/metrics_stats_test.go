package services

import (
	"testing"
	"time"
)

// Only whole buckets inside the window that have ended by now count.
func TestStatsWindow(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	later := base.Add(24 * time.Hour)
	m := func(n int) time.Time { return base.Add(time.Duration(n) * time.Minute) }
	cases := []struct {
		name         string
		from, to     time.Time
		now          time.Time
		bucket       time.Duration
		lo, hi       time.Time
		wantExpected int
	}{
		{"partial buckets at both ends", m(-2), m(32), later, statsBucket, m(0), m(30), 6},
		{"a rolling window ending now", m(0), m(32), m(32), statsBucket, m(0), m(30), 6},
		{"now inside the window", m(0), m(30), m(17), statsBucket, m(0), m(15), 3},
		{"a window in the future", m(60), m(90), m(10), statsBucket, m(60), m(60), 0},
		{"hourly buckets", m(10), m(300), later, time.Hour, m(60), m(300), 4},
	}
	for _, c := range cases {
		lo, hi, n := statsWindow(c.from, c.to, c.now, c.bucket)
		if !lo.Equal(c.lo) || !hi.Equal(c.hi) || n != c.wantExpected {
			t.Errorf("%s: [%v, %v) %d buckets, want [%v, %v) %d", c.name, lo, hi, n, c.lo, c.hi, c.wantExpected)
		}
	}
}

func TestSeriesStatCoverage(t *testing.T) {
	if got := (SeriesStat{Buckets: 5, Expected: 6}).Coverage(); got < 83.33 || got > 83.34 {
		t.Errorf("5 of 6 buckets = %v%%, want 83.33", got)
	}
	if got := (SeriesStat{}).Coverage(); got != 0 {
		t.Errorf("an empty window = %v%%, want 0", got)
	}
}

// Report charts read the 5-minute rollup up to 7 days and the hourly one
// beyond (PickResolution's split, never raw), at about maxPoints points.
func TestChartResolution(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		span      time.Duration
		maxPoints int
		table     string
		base      time.Duration
		step      time.Duration
	}{
		{2 * time.Hour, 12, "metrics.samples_5m", 5 * time.Minute, 10 * time.Minute},
		{time.Hour, 200, "metrics.samples_5m", 5 * time.Minute, 5 * time.Minute},
		{7 * 24 * time.Hour, 200, "metrics.samples_5m", 5 * time.Minute, 55 * time.Minute}, // 50.4 min, rounded up
		{7*24*time.Hour + time.Hour, 200, "metrics.samples_1h", time.Hour, time.Hour},      // 50.7 min < 1 h
		{30 * 24 * time.Hour, 200, "metrics.samples_1h", time.Hour, 4 * time.Hour},         // 3.6 h, rounded up
		{365 * 24 * time.Hour, 200, "metrics.samples_1h", time.Hour, 44 * time.Hour},       // 43.8 h, rounded up
	}
	for _, c := range cases {
		table, base, step := chartResolution(from, from.Add(c.span), c.maxPoints)
		if table != c.table || base != c.base || step != c.step {
			t.Errorf("%v at %d points: %s / %v / %v, want %s / %v / %v", c.span, c.maxPoints, table, base, step, c.table, c.base, c.step)
		}
	}
}
