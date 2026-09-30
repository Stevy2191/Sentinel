package portmon

import (
	"testing"
	"time"
)

var th = Thresholds{ErrorsPerMin: 10, UtilPct: 80, DownGrace: 2 * time.Minute}

func upObs(at time.Time) Observation {
	return Observation{At: at, OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500, HaveRates: true, UtilPct: 5}
}

func upTracker() *Tracker {
	return NewTracker(Snapshot{OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500})
}

func changes(r Result, c Condition, started bool) int {
	n := 0
	for _, ch := range r.Changes {
		if ch.Condition == c && ch.Started == started {
			n++
		}
	}
	return n
}

func events(r Result, kind string) int {
	n := 0
	for _, e := range r.Events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func TestTrackerErrorsNeedFivePollsBothWays(t *testing.T) {
	tr := upTracker()
	at := t0
	for i := 0; i < 4; i++ {
		o := upObs(at)
		o.ErrorsPerMin = 50
		if r := tr.Observe(o, th, 0); changes(r, Errors, true) != 0 {
			t.Fatalf("errors started after %d polls", i+1)
		}
		at = at.Add(time.Minute)
	}
	o := upObs(at)
	o.ErrorsPerMin = 50
	if r := tr.Observe(o, th, 0); changes(r, Errors, true) != 1 {
		t.Fatal("errors did not start on the 5th poll at threshold")
	}
	for i := 0; i < 4; i++ {
		at = at.Add(time.Minute)
		if r := tr.Observe(upObs(at), th, 0); changes(r, Errors, false) != 0 {
			t.Fatalf("errors ended after %d quiet polls", i+1)
		}
	}
	at = at.Add(time.Minute)
	if r := tr.Observe(upObs(at), th, 0); changes(r, Errors, false) != 1 {
		t.Fatal("errors did not end after 5 quiet polls")
	}
}

func TestTrackerSaturationHysteresis(t *testing.T) {
	tr := upTracker()
	at := t0
	obs := func(util float64) Result {
		at = at.Add(time.Minute)
		o := upObs(at)
		o.UtilPct = util
		return tr.Observe(o, th, 0)
	}
	for i := 0; i < 4; i++ {
		if r := obs(90); changes(r, Saturated, true) != 0 {
			t.Fatal("saturated before 5 polls")
		}
	}
	if r := obs(90); changes(r, Saturated, true) != 1 {
		t.Fatal("saturated did not start")
	}
	// Averages between 70 and 80 keep it on.
	for i := 0; i < 5; i++ {
		if r := obs(72); changes(r, Saturated, false) != 0 {
			t.Fatal("saturated ended above the 70% clear line")
		}
	}
	if r := obs(20); changes(r, Saturated, false) != 1 {
		t.Fatal("saturated did not end once the 5-poll average fell below 70%")
	}
}

func TestTrackerFlapping(t *testing.T) {
	tr := upTracker()
	down := func(at time.Time) Observation { o := upObs(at); o.OperUp = false; o.HaveRates = false; return o }
	r1 := tr.Observe(down(t0), th, 0)
	r2 := tr.Observe(upObs(t0.Add(time.Minute)), th, 0)
	if events(r1, "link_down") != 1 || events(r2, "link_up") != 1 {
		t.Fatal("first two transitions should log link events")
	}
	r3 := tr.Observe(down(t0.Add(2*time.Minute)), th, 0)
	if changes(r3, Flapping, true) != 1 {
		t.Fatal("third transition within 10 minutes should start flapping")
	}
	// Still bouncing: transitions while flapping are grouped into the flap,
	// not logged one by one.
	if r := tr.Observe(upObs(t0.Add(3*time.Minute)), th, 0); len(r.Events) != 0 {
		t.Fatalf("events while flapping: %+v", r.Events)
	}
	// 10 quiet minutes after the last transition end it, with the count.
	r := tr.Observe(upObs(t0.Add(13*time.Minute+time.Second)), th, 0)
	if changes(r, Flapping, false) != 1 {
		t.Fatal("flapping did not end after 10 quiet minutes")
	}
	for _, c := range r.Changes {
		if c.Condition == Flapping && c.Detail["transitions"] != 4 {
			t.Errorf("flap count %v, want 4", c.Detail["transitions"])
		}
	}
}

// A down-and-up between two polls is invisible in the status but moves
// ifLastChange: it counts as two transitions.
func TestTrackerBounceBetweenPolls(t *testing.T) {
	tr := upTracker()
	o := upObs(t0)
	o.LastChangeSeconds = 900
	r := tr.Observe(o, th, 0)
	if events(r, "link_down") != 1 || events(r, "link_up") != 1 {
		t.Fatalf("hidden bounce events: %+v", r.Events)
	}
	o = upObs(t0.Add(time.Minute))
	o.LastChangeSeconds = 960
	if r := tr.Observe(o, th, 0); changes(r, Flapping, true) != 1 {
		t.Fatal("two hidden bounces (4 transitions) should start flapping")
	}
	// Unknown last-change (e.g. the device restarted) is not a bounce.
	tr = upTracker()
	o = upObs(t0)
	o.LastChangeSeconds = -1
	if r := tr.Observe(o, th, 0); len(r.Events) != 0 {
		t.Fatalf("unknown last change produced events: %+v", r.Events)
	}
}

func TestTrackerLinkDownGrace(t *testing.T) {
	tr := upTracker()
	down := func(at time.Time) Observation { o := upObs(at); o.OperUp = false; o.HaveRates = false; return o }
	if r := tr.Observe(down(t0), th, 0); changes(r, LinkDown, true) != 0 {
		t.Fatal("link_down before the grace period")
	}
	if r := tr.Observe(down(t0.Add(time.Minute)), th, 0); changes(r, LinkDown, true) != 0 {
		t.Fatal("link_down at 1 minute")
	}
	if r := tr.Observe(down(t0.Add(2*time.Minute)), th, 0); changes(r, LinkDown, true) != 1 {
		t.Fatal("link_down not started at the grace period")
	}
	if r := tr.Observe(upObs(t0.Add(3*time.Minute)), th, 0); changes(r, LinkDown, false) != 1 {
		t.Fatal("link_down not ended on link up")
	}
	// An administratively disabled port is not "down".
	tr = upTracker()
	o := down(t0)
	o.AdminUp = false
	r := tr.Observe(o, th, 0)
	o.At = t0.Add(5 * time.Minute)
	r2 := tr.Observe(o, th, 0)
	if changes(r, LinkDown, true)+changes(r2, LinkDown, true) != 0 || events(r, "admin_down") != 1 {
		t.Fatal("admin-down port should log admin_down and never link_down")
	}
}

// A port first seen already down counts its grace from the first sighting.
func TestTrackerFreshPortSeenDown(t *testing.T) {
	tr := NewTracker(Snapshot{OperUp: false, AdminUp: true, LastChangeSeconds: -1})
	down := Observation{At: t0, AdminUp: true, LastChangeSeconds: 100, UtilPct: -1}
	tr.Observe(down, th, 0)
	down.At = t0.Add(2 * time.Minute)
	if r := tr.Observe(down, th, 0); changes(r, LinkDown, true) != 1 {
		t.Fatal("fresh down port never reached link_down")
	}
}

func TestTrackerSlowLinkAndSpeedChange(t *testing.T) {
	tr := upTracker()
	o := upObs(t0)
	o.SpeedBps = 100_000_000
	r := tr.Observe(o, th, 1e9)
	if changes(r, SlowLink, true) != 1 || events(r, "speed_change") != 1 {
		t.Fatalf("slow link: %+v", r)
	}
	if r := tr.Observe(upObs(t0.Add(time.Minute)), th, 1e9); changes(r, SlowLink, false) != 1 {
		t.Fatal("slow_link did not end at the usual speed")
	}
	// No usual speed yet (under 24 h of history): never slow.
	tr = upTracker()
	if r := tr.Observe(o, th, 0); changes(r, SlowLink, true) != 0 {
		t.Fatal("slow_link without a usual speed")
	}
}

func TestTrackerLinkDownEndsTrafficConditions(t *testing.T) {
	tr := NewTracker(Snapshot{OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500,
		Active: map[Condition]time.Time{Errors: t0, Saturated: t0}})
	o := upObs(t0.Add(time.Minute))
	o.OperUp, o.HaveRates = false, false
	r := tr.Observe(o, th, 0)
	if changes(r, Errors, false) != 1 || changes(r, Saturated, false) != 1 {
		t.Fatalf("link down should end traffic conditions: %+v", r.Changes)
	}
}

// Restored after a restart with errors active: one quiet poll neither ends it
// nor starts it again (Review Focus 5).
func TestTrackerRestoredDoesNotRestartOrEnd(t *testing.T) {
	since := t0.Add(-time.Hour)
	tr := NewTracker(Snapshot{OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500,
		Active: map[Condition]time.Time{Errors: since}})
	o := upObs(t0)
	o.ErrorsPerMin = 50
	r := tr.Observe(o, th, 0)
	if len(r.Changes) != 0 {
		t.Fatalf("restored tracker changed state on its first poll: %+v", r.Changes)
	}
	if got := tr.Active()[Errors]; !got.Equal(since) {
		t.Errorf("since = %v, want %v", got, since)
	}
}

// Restored with Flapping active: there is no transition history to prune
// against, so the flap needs its own quiet window measured from the first
// poll after the restore, not an immediate end for lack of transitions
// (Review Focus 5).
func TestTrackerRestoredFlappingNeedsQuietWindow(t *testing.T) {
	tr := NewTracker(Snapshot{OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500,
		Active: map[Condition]time.Time{Flapping: t0.Add(-5 * time.Minute)}})
	if r := tr.Observe(upObs(t0), th, 0); len(r.Changes) != 0 {
		t.Fatalf("restored flap changed state on its first poll: %+v", r.Changes)
	}
	if r := tr.Observe(upObs(t0.Add(9*time.Minute)), th, 0); len(r.Changes) != 0 {
		t.Fatalf("restored flap ended before its own quiet window passed: %+v", r.Changes)
	}
	r := tr.Observe(upObs(t0.Add(10*time.Minute+time.Second)), th, 0)
	if changes(r, Flapping, false) != 1 {
		t.Fatal("restored flap did not end once its own quiet window passed")
	}
	for _, c := range r.Changes {
		if c.Condition == Flapping {
			if _, ok := c.Detail["transitions"]; ok {
				t.Errorf("restored flap end reported a transitions count it never had: %v", c.Detail)
			}
		}
	}
}

// Restored with Flapping active, and the very first post-restore poll itself
// carries a real transition (a hidden bounce via LastChangeSeconds, here).
// The one-shot anchor must still be planted on that first poll, not
// re-armed once that transition ages out of the window: the flap should end
// at the first poll 10 minutes after the first poll's transition, not
// ~10 minutes later than that.
func TestTrackerRestoredFlappingWithBounceOnFirstPoll(t *testing.T) {
	tr := NewTracker(Snapshot{OperUp: true, AdminUp: true, SpeedBps: 1e9, LastChangeSeconds: 500,
		Active: map[Condition]time.Time{Flapping: t0.Add(-5 * time.Minute)}})
	// LastChangeSeconds moves from the snapshot's 500 to 560 on the first
	// poll (the hidden bounce), then holds steady at 560: quiet from there.
	quiet := func(at time.Time) Observation { o := upObs(at); o.LastChangeSeconds = 560; return o }
	if r := tr.Observe(quiet(t0), th, 0); len(r.Changes) != 0 {
		t.Fatalf("restored flap changed state on its first (bouncing) poll: %+v", r.Changes)
	}
	if r := tr.Observe(quiet(t0.Add(9*time.Minute+59*time.Second)), th, 0); len(r.Changes) != 0 {
		t.Fatalf("restored flap ended before 10 minutes past its first poll's transition: %+v", r.Changes)
	}
	r := tr.Observe(quiet(t0.Add(10*time.Minute+time.Second)), th, 0)
	if changes(r, Flapping, false) != 1 {
		t.Fatal("restored flap did not end 10 minutes after its first poll's transition (anchor was re-armed instead)")
	}
	for _, c := range r.Changes {
		if c.Condition == Flapping {
			if _, ok := c.Detail["transitions"]; ok {
				t.Errorf("restored flap end reported a transitions count it never had: %v", c.Detail)
			}
		}
	}
}
