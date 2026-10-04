package services

import (
	"math"
	"testing"
)

func TestFormatValue(t *testing.T) {
	cases := []struct {
		v    float64
		unit string
		want string
	}{
		{0, "bps", "0 bps"},
		{950, "bps", "950 bps"},
		{12_345, "bps", "12.3 Kbps"},
		{640e6, "bps", "640 Mbps"},
		{1.25e9, "bps", "1.25 Gbps"},
		{999.6e6, "bps", "1 Gbps"}, // would round to "1000 Mbps"
		{2.5e12, "bps", "2500 Gbps"},
		{1.5, "per_min", "1.5/min"},
		{0.25, "per_min", "0.25/min"},
		{12.34, "%", "12.3%"},
		{40, "%", "40%"},
		{92.5, "%", "92.5%"},
		{0.0030787, "bool", "0.3%"},
		{1, "bool", "100%"},
		{31.5, "°C", "31.5 °C"},
		{230, "V", "230 V"},
		{42, "min", "42 min"},
		{1200, "rpm", "1200 rpm"},
		{3.14159, "", "3.14"},
		{math.NaN(), "bps", "-"},
		{math.Inf(1), "%", "-"},
	}
	for _, c := range cases {
		if got := formatValue(c.v, c.unit); got != c.want {
			t.Errorf("formatValue(%v, %q) = %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}

func TestFormatTotal(t *testing.T) {
	cases := []struct {
		v    float64
		unit string
		want string
	}{
		{512, "bps", "512 B"},
		{1.5e6, "bps", "1.5 MB"},
		{2.1e12, "bps", "2.1 TB"},
		{3.25e15, "bps", "3.25 PB"},
		{0, "per_min", "0"},
		{999, "per_min", "999"},
		{1000, "per_min", "1,000"},
		{1234567.4, "per_min", "1,234,567"},
		{42, "%", ""},
		{42, "bool", ""},
	}
	for _, c := range cases {
		if got := formatTotal(c.v, c.unit); got != c.want {
			t.Errorf("formatTotal(%v, %q) = %q, want %q", c.v, c.unit, got, c.want)
		}
	}
}

func TestFormatDurationHM(t *testing.T) {
	cases := map[float64]string{
		0: "0 m", -5: "0 m", 29: "< 1 m", 30: "1 m", 2700: "45 m", 3600: "1 h", 7980: "2 h 13 m", 90000: "25 h",
	}
	for in, want := range cases {
		if got := formatDurationHM(in); got != want {
			t.Errorf("formatDurationHM(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatChange(t *testing.T) {
	cases := []struct {
		change *float64
		isNew  bool
		want   string
	}{
		{f64(18), false, "+18%"},
		{f64(-25), false, "-25%"},
		{f64(2), false, "+2%"},
		{f64(0.44), false, "+0.4%"},
		{f64(-3.46), false, "-3.5%"},
		{f64(9.96), false, "+10%"},
		{f64(0.04), false, "0%"},
		{f64(-0.04), false, "0%"},
		{f64(150), false, "+150%"},
		{nil, true, "new"},
		{f64(18), true, "new"},
		{nil, false, ""},
		{f64(math.Inf(1)), false, ""},
	}
	for _, c := range cases {
		if got := formatChange(c.change, c.isNew); got != c.want {
			t.Errorf("formatChange(%v, %v) = %q, want %q", c.change, c.isNew, got, c.want)
		}
	}
}
