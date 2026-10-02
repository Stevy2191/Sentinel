package custommetric

import (
	"testing"
	"time"
)

var r0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestNotOKStartsAndClears(t *testing.T) {
	r := Rule{Kind: "not_ok", Enabled: true}
	st, ch := EvalRule(r, "status", RuleState{}, []Row{{Instance: "2", Label: "Fan 2", State: "critical"}}, r0)
	if len(ch) != 1 || !ch[0].Started || ch[0].State != "critical" || ch[0].Label != "Fan 2" {
		t.Fatalf("start %+v", ch)
	}
	st, ch = EvalRule(r, "status", st, []Row{}, r0.Add(time.Minute)) // row missing: keep
	if len(ch) != 0 || st.Active["2"].IsZero() {
		t.Fatalf("missing row changed state %+v", ch)
	}
	_, ch = EvalRule(r, "status", st, []Row{{Instance: "2", State: "normal", OK: true}}, r0.Add(2*time.Minute))
	if len(ch) != 1 || ch[0].Started {
		t.Fatalf("clear %+v", ch)
	}
}

func TestAboveWithHold(t *testing.T) {
	r := Rule{Kind: "above", Value: 90, Hold: 10 * time.Minute, Enabled: true}
	st := RuleState{}
	var ch []Change
	for m := 0; m < 10; m++ {
		st, ch = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 95}}, r0.Add(time.Duration(m)*time.Minute))
		if len(ch) != 0 {
			t.Fatalf("minute %d started early", m)
		}
	}
	st, ch = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 95}}, r0.Add(10*time.Minute))
	if len(ch) != 1 || !ch[0].Started || ch[0].Value != 95 {
		t.Fatalf("start %+v", ch)
	}
	_, ch = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 90}}, r0.Add(11*time.Minute)) // 90 is not above 90
	if len(ch) != 1 || ch[0].Started {
		t.Fatalf("clear %+v", ch)
	}
}

func TestBelowAndDipResets(t *testing.T) {
	r := Rule{Kind: "below", Value: 10, Hold: 5 * time.Minute, Enabled: true}
	st, _ := EvalRule(r, "gauge", RuleState{}, []Row{{Instance: "1", Value: 5}}, r0)
	st, _ = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 50}}, r0.Add(4*time.Minute))
	st, ch := EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 5}}, r0.Add(6*time.Minute))
	if len(ch) != 0 {
		t.Fatalf("dip did not reset %+v", ch)
	}
	if _, ch = EvalRule(r, "gauge", st, []Row{{Instance: "1", Value: 5}}, r0.Add(11*time.Minute)); len(ch) != 1 {
		t.Fatalf("no start after hold %+v", ch)
	}
}

func TestDisabledRuleNeverFires(t *testing.T) {
	_, ch := EvalRule(Rule{Kind: "not_ok"}, "status", RuleState{}, []Row{{Instance: "1", State: "critical"}}, r0)
	if len(ch) != 0 {
		t.Fatalf("disabled rule fired %+v", ch)
	}
}

func TestEvalRuleDoesNotMutatePrev(t *testing.T) {
	prev := RuleState{Active: map[string]time.Time{}, Since: map[string]time.Time{}}
	EvalRule(Rule{Kind: "not_ok", Enabled: true}, "status", prev, []Row{{Instance: "1", State: "x"}}, r0)
	if len(prev.Active) != 0 || len(prev.Since) != 0 {
		t.Error("prev mutated")
	}
}
