package services

import "testing"

func TestEvaluateThreshold(t *testing.T) {
	val := func(v float64) *float64 { return &v }
	thr := func(v int) *int { return &v }

	cases := []struct {
		name           string
		value          *float64
		threshold      *int
		currentlyActive bool
		wantActive     bool
		wantNotify     bool
		wantBreached   bool
	}{
		{"not configured, no value", nil, nil, false, false, false, false},
		{"not configured, has a value", val(95), nil, false, false, false, false},
		{"configured, no reading this cycle", nil, thr(90), false, false, false, false},
		{"configured, no reading this cycle, was already active", nil, thr(90), true, true, false, false},
		{"under threshold, was ok", val(50), thr(90), false, false, false, false},
		{"under threshold, was already active", val(50), thr(90), true, false, true, false},
		{"exactly at threshold breaches", val(90), thr(90), false, true, true, true},
		{"over threshold, first breach", val(95), thr(90), false, true, true, true},
		{"over threshold, already active", val(96), thr(90), true, true, false, false},
		{"drops back to exactly under threshold recovers", val(89), thr(90), true, false, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := evaluateThreshold(c.value, c.threshold, c.currentlyActive)
			if got.newActive != c.wantActive {
				t.Errorf("newActive = %v, want %v", got.newActive, c.wantActive)
			}
			if got.notify != c.wantNotify {
				t.Errorf("notify = %v, want %v", got.notify, c.wantNotify)
			}
			if got.breached != c.wantBreached {
				t.Errorf("breached = %v, want %v", got.breached, c.wantBreached)
			}
		})
	}
}

func TestNormalizeThreshold(t *testing.T) {
	v := func(n int) *int { return &n }

	if got := normalizeThreshold(nil); got != nil {
		t.Errorf("nil input should stay nil, got %v", *got)
	}
	if got := normalizeThreshold(v(0)); got != nil {
		t.Errorf("0 is the clear sentinel and should normalize to nil, got %v", *got)
	}
	if got := normalizeThreshold(v(90)); got == nil || *got != 90 {
		t.Errorf("90 should pass through unchanged, got %v", got)
	}
}
