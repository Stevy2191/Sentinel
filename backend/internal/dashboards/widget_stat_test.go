package dashboards

import "testing"

func TestStatLevel(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		v          float64
		warn, crit *float64
		dir        string
		want       string
	}{
		{50, f(80), f(90), "above", "ok"},
		{85, f(80), f(90), "above", "warn"},
		{95, f(80), f(90), "above", "crit"},
		{90, f(80), f(90), "above", "crit"},
		{30, f(25), f(10), "below", "ok"},
		{20, f(25), f(10), "below", "warn"},
		{5, f(25), f(10), "below", "crit"},
		{99, nil, nil, "above", "ok"},
		{95, nil, f(90), "above", "crit"},
	}
	for _, c := range cases {
		if got := statLevel(c.v, c.warn, c.crit, c.dir); got != c.want {
			t.Errorf("statLevel(%v, %v, %v, %s) = %s, want %s", c.v, c.warn, c.crit, c.dir, got, c.want)
		}
	}
}
