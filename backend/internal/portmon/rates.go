// Package portmon is the pure logic behind port monitoring: turning counter
// readings into rates, deciding which interfaces are ports, laying out the
// faceplate, and tracking each port's events and conditions. Nothing here
// touches the network or the database.
package portmon

import "time"

// Reading is one stats poll of one interface.
type Reading struct {
	At time.Time
	// UptimeSeconds is the device's sysUpTime when read; < 0 when unknown.
	UptimeSeconds int64
	// HC: the octet counters are the 64-bit ifHC* ones.
	HC                              bool
	InOctets, OutOctets             uint64
	InErrors, OutErrors             uint64
	InDiscards, OutDiscards         uint64
	SpeedBps                        int64
}

// Rates are derived from two consecutive readings.
type Rates struct {
	InBps, OutBps float64
	// Nil when the link speed is unknown (0).
	InUtilPct, OutUtilPct       *float64
	InErrorsPM, OutErrorsPM     float64
	InDiscardsPM, OutDiscardsPM float64
}

// Skip says why no rates were produced; SkipNone means they were.
type Skip string

const (
	SkipNone        Skip = ""
	SkipFirst       Skip = "first reading"
	SkipReboot      Skip = "device restarted"
	SkipReset       Skip = "counter went backwards or jumped implausibly"
	SkipInterval    Skip = "interval out of range"
	SkipCounterType Skip = "counter width changed"
)

const two32 = uint64(1) << 32

// unknownSpeedCeilingBps bounds a plausible rate when the link speed is not
// known: nothing Sentinel polls moves more than 10 Gb/s on one unknown port,
// and a counter reset read as a wrap would claim far more.
const unknownSpeedCeilingBps = 10e9

// ComputeRates turns two readings into rates. It refuses (returns a Skip)
// rather than guess whenever the pair cannot be trusted, because a single
// bogus terabit sample ruins a chart's scale and a report's 95th percentile:
// the device restarted (sysUpTime went backwards), the counters went down
// other than by a plausible 32-bit wrap, the delta is faster than the link
// can carry, the interval is outside 0.5x-3x the poll interval, or the counter
// width changed between readings.
func ComputeRates(prev *Reading, cur Reading, interval time.Duration) (Rates, Skip) {
	if prev == nil {
		return Rates{}, SkipFirst
	}
	dt := cur.At.Sub(prev.At)
	if dt <= 0 || (interval > 0 && (dt < interval/2 || dt > 3*interval)) {
		return Rates{}, SkipInterval
	}
	if prev.UptimeSeconds >= 0 && cur.UptimeSeconds >= 0 && cur.UptimeSeconds < prev.UptimeSeconds {
		return Rates{}, SkipReboot
	}
	if prev.HC != cur.HC {
		return Rates{}, SkipCounterType
	}
	secs := dt.Seconds()
	ceiling := unknownSpeedCeilingBps
	if cur.SpeedBps > 0 {
		ceiling = float64(cur.SpeedBps) * 1.1
	}
	maxOctets := ceiling / 8 * secs

	in, okIn := octetDelta(prev.InOctets, cur.InOctets, cur.HC, maxOctets)
	out, okOut := octetDelta(prev.OutOctets, cur.OutOctets, cur.HC, maxOctets)
	if !okIn || !okOut {
		return Rates{}, SkipReset
	}
	var deltas [4]uint64
	for i, p := range [4][2]uint64{
		{prev.InErrors, cur.InErrors}, {prev.OutErrors, cur.OutErrors},
		{prev.InDiscards, cur.InDiscards}, {prev.OutDiscards, cur.OutDiscards},
	} {
		d, ok := counter32Delta(p[0], p[1])
		if !ok {
			return Rates{}, SkipReset
		}
		deltas[i] = d
	}

	perMin := 60 / secs
	r := Rates{
		InBps:         float64(in) * 8 / secs,
		OutBps:        float64(out) * 8 / secs,
		InErrorsPM:    float64(deltas[0]) * perMin,
		OutErrorsPM:   float64(deltas[1]) * perMin,
		InDiscardsPM:  float64(deltas[2]) * perMin,
		OutDiscardsPM: float64(deltas[3]) * perMin,
	}
	if cur.SpeedBps > 0 {
		inPct := r.InBps / float64(cur.SpeedBps) * 100
		outPct := r.OutBps / float64(cur.SpeedBps) * 100
		r.InUtilPct, r.OutUtilPct = &inPct, &outPct
	}
	return r, SkipNone
}

// octetDelta is the octets moved between two readings. A 64-bit counter never
// wraps in practice, so going down means a reset; a 32-bit one going down is a
// wrap only when the wrapped delta is plausible for the link.
func octetDelta(prev, cur uint64, hc bool, maxOctets float64) (uint64, bool) {
	if cur >= prev {
		d := cur - prev
		return d, float64(d) <= maxOctets
	}
	if hc {
		return 0, false
	}
	d := cur + two32 - prev
	return d, float64(d) <= maxOctets
}

// counter32Delta handles the error and discard counters (always Counter32).
// A wrap is accepted when the wrapped delta is under half the counter's range;
// anything larger is a reset.
func counter32Delta(prev, cur uint64) (uint64, bool) {
	if cur >= prev {
		return cur - prev, true
	}
	d := cur + two32 - prev
	return d, d < two32/2
}
