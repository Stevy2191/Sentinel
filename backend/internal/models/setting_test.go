package models

import "testing"

// The instance default an admin may set has to be a value a monitor can
// actually hold; if these ever drift, every monitor created from the default
// would fail validation. They are aliased in monitor.go for exactly this
// reason, and this pins the relationship.
func TestCheckIntervalBoundsMatchMonitorValidation(t *testing.T) {
	if minIntervalSeconds != MinCheckIntervalSeconds {
		t.Errorf("monitor min interval %d != settings min %d", minIntervalSeconds, MinCheckIntervalSeconds)
	}
	if maxIntervalSeconds != MaxCheckIntervalSeconds {
		t.Errorf("monitor max interval %d != settings max %d", maxIntervalSeconds, MaxCheckIntervalSeconds)
	}

	// And the bounds themselves must survive Validate.
	for _, interval := range []int{MinCheckIntervalSeconds, MaxCheckIntervalSeconds} {
		m := &Monitor{
			Name:            "boundary",
			Type:            MonitorTypeHTTP,
			URL:             "https://example.com",
			IntervalSeconds: interval,
			TimeoutSeconds:  1,
			Retries:         0,
		}
		if err := m.Validate(); err != nil {
			t.Errorf("interval %d should be valid for a monitor: %v", interval, err)
		}
	}
}
