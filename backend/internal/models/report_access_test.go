package models

import (
	"testing"
	"time"
)

func TestReportAccessIsExpired(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	cases := []struct {
		name      string
		expiresAt *time.Time
		want      bool
	}{
		// A link created before expiry existed has no expiry and must keep
		// working; silently killing links already in circulation would break
		// access with no warning.
		{"nil never expires", nil, false},
		{"future is live", &future, false},
		{"past has lapsed", &past, true},
		// Exactly at the boundary counts as expired, so a link is never
		// usable at the instant it is supposed to stop.
		{"exact expiry instant has lapsed", &now, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ra := &ReportAccess{ExpiresAt: c.expiresAt}
			if got := ra.IsExpired(now); got != c.want {
				t.Errorf("IsExpired = %v, want %v", got, c.want)
			}
		})
	}
}
