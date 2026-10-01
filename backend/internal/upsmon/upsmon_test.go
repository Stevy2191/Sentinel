package upsmon

import (
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }
func i64(v int64) *int64   { return &v }

var th = Thresholds{LowBatteryPct: 25, HighLoadPct: 80}
var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func mains() Readings {
	return Readings{BatteryStatus: BatteryNormal, OutputSource: 3, ChargePct: f(100), RuntimeMin: f(40), LoadPct: f(30)}
}

func only(t *testing.T, ch []Change, c Condition, started bool) Change {
	t.Helper()
	if len(ch) != 1 || ch[0].Condition != c || ch[0].Started != started {
		t.Fatalf("changes %+v, want one %s started=%v", ch, c, started)
	}
	return ch[0]
}

func TestOnBatteryStartsAndClears(t *testing.T) {
	s, ch := Evaluate(State{}, mains(), th, t0)
	if len(ch) != 0 {
		t.Fatalf("mains: %+v", ch)
	}
	r := mains()
	r.OutputSource, r.ChargePct = SourceBattery, f(96)
	s, ch = Evaluate(s, r, th, t0.Add(time.Minute))
	c := only(t, ch, OnBattery, true)
	if c.Detail["charge_pct"] != 96 || c.Detail["runtime_min"] != 40 {
		t.Errorf("detail %v", c.Detail)
	}
	if _, ok := s.Active[OnBattery]; !ok {
		t.Fatal("not active")
	}
	_, ch = Evaluate(s, mains(), th, t0.Add(2*time.Minute))
	only(t, ch, OnBattery, false)
}

// No output source: seconds on battery decides; neither reported leaves it.
func TestOnBatteryFallbackAndUnknown(t *testing.T) {
	r := Readings{SecondsOnBattery: i64(30)}
	if on, known := r.OnBattery(); !on || !known {
		t.Errorf("fallback: %v %v", on, known)
	}
	if _, known := (Readings{}).OnBattery(); known {
		t.Error("nothing reported counted as known")
	}
	s := Restore(map[Condition]time.Time{OnBattery: t0})
	s, ch := Evaluate(s, Readings{LoadPct: f(30)}, th, t0.Add(time.Minute))
	if len(ch) != 0 || s.Active[OnBattery] != t0 {
		t.Errorf("unknown source changed state: %+v %+v", ch, s)
	}
}

func TestLowBatteryByChargeAndByStatus(t *testing.T) {
	r := mains()
	r.ChargePct = f(22)
	s, ch := Evaluate(State{}, r, th, t0)
	only(t, ch, LowBattery, true)
	r.ChargePct = f(24.9)
	if _, ch = Evaluate(s, r, th, t0.Add(time.Minute)); len(ch) != 0 {
		t.Errorf("still low: %+v", ch)
	}
	r.ChargePct = f(25)
	_, ch = Evaluate(s, r, th, t0.Add(2*time.Minute))
	only(t, ch, LowBattery, false)

	// Status alone (no charge reported), e.g. an Eaton card.
	st := Readings{BatteryStatus: BatteryLow, OutputSource: 3}
	s, ch = Evaluate(State{}, st, th, t0)
	only(t, ch, LowBattery, true)
	st.BatteryStatus = BatteryNormal
	_, ch = Evaluate(s, st, th, t0.Add(time.Minute))
	only(t, ch, LowBattery, false)

	// Depleted counts as low even with charge above the threshold.
	d := mains()
	d.BatteryStatus = BatteryDepleted
	_, ch = Evaluate(State{}, d, th, t0)
	only(t, ch, LowBattery, true)
}

func TestLowBatteryUnknownKeepsState(t *testing.T) {
	s := Restore(map[Condition]time.Time{LowBattery: t0})
	s, ch := Evaluate(s, Readings{OutputSource: 3}, th, t0.Add(time.Minute))
	if len(ch) != 0 || s.Active[LowBattery] != t0 {
		t.Errorf("no battery readings changed state: %+v", ch)
	}
}

func TestHighLoadNeedsFiveMinutes(t *testing.T) {
	r := mains()
	r.LoadPct = f(86)
	s := State{}
	var ch []Change
	for m := 0; m < 5; m++ {
		s, ch = Evaluate(s, r, th, t0.Add(time.Duration(m)*time.Minute))
		if len(ch) != 0 {
			t.Fatalf("minute %d: started early %+v", m, ch)
		}
	}
	s, ch = Evaluate(s, r, th, t0.Add(5*time.Minute))
	c := only(t, ch, HighLoad, true)
	if c.Detail["load_pct"] != 86 || c.Detail["threshold_pct"] != 80 {
		t.Errorf("detail %v", c.Detail)
	}
	r.LoadPct = f(64)
	s, ch = Evaluate(s, r, th, t0.Add(6*time.Minute))
	only(t, ch, HighLoad, false)
	if s.OverLoadSince != nil {
		t.Error("clock not reset")
	}
}

func TestHighLoadDipResetsClock(t *testing.T) {
	hi, lo := mains(), mains()
	hi.LoadPct, lo.LoadPct = f(90), f(70)
	s, _ := Evaluate(State{}, hi, th, t0)
	s, _ = Evaluate(s, hi, th, t0.Add(4*time.Minute))
	s, _ = Evaluate(s, lo, th, t0.Add(5*time.Minute))
	s, ch := Evaluate(s, hi, th, t0.Add(6*time.Minute))
	if len(ch) != 0 {
		t.Fatalf("dip did not reset: %+v", ch)
	}
	_, ch = Evaluate(s, hi, th, t0.Add(11*time.Minute))
	only(t, ch, HighLoad, true)
}

func TestHighLoadMissingLoadKeepsClockAndState(t *testing.T) {
	hi := mains()
	hi.LoadPct = f(90)
	s, _ := Evaluate(State{}, hi, th, t0)
	s, ch := Evaluate(s, Readings{OutputSource: 3}, th, t0.Add(2*time.Minute))
	if len(ch) != 0 || s.OverLoadSince == nil || !s.OverLoadSince.Equal(t0) {
		t.Fatalf("missing load reset the clock: %+v %+v", ch, s)
	}
	_, ch = Evaluate(s, hi, th, t0.Add(5*time.Minute))
	only(t, ch, HighLoad, true)
}

// Restarting mid-outage: Restore seeds Active, so the next poll on battery
// is not a new start, and the return to mains still clears it.
func TestRestoreNoDuplicateStart(t *testing.T) {
	s := Restore(map[Condition]time.Time{OnBattery: t0})
	r := mains()
	r.OutputSource = SourceBattery
	s, ch := Evaluate(s, r, th, t0.Add(10*time.Minute))
	if len(ch) != 0 {
		t.Fatalf("restored outage re-started: %+v", ch)
	}
	_, ch = Evaluate(s, mains(), th, t0.Add(11*time.Minute))
	only(t, ch, OnBattery, false)
}

// Evaluate must not mutate the State it was given.
func TestEvaluateDoesNotMutatePrev(t *testing.T) {
	prev := State{}
	r := mains()
	r.OutputSource = SourceBattery
	_, _ = Evaluate(prev, r, th, t0)
	if len(prev.Active) != 0 {
		t.Error("prev mutated")
	}
}

func TestAny(t *testing.T) {
	if (Readings{}).Any() {
		t.Error("empty readings reported Any")
	}
	if !(Readings{BatteryTempC: f(-5)}).Any() {
		t.Error("temperature alone not Any")
	}
}
