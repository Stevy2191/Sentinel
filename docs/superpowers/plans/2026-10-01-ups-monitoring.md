# UPS Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Poll every UPS-typed device's UPS-MIB readings each minute, show them in a Power panel on the device page with history, and alert on battery, low battery and high load.

**Architecture:** A pure package `internal/upsmon` decides conditions from readings (like `portmon`). `snmp.ReadUPS` does one GET of nine UPS-MIB scalars. A new `services.UPSMonitor`, called by `DevicePoller` after each successful poll of an up device, writes samples to the existing metrics store and opens/closes device-level condition incidents (new: `interface_id` NULL, `condition` set). `PortService.UPSStatus` serves the latest readings to a new `GET /devices/:id/ups`, and the frontend draws a Power panel plus two charts through the existing metrics query.

**Tech Stack:** Go 1.26, Gin, GORM, PostgreSQL 16 + TimescaleDB, gosnmp; React/TS/Vite/Tailwind/Recharts.

**Spec:** `docs/superpowers/specs/2026-10-01-ups-monitoring-design.md`

## Global Constraints

- Work only in the worktree `/home/sysadmin/sentinel-ups` (branch `feature/ups-monitoring`). The live checkout `/home/sysadmin/sentinel` stays on `main`; never check out another branch there.
- The new migration is `backend/migrations/055_ups_monitoring.sql` (054 is the latest on dev). Never edit an applied migration. New timestamp columns are TIMESTAMPTZ (none are needed here).
- Condition strings, exactly: `ups_on_battery`, `ups_low_battery`, `ups_high_load`.
- Metric keys, exactly: `ups_charge_pct`, `ups_runtime_min`, `ups_load_pct`, `ups_input_v`, `ups_output_v`, `ups_battery_temp_c`, `ups_on_battery`, `ups_battery_status`. All samples use instance `""` and no interface.
- Settings: `ups_low_battery_pct` default 25, allowed 5–95; `ups_high_load_pct` default 80, allowed 10–100. Per-device override columns `devices.ups_low_battery_pct`, `devices.ups_high_load_pct` (NULL = the setting). The high-load hold is 5 minutes (a constant).
- UPS incidents never count against device availability and are never touched by the device-down open/close.
- Alert wording (display name first): "X is on battery (charge 96 %, 41 min left)" / "X is back on mains power"; "X battery is low (charge 22 %, 9 min left)" / "X battery has recovered (charge 30 %)"; "X load is high (86 %, threshold 80 %)" / "X load is back to normal (64 %)". A missing figure drops its clause (e.g. "X is on battery").
- Incident subject: "Device · On battery" / "· Low battery" / "· High load". Incident page "Detected by": "The UPS poll: On battery" (etc.).
- Go: `gofmt`, `go vet ./...` clean. DB tests: `cd backend && ./scripts/test-db.sh -run <Name> -v` (the `make test-db` target does not pass args). Full DB suite: `./scripts/test-db.sh`.
- Frontend check (run from the worktree; node_modules must be created as uid 1000): `docker run --rm --user 1000:1000 -e HOME=/tmp -v /home/sysadmin/sentinel-ups/frontend:/app -w /app node:20-alpine sh -c "npm ci --no-audit --no-fund --silent && npx tsc --noEmit && npx eslint src --quiet && npx vite build --logLevel error"` → exit 0.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Do not push or merge.

## Review Focus

