package services

import (
	"testing"
	"time"
)

// The purge must land at 02:00 local on the next day that has not passed yet,
// and must never schedule itself in the past.
func TestNextRunAt(t *testing.T) {
	loc := time.UTC
	cases := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{"before the hour, runs today",
			time.Date(2026, 3, 15, 1, 30, 0, 0, loc),
			time.Date(2026, 3, 15, 2, 0, 0, 0, loc)},
		{"after the hour, runs tomorrow",
			time.Date(2026, 3, 15, 2, 30, 0, 0, loc),
			time.Date(2026, 3, 16, 2, 0, 0, 0, loc)},
		{"exactly on the hour does not fire twice",
			time.Date(2026, 3, 15, 2, 0, 0, 0, loc),
			time.Date(2026, 3, 16, 2, 0, 0, 0, loc)},
		{"just before midnight",
			time.Date(2026, 3, 15, 23, 59, 0, 0, loc),
			time.Date(2026, 3, 16, 2, 0, 0, 0, loc)},
		{"month boundary",
			time.Date(2026, 3, 31, 5, 0, 0, 0, loc),
			time.Date(2026, 4, 1, 2, 0, 0, 0, loc)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextRunAt(tc.now, PurgeHour)
			if !got.Equal(tc.want) {
				t.Errorf("nextRunAt(%s) = %s, want %s", tc.now, got, tc.want)
			}
			if !got.After(tc.now) {
				t.Errorf("nextRunAt(%s) = %s is not in the future", tc.now, got)
			}
		})
	}
}
