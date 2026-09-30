package portmon

import (
	"math"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func reading(at time.Time, uptime int64, in, out uint64) Reading {
	return Reading{At: at, UptimeSeconds: uptime, HC: true, InOctets: in, OutOctets: out, SpeedBps: 1_000_000_000}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6*math.Max(1, math.Abs(b)) }

func TestComputeRates(t *testing.T) {
	minute := time.Minute
	cases := []struct {
		name     string
		prev     *Reading
		cur      Reading
		wantSkip Skip
		check    func(t *testing.T, r Rates)
	}{
		{name: "first reading", prev: nil, cur: reading(t0, 100, 0, 0), wantSkip: SkipFirst},
		{
			name: "100 Mb/s in on a 1 Gb/s port",
			prev: ptr(reading(t0, 100, 0, 0)),
			cur:  reading(t0.Add(minute), 160, 750_000_000, 75_000_000),
			check: func(t *testing.T, r Rates) {
				if !near(r.InBps, 100e6) || !near(r.OutBps, 10e6) {
					t.Errorf("bps in %v out %v", r.InBps, r.OutBps)
				}
				if r.InUtilPct == nil || !near(*r.InUtilPct, 10) || !near(*r.OutUtilPct, 1) {
					t.Errorf("util %v %v", r.InUtilPct, r.OutUtilPct)
				}
			},
		},
		{
			name: "32-bit counter wrap is a rate, not a spike",
			prev: &Reading{At: t0, UptimeSeconds: 100, InOctets: 4_294_000_000, OutOctets: 10, SpeedBps: 1_000_000_000},
			cur:  Reading{At: t0.Add(minute), UptimeSeconds: 160, InOctets: 1_000_000, OutOctets: 10, SpeedBps: 1_000_000_000},
			check: func(t *testing.T, r Rates) {
				want := float64(1_000_000+(1<<32)-4_294_000_000) * 8 / 60
				if !near(r.InBps, want) {
					t.Errorf("wrapped in bps %v, want %v", r.InBps, want)
				}
			},
		},
		{
			name:     "32-bit drop too big to be a wrap on a 10 Mb/s link is a reset",
			prev:     &Reading{At: t0, UptimeSeconds: 100, InOctets: 4_000_000_000, SpeedBps: 10_000_000},
			cur:      Reading{At: t0.Add(minute), UptimeSeconds: 160, InOctets: 10, SpeedBps: 10_000_000},
			wantSkip: SkipReset,
		},
		{name: "64-bit counter going down is a reset", prev: ptr(reading(t0, 100, 5000, 0)), cur: reading(t0.Add(minute), 160, 10, 0), wantSkip: SkipReset},
		{name: "device restarted", prev: ptr(reading(t0, 100_000, 0, 0)), cur: reading(t0.Add(minute), 30, 10, 10), wantSkip: SkipReboot},
		{name: "interval far too short", prev: ptr(reading(t0, 100, 0, 0)), cur: reading(t0.Add(20*time.Second), 120, 1, 1), wantSkip: SkipInterval},
		{name: "interval far too long", prev: ptr(reading(t0, 100, 0, 0)), cur: reading(t0.Add(200*time.Second), 300, 1, 1), wantSkip: SkipInterval},
		{
			name:     "counter width changed",
			prev:     ptr(reading(t0, 100, 0, 0)),
			cur:      Reading{At: t0.Add(minute), UptimeSeconds: 160, HC: false, SpeedBps: 1_000_000_000},
			wantSkip: SkipCounterType,
		},
		{
			name:     "more traffic than the link can carry is discarded",
			prev:     ptr(reading(t0, 100, 0, 0)),
			cur:      reading(t0.Add(minute), 160, 1_000_000_000_000, 0),
			wantSkip: SkipReset,
		},
		{
			name: "speed unknown: rates without utilisation",
			prev: &Reading{At: t0, UptimeSeconds: 100, HC: true},
			cur:  Reading{At: t0.Add(minute), UptimeSeconds: 160, HC: true, InOctets: 7_500_000},
			check: func(t *testing.T, r Rates) {
				if !near(r.InBps, 1e6) || r.InUtilPct != nil {
					t.Errorf("in %v util %v", r.InBps, r.InUtilPct)
				}
			},
		},
		{
			name: "errors and discards per minute",
			prev: &Reading{At: t0, UptimeSeconds: 100, HC: true, SpeedBps: 1e9, InErrors: 5, OutErrors: 0, InDiscards: 100, OutDiscards: 0},
			cur:  Reading{At: t0.Add(2 * minute), UptimeSeconds: 220, HC: true, SpeedBps: 1e9, InErrors: 605, OutErrors: 20, InDiscards: 100, OutDiscards: 4},
			check: func(t *testing.T, r Rates) {
				if !near(r.InErrorsPM, 300) || !near(r.OutErrorsPM, 10) || r.InDiscardsPM != 0 || !near(r.OutDiscardsPM, 2) {
					t.Errorf("%+v", r)
				}
			},
		},
		{
			name:     "unknown uptime does not block a normal reading",
			prev:     &Reading{At: t0, UptimeSeconds: -1, HC: true, SpeedBps: 1e9},
			cur:      Reading{At: t0.Add(minute), UptimeSeconds: -1, HC: true, SpeedBps: 1e9, InOctets: 60},
			wantSkip: SkipNone,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, skip := ComputeRates(c.prev, c.cur, minute)
			if skip != c.wantSkip {
				t.Fatalf("skip = %q, want %q", skip, c.wantSkip)
			}
			if c.check != nil {
				c.check(t, r)
			}
		})
	}
}

func ptr(r Reading) *Reading { return &r }
