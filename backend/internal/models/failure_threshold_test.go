package models

import (
	"testing"
	"time"
)

// A blip is the case this whole mechanism exists for: one failed check between
// successes must not be called an outage.
func TestEvaluateCheck_SingleFailureIsNotAnOutage(t *testing.T) {
	m := &Monitor{CurrentStatus: StatusOnline, FailureThreshold: 2}
	at := time.Now()

	d := m.EvaluateCheck(CheckStatusFailed, at)
	if d.Status != StatusOnline {
		t.Fatalf("after one failure status = %q, want %q (not yet confirmed)", d.Status, StatusOnline)
	}
	if d.Confirmed {
		t.Fatal("one failure must not confirm an incident")
	}
	if d.ConsecutiveFailures != 1 {
		t.Fatalf("consecutive = %d, want 1", d.ConsecutiveFailures)
	}

	// The next check succeeds: the streak is forgotten entirely.
	m.ConsecutiveFailures, m.FailureStreakStartedAt = d.ConsecutiveFailures, d.StreakStartedAt
	d = m.EvaluateCheck(CheckStatusSuccess, at.Add(time.Minute))
	if d.Status != StatusOnline || d.ConsecutiveFailures != 0 || d.StreakStartedAt != nil {
		t.Fatalf("recovery did not clear the streak: %+v", d)
	}
}

func TestEvaluateCheck_ConfirmsAtThreshold(t *testing.T) {
	m := &Monitor{CurrentStatus: StatusOnline, FailureThreshold: 2}
	first := time.Now()

	d := m.EvaluateCheck(CheckStatusTimeout, first)
	m.ConsecutiveFailures, m.FailureStreakStartedAt, m.CurrentStatus = d.ConsecutiveFailures, d.StreakStartedAt, d.Status

	second := first.Add(time.Minute)
	d = m.EvaluateCheck(CheckStatusTimeout, second)
	if d.Status != StatusOffline {
		t.Fatalf("status = %q, want %q on the second failure", d.Status, StatusOffline)
	}
	if !d.Confirmed {
		t.Fatal("the check meeting the threshold must confirm")
	}
	// Dated from the first failure, so a threshold of 2 does not silently drop
	// an interval from every outage.
	if d.StreakStartedAt == nil || !d.StreakStartedAt.Equal(first) {
		t.Fatalf("incident start = %v, want the first failure at %v", d.StreakStartedAt, first)
	}
}

// Once down, further failures must not re-confirm: that would open a second
// incident for the same outage.
func TestEvaluateCheck_DoesNotReconfirmWhileDown(t *testing.T) {
	first := time.Now()
	m := &Monitor{
		CurrentStatus:          StatusOffline,
		FailureThreshold:       2,
		ConsecutiveFailures:    5,
		FailureStreakStartedAt: &first,
	}
	d := m.EvaluateCheck(CheckStatusFailed, first.Add(5*time.Minute))
	if d.Status != StatusOffline {
		t.Fatalf("status = %q, want %q", d.Status, StatusOffline)
	}
	if d.Confirmed {
		t.Fatal("an already-offline monitor must not confirm a new incident")
	}
}

func TestEvaluateCheck_ThresholdOfOneIsImmediate(t *testing.T) {
	m := &Monitor{CurrentStatus: StatusOnline, FailureThreshold: 1}
	d := m.EvaluateCheck(CheckStatusFailed, time.Now())
	if d.Status != StatusOffline || !d.Confirmed {
		t.Fatalf("threshold 1 must go down on the first failure, got %+v", d)
	}
}

// A monitor that has never reported must not be called online merely because a
// failure was not yet confirmed.
func TestEvaluateCheck_FirstEverCheckStaysUnknown(t *testing.T) {
	m := &Monitor{CurrentStatus: StatusUnknown, FailureThreshold: 3}
	d := m.EvaluateCheck(CheckStatusFailed, time.Now())
	if d.Status != StatusUnknown {
		t.Fatalf("status = %q, want %q", d.Status, StatusUnknown)
	}
	if d.Confirmed {
		t.Fatal("must not confirm below threshold")
	}
}

// Rows written before the column existed read as zero.
func TestFailureThresholdOrDefault(t *testing.T) {
	for _, tc := range []struct{ set, want int }{
		{0, DefaultFailureThreshold},
		{-3, DefaultFailureThreshold},
		{1, 1},
		{10, 10},
		{99, MaxFailureThreshold},
	} {
		m := &Monitor{FailureThreshold: tc.set}
		if got := m.FailureThresholdOrDefault(); got != tc.want {
			t.Errorf("FailureThreshold %d -> %d, want %d", tc.set, got, tc.want)
		}
	}
}

func TestValidate_RejectsOutOfRangeThreshold(t *testing.T) {
	base := func() *Monitor {
		return &Monitor{
			Name: "m", URL: "https://example.com", Type: MonitorTypeHTTP,
			IntervalSeconds: 60, TimeoutSeconds: 10,
		}
	}
	m := base()
	m.FailureThreshold = 11
	if err := m.Validate(); err == nil {
		t.Fatal("threshold 11 should be rejected")
	}
	m = base()
	m.FailureThreshold = 0 // omitted on a partial update
	if err := m.Validate(); err != nil {
		t.Fatalf("threshold 0 means unset and must be accepted: %v", err)
	}
}
