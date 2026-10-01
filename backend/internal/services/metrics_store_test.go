package services

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPickResolution(t *testing.T) {
	end := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	farPast := end.Add(-3650 * 24 * time.Hour)
	cases := []struct {
		span     time.Duration
		rawSince time.Time
		source   string
		step     time.Duration
	}{
		{time.Hour, farPast, "raw", time.Minute},
		{6 * time.Hour, farPast, "raw", time.Minute},
		{24 * time.Hour, farPast, "5m", 5 * time.Minute},
		{7 * 24 * time.Hour, farPast, "5m", 25 * time.Minute},
		{30 * 24 * time.Hour, farPast, "1h", 2 * time.Hour},
		{90 * 24 * time.Hour, farPast, "1h", 5 * time.Hour},
		{365 * 24 * time.Hour, farPast, "1h", 18 * time.Hour},
		// A 1h window entirely after rawSince still reads raw.
		{time.Hour, end.Add(-2 * time.Hour), "raw", time.Minute},
		// A 1h window starting before rawSince (raw retention has already
		// dropped the start of the window) must fall back to the 5m rollup.
		{time.Hour, end.Add(-30 * time.Minute), "5m", 5 * time.Minute},
	}
	for _, c := range cases {
		from := end.Add(-c.span)
		source, step := PickResolution(from, end, c.rawSince)
		if source != c.source || step != c.step {
			t.Errorf("span %v rawSince %v: %s/%v, want %s/%v", c.span, c.rawSince, source, step, c.source, c.step)
		}
		if points := int(c.span / step); points > maxQueryPoints {
			t.Errorf("%v: %d points", c.span, points)
		}
	}
}

func TestMetricCatalogue(t *testing.T) {
	for _, k := range []string{"if_in_bps", "if_out_bps", "if_in_util_pct", "if_out_util_pct", "if_in_errors_pm",
		"if_out_errors_pm", "if_in_discards_pm", "if_out_discards_pm", "if_speed_bps"} {
		if !KnownMetric(k) {
			t.Errorf("%s missing from the catalogue", k)
		}
	}
	// The phase 2 port metrics plus the UPS poll's eight.
	if KnownMetric("if_in_octets") || len(MetricCatalogue) != 9+len(UPSMetrics) || len(UPSMetrics) != 8 {
		t.Error("catalogue must be exactly the nine port metrics and the eight UPS metrics")
	}
}

// M7: Forget drops only the given device's cached series ids, so a device id
// reused by a restored backup resolves fresh series instead of writing new
// samples under ids that were queued for the nightly cleanup to remove.
func TestMetricsStoreForgetDropsOnlyThatDevicesSeriesIDs(t *testing.T) {
	m := NewMetricsStore(nil)
	dead, alive := uuid.New(), uuid.New()
	m.ids[seriesKey{device: dead, metric: MetricIfInBps, instance: "1"}] = 10
	m.ids[seriesKey{device: dead, metric: MetricIfOutBps, instance: "1"}] = 11
	m.ids[seriesKey{device: alive, metric: MetricIfInBps, instance: "1"}] = 20

	m.Forget(dead)

	if len(m.ids) != 1 {
		t.Fatalf("after Forget: %d cached ids, want 1: %+v", len(m.ids), m.ids)
	}
	if id, ok := m.ids[seriesKey{device: alive, metric: MetricIfInBps, instance: "1"}]; !ok || id != 20 {
		t.Errorf("the other device's cached id should survive: %v %v", id, ok)
	}
}
