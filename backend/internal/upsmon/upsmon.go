// Package upsmon decides a UPS's alert conditions from its UPS-MIB readings.
// It is pure: the caller keeps State between polls and acts on the Changes.
package upsmon

import "time"

// Condition is one UPS alert; the values are the incidents.condition strings.
type Condition string

const (
	OnBattery  Condition = "ups_on_battery"
	LowBattery Condition = "ups_low_battery"
	HighLoad   Condition = "ups_high_load"
)

// Conditions lists every condition, in display order.
var Conditions = []Condition{OnBattery, LowBattery, HighLoad}

// HighLoadHold is how long load must stay at or above the threshold before
// the high-load condition starts, so a brief spike does not alert.
const HighLoadHold = 5 * time.Minute

// upsBatteryStatus values (RFC 1628).
const (
	BatteryUnknown  = 1
	BatteryNormal   = 2
	BatteryLow      = 3
	BatteryDepleted = 4
)

// SourceBattery is upsOutputSource's "battery" value (RFC 1628).
const SourceBattery = 5

// Readings is one poll's UPS-MIB values. A nil pointer, or 0 for the two
// enumerations, means the UPS did not report it.
type Readings struct {
	BatteryStatus    int
	SecondsOnBattery *int64
	RuntimeMin       *float64
	ChargePct        *float64
	BatteryTempC     *float64
	InputV           *float64
	OutputSource     int
	OutputV          *float64
	LoadPct          *float64
}

// OnBattery reports whether the UPS is running from battery: by its output
// source, else by seconds on battery. known is false when it reports neither.
func (r Readings) OnBattery() (onBattery, known bool) {
	if r.OutputSource != 0 {
		return r.OutputSource == SourceBattery, true
	}
	if r.SecondsOnBattery != nil {
		return *r.SecondsOnBattery > 0, true
	}
	return false, false
}

// Any reports whether the UPS answered at least one reading.
func (r Readings) Any() bool {
	return r.BatteryStatus != 0 || r.OutputSource != 0 || r.SecondsOnBattery != nil || r.RuntimeMin != nil ||
		r.ChargePct != nil || r.BatteryTempC != nil || r.InputV != nil || r.OutputV != nil || r.LoadPct != nil
}

// Thresholds are the percentages the conditions use.
type Thresholds struct {
	LowBatteryPct float64
	HighLoadPct   float64
}

// State is what is remembered between polls: each active condition and when
// it started, and when load first went over the threshold (nil when it is
// not over).
type State struct {
	Active        map[Condition]time.Time
	OverLoadSince *time.Time
}

// Change is a condition starting or ending. Detail carries the figures for
// the alert sentence: charge_pct, runtime_min, load_pct, threshold_pct, each
// only when known.
type Change struct {
	Condition Condition
	Started   bool
	At        time.Time
	Detail    map[string]float64
}

// Restore rebuilds State from the conditions that have an open incident, so
// a restart does not alert again for what is already open.
func Restore(open map[Condition]time.Time) State {
	s := State{Active: map[Condition]time.Time{}}
	for c, at := range open {
		s.Active[c] = at
	}
	return s
}

// Evaluate applies one poll's readings. A condition whose inputs are all
// missing keeps its previous state. prev is not modified.
func Evaluate(prev State, r Readings, th Thresholds, now time.Time) (State, []Change) {
	next := State{Active: map[Condition]time.Time{}, OverLoadSince: prev.OverLoadSince}
	for c, at := range prev.Active {
		next.Active[c] = at
	}
	detail := func(extra map[string]float64) map[string]float64 {
		d := map[string]float64{}
		if r.ChargePct != nil {
			d["charge_pct"] = *r.ChargePct
		}
		if r.RuntimeMin != nil {
			d["runtime_min"] = *r.RuntimeMin
		}
		if r.LoadPct != nil {
			d["load_pct"] = *r.LoadPct
		}
		for k, v := range extra {
			d[k] = v
		}
		return d
	}
	var changes []Change
	set := func(c Condition, active bool, extra map[string]float64) {
		_, was := next.Active[c]
		switch {
		case active && !was:
			next.Active[c] = now
			changes = append(changes, Change{Condition: c, Started: true, At: now, Detail: detail(extra)})
		case !active && was:
			delete(next.Active, c)
			changes = append(changes, Change{Condition: c, Started: false, At: now, Detail: detail(extra)})
		}
	}

	if on, known := r.OnBattery(); known {
		set(OnBattery, on, nil)
	}

	if r.BatteryStatus != 0 || r.ChargePct != nil {
		statusLow := r.BatteryStatus == BatteryLow || r.BatteryStatus == BatteryDepleted
		chargeLow := r.ChargePct != nil && *r.ChargePct < th.LowBatteryPct
		set(LowBattery, statusLow || chargeLow, map[string]float64{"threshold_pct": th.LowBatteryPct})
	}

	if r.LoadPct != nil {
		limit := map[string]float64{"threshold_pct": th.HighLoadPct}
		if *r.LoadPct >= th.HighLoadPct {
			if next.OverLoadSince == nil {
				since := now
				next.OverLoadSince = &since
			}
			if now.Sub(*next.OverLoadSince) >= HighLoadHold {
				set(HighLoad, true, limit)
			}
		} else {
			next.OverLoadSince = nil
			set(HighLoad, false, limit)
		}
	}
	return next, changes
}
