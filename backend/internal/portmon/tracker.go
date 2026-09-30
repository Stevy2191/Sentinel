package portmon

import (
	"math"
	"time"
)

// Condition is a port problem that has a start and an end.
type Condition string

const (
	LinkDown  Condition = "link_down"
	Errors    Condition = "errors"
	Flapping  Condition = "flapping"
	SlowLink  Condition = "slow_link"
	Saturated Condition = "saturated"
)

// WarningConditions turn a port yellow. LinkDown is shown separately: red for
// an important port, grey otherwise.
var WarningConditions = []Condition{Errors, Flapping, SlowLink, Saturated}

const (
	// Window is how many polls the errors and saturation rules look at.
	Window = 5
	// FlapWindow and FlapThreshold: this many link transitions within the
	// window is flapping; the window passing with none ends it.
	FlapWindow    = 10 * time.Minute
	FlapThreshold = 3
	// SaturationHysteresis: nearly-full ends this many points below the
	// threshold, so a port hovering at the line does not flap the condition.
	SaturationHysteresis = 10.0
)

// Thresholds are one port's limits (its overrides, else the defaults).
type Thresholds struct {
	ErrorsPerMin float64
	UtilPct      float64
	DownGrace    time.Duration
}

// Observation is one stats poll of one port.
type Observation struct {
	At      time.Time
	OperUp  bool
	AdminUp bool
	// SpeedBps is 0 when unknown.
	SpeedBps int64
	// LastChangeSeconds is ifLastChange in seconds of device uptime; < 0 when
	// unknown or not comparable (the device restarted).
	LastChangeSeconds int64
	// HaveRates: ErrorsPerMin and UtilPct come from a valid rate interval.
	HaveRates    bool
	ErrorsPerMin float64 // errors + discards, in + out
	UtilPct      float64 // max of in and out; < 0 when unknown
}

// Event is a point-in-time log entry: link_up, link_down, admin_up,
// admin_down, speed_change.
type Event struct {
	Kind   string
	At     time.Time
	Detail map[string]any
}

// Change is a condition starting or ending.
type Change struct {
	Condition Condition
	Started   bool
	At        time.Time
	Detail    map[string]any
}

// Result is what one observation produced.
type Result struct {
	Events  []Event
	Changes []Change
}

// Snapshot is what a tracker is restored from (the stored interface row).
type Snapshot struct {
	OperUp, AdminUp   bool
	SpeedBps          int64
	LastChangeSeconds int64
	OperChangedAt     time.Time
	Active            map[Condition]time.Time
}

// Tracker follows one port across polls. It is not safe for concurrent use;
// the poller never polls one device twice at once.
type Tracker struct {
	operUp, adminUp bool
	speed           int64
	lastChange      int64
	operChangedAt   time.Time
	transitions     []time.Time
	flapCount       int
	// flapRestored: Flapping was active in the Snapshot this tracker was
	// restored from, so its transition history (and true total count) is
	// unknown. It stays true until the flap actually ends.
	flapRestored bool
	// flapSeeded: the first Observe since such a restore has planted a
	// quiet-window anchor in transitions (see Observe); only happens once.
	flapSeeded bool
	errWindow  []float64
	errBelow   int
	utilWindow []float64
	active     map[Condition]time.Time
}

// NewTracker restores a tracker. Active conditions carry over with their
// start times; the rolling windows start empty, and since a condition only
// ends on evidence (5 quiet polls, a quiet flap window), a restart never ends
// or re-opens one. Flapping is the exception that proves the rule: with no
// transition history to judge quiet time from, a restored flap measures its
// 10-minute quiet window from the first Observe call after the restore
// (rather than ending on it for lack of any transitions), and when it does
// end that way its Change detail omits "transitions" since the true count
// predates the restore.
func NewTracker(s Snapshot) *Tracker {
	active := make(map[Condition]time.Time, len(s.Active))
	for c, at := range s.Active {
		active[c] = at
	}
	_, flapRestored := s.Active[Flapping]
	return &Tracker{
		operUp: s.OperUp, adminUp: s.AdminUp, speed: s.SpeedBps,
		lastChange: s.LastChangeSeconds, operChangedAt: s.OperChangedAt, active: active,
		flapRestored: flapRestored,
	}
}

// Active returns a copy of the active conditions and when each started.
func (t *Tracker) Active() map[Condition]time.Time {
	out := make(map[Condition]time.Time, len(t.active))
	for c, at := range t.active {
		out[c] = at
	}
	return out
}

// OperChangedAt is when the link last went up or down.
func (t *Tracker) OperChangedAt() time.Time { return t.operChangedAt }

func (t *Tracker) isActive(c Condition) bool {
	_, ok := t.active[c]
	return ok
}

