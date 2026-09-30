package services

import (
	"testing"
	"time"
)

func TestPickResolution(t *testing.T) {
	end := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		span   time.Duration
		source string
		step   time.Duration
	}{
		{time.Hour, "raw", time.Minute},
		{6 * time.Hour, "raw", time.Minute},
		{24 * time.Hour, "5m", 5 * time.Minute},
		{7 * 24 * time.Hour, "5m", 25 * time.Minute},
		{30 * 24 * time.Hour, "1h", 2 * time.Hour},
		{90 * 24 * time.Hour, "1h", 5 * time.Hour},
		{365 * 24 * time.Hour, "1h", 18 * time.Hour},
	}
	for _, c := range cases {
		source, step := PickResolution(end.Add(-c.span), end)
		if source != c.source || step != c.step {
			t.Errorf("%v: %s/%v, want %s/%v", c.span, source, step, c.source, c.step)
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
	if KnownMetric("if_in_octets") || len(MetricCatalogue) != 9 {
		t.Error("catalogue must be exactly the spec's nine metrics")
	}
}