1. **A UPS with no network ports** (the user's Tripp Lite reports no ifTable): the UPS poll must still run, though `PortMonitor.PollStats` returns early with no rows — pinned by the DevicePoller test in Task 5.
2. **A UPS that goes unreachable while on battery** (common in a real outage): the device-down incident opens alongside the open on-battery incident, and the device coming back closes only the device-down one — pinned by the DB test in Task 3.
3. **A UPS that reports battery status but no charge** (some Eaton cards): low battery must still start from status 3/4, and clear when status returns to normal — pinned in Task 1.
4. **Sentinel restarting mid-outage**: no second "on battery" alert, and the recovery still arrives — pinned in Task 1 (Restore) and Task 5 (monitor rebuilds from open incidents).
5. **Negative battery temperature, and an agent answering only some OIDs**: −5 °C is stored as −5, and noSuchObject readings are absent (not zero) — pinned in Task 2.

---

### Task 1: `upsmon` — the condition rules

**Files:**
- Create: `backend/internal/upsmon/upsmon.go`
- Test: `backend/internal/upsmon/upsmon_test.go`

**Interfaces:**
- Produces:
  - `type Condition string`; consts `OnBattery Condition = "ups_on_battery"`, `LowBattery = "ups_low_battery"`, `HighLoad = "ups_high_load"`; `var Conditions = []Condition{OnBattery, LowBattery, HighLoad}`
  - `const HighLoadHold = 5 * time.Minute`
  - battery status consts `BatteryUnknown=1, BatteryNormal=2, BatteryLow=3, BatteryDepleted=4`; output source const `SourceBattery = 5`
  - `type Readings struct { BatteryStatus int; SecondsOnBattery *int64; RuntimeMin, ChargePct, BatteryTempC, InputV, OutputV, LoadPct *float64; OutputSource int }` (0 = not reported for the two ints)
  - `func (r Readings) OnBattery() (onBattery, known bool)`; `func (r Readings) Any() bool`
  - `type Thresholds struct { LowBatteryPct, HighLoadPct float64 }`
  - `type State struct { Active map[Condition]time.Time; OverLoadSince *time.Time }`
  - `type Change struct { Condition Condition; Started bool; At time.Time; Detail map[string]float64 }`
  - `func Evaluate(prev State, r Readings, th Thresholds, now time.Time) (State, []Change)`
  - `func Restore(open map[Condition]time.Time) State`

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/upsmon/`
Expected: FAIL — package has no non-test Go files / undefined identifiers.

- [ ] **Step 3: Implement**

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && gofmt -l internal/upsmon; go vet ./internal/upsmon/ && go test ./internal/upsmon/ -v 2>&1 | tail -15`
Expected: no gofmt output; all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/upsmon
git commit -m "feat(ups): condition rules for on battery, low battery and high load

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `snmp.ReadUPS` — read the UPS-MIB scalars

**Files:**
- Create: `backend/internal/snmp/ups_readings.go`
- Test: `backend/internal/snmp/ups_readings_test.go`
- Create: `deploy/snmpsim/data/ups.snmprec`
- Modify: `backend/internal/snmp/simulator_test.go` (add `TestSimUPS`)

**Interfaces:**
- Consumes: `upsmon.Readings` (Task 1); existing `snmp.Client`, `snmp.Target`, `PDU`, `Clean`.
- Produces:
  - `var UPSReadingOIDs []string` (the nine OIDs, in the order below)
  - `func ParseUPSReadings(pdus []PDU) upsmon.Readings`
  - `func ReadUPS(ctx context.Context, c Client, t Target) (upsmon.Readings, error)`

OIDs (prefix `1.3.6.1.2.1.33.1`): `.2.1.0` battery status, `.2.2.0` seconds on battery, `.2.3.0` minutes remaining, `.2.4.0` charge %, `.2.7.0` battery temperature °C (INTEGER, may be negative), `.3.3.1.3.1` input volts, `.4.1.0` output source, `.4.4.1.2.1` output volts, `.4.4.1.5.1` output load %.

- [ ] **Step 1: Write the failing tests**

`backend/internal/snmp/ups_readings_test.go`:

```go
package snmp

import (
	"context"
	"errors"
	"testing"
)

func upsPDUs(vals map[string]any) []PDU {
	out := make([]PDU, len(UPSReadingOIDs))
	for i, o := range UPSReadingOIDs {
		out[i] = PDU{OID: o, Value: vals[o]}
	}
	return out
}

func TestParseUPSReadings(t *testing.T) {
	const u = "1.3.6.1.2.1.33.1"
	r := ParseUPSReadings(upsPDUs(map[string]any{
		u + ".2.1.0": int64(2), u + ".2.2.0": int64(0), u + ".2.3.0": int64(41), u + ".2.4.0": int64(96),
		u + ".2.7.0": int64(-5), u + ".3.3.1.3.1": int64(121), u + ".4.1.0": int64(3),
		u + ".4.4.1.2.1": int64(120), u + ".4.4.1.5.1": uint64(34),
	}))
	if r.BatteryStatus != 2 || r.OutputSource != 3 || *r.SecondsOnBattery != 0 || *r.RuntimeMin != 41 ||
		*r.ChargePct != 96 || *r.BatteryTempC != -5 || *r.InputV != 121 || *r.OutputV != 120 || *r.LoadPct != 34 {
		t.Errorf("readings %+v", r)
	}
}

// noSuchObject (nil) readings are absent, never zero.
func TestParseUPSReadingsAbsent(t *testing.T) {
	const u = "1.3.6.1.2.1.33.1"
	r := ParseUPSReadings(upsPDUs(map[string]any{u + ".2.4.0": int64(80)}))
	if r.ChargePct == nil || *r.ChargePct != 80 {
		t.Fatalf("charge %+v", r.ChargePct)
	}
	if r.BatteryStatus != 0 || r.OutputSource != 0 || r.LoadPct != nil || r.BatteryTempC != nil || r.SecondsOnBattery != nil {
		t.Errorf("absent readings filled in: %+v", r)
	}
}

type upsGetFake struct {
	vals  map[string]any
	calls int
	fail  bool // a multi-OID GET fails (v1 noSuchName)
}

func (f *upsGetFake) Get(_ context.Context, _ Target, oids []string) ([]PDU, error) {
	f.calls++
	if f.fail && len(oids) > 1 {
		return nil, errors.New("agent returned NoSuchName")
	}
	out := make([]PDU, 0, len(oids))
	for _, o := range oids {
		v, ok := f.vals[o]
		if !ok && f.fail {
			return nil, errors.New("agent returned NoSuchName")
		}
		out = append(out, PDU{OID: o, Value: v})
	}
	return out, nil
}
func (f *upsGetFake) Walk(context.Context, Target, string) ([]PDU, error) { return nil, nil }

func TestReadUPSOneRequest(t *testing.T) {
	f := &upsGetFake{vals: map[string]any{"1.3.6.1.2.1.33.1.4.4.1.5.1": int64(50)}}
	r, err := ReadUPS(context.Background(), f, Target{Credential: Credential{Version: "2c"}})
	if err != nil || f.calls != 1 || r.LoadPct == nil || *r.LoadPct != 50 {
		t.Fatalf("r %+v err %v calls %d", r, err, f.calls)
	}
}

// v1 fails a whole GET when one OID is missing; each OID is then asked alone.
func TestReadUPSv1FallsBackPerOID(t *testing.T) {
	f := &upsGetFake{fail: true, vals: map[string]any{"1.3.6.1.2.1.33.1.2.4.0": int64(77)}}
	r, err := ReadUPS(context.Background(), f, Target{Credential: Credential{Version: "1"}})
	if err != nil || r.ChargePct == nil || *r.ChargePct != 77 || f.calls != 1+len(UPSReadingOIDs) {
		t.Fatalf("r %+v err %v calls %d", r, err, f.calls)
	}
}

// v2c/v3: a failed GET is an error (timeout), not retried per OID.
func TestReadUPSv2Error(t *testing.T) {
	f := &upsGetFake{fail: true}
	if _, err := ReadUPS(context.Background(), f, Target{Credential: Credential{Version: "2c"}}); err == nil || f.calls != 1 {
		t.Fatalf("err %v calls %d", err, f.calls)
	}
}
```

Check the exact v1 version constant first: `grep -n "SNMPVersion1\|Version ==" backend/internal/snmp/*.go backend/internal/models/snmp.go | head`. Use whatever string `models.SNMPVersion1` holds (the tests above assume `"1"`; change the test's `Version` literal if it differs).

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./internal/snmp/ -run 'UPSReadings|ReadUPS'`
Expected: FAIL — undefined: UPSReadingOIDs / ParseUPSReadings / ReadUPS.

- [ ] **Step 3: Implement** `backend/internal/snmp/ups_readings.go`:

```go
package snmp

import (
	"context"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/upsmon"
)

const oidUPSMIB = "1.3.6.1.2.1.33.1"

// UPSReadingOIDs are the UPS-MIB (RFC 1628) values a UPS poll reads, line
// tables at index 1 only (single-phase).
var UPSReadingOIDs = []string{
	oidUPSMIB + ".2.1.0",     // upsBatteryStatus
	oidUPSMIB + ".2.2.0",     // upsSecondsOnBattery
	oidUPSMIB + ".2.3.0",     // upsEstimatedMinutesRemaining
	oidUPSMIB + ".2.4.0",     // upsEstimatedChargeRemaining (%)
	oidUPSMIB + ".2.7.0",     // upsBatteryTemperature (°C, may be negative)
	oidUPSMIB + ".3.3.1.3.1", // upsInputVoltage, line 1 (V RMS)
	oidUPSMIB + ".4.1.0",     // upsOutputSource
	oidUPSMIB + ".4.4.1.2.1", // upsOutputVoltage, line 1 (V RMS)
	oidUPSMIB + ".4.4.1.5.1", // upsOutputPercentLoad, line 1 (%)
}

// signed returns a numeric value of either sign; ok is false for a missing
// (noSuchObject) or non-numeric value.
func signed(p PDU) (float64, bool) {
	switch v := p.Value.(type) {
	case int64:
		return float64(v), true
	case uint64:
		return float64(v), true
	}
	return 0, false
}

// ParseUPSReadings turns a GET of UPSReadingOIDs into Readings; anything the
// agent did not answer stays absent.
func ParseUPSReadings(pdus []PDU) upsmon.Readings {
	var r upsmon.Readings
	ptr := func(v float64) *float64 { return &v }
	for _, p := range pdus {
		v, ok := signed(p)
		if !ok {
			continue
		}
		switch p.OID {
		case oidUPSMIB + ".2.1.0":
			r.BatteryStatus = int(v)
		case oidUPSMIB + ".2.2.0":
			s := int64(v)
			r.SecondsOnBattery = &s
		case oidUPSMIB + ".2.3.0":
			r.RuntimeMin = ptr(v)
		case oidUPSMIB + ".2.4.0":
			r.ChargePct = ptr(v)
		case oidUPSMIB + ".2.7.0":
			r.BatteryTempC = ptr(v)
		case oidUPSMIB + ".3.3.1.3.1":
			r.InputV = ptr(v)
		case oidUPSMIB + ".4.1.0":
			r.OutputSource = int(v)
		case oidUPSMIB + ".4.4.1.2.1":
			r.OutputV = ptr(v)
		case oidUPSMIB + ".4.4.1.5.1":
			r.LoadPct = ptr(v)
		}
	}
	return r
}

// ReadUPS reads a UPS's readings in one GET. SNMPv1 fails a whole GET when
// any one OID is missing, so on v1 a failed GET is retried one OID at a time
// and whatever answers is kept.
func ReadUPS(ctx context.Context, c Client, t Target) (upsmon.Readings, error) {
	pdus, err := c.Get(ctx, t, UPSReadingOIDs)
	if err == nil {
		return ParseUPSReadings(pdus), nil
	}
	if t.Credential.Version != models.SNMPVersion1 {
		return upsmon.Readings{}, err
	}
	var all []PDU
	for _, o := range UPSReadingOIDs {
		if p, err := c.Get(ctx, t, []string{o}); err == nil {
			all = append(all, p...)
		}
	}
	return ParseUPSReadings(all), nil
}
```

If `snmp` importing `models` creates an import cycle (check with `go build ./...`), compare against the literal the package already uses for v1 (see `grep -n '"1"' backend/internal/snmp/*.go`) instead.

- [ ] **Step 4: Add the simulated UPS** `deploy/snmpsim/data/ups.snmprec` (community `ups`):

```
1.3.6.1.2.1.1.1.0|4|Ubuntu 18.04 Linux 4.4.31 software: PowerAlert 20.2.1 (Build 942)
1.3.6.1.2.1.1.2.0|6|1.3.6.1.4.1.850.1.1.1
1.3.6.1.2.1.1.3.0|67|123456789
1.3.6.1.2.1.1.4.0|4|noc@example.test
1.3.6.1.2.1.1.5.0|4|sim-ups
1.3.6.1.2.1.1.6.0|4|Lab rack
1.3.6.1.2.1.33.1.1.1.0|4|TRIPP LITE
1.3.6.1.2.1.33.1.1.2.0|4|SMART1500RM2UN
1.3.6.1.2.1.33.1.2.1.0|2|2
1.3.6.1.2.1.33.1.2.2.0|2|0
1.3.6.1.2.1.33.1.2.3.0|2|41
1.3.6.1.2.1.33.1.2.4.0|2|96
1.3.6.1.2.1.33.1.2.7.0|2|24
1.3.6.1.2.1.33.1.3.3.1.3.1|2|121
1.3.6.1.2.1.33.1.4.1.0|2|3
1.3.6.1.2.1.33.1.4.4.1.2.1|2|120
1.3.6.1.2.1.33.1.4.4.1.5.1|2|34
```

Add to `backend/internal/snmp/simulator_test.go` (it skips without `SENTINEL_TEST_SNMPSIM`, like its neighbours):

```go
// The simulated Tripp Lite: UPS-MIB readings, and inventory with no ports.
func TestSimUPS(t *testing.T) {
	target := simTarget(t, Credential{Version: "2c", Community: "ups"})
	r, err := ReadUPS(context.Background(), GoSNMPClient{}, target)
	if err != nil {
		t.Fatal(err)
	}
	if r.ChargePct == nil || *r.ChargePct != 96 || r.LoadPct == nil || *r.LoadPct != 34 || r.OutputSource != 3 {
		t.Errorf("readings %+v", r)
	}
	inv, err := ReadInventory(context.Background(), GoSNMPClient{}, target)
	if err != nil || inv.Model != "SMART1500RM2UN" || inv.Vendor != "Tripp Lite" || len(inv.Interfaces) != 0 {
		t.Errorf("inventory %+v (%d interfaces) err %v", inv.System, len(inv.Interfaces), err)
	}
}
```

- [ ] **Step 5: Run tests**

Run: `cd backend && gofmt -l internal/snmp; go vet ./internal/snmp/ && go test ./internal/snmp/ 2>&1 | tail -3`
Expected: `ok`. Then, if the simulator container runs (`docker ps --format '{{.Names}}' | grep snmpsim`), restart it so it loads the new file (`cd deploy/snmpsim && SNMPSIM_VERSION=1.2.2 ./run.sh`) and run `SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 go test ./internal/snmp/ -run Sim -v 2>&1 | tail -5` → all PASS. If the simulator cannot be started, say so in the report; do not skip silently.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/snmp/ups_readings.go backend/internal/snmp/ups_readings_test.go backend/internal/snmp/simulator_test.go deploy/snmpsim/data/ups.snmprec
git commit -m "feat(ups): read UPS-MIB battery, load and power readings

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Device-level condition incidents (migration 055)

**Files:**
- Create: `backend/migrations/055_ups_monitoring.sql`
- Modify: `backend/internal/models/snmp.go` (UPS condition consts, device override fields, `DeviceDetailsPatch`), `backend/internal/models/setting.go` (UPS setting keys and limits)
- Modify: `backend/internal/services/incident_service.go` (device-down queries + new device-condition functions + subject name)
- Modify: `backend/internal/services/device_service.go:84` (availability join)
- Modify: `backend/internal/api/device_handler.go` (`deviceDetailsAudit`)
- Test: `backend/internal/services/ups_incidents_db_test.go`, `backend/internal/models/snmp_test.go`

**Interfaces:**
- Consumes: none from earlier tasks (the condition strings equal Task 1's).
- Produces:
  - `models.UPSConditionOnBattery = "ups_on_battery"`, `models.UPSConditionLowBattery = "ups_low_battery"`, `models.UPSConditionHighLoad = "ups_high_load"`
  - `models.Device.UPSLowBatteryPct *int` (`json:"ups_low_battery_pct"`), `models.Device.UPSHighLoadPct *int` (`json:"ups_high_load_pct"`)
  - `models.DeviceDetailsPatch.UPSLowBatteryPct Opt[int]`, `.UPSHighLoadPct Opt[int]`
  - `models.SettingUPSLowBatteryPct = "ups_low_battery_pct"`, `DefaultUPSLowBatteryPct = 25`, `MinUPSLowBatteryPct = 5`, `MaxUPSLowBatteryPct = 95`; `models.SettingUPSHighLoadPct = "ups_high_load_pct"`, `DefaultUPSHighLoadPct = 80`, `MinUPSHighLoadPct = 10`, `MaxUPSHighLoadPct = 100`
  - `func (s *IncidentService) OpenDeviceConditionIncident(ctx context.Context, deviceID uuid.UUID, condition string, start time.Time, reason string) (*models.Incident, bool, error)`
  - `func (s *IncidentService) CloseDeviceConditionIncident(ctx context.Context, deviceID uuid.UUID, condition string, end time.Time, note string) (*models.Incident, error)`
  - `func (s *IncidentService) OpenDeviceConditionIncidents(ctx context.Context, deviceID uuid.UUID) ([]models.Incident, error)`

- [ ] **Step 1: Write the failing tests**

`backend/internal/services/ups_incidents_db_test.go` (uses the existing `seedDevice` and `testdb` helpers, as `device_service_db_test.go` does):

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBDeviceConditionIncidentOpenCloseIdempotent(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	inc := NewIncidentService(db)
	start := time.Now().UTC().Add(-time.Minute)

	a, opened, err := inc.OpenDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, start, "on battery")
	if err != nil || !opened || a.Condition == nil || *a.Condition != "ups_on_battery" || a.InterfaceID != nil {
		t.Fatalf("open: %+v %v %v", a, opened, err)
	}
	b, opened, err := inc.OpenDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, start, "again")
	if err != nil || opened || b.ID != a.ID {
		t.Fatalf("second open: %+v %v %v", b, opened, err)
	}
	open, err := inc.OpenDeviceConditionIncidents(ctx, s.DeviceID)
	if err != nil || len(open) != 1 {
		t.Fatalf("open list %d %v", len(open), err)
	}
	closed, err := inc.CloseDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, time.Now().UTC(), "")
	if err != nil || closed == nil || closed.EndTime == nil {
		t.Fatalf("close: %+v %v", closed, err)
	}
	if again, err := inc.CloseDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, time.Now().UTC(), ""); err != nil || again != nil {
		t.Fatalf("second close: %+v %v", again, err)
	}
}

func TestDBDeviceConditionCheckRejectsUnknownCondition(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	err := db.Exec(`INSERT INTO incidents (device_id, condition, start_time, incident_type) VALUES (?, 'toaster', now(), 'error')`, s.DeviceID).Error
	if err == nil {
		t.Fatal("unknown device condition accepted")
	}
}

// A UPS on battery that then stops answering: the device-down incident opens
// beside the UPS one, the device coming back closes only the device-down
// one, and the UPS incident never lowers availability.
func TestDBUPSIncidentIndependentOfDeviceDown(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET created_at = now() - interval '2 days' WHERE id = ?`, s.DeviceID)
	inc := NewIncidentService(db)
	now := time.Now().UTC()

	if _, _, err := inc.OpenDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, now.Add(-time.Hour), "on battery"); err != nil {
		t.Fatal(err)
	}
	down, opened, err := inc.OpenDeviceIncident(ctx, s.DeviceID, now.Add(-30*time.Minute), "unreachable")
	if err != nil || !opened || down.Condition != nil {
		t.Fatalf("device-down open blocked by UPS incident: %+v %v %v", down, opened, err)
	}
	closed, err := inc.CloseDeviceIncident(ctx, s.DeviceID, now, "")
	if err != nil || closed == nil || closed.ID != down.ID {
		t.Fatalf("closed the wrong incident: %+v %v", closed, err)
	}
	open, _ := inc.OpenDeviceConditionIncidents(ctx, s.DeviceID)
	if len(open) != 1 || *open[0].Condition != models.UPSConditionOnBattery {
		t.Fatalf("UPS incident closed by device recovery: %+v", open)
	}

	devices := NewDeviceService(db, NewSNMPCredentialService(db), inc)
	// Close the remaining device-down window effect: only the 30-minute
	// device-down incident may count, never the hour-long UPS one.
	d, err := devices.Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	if d.Availability30d == nil || *d.Availability30d < 99 {
		t.Errorf("availability %v: the UPS incident was counted", d.Availability30d)
	}
}
```

Before writing the availability assertion, read `device_service_db_test.go`'s `TestDBAvailabilityWithoutIncidentsIs100` and use the same field name it asserts on (the line above assumes `Availability30d *float64`; change it to match). A 30-minute outage over 2 days is ~98.96 %, so with only the device-down incident counted the 30-day figure is ≥ 98.9 — use the threshold `98.5` if the window is 2 days rather than 30 (compute it; do not loosen it past what the device-down incident alone explains). An hour-long UPS incident counted would push it lower still — the test must fail on today's code.

Add to `backend/internal/models/snmp_test.go`:

```go
func TestDeviceDetailsPatchUPSThresholds(t *testing.T) {
	var p DeviceDetailsPatch
	if err := json.Unmarshal([]byte(`{"ups_low_battery_pct": 30, "ups_high_load_pct": null}`), &p); err != nil {
		t.Fatal(err)
	}
	u, err := p.Updates()
	if err != nil || u["ups_low_battery_pct"] != 30 || u["ups_high_load_pct"] != nil {
		t.Fatalf("updates %v %v", u, err)
	}
	if _, ok := u["ups_high_load_pct"]; !ok {
		t.Error("null did not clear the override")
	}
	for _, body := range []string{`{"ups_low_battery_pct": 4}`, `{"ups_low_battery_pct": 96}`, `{"ups_high_load_pct": 9}`, `{"ups_high_load_pct": 101}`} {
		var bad DeviceDetailsPatch
		_ = json.Unmarshal([]byte(body), &bad)
		if _, err := bad.Updates(); err == nil {
			t.Errorf("%s accepted", body)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go vet ./internal/services/ ./internal/models/`
Expected: compile errors (undefined `OpenDeviceConditionIncident`, `UPSConditionOnBattery`, `UPSLowBatteryPct`).

- [ ] **Step 3: Migration** `backend/migrations/055_ups_monitoring.sql`:

```sql
-- 055_ups_monitoring.sql
-- UPS monitoring: device-level condition incidents (a UPS on battery, low
-- battery, high load) and per-device thresholds.
--
-- A device-level condition incident has device_id and condition set and no
-- interface. The CHECK spells out "condition IS NOT NULL" because
-- "condition IN (...)" alone is NULL, not false, for a NULL condition.
ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incidents_port_check;
ALTER TABLE incidents ADD CONSTRAINT incidents_port_check CHECK (
    (interface_id IS NULL AND condition IS NULL)
    OR (interface_id IS NOT NULL AND device_id IS NOT NULL AND condition IS NOT NULL
        AND condition IN ('link_down', 'errors', 'flapping', 'slow_link', 'saturated'))
    OR (interface_id IS NULL AND device_id IS NOT NULL AND condition IS NOT NULL
        AND condition IN ('ups_on_battery', 'ups_low_battery', 'ups_high_load'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_incidents_open_device_condition
    ON incidents (device_id, condition)
    WHERE end_time IS NULL AND interface_id IS NULL AND condition IS NOT NULL;

-- NULL = the instance setting.
ALTER TABLE devices
    ADD COLUMN IF NOT EXISTS ups_low_battery_pct INTEGER CHECK (ups_low_battery_pct BETWEEN 5 AND 95),
    ADD COLUMN IF NOT EXISTS ups_high_load_pct   INTEGER CHECK (ups_high_load_pct BETWEEN 10 AND 100);
```

- [ ] **Step 4: Models.** In `backend/internal/models/snmp.go`, next to the `PortCondition*` consts:

```go
// UPS conditions: device-level incidents on a UPS (no interface).
const (
	UPSConditionOnBattery  = "ups_on_battery"
	UPSConditionLowBattery = "ups_low_battery"
	UPSConditionHighLoad   = "ups_high_load"
)
```

In `Device`, after `FaceplatePortStyle`:

```go
	// UPS alert thresholds; nil = the instance setting.
	UPSLowBatteryPct *int `json:"ups_low_battery_pct" gorm:"column:ups_low_battery_pct"`
	UPSHighLoadPct   *int `json:"ups_high_load_pct" gorm:"column:ups_high_load_pct"`
```

In `DeviceDetailsPatch` add `UPSLowBatteryPct Opt[int] \`json:"ups_low_battery_pct"\`` and `UPSHighLoadPct Opt[int] \`json:"ups_high_load_pct"\``, and in `Updates()` before the `len(u) == 0` check:

```go
	for _, f := range []struct {
		o          Opt[int]
		col, label string
		min, max   int
	}{{p.UPSLowBatteryPct, "ups_low_battery_pct", "the low battery threshold", MinUPSLowBatteryPct, MaxUPSLowBatteryPct},
		{p.UPSHighLoadPct, "ups_high_load_pct", "the high load threshold", MinUPSHighLoadPct, MaxUPSHighLoadPct}} {
		if !f.o.Set {
			continue
		}
		if v := f.o.Value; v != nil && (*v < f.min || *v > f.max) {
			return nil, fmt.Errorf("%s must be between %d and %d", f.label, f.min, f.max)
		}
		u[f.col] = optValue(f.o)
	}
```

(`optValue` already exists and is used for `faceplate_rows`; confirm it returns `nil` for a nil value and the int otherwise, and that the test's `u["ups_low_battery_pct"] != 30` compares an `int` — adjust the test literal's type to what `optValue` returns if it returns `*int`.)

In `backend/internal/models/setting.go`, next to the port setting keys and limits:

```go
	// SettingUPSLowBatteryPct and SettingUPSHighLoadPct are the UPS alert
	// thresholds (a device may override either).
	SettingUPSLowBatteryPct = "ups_low_battery_pct"
	SettingUPSHighLoadPct   = "ups_high_load_pct"
```

```go
	DefaultUPSLowBatteryPct = 25
	MinUPSLowBatteryPct     = 5
	MaxUPSLowBatteryPct     = 95
	DefaultUPSHighLoadPct   = 80
	MinUPSHighLoadPct       = 10
	MaxUPSHighLoadPct       = 100
```

In `backend/internal/api/device_handler.go` `deviceDetailsAudit`, add `"ups_low_battery_pct": d.UPSLowBatteryPct, "ups_high_load_pct": d.UPSHighLoadPct`.

- [ ] **Step 5: Incident service.** In `backend/internal/services/incident_service.go`:

1. In `closeDeviceIncident` and `activeDeviceIncident`, change `"device_id = ? AND interface_id IS NULL AND end_time IS NULL"` to `"device_id = ? AND interface_id IS NULL AND condition IS NULL AND end_time IS NULL"`.
2. In `incidentSubjectSelect`, replace the `subject_name` CASE with:

```sql
	CASE WHEN di.id IS NOT NULL THEN
		d.name || ' · ' || COALESCE(NULLIF(di.name, ''), di.if_index::text)
		|| CASE WHEN COALESCE(di.alias, '') <> '' THEN ' (' || di.alias || ')' ELSE '' END
	WHEN i.device_id IS NOT NULL AND i.condition IS NOT NULL THEN
		d.name || ' · ' || CASE i.condition
			WHEN 'ups_on_battery' THEN 'On battery'
			WHEN 'ups_low_battery' THEN 'Low battery'
			WHEN 'ups_high_load' THEN 'High load'
			ELSE i.condition END
	ELSE COALESCE(m.name, d.name) END AS subject_name,
```

and update the comment above it to mention the UPS form ("Device · On battery").
3. Add after `activePortIncident`:

```go
// OpenDeviceConditionIncident opens a device-level condition incident (a
// UPS condition), unless one is already open for that device and condition,
// in which case that one is returned with opened=false. A concurrent open
// losing the race on the partial unique index is reported the same way.
func (s *IncidentService) OpenDeviceConditionIncident(ctx context.Context, deviceID uuid.UUID, condition string, start time.Time, reason string) (*models.Incident, bool, error) {
	if active, err := s.activeDeviceConditionIncident(ctx, deviceID, condition); err != nil || active != nil {
		return active, false, err
	}
	now := time.Now()
	cond := condition
	incident := &models.Incident{
		ID: uuid.New(), DeviceID: &deviceID, Condition: &cond,
		StartTime: start, Severity: defaultIncidentSeverity, IncidentType: models.IncidentTypeError,
		RootCause: reason, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.db.WithContext(ctx).Create(incident).Error; err != nil {
		if isDuplicateKey(err) {
			active, err := s.activeDeviceConditionIncident(ctx, deviceID, condition)
			return active, false, err
		}
		return nil, false, fmt.Errorf("creating %s incident for device %s: %w", condition, deviceID, err)
	}
	s.logger.Printf("[incident] opened id=%s device=%s condition=%s", incident.ID, deviceID, condition)
	return incident, true, nil
}

// CloseDeviceConditionIncident closes the open incident for one device and
// condition. Returns nil, nil when none is open.
func (s *IncidentService) CloseDeviceConditionIncident(ctx context.Context, deviceID uuid.UUID, condition string, end time.Time, note string) (*models.Incident, error) {
	active, err := s.activeDeviceConditionIncident(ctx, deviceID, condition)
	if err != nil || active == nil {
		return nil, err
	}
	return s.closeIncidentRow(s.db.WithContext(ctx), *active, end, note)
}

// OpenDeviceConditionIncidents lists a device's open device-level condition
// incidents, for UPSMonitor's restart rebuild and reconciliation.
func (s *IncidentService) OpenDeviceConditionIncidents(ctx context.Context, deviceID uuid.UUID) ([]models.Incident, error) {
	var rows []models.Incident
	err := s.db.WithContext(ctx).
		Where("device_id = ? AND interface_id IS NULL AND condition IS NOT NULL AND end_time IS NULL", deviceID).
		Order("start_time DESC").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing open condition incidents for device %s: %w", deviceID, err)
	}
	return rows, nil
}

func (s *IncidentService) activeDeviceConditionIncident(ctx context.Context, deviceID uuid.UUID, condition string) (*models.Incident, error) {
	var rows []models.Incident
	err := s.db.WithContext(ctx).
		Where("device_id = ? AND interface_id IS NULL AND condition = ? AND end_time IS NULL", deviceID, condition).
		Limit(1).Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("querying open %s incident for device %s: %w", condition, deviceID, err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}
```

4. In `backend/internal/services/device_service.go:84`, change the join to `LEFT JOIN incidents i ON i.device_id = d.id AND i.interface_id IS NULL AND i.condition IS NULL`, and add a short line to the comment above `availabilitySQL`: UPS condition incidents (`condition` set) are not downtime.

5. Search for any other "device incident" query that would now catch UPS rows: `grep -rn "interface_id IS NULL" backend --include=*.go | grep -v _test`. Every hit must either add `AND condition IS NULL` or be confirmed (with a comment in your report) to want both kinds.

- [ ] **Step 6: Run tests**

Run: `cd backend && gofmt -l internal; go vet ./... && go test ./internal/models/ && ./scripts/test-db.sh -run 'DeviceCondition|UPSIncident|Availability|DeviceIncident|PortIncident' -v 2>&1 | grep -E "^(=== RUN|--- |ok|FAIL)"`
Expected: every listed test PASS. Then the full suite `./scripts/test-db.sh 2>&1 | grep -E "^(FAIL|---|ok)"` → only `ok` lines.

- [ ] **Step 7: Commit**

```bash
git add backend/migrations/055_ups_monitoring.sql backend/internal/models backend/internal/services/incident_service.go backend/internal/services/device_service.go backend/internal/services/ups_incidents_db_test.go backend/internal/api/device_handler.go
git commit -m "feat(ups): device-level condition incidents and UPS thresholds

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Settings and metric catalogue

**Files:**
- Modify: `backend/internal/services/settings_service.go` (add `UPSThresholds`)
- Modify: `backend/internal/services/network_settings.go` (two fields, validation)
- Modify: `backend/internal/services/metrics_catalog.go` (eight metric keys)
- Test: `backend/internal/services/network_settings_db_test.go` (or the file holding the existing network settings DB test — find it with `grep -ln "NetworkSettingsService" backend/internal/services/*_test.go`)

**Interfaces:**
- Consumes: Task 1 `upsmon.Thresholds`; Task 3 setting keys and limits.
- Produces:
  - `func (s *SettingsService) UPSThresholds(ctx context.Context) upsmon.Thresholds`
  - `NetworkSettings.UPSLowBatteryPct int \`json:"ups_low_battery_pct"\``, `NetworkSettings.UPSHighLoadPct int \`json:"ups_high_load_pct"\``, same names as `*int` in `NetworkSettingsPatch`
  - metric consts `MetricUPSChargePct = "ups_charge_pct"`, `MetricUPSRuntimeMin = "ups_runtime_min"`, `MetricUPSLoadPct = "ups_load_pct"`, `MetricUPSInputV = "ups_input_v"`, `MetricUPSOutputV = "ups_output_v"`, `MetricUPSBatteryTempC = "ups_battery_temp_c"`, `MetricUPSOnBattery = "ups_on_battery"`, `MetricUPSBatteryStatus = "ups_battery_status"`; `var UPSMetrics = []string{...all eight...}`

- [ ] **Step 1: Write the failing test** (in the network settings DB test file, using its existing fixture for building a `NetworkSettingsService`; read it first):

```go
func TestDBNetworkSettingsUPSThresholds(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	settings := NewSettingsService(db)
	svc := NewNetworkSettingsService(settings, NewMetricsStore(db))
	got := svc.Get(ctx)
	if got.UPSLowBatteryPct != 25 || got.UPSHighLoadPct != 80 {
		t.Fatalf("defaults %+v", got)
	}
	low, high := 30, 90
	got, err := svc.Update(ctx, NetworkSettingsPatch{UPSLowBatteryPct: &low, UPSHighLoadPct: &high})
	if err != nil || got.UPSLowBatteryPct != 30 || got.UPSHighLoadPct != 90 {
		t.Fatalf("update %+v %v", got, err)
	}
	if th := settings.UPSThresholds(ctx); th.LowBatteryPct != 30 || th.HighLoadPct != 90 {
		t.Errorf("thresholds %+v", th)
	}
	bad := 4
	if _, err := svc.Update(ctx, NetworkSettingsPatch{UPSLowBatteryPct: &bad}); err == nil {
		t.Error("4 % accepted")
	}
	for _, m := range UPSMetrics {
		if !KnownMetric(m) {
			t.Errorf("%s not in the catalogue", m)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd backend && go vet ./internal/services/`
Expected: compile errors (unknown fields `UPSLowBatteryPct`, undefined `UPSMetrics`).

- [ ] **Step 3: Implement.**

`settings_service.go` (import `upsmon`):

```go
// UPSThresholds are the instance-wide UPS alert thresholds.
func (s *SettingsService) UPSThresholds(ctx context.Context) upsmon.Thresholds {
	return upsmon.Thresholds{
		LowBatteryPct: float64(s.intSetting(ctx, models.SettingUPSLowBatteryPct, models.DefaultUPSLowBatteryPct,
			models.MinUPSLowBatteryPct, models.MaxUPSLowBatteryPct)),
		HighLoadPct: float64(s.intSetting(ctx, models.SettingUPSHighLoadPct, models.DefaultUPSHighLoadPct,
			models.MinUPSHighLoadPct, models.MaxUPSHighLoadPct)),
	}
}
```

`network_settings.go`: add the two fields to both structs; in `Get` add

```go
	ups := s.settings.UPSThresholds(ctx)
	...
		UPSLowBatteryPct: int(ups.LowBatteryPct),
		UPSHighLoadPct:   int(ups.HighLoadPct),
```

and in `Update`'s `fields` slice:

```go
		{p.UPSLowBatteryPct, models.SettingUPSLowBatteryPct, "The low battery threshold",
			models.MinUPSLowBatteryPct, models.MaxUPSLowBatteryPct},
		{p.UPSHighLoadPct, models.SettingUPSHighLoadPct, "The high load threshold",
			models.MinUPSHighLoadPct, models.MaxUPSHighLoadPct},
```

`metrics_catalog.go`: add the eight consts to the const block, then to `MetricCatalogue`:

```go
	{MetricUPSChargePct, "%", "Battery charge"},
	{MetricUPSRuntimeMin, "min", "Runtime left"},
	{MetricUPSLoadPct, "%", "Load"},
	{MetricUPSInputV, "V", "Input voltage"},
	{MetricUPSOutputV, "V", "Output voltage"},
	{MetricUPSBatteryTempC, "°C", "Battery temperature"},
	{MetricUPSOnBattery, "bool", "On battery"},
	{MetricUPSBatteryStatus, "enum", "Battery status"},
```

and below the catalogue:

```go
// UPSMetrics are the keys a UPS poll writes (instance "", no interface).
var UPSMetrics = []string{MetricUPSChargePct, MetricUPSRuntimeMin, MetricUPSLoadPct, MetricUPSInputV,
	MetricUPSOutputV, MetricUPSBatteryTempC, MetricUPSOnBattery, MetricUPSBatteryStatus}
```

If the API layer or frontend lists catalogue units anywhere with a closed set (`grep -rn '"per_min"' backend frontend/src --include=*.go --include=*.ts --include=*.tsx`), make sure the new units do not break it.

- [ ] **Step 4: Run tests**

Run: `cd backend && gofmt -l internal; go vet ./... && ./scripts/test-db.sh -run 'NetworkSettings' -v 2>&1 | grep -E "^(--- |ok|FAIL)"; go test ./internal/api/ 2>&1 | tail -2`
Expected: PASS / ok (the API test `TestNetworkSettingsAdminOnly` must still pass; update its fake only if the compiler requires it).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services
git commit -m "feat(ups): UPS threshold settings and metric keys

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: `UPSMonitor` and poller wiring

**Files:**
- Create: `backend/internal/services/ups_monitor.go`
- Test: `backend/internal/services/ups_monitor_test.go`
- Modify: `backend/internal/services/device_poller.go` (interface, field, setter, call)
- Modify: `backend/internal/services/device_poller_test.go` (or the poller's existing unit test file — `grep -ln "NewDevicePoller" backend/internal/services/*_test.go`)
- Modify: `backend/cmd/sentinel/main.go:394` (wire it)

**Interfaces:**
- Consumes: Task 1 (`upsmon.*`), Task 2 (`snmp.ReadUPS`), Task 3 (incident functions, `models.Device.UPS*Pct`), Task 4 (`UPSThresholds`, `MetricUPS*`), existing `MetricsWriter`, `Notifier`, `displayName`, `humanDuration`.
- Produces:
  - `type UPSIncidents interface { OpenDeviceConditionIncident(...); CloseDeviceConditionIncident(...); OpenDeviceConditionIncidents(...) }` (signatures as in Task 3)
  - `type UPSThresholdSource interface { UPSThresholds(ctx context.Context) upsmon.Thresholds }`
  - `type SiteNamer interface { SiteName(ctx context.Context, siteID uuid.UUID) (string, error) }`
  - `func NewUPSMonitor(metrics MetricsWriter, incidents UPSIncidents, notifier Notifier, client snmp.Client, thresholds UPSThresholdSource, sites SiteNamer) *UPSMonitor`
  - `func (m *UPSMonitor) PollUPS(ctx context.Context, d models.Device, t snmp.Target)`
  - `func (m *UPSMonitor) ReconcileUPS(ctx context.Context, d models.Device)`
  - `func IsUPS(d models.Device) bool`
  - `type UPSPoller interface { PollUPS(...); ReconcileUPS(...) }`; `func (p *DevicePoller) SetUPS(u UPSPoller)`

- [ ] **Step 1: Write the failing tests** `backend/internal/services/ups_monitor_test.go`:

```go
package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/upsmon"
)

const upsPrefix = "1.3.6.1.2.1.33.1"

// fakeUPSAgent answers UPS GETs from vals; fail makes every GET time out.
type fakeUPSAgent struct {
	vals map[string]any
	fail bool
}

func (f *fakeUPSAgent) Get(_ context.Context, _ snmp.Target, oids []string) ([]snmp.PDU, error) {
	if f.fail {
		return nil, errors.New("timeout")
	}
	out := make([]snmp.PDU, len(oids))
	for i, o := range oids {
		out[i] = snmp.PDU{OID: o, Value: f.vals[o]}
	}
	return out, nil
}
func (f *fakeUPSAgent) Walk(context.Context, snmp.Target, string) ([]snmp.PDU, error) { return nil, nil }

func onMains() map[string]any {
	return map[string]any{upsPrefix + ".2.1.0": int64(2), upsPrefix + ".2.3.0": int64(41), upsPrefix + ".2.4.0": int64(96),
		upsPrefix + ".4.1.0": int64(3), upsPrefix + ".4.4.1.5.1": int64(34)}
}

type fakeUPSIncidents struct {
	open    map[string]*models.Incident
	opened  []string
	closed  []string
	failOps bool
}

func newFakeUPSIncidents() *fakeUPSIncidents { return &fakeUPSIncidents{open: map[string]*models.Incident{}} }

func (f *fakeUPSIncidents) OpenDeviceConditionIncident(_ context.Context, id uuid.UUID, c string, start time.Time, _ string) (*models.Incident, bool, error) {
	if inc, ok := f.open[c]; ok {
		return inc, false, nil
	}
	cond := c
	inc := &models.Incident{ID: uuid.New(), DeviceID: &id, Condition: &cond, StartTime: start}
	f.open[c] = inc
	f.opened = append(f.opened, c)
	return inc, true, nil
}
func (f *fakeUPSIncidents) CloseDeviceConditionIncident(_ context.Context, _ uuid.UUID, c string, end time.Time, _ string) (*models.Incident, error) {
	inc, ok := f.open[c]
	if !ok {
		return nil, nil
	}
	delete(f.open, c)
	f.closed = append(f.closed, c)
	inc.EndTime = &end
	inc.DurationSeconds = int(end.Sub(inc.StartTime).Seconds())
	return inc, nil
}
func (f *fakeUPSIncidents) OpenDeviceConditionIncidents(context.Context, uuid.UUID) ([]models.Incident, error) {
	var out []models.Incident
	for _, inc := range f.open {
		out = append(out, *inc)
	}
	return out, nil
}

type fakeUPSMetrics struct{ points []SamplePoint }

func (f *fakeUPSMetrics) Write(_ context.Context, _ uuid.UUID, _ time.Time, p []SamplePoint) error {
	f.points = append(f.points, p...)
	return nil
}

type fakeNotifier struct{ sent []*notifications.NotificationMessage }

func (f *fakeNotifier) SendNotification(_ context.Context, m *notifications.NotificationMessage) error {
	f.sent = append(f.sent, m)
	return nil
}

type fixedUPSThresholds struct{}

func (fixedUPSThresholds) UPSThresholds(context.Context) upsmon.Thresholds {
	return upsmon.Thresholds{LowBatteryPct: 25, HighLoadPct: 80}
}

type fakeSiteNamer struct{}

func (fakeSiteNamer) SiteName(context.Context, uuid.UUID) (string, error) { return "Expo", nil }

type upsRig struct {
	agent *fakeUPSAgent
	inc   *fakeUPSIncidents
	mets  *fakeUPSMetrics
	notif *fakeNotifier
	mon   *UPSMonitor
	dev   models.Device
	now   time.Time
}

func newUPSRig() *upsRig {
	r := &upsRig{agent: &fakeUPSAgent{vals: onMains()}, inc: newFakeUPSIncidents(), mets: &fakeUPSMetrics{}, notif: &fakeNotifier{},
		now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	r.mon = NewUPSMonitor(r.mets, r.inc, r.notif, r.agent, fixedUPSThresholds{}, fakeSiteNamer{})
	r.mon.now = func() time.Time { return r.now }
	ups := "ups"
	r.dev = models.Device{ID: uuid.New(), Name: "EXP-BEB-SMART1500", Host: "10.10.255.202", DeviceType: &ups}
	return r
}

func (r *upsRig) poll() { r.mon.PollUPS(context.Background(), r.dev, snmp.Target{}); r.now = r.now.Add(time.Minute) }

func TestUPSMonitorWritesSamples(t *testing.T) {
	r := newUPSRig()
	r.poll()
	got := map[string]float64{}
	for _, p := range r.mets.points {
		if p.Instance != "" || p.InterfaceID != nil {
			t.Errorf("point %+v has an instance or interface", p)
		}
		got[p.Metric] = p.Value
	}
	if got[MetricUPSChargePct] != 96 || got[MetricUPSLoadPct] != 34 || got[MetricUPSRuntimeMin] != 41 ||
		got[MetricUPSOnBattery] != 0 || got[MetricUPSBatteryStatus] != 2 {
		t.Errorf("samples %v", got)
	}
	if _, ok := got[MetricUPSInputV]; ok {
		t.Error("absent input voltage was written")
	}
}

func TestUPSMonitorOnBatteryOneIncidentOneAlertOneRecovery(t *testing.T) {
	r := newUPSRig()
	r.poll()
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	r.poll() // still on battery: nothing new
	r.agent.vals[upsPrefix+".4.1.0"] = int64(3)
	r.poll()
	if len(r.inc.opened) != 1 || len(r.inc.closed) != 1 || r.inc.opened[0] != models.UPSConditionOnBattery {
		t.Fatalf("opened %v closed %v", r.inc.opened, r.inc.closed)
	}
	if len(r.notif.sent) != 2 {
		t.Fatalf("sent %d notifications", len(r.notif.sent))
	}
	start, end := r.notif.sent[0], r.notif.sent[1]
	if start.Message != "EXP-BEB-SMART1500 is on battery (charge 96 %, 41 min left)" || start.Status != "warning" ||
		start.MonitorName != "EXP-BEB-SMART1500 · On battery" || start.SiteName != "Expo" || start.IncidentID == nil {
		t.Errorf("start %+v", start)
	}
	if !strings.HasPrefix(end.Message, "EXP-BEB-SMART1500 is back on mains power") || end.Status != "recovered" {
		t.Errorf("end %+v", end)
	}
}

func TestUPSMonitorFailedGetChangesNothing(t *testing.T) {
	r := newUPSRig()
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	r.agent.fail = true
	before := len(r.mets.points)
	r.poll()
	if len(r.mets.points) != before || len(r.inc.closed) != 0 || len(r.notif.sent) != 1 {
		t.Errorf("failed GET acted: points %d->%d closed %v sent %d", before, len(r.mets.points), r.inc.closed, len(r.notif.sent))
	}
}

// After a restart the monitor rebuilds from open incidents: no second alert,
// and the recovery still closes and notifies.
func TestUPSMonitorRestartMidOutage(t *testing.T) {
	r := newUPSRig()
	id := r.dev.ID
	cond := models.UPSConditionOnBattery
	r.inc.open[cond] = &models.Incident{ID: uuid.New(), DeviceID: &id, Condition: &cond, StartTime: r.now.Add(-time.Hour)}
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	if len(r.notif.sent) != 0 || len(r.inc.opened) != 0 {
		t.Fatalf("re-alerted after restart: %d sent, opened %v", len(r.notif.sent), r.inc.opened)
	}
	r.agent.vals[upsPrefix+".4.1.0"] = int64(3)
	r.poll()
	if len(r.inc.closed) != 1 || len(r.notif.sent) != 1 || r.notif.sent[0].Status != "recovered" {
		t.Errorf("recovery: closed %v sent %+v", r.inc.closed, r.notif.sent)
	}
}

// A device override replaces the instance threshold.
func TestUPSMonitorDeviceThresholdOverride(t *testing.T) {
	r := newUPSRig()
	fifty := 50
	r.dev.UPSLowBatteryPct = &fifty
	r.agent.vals[upsPrefix+".2.4.0"] = int64(40)
	r.poll()
	if len(r.inc.opened) != 1 || r.inc.opened[0] != models.UPSConditionLowBattery {
		t.Fatalf("override ignored: %v", r.inc.opened)
	}
	if !strings.Contains(r.notif.sent[0].Message, "battery is low (charge 40 %") {
		t.Errorf("message %q", r.notif.sent[0].Message)
	}
}

// A device that is no longer a UPS has its open UPS incidents closed quietly.
func TestUPSMonitorReconcileClosesQuietly(t *testing.T) {
	r := newUPSRig()
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	sent := len(r.notif.sent)
	r.dev.DeviceType = nil
	r.dev.DeviceTypeDetected = "other"
	r.mon.ReconcileUPS(context.Background(), r.dev)
	if len(r.inc.open) != 0 || len(r.notif.sent) != sent {
		t.Errorf("open %v, notifications %d->%d", r.inc.open, sent, len(r.notif.sent))
	}
}

func TestUPSMonitorNotifyChannelsNone(t *testing.T) {
	r := newUPSRig()
	r.dev.NotifyChannels = []string{}
	r.agent.vals[upsPrefix+".4.1.0"] = int64(5)
	r.poll()
	if len(r.inc.opened) != 1 || len(r.notif.sent) != 0 {
		t.Errorf("opened %v sent %d", r.inc.opened, len(r.notif.sent))
	}
}

func TestIsUPS(t *testing.T) {
	ups, sw := "ups", "switch"
	for _, c := range []struct {
		d    models.Device
		want bool
	}{
		{models.Device{DeviceTypeDetected: "ups"}, true},
		{models.Device{DeviceType: &ups, DeviceTypeDetected: "other"}, true},
		{models.Device{DeviceType: &sw, DeviceTypeDetected: "ups"}, false},
		{models.Device{DeviceTypeDetected: "switch"}, false},
	} {
		if got := IsUPS(c.d); got != c.want {
			t.Errorf("IsUPS(%+v) = %v", c.d, got)
		}
	}
}
```

Check `models.Device.NotifyChannels`' type before writing `[]string{}` (port_monitor uses `[]string(d.NotifyChannels)`, so it is a named slice type — use that type's empty literal). Check whether the existing poller tests already define `fakeNotifier`; if so, reuse it and drop the one above.

In the DevicePoller test file, add a test that a poll of an up UPS-typed device with no interfaces calls `PollUPS`, and a poll of a switch calls `ReconcileUPS` — using the file's existing fake store/client for a successful poll and a recording `UPSPoller`:

```go
type recordingUPS struct{ polled, reconciled int }

func (r *recordingUPS) PollUPS(context.Context, models.Device, snmp.Target) { r.polled++ }
func (r *recordingUPS) ReconcileUPS(context.Context, models.Device)         { r.reconciled++ }
```

Build the poller the way the file's existing "successful poll runs stats" test does, call `SetUPS(rec)`, poll once with the device typed `ups` and once typed `switch`, and assert `polled == 1 && reconciled == 1`.

- [ ] **Step 2: Run to verify it fails**

Run: `cd backend && go vet ./internal/services/`
Expected: compile errors (undefined `NewUPSMonitor`, `IsUPS`, `SetUPS`).

- [ ] **Step 3: Implement** `backend/internal/services/ups_monitor.go`:

```go
package services

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/upsmon"
)

// UPSIncidents opens and closes UPS condition incidents (IncidentService).
type UPSIncidents interface {
	OpenDeviceConditionIncident(ctx context.Context, deviceID uuid.UUID, condition string, start time.Time, reason string) (*models.Incident, bool, error)
	CloseDeviceConditionIncident(ctx context.Context, deviceID uuid.UUID, condition string, end time.Time, note string) (*models.Incident, error)
	OpenDeviceConditionIncidents(ctx context.Context, deviceID uuid.UUID) ([]models.Incident, error)
}

// UPSThresholdSource gives the instance-wide UPS thresholds (SettingsService).
type UPSThresholdSource interface {
	UPSThresholds(ctx context.Context) upsmon.Thresholds
}

// SiteNamer names a site for alerts (PortService).
type SiteNamer interface {
	SiteName(ctx context.Context, siteID uuid.UUID) (string, error)
}

// UPSMonitor runs a UPS's poll: UPS-MIB readings to samples, and on battery,
// low battery and high load to incidents and alerts.
type UPSMonitor struct {
	metrics    MetricsWriter
	incidents  UPSIncidents
	notifier   Notifier
	client     snmp.Client
	thresholds UPSThresholdSource
	sites      SiteNamer

	now    func() time.Time
	logger *log.Logger

	mu     sync.Mutex
	states map[uuid.UUID]*upsmon.State
}

func NewUPSMonitor(metrics MetricsWriter, incidents UPSIncidents, notifier Notifier, client snmp.Client, thresholds UPSThresholdSource, sites SiteNamer) *UPSMonitor {
	return &UPSMonitor{metrics: metrics, incidents: incidents, notifier: notifier, client: client, thresholds: thresholds,
		sites: sites, now: time.Now, logger: log.Default(), states: map[uuid.UUID]*upsmon.State{}}
}

// IsUPS reports whether a device's effective type (the user's choice, else
// what inventory detected) is UPS.
func IsUPS(d models.Device) bool {
	if d.DeviceType != nil {
		return *d.DeviceType == models.DeviceTypeUPS
	}
	return d.DeviceTypeDetected == models.DeviceTypeUPS
}

var upsConditionLabel = map[upsmon.Condition]string{
	upsmon.OnBattery: "On battery", upsmon.LowBattery: "Low battery", upsmon.HighLoad: "High load",
}

// thresholdsFor applies a device's overrides to the defaults.
func thresholdsFor(d models.Device, def upsmon.Thresholds) upsmon.Thresholds {
	if d.UPSLowBatteryPct != nil {
		def.LowBatteryPct = float64(*d.UPSLowBatteryPct)
	}
	if d.UPSHighLoadPct != nil {
		def.HighLoadPct = float64(*d.UPSHighLoadPct)
	}
	return def
}

// state returns the device's remembered state, rebuilding it from open
// incidents the first time (after a restart). ok is false when that lookup
// failed; the poll then skips alerting rather than alert twice.
func (m *UPSMonitor) state(ctx context.Context, deviceID uuid.UUID) (*upsmon.State, bool) {
	m.mu.Lock()
	st := m.states[deviceID]
	m.mu.Unlock()
	if st != nil {
		return st, true
	}
	open, err := m.incidents.OpenDeviceConditionIncidents(ctx, deviceID)
	if err != nil {
		m.logger.Printf("[ups] listing open incidents for %s: %v", deviceID, err)
		return nil, false
	}
	seed := map[upsmon.Condition]time.Time{}
	for _, inc := range open {
		if inc.Condition != nil {
			seed[upsmon.Condition(*inc.Condition)] = inc.StartTime
		}
	}
	restored := upsmon.Restore(seed)
	m.mu.Lock()
	m.states[deviceID] = &restored
	m.mu.Unlock()
	return &restored, true
}

// PollUPS reads one UPS and applies the result. A failed read changes
// nothing: an unreachable UPS is the device-down incident's business.
func (m *UPSMonitor) PollUPS(ctx context.Context, d models.Device, t snmp.Target) {
	r, err := snmp.ReadUPS(ctx, m.client, t)
	if err != nil {
		m.logger.Printf("[ups] poll of %s: %v", d.Host, err)
		return
	}
	now := m.now().UTC()
	if r.Any() {
		if err := m.metrics.Write(ctx, d.ID, now, upsPoints(r)); err != nil {
			m.logger.Printf("[ups] writing samples for %s: %v", d.Host, err)
		}
	}
	st, ok := m.state(ctx, d.ID)
	if !ok {
		return
	}
	next, changes := upsmon.Evaluate(*st, r, thresholdsFor(d, m.thresholds.UPSThresholds(ctx)), now)
	m.mu.Lock()
	m.states[d.ID] = &next
	m.mu.Unlock()

	site := ""
	handled := map[upsmon.Condition]bool{}
	for _, c := range changes {
		handled[c.Condition] = true
		if c.Started {
			inc, opened, err := m.incidents.OpenDeviceConditionIncident(ctx, d.ID, string(c.Condition), c.At, upsProblem(d, c))
			if err != nil {
				m.logger.Printf("[ups] opening %s incident for %s: %v", c.Condition, d.Host, err)
				continue
			}
			if opened {
				m.notify(ctx, d, c, inc, &site)
			}
			continue
		}
		inc, err := m.incidents.CloseDeviceConditionIncident(ctx, d.ID, string(c.Condition), c.At, "")
		if err != nil {
			m.logger.Printf("[ups] closing %s incident for %s: %v", c.Condition, d.Host, err)
			continue
		}
		if inc != nil {
			m.notify(ctx, d, c, inc, &site)
		}
	}
	// Retry an open that failed on an earlier poll (idempotent).
	for c, since := range next.Active {
		if handled[c] {
			continue
		}
		ch := upsmon.Change{Condition: c, Started: true, At: since}
		inc, opened, err := m.incidents.OpenDeviceConditionIncident(ctx, d.ID, string(c), since, upsProblem(d, ch))
		if err != nil {
			m.logger.Printf("[ups] ensuring %s incident for %s: %v", c, d.Host, err)
			continue
		}
		if opened {
			m.notify(ctx, d, ch, inc, &site)
		}
	}
}

// ReconcileUPS closes, without notifying, any UPS incident left open on a
// device that is no longer a UPS, and forgets its state.
func (m *UPSMonitor) ReconcileUPS(ctx context.Context, d models.Device) {
	m.mu.Lock()
	delete(m.states, d.ID)
	m.mu.Unlock()
	open, err := m.incidents.OpenDeviceConditionIncidents(ctx, d.ID)
	if err != nil {
		m.logger.Printf("[ups] listing open incidents for %s: %v", d.Host, err)
		return
	}
	for _, inc := range open {
		if inc.Condition == nil {
			continue
		}
		if _, err := m.incidents.CloseDeviceConditionIncident(ctx, d.ID, *inc.Condition, m.now().UTC(), "Closed: the device is no longer a UPS."); err != nil {
			m.logger.Printf("[ups] closing %s incident for %s: %v", *inc.Condition, d.Host, err)
		}
	}
}

// upsPoints are one poll's samples: every reading the UPS answered.
func upsPoints(r upsmon.Readings) []SamplePoint {
	var out []SamplePoint
	add := func(metric string, v *float64) {
		if v != nil {
			out = append(out, SamplePoint{Metric: metric, Value: *v})
		}
	}
	add(MetricUPSChargePct, r.ChargePct)
	add(MetricUPSRuntimeMin, r.RuntimeMin)
	add(MetricUPSLoadPct, r.LoadPct)
	add(MetricUPSInputV, r.InputV)
	add(MetricUPSOutputV, r.OutputV)
	add(MetricUPSBatteryTempC, r.BatteryTempC)
	if on, known := r.OnBattery(); known {
		v := 0.0
		if on {
			v = 1
		}
		out = append(out, SamplePoint{Metric: MetricUPSOnBattery, Value: v})
	}
	if r.BatteryStatus != 0 {
		out = append(out, SamplePoint{Metric: MetricUPSBatteryStatus, Value: float64(r.BatteryStatus)})
	}
	return out
}

func pct(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + " %" }

// figures renders "(charge 96 %, 41 min left)" from whichever details exist;
// "" when there are none.
func figures(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	out := " ("
	for i, p := range kept {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out + ")"
}

func detail(c upsmon.Change, key, format string) string {
	if v, ok := c.Detail[key]; ok {
		return fmt.Sprintf(format, strconv.FormatFloat(v, 'f', -1, 64))
	}
	return ""
}

// upsProblem is the alert sentence for a condition starting.
func upsProblem(d models.Device, c upsmon.Change) string {
	who := displayName(d)
	switch c.Condition {
	case upsmon.OnBattery:
		return who + " is on battery" + figures(detail(c, "charge_pct", "charge %s %%"), detail(c, "runtime_min", "%s min left"))
	case upsmon.LowBattery:
		return who + " battery is low" + figures(detail(c, "charge_pct", "charge %s %%"), detail(c, "runtime_min", "%s min left"))
	default:
		return who + " load is high" + figures(detail(c, "load_pct", "%s %%"), detail(c, "threshold_pct", "threshold %s %%"))
	}
}

// upsRecovered is the sentence for a condition ending.
func upsRecovered(d models.Device, c upsmon.Change) string {
	who := displayName(d)
	switch c.Condition {
	case upsmon.OnBattery:
		return who + " is back on mains power"
	case upsmon.LowBattery:
		return who + " battery has recovered" + figures(detail(c, "charge_pct", "charge %s %%"))
	default:
		return who + " load is back to normal" + figures(detail(c, "load_pct", "%s %%"))
	}
}

func (m *UPSMonitor) notify(ctx context.Context, d models.Device, c upsmon.Change, inc *models.Incident, site *string) {
	// Existing convention: nil = every enabled channel, [] = none.
	if d.NotifyChannels != nil && len(d.NotifyChannels) == 0 {
		return
	}
	if *site == "" {
		name, err := m.sites.SiteName(ctx, d.SiteID)
		if err != nil || name == "" {
			name = "its site"
		}
		*site = name
	}
	id := d.ID
	msg := &notifications.NotificationMessage{
		DeviceID: &id, SiteName: *site, MonitorName: displayName(d) + " · " + upsConditionLabel[c.Condition],
		MonitorURL: d.Host, Timestamp: c.At,
	}
	if d.NotifyChannels != nil {
		msg.Channels = []string(d.NotifyChannels)
	}
	if inc != nil {
		msg.IncidentID = &inc.ID
	}
	status := "warning"
	if c.Condition == upsmon.LowBattery {
		status = "down"
	}
	if c.Started {
		msg.Status, msg.PreviousStatus, msg.Message = status, "up", upsProblem(d, c)
	} else {
		dur := time.Duration(0)
		if inc != nil {
			dur = time.Duration(inc.DurationSeconds) * time.Second
		}
		msg.Status, msg.PreviousStatus, msg.DowntimeDuration = "recovered", status, dur
		msg.Message = fmt.Sprintf("%s after %s.", upsRecovered(d, c), humanDuration(dur))
	}
	if err := m.notifier.SendNotification(ctx, msg); err != nil {
		m.logger.Printf("[ups] sending %s notification for %s: %v", msg.Status, d.Host, err)
	}
}
```

Notes for the implementer:
- `models.DeviceTypeUPS`: check the constant's name (`grep -n "DeviceTypeUPS\|DeviceTypeSwitch *=" backend/internal/models/*.go`); add `DeviceTypeUPS = "ups"` beside the others if it does not exist.
- The test above expects the start message exactly `"EXP-BEB-SMART1500 is on battery (charge 96 %, 41 min left)"` and the recovery to start with `"EXP-BEB-SMART1500 is back on mains power"` (the code appends " after <duration>."). Match the spec wording; adjust the code, not the test, if they disagree.
- If `humanDuration` or `displayName` have different names, use the ones `port_monitor.go` uses.

In `backend/internal/services/device_poller.go`:

```go
// UPSPoller runs a UPS's poll (UPSMonitor).
type UPSPoller interface {
	PollUPS(ctx context.Context, d models.Device, t snmp.Target)
	// ReconcileUPS closes UPS incidents left on a device that is no longer a UPS.
	ReconcileUPS(ctx context.Context, d models.Device)
}
```

add `ups UPSPoller` to `DevicePoller`, a setter

```go
// SetUPS makes every successful poll of an up device run its UPS poll when
// it is a UPS, and tidy up UPS incidents when it no longer is.
func (p *DevicePoller) SetUPS(u UPSPoller) { p.ups = u }
```

and directly after the `p.stats.PollStats(...)` block:

```go
	if result == PollOK && tr.Status == models.DeviceStatusUp && p.ups != nil {
		if IsUPS(d) {
			p.ups.PollUPS(ctx, d, target)
		} else {
			p.ups.ReconcileUPS(ctx, d)
		}
	}
```

In `backend/cmd/sentinel/main.go`, after the `SetPortStats` line:

```go
	devicePoller.SetUPS(services.NewUPSMonitor(metricsStore, incidentService, notificationManager, snmpClient, settingsService, portService))
```

- [ ] **Step 4: Run tests**

Run: `cd backend && gofmt -l internal cmd; go vet ./... && go test ./internal/services/ -run 'UPS|IsUPS|Poller' -v 2>&1 | grep -E "^(--- |ok|FAIL)" && go build ./...`
Expected: all PASS, build ok. Then the full DB suite `./scripts/test-db.sh 2>&1 | grep -E "^(FAIL|---|ok)"` → only `ok`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services backend/cmd/sentinel/main.go
git commit -m "feat(ups): poll UPS readings each minute and alert on battery, low battery and high load

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `GET /devices/:id/ups`

**Files:**
- Modify: `backend/internal/services/port_service.go` (add `UPSStatusView`, `UPSStatus`)
- Modify: `backend/internal/api/port_handler.go` (interface method, route, handler)
- Modify: `backend/internal/api/port_handler_test.go` (fake method, access test)
- Test: `backend/internal/services/ups_status_db_test.go`

**Interfaces:**
- Consumes: Task 3 `OpenDeviceConditionIncidents`, Task 4 `UPSMetrics`, `UPSThresholds`; existing `PortService` fields (`db`, `metrics`, `incidents`, `settings`) and `liveSince`.
- Produces:
  - `type UPSStatusView struct { Readings map[string]float64 \`json:"readings"\`; Conditions []string \`json:"conditions"\`; LowBatteryPct int \`json:"low_battery_pct"\`; HighLoadPct int \`json:"high_load_pct"\`; DefaultLowBatteryPct int \`json:"default_low_battery_pct"\`; DefaultHighLoadPct int \`json:"default_high_load_pct"\` }`
  - `func (s *PortService) UPSStatus(ctx context.Context, d *DeviceView) (*UPSStatusView, error)`
  - route `GET /devices/:id/ups` (readonly access, like `/devices/:id/ports`)

- [ ] **Step 1: Write the failing tests**

`backend/internal/services/ups_status_db_test.go` (reuse `seedDevice`, follow `portsFixture` for building services; check `PortService`'s field names for incidents and settings before writing):

```go
package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBUPSStatus(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'ups', ups_high_load_pct = 70 WHERE id = ?`, s.DeviceID)
	incidents := NewIncidentService(db)
	metrics := NewMetricsStore(db)
	devices := NewDeviceService(db, NewSNMPCredentialService(db), incidents)
	ports := NewPortService(db, metrics, incidents, NewSettingsService(db))
	testdb.Must(t, metrics.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{
		{Metric: MetricUPSChargePct, Value: 96}, {Metric: MetricUPSLoadPct, Value: 34}, {Metric: MetricUPSOnBattery, Value: 1},
	}))
	if _, _, err := incidents.OpenDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, time.Now().UTC(), "x"); err != nil {
		t.Fatal(err)
	}
	d, err := devices.Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	v, err := ports.UPSStatus(ctx, d)
	testdb.Must(t, err)
	if v.Readings[MetricUPSChargePct] != 96 || v.Readings[MetricUPSOnBattery] != 1 || len(v.Readings) != 3 {
		t.Errorf("readings %v", v.Readings)
	}
	if len(v.Conditions) != 1 || v.Conditions[0] != models.UPSConditionOnBattery {
		t.Errorf("conditions %v", v.Conditions)
	}
	if v.LowBatteryPct != 25 || v.HighLoadPct != 70 || v.DefaultLowBatteryPct != 25 || v.DefaultHighLoadPct != 80 {
		t.Errorf("thresholds %+v", v)
	}
}
```

In `port_handler_test.go`, add to `fakePorts`:

```go
func (f *fakePorts) UPSStatus(context.Context, *services.DeviceView) (*services.UPSStatusView, error) {
	return &services.UPSStatusView{Readings: map[string]float64{}, Conditions: []string{}}, nil
}
```

and extend `TestPortRoutesAccess` (read it first) so `GET /devices/:id/ups` is 200 for a readonly caller and 404 for a caller with no access to the site — the same table the test already uses for `/devices/:id/ports`.

- [ ] **Step 2: Run to verify it fails**

Run: `cd backend && go vet ./internal/services/ ./internal/api/`
Expected: compile errors (undefined `UPSStatus` / `UPSStatusView`).

- [ ] **Step 3: Implement.** In `port_service.go`:

```go
// UPSStatusView is a UPS's latest readings (metric key -> value, only those
// reported within the live window), its open UPS conditions, and the
// thresholds in force with the instance defaults beside them.
type UPSStatusView struct {
	Readings             map[string]float64 `json:"readings"`
	Conditions           []string           `json:"conditions"`
	LowBatteryPct        int                `json:"low_battery_pct"`
	HighLoadPct          int                `json:"high_load_pct"`
	DefaultLowBatteryPct int                `json:"default_low_battery_pct"`
	DefaultHighLoadPct   int                `json:"default_high_load_pct"`
}

// UPSStatus is the device page's Power panel.
func (s *PortService) UPSStatus(ctx context.Context, d *DeviceView) (*UPSStatusView, error) {
	latest, err := s.metrics.LatestMany(ctx, []uuid.UUID{d.ID}, UPSMetrics, liveSince(time.Now(), d.PollInterval))
	if err != nil {
		return nil, err
	}
	out := &UPSStatusView{Readings: map[string]float64{}, Conditions: []string{}}
	for metric, byInst := range latest[d.ID] {
		if v, ok := byInst[""]; ok {
			out.Readings[metric] = v
		}
	}
	open, err := s.incidents.OpenDeviceConditionIncidents(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	for _, inc := range open {
		if inc.Condition != nil {
			out.Conditions = append(out.Conditions, *inc.Condition)
		}
	}
	def := s.settings.UPSThresholds(ctx)
	eff := thresholdsFor(d.Device, def)
	out.DefaultLowBatteryPct, out.DefaultHighLoadPct = int(def.LowBatteryPct), int(def.HighLoadPct)
	out.LowBatteryPct, out.HighLoadPct = int(eff.LowBatteryPct), int(eff.HighLoadPct)
	return out, nil
}
```

(`d.Device` assumes `DeviceView` embeds `models.Device`; check with `grep -n "type DeviceView struct" -A3 backend/internal/services/device_service.go` and adapt. If `PortService`'s incidents field is typed to an interface without `OpenDeviceConditionIncidents`, use the concrete `*IncidentService` it holds or widen that interface. If `metrics` is an interface without `LatestMany`, it already has it — `DevicePorts` calls it.)

In `port_handler.go`: add `UPSStatus(ctx context.Context, d *services.DeviceView) (*services.UPSStatusView, error)` to `portStore`, register `rg.GET("/devices/:id/ups", deviceUPSHandler(devices, ports, sites))`, and:

```go
func deviceUPSHandler(devices deviceStore, ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		v, err := ports.UPSStatus(c.Request.Context(), d)
		if err != nil {
			respondInternal(c, "deviceUPS", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd backend && gofmt -l internal; go vet ./... && go test ./internal/api/ && ./scripts/test-db.sh -run 'UPSStatus' -v 2>&1 | grep -E "^(--- |ok|FAIL)"`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/services/port_service.go backend/internal/services/ups_status_db_test.go backend/internal/api/port_handler.go backend/internal/api/port_handler_test.go
git commit -m "feat(ups): API for a UPS's latest readings and open conditions

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Frontend — Power panel, settings, Edit details, incident labels

**Files:**
- Modify: `frontend/src/hooks/usePorts.ts` (types + `useUPSStatus`; `DeviceDetailsPatch` fields)
- Modify: `frontend/src/hooks/useDevices.ts` (`Device.ups_low_battery_pct`, `ups_high_load_pct`)
- Modify: `frontend/src/hooks/useNetworkSettings.ts` (two fields)
- Modify: `frontend/src/components/network/TrafficChart.tsx` (unit `'min'`)
- Create: `frontend/src/components/network/UPSPanel.tsx`
- Modify: `frontend/src/pages/network/DeviceDetail.tsx` (render the panel; pass defaults to Edit details)
- Modify: `frontend/src/components/network/EditDetailsModal.tsx` (UPS alerts group)
- Modify: `frontend/src/pages/network/NetworkSettings.tsx` (two fields)
- Modify: `frontend/src/utils/network.ts` (`CONDITION_LABEL` entries)
- Modify: `frontend/src/pages/IncidentDetail.tsx` ("Detected by")

**Interfaces:**
- Consumes: Task 6 response shape; Task 4 settings names; Task 3 patch names.
- Produces: `UPSStatus` type, `useUPSStatus(deviceId: string | undefined, enabled: boolean)`, `UPSPanel` component (`{ deviceId: string; status: UPSStatus | null }`).

The design system is slate/Tailwind with the existing `card`, `btn-*` classes; match the look of `DeviceDetail`'s existing `dl` and sections.

- [ ] **Step 1: Types and hooks.** In `usePorts.ts`:

```ts
/** GET /devices/:id/ups: latest UPS-MIB readings (metric key -> value,
 *  absent when not reported recently), open UPS conditions, thresholds. */
export interface UPSStatus {
  readings: Partial<Record<UPSMetric, number>>
  conditions: string[]
  low_battery_pct: number
  high_load_pct: number
  default_low_battery_pct: number
  default_high_load_pct: number
}

export type UPSMetric =
  | 'ups_charge_pct'
  | 'ups_runtime_min'
  | 'ups_load_pct'
  | 'ups_input_v'
  | 'ups_output_v'
  | 'ups_battery_temp_c'
  | 'ups_on_battery'
  | 'ups_battery_status'

export function useUPSStatus(deviceId: string | undefined, enabled: boolean) {
  return useResource<UPSStatus>(deviceId && enabled ? `/devices/${deviceId}/ups` : null, undefined, LIVE_MS)
}
```

Add `ups_low_battery_pct?: number | null` and `ups_high_load_pct?: number | null` to `DeviceDetailsPatch`; add `ups_low_battery_pct: number | null` and `ups_high_load_pct: number | null` to `Device` in `useDevices.ts`; add `ups_low_battery_pct: number` and `ups_high_load_pct: number` to `NetworkSettings` in `useNetworkSettings.ts`.

In `TrafficChart.tsx`: `export type Unit = 'bps' | 'pct' | 'per_min' | 'min'` and in `formatValue` add `if (unit === 'min') return \`${Math.round(v)} min\`` before the per-minute fallback.

In `utils/network.ts` `CONDITION_LABEL`, add `ups_on_battery: 'On battery'`, `ups_low_battery: 'Low battery'`, `ups_high_load: 'High load'`.

- [ ] **Step 2: `UPSPanel.tsx`:**

```tsx
import { BatteryCharging, BatteryWarning, Plug } from 'lucide-react'
import type { UPSStatus } from '@/hooks/usePorts'
import TrafficChart from '@/components/network/TrafficChart'
import { CONDITION_LABEL } from '@/utils/network'

const BATTERY_STATUS: Record<number, string> = { 1: 'Battery status unknown', 3: 'Battery low', 4: 'Battery depleted' }

interface Props {
  deviceId: string
  status: UPSStatus | null
}

/** A UPS's Power section: source badge, live tiles, and history charts. */
export default function UPSPanel({ deviceId, status }: Props) {
  const r = status?.readings ?? {}
  const hasData = Object.keys(r).length > 0
  const onBattery = r.ups_on_battery
  const tiles: [string, string][] = []
  const add = (label: string, v: number | undefined, fmt: (n: number) => string) => {
    if (v !== undefined) tiles.push([label, fmt(v)])
  }
  add('Charge', r.ups_charge_pct, (n) => `${Math.round(n)}%`)
  add('Runtime left', r.ups_runtime_min, (n) => `${Math.round(n)} min`)
  add('Load', r.ups_load_pct, (n) => `${Math.round(n)}%`)
  add('Input', r.ups_input_v, (n) => `${Math.round(n)} V`)
  add('Output', r.ups_output_v, (n) => `${Math.round(n)} V`)
  add('Battery temperature', r.ups_battery_temp_c, (n) => `${Math.round(n)} °C`)
  const batteryNote = r.ups_battery_status !== undefined ? BATTERY_STATUS[r.ups_battery_status] : undefined

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-light text-white">Power</h2>
        {hasData &&
          (onBattery === 1 ? (
            <span className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/30 bg-amber-500/15 px-2.5 py-0.5 text-xs font-medium text-amber-300">
              <BatteryWarning className="h-3.5 w-3.5" /> On battery
            </span>
          ) : onBattery === 0 ? (
            <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-500/30 bg-emerald-500/15 px-2.5 py-0.5 text-xs font-medium text-emerald-300">
              <Plug className="h-3.5 w-3.5" /> On mains
            </span>
          ) : (
            <span className="rounded-full border border-slate-500/30 bg-slate-500/15 px-2.5 py-0.5 text-xs text-slate-300">Source unknown</span>
          ))}
        {batteryNote && <span className="text-xs text-amber-300">{batteryNote}</span>}
        {status?.conditions
          .filter((c) => c !== 'ups_on_battery')
          .map((c) => (
            <span key={c} className="rounded-full border border-red-500/30 bg-red-500/15 px-2.5 py-0.5 text-xs text-red-300">
              {CONDITION_LABEL[c] ?? c}
            </span>
          ))}
      </div>
      {!hasData ? (
        <p className="text-sm text-slate-500">
          No UPS readings in the last few minutes. If this lasts, the UPS may not report standard UPS readings (UPS-MIB).
        </p>
      ) : (
        <>
          <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            {tiles.map(([k, v]) => (
              <div key={k} className="rounded-lg border border-white/10 bg-slate-800/40 p-3">
                <dt className="flex items-center gap-1 text-xs uppercase tracking-widest text-slate-500">
                  {k === 'Charge' && <BatteryCharging className="h-3.5 w-3.5" />}
                  {k}
                </dt>
                <dd className="mt-1 text-xl font-light tabular-nums text-white">{v}</dd>
              </div>
            ))}
          </dl>
          <TrafficChart
            title="Battery and load"
            query={{ deviceIds: [deviceId] }}
            lines={[
              { metric: 'ups_charge_pct', label: 'Charge', colour: '#34d399' },
              { metric: 'ups_load_pct', label: 'Load', colour: '#60a5fa' },
            ]}
            unit="pct"
            threshold={status?.high_load_pct}
          />
          <TrafficChart title="Runtime left" query={{ deviceIds: [deviceId] }} lines={[{ metric: 'ups_runtime_min', label: 'Runtime', colour: '#a78bfa' }]} unit="min" />
        </>
      )}
    </section>
  )
}
```

Use the accent colours the design system already defines (`frontend/src/utils/colors.ts`) instead of the hex literals above if that file exports suitable ones — check how `TRAFFIC_LINES` in `DeviceDetail.tsx` picks its colours and do the same.

- [ ] **Step 3: Device page.** In `DeviceDetail.tsx`: `const isUPS = type === 'ups'` (after `type` is computed — move the hook call above any early return; React hooks must not be conditional, so call `useUPSStatus(id, device?.effective_type === 'ups' || (!device?.effective_type && device?.device_type_detected === 'ups'))` next to the other hooks at the top), and render `{isUPS && <UPSPanel deviceId={device.id} status={upsStatus} />}` directly above the `<TrafficChart title="Traffic" …/>` line. Pass `upsDefaults={upsStatus ? { low: upsStatus.default_low_battery_pct, high: upsStatus.default_high_load_pct } : undefined}` to `EditDetailsModal`.

- [ ] **Step 4: Edit details.** In `EditDetailsModal.tsx` add the prop `upsDefaults?: { low: number; high: number }`, state `const [lowBattery, setLowBattery] = useState(device.ups_low_battery_pct != null ? String(device.ups_low_battery_pct) : '')` and the same for `highLoad`; when the chosen type is UPS (`(type || device.device_type_detected) === 'ups'`), render after the Faceplate fieldset:

```tsx
{(type || device.device_type_detected) === 'ups' && (
  <fieldset className="space-y-3 rounded-lg border border-white/10 p-3">
    <legend className="px-1 text-sm text-slate-300">UPS alerts</legend>
    <label className="block space-y-1">
      <span className="text-xs text-slate-400">Low battery below (%)</span>
      <input className={inputCls} type="number" min={5} max={95} value={lowBattery} onChange={(e) => setLowBattery(e.target.value)}
        placeholder={upsDefaults ? `Use the default (${upsDefaults.low} %)` : 'Use the default'} />
    </label>
    <label className="block space-y-1">
      <span className="text-xs text-slate-400">High load at (%)</span>
      <input className={inputCls} type="number" min={10} max={100} value={highLoad} onChange={(e) => setHighLoad(e.target.value)}
        placeholder={upsDefaults ? `Use the default (${upsDefaults.high} %)` : 'Use the default'} />
    </label>
    <span className="block text-xs text-slate-500">Leave blank to use the default from Network settings.</span>
  </fieldset>
)}
```

and in `submit` send `ups_low_battery_pct: lowBattery ? Number(lowBattery) : null, ups_high_load_pct: highLoad ? Number(highLoad) : null` — but only when the effective type is UPS (otherwise omit both keys, so saving a switch never touches them).

- [ ] **Step 5: Network settings.** Append to `FIELDS` in `NetworkSettings.tsx`:

```ts
  { key: 'ups_low_battery_pct', label: 'UPS low battery below', help: 'Also alerts whenever the UPS itself reports a low or depleted battery.', min: 5, max: 95, unit: '%' },
  { key: 'ups_high_load_pct', label: 'UPS high load at', help: 'Alerts when load stays at or above this for 5 minutes.', min: 10, max: 100, unit: '%' },
```

- [ ] **Step 6: Incident page.** In `IncidentDetail.tsx` "Detected by", replace the ternary with:

```tsx
{inc.port_if_index != null
  ? `The port's stats poll: ${(inc.condition && CONDITION_LABEL[inc.condition]) || inc.condition}`
  : inc.condition
    ? `The UPS poll: ${CONDITION_LABEL[inc.condition] || inc.condition}`
    : '3 consecutive SNMP polls without an answer'}
```

- [ ] **Step 7: Check**

Run the frontend check from Global Constraints.
Expected: exit 0.

- [ ] **Step 8: Commit**

```bash
git add frontend/src
git commit -m "feat(ups): Power panel, UPS thresholds in settings and Edit details

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Sandbox verification (controller, not dispatched)

**Files:** none (findings become fix tasks with their own tests).

- [ ] **Step 1: Full runs.** `cd backend && go vet ./... && go test ./... 2>&1 | grep -v "^ok\|no test files"; ./scripts/test-db.sh 2>&1 | grep -E "^(FAIL|---|panic)"; SENTINEL_TEST_SNMPSIM=127.0.0.1:1161 go test ./internal/snmp/ -run Sim 2>&1 | tail -1` → no failures, simulator `ok`. Frontend check → exit 0.
- [ ] **Step 2: Deploy the branch to the sandbox** (`git -C /srv/docker/sentinel-dev fetch /home/sysadmin/sentinel-ups feature/ups-monitoring && git -C /srv/docker/sentinel-dev checkout -B feature/ups-monitoring FETCH_HEAD && cd /srv/docker/sentinel-dev && docker compose up -d --build`), confirm `applying migration 055_ups_monitoring.sql` and no `panic` in the backend log.
- [ ] **Step 3: Simulated UPS.** Add a device in the sandbox at the snmpsim container's address with community `ups`; within two minutes its page shows the Power panel (On mains, Charge 96 %, Load 34 %, Runtime 41 min, Input 121 V, Output 120 V, 24 °C) and "Tripp Lite" / "SMART1500RM2UN". To exercise an alert, edit `deploy/snmpsim/data/ups.snmprec` to `1.3.6.1.2.1.33.1.4.1.0|2|5`, restart the simulator, and confirm one "On battery" incident appears on the Incidents page; restore `3` and confirm it closes. Revert the file afterwards.
- [ ] **Step 4: Report** to the user what to check on their real Tripp Lite and Eaton after their work install is rebuilt.