// Observe applies one poll and returns the events and condition changes it
// caused.
func (t *Tracker) Observe(o Observation, th Thresholds, usualSpeed int64) Result {
	var r Result
	event := func(kind string, detail map[string]any) {
		r.Events = append(r.Events, Event{Kind: kind, At: o.At, Detail: detail})
	}
	start := func(c Condition, detail map[string]any) {
		if t.isActive(c) {
			return
		}
		t.active[c] = o.At
		r.Changes = append(r.Changes, Change{Condition: c, Started: true, At: o.At, Detail: detail})
	}
	end := func(c Condition, detail map[string]any) {
		if !t.isActive(c) {
			return
		}
		delete(t.active, c)
		r.Changes = append(r.Changes, Change{Condition: c, Started: false, At: o.At, Detail: detail})
	}

	if o.AdminUp != t.adminUp {
		if o.AdminUp {
			event("admin_up", nil)
		} else {
			event("admin_down", nil)
		}
	}

	// Link transitions. While flapping, individual transitions are counted
	// into the flap, not logged one by one.
	flapping := t.isActive(Flapping)
	transitions := 0
	switch {
	case o.OperUp != t.operUp:
		transitions = 1
		t.operChangedAt = o.At
		if !flapping {
			if o.OperUp {
				event("link_up", map[string]any{"speed_bps": o.SpeedBps})
			} else {
				event("link_down", nil)
			}
		}
	case o.LastChangeSeconds >= 0 && t.lastChange >= 0 && o.LastChangeSeconds != t.lastChange:
		// Same state as last poll, but ifLastChange moved: it went down and
		// came back (or up and down) between polls.
		transitions = 2
		if !flapping {
			between := map[string]any{"between_polls": true}
			if o.OperUp {
				event("link_down", between)
				event("link_up", between)
			} else {
				event("link_up", between)
				event("link_down", between)
			}
		}
	}
	for i := 0; i < transitions; i++ {
		t.transitions = append(t.transitions, o.At)
	}
	if flapping {
		t.flapCount += transitions
	}
	cutoff := o.At.Add(-FlapWindow)
	kept := t.transitions[:0]
	for _, at := range t.transitions {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	t.transitions = kept
	switch {
	case !flapping && len(t.transitions) >= FlapThreshold:
		t.flapCount = len(t.transitions)
		start(Flapping, map[string]any{"transitions": t.flapCount})
	case flapping && len(t.transitions) == 0 && t.flapRestored && !t.flapSeeded:
		// First Observe since a restore with Flapping active: there is no
		// transition history to prune against, so plant this poll as the
		// start of the quiet window instead of ending the flap for lack of
		// one. A real transition on this same poll would have already left
		// transitions non-empty above, skipping this case as intended.
		t.transitions = append(t.transitions, o.At)
		t.flapSeeded = true
	case flapping && len(t.transitions) == 0:
		detail := map[string]any{"transitions": t.flapCount}
		if t.flapRestored {
			// The count predates the restore; we never knew the real total.
			detail = nil
		}
		end(Flapping, detail)
		t.flapCount = 0
		t.flapRestored = false
		t.flapSeeded = false
	}

	if o.OperUp && t.operUp && transitions == 0 && o.SpeedBps > 0 && t.speed > 0 && o.SpeedBps != t.speed {
		event("speed_change", map[string]any{"from_bps": t.speed, "to_bps": o.SpeedBps})
	}

	// Link down, after the grace period. A port first seen already down counts
	// from that first sighting.
	if !o.OperUp && t.operChangedAt.IsZero() {
		t.operChangedAt = o.At
	}
	switch {
	case !o.AdminUp, o.OperUp:
		end(LinkDown, nil)
	case o.At.Sub(t.operChangedAt) >= th.DownGrace:
		start(LinkDown, map[string]any{"down_since": t.operChangedAt})
	}

	if !o.OperUp {
		// No link, no traffic: traffic conditions end, windows reset.
		end(Errors, nil)
		end(Saturated, nil)
		end(SlowLink, nil)
		t.errWindow, t.utilWindow, t.errBelow = nil, nil, 0
	} else {
		if o.HaveRates {
			t.errWindow = push(t.errWindow, o.ErrorsPerMin)
			if t.isActive(Errors) {
				if o.ErrorsPerMin < th.ErrorsPerMin {
					t.errBelow++
				} else {
					t.errBelow = 0
				}
				if t.errBelow >= Window {
					end(Errors, map[string]any{"per_minute": round1(o.ErrorsPerMin)})
					t.errBelow = 0
				}
			} else if len(t.errWindow) == Window && allAtLeast(t.errWindow, th.ErrorsPerMin) {
				start(Errors, map[string]any{"per_minute": round1(maxOf(t.errWindow))})
				t.errBelow = 0
			}
			if o.UtilPct >= 0 {
				t.utilWindow = push(t.utilWindow, o.UtilPct)
				if len(t.utilWindow) == Window {
					avg := mean(t.utilWindow)
					if !t.isActive(Saturated) && avg >= th.UtilPct {
						start(Saturated, map[string]any{"util_pct": round1(avg)})
					} else if t.isActive(Saturated) && avg < th.UtilPct-SaturationHysteresis {
						end(Saturated, map[string]any{"util_pct": round1(avg)})
					}
				}
			}
		}
		if usualSpeed > 0 && o.SpeedBps > 0 {
			if o.SpeedBps < usualSpeed {
				start(SlowLink, map[string]any{"speed_bps": o.SpeedBps, "usual_speed_bps": usualSpeed})
			} else {
				end(SlowLink, map[string]any{"speed_bps": o.SpeedBps})
			}
		}
	}

	t.operUp, t.adminUp = o.OperUp, o.AdminUp
	if o.SpeedBps > 0 {
		t.speed = o.SpeedBps
	}
	t.lastChange = o.LastChangeSeconds
	return r
}

func push(w []float64, v float64) []float64 {
	w = append(w, v)
	if len(w) > Window {
		w = w[len(w)-Window:]
	}
	return w
}

func allAtLeast(w []float64, min float64) bool {
	for _, v := range w {
		if v < min {
			return false
		}
	}
	return true
}

func mean(w []float64) float64 {
	sum := 0.0
	for _, v := range w {
		sum += v
	}
	return sum / float64(len(w))
}

func maxOf(w []float64) float64 {
	m := math.Inf(-1)
	for _, v := range w {
		m = math.Max(m, v)
	}
	return m
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
