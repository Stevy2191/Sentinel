package toolruns

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBVantages(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.newAgent(t, "a-too-old", true, nil)
	e.newAgent(t, "b-tools-off", false, boolPtr(true))
	e.newAgent(t, "C-not-on-server", true, boolPtr(false))
	e.newAgent(t, "d-offline", true, boolPtr(true))
	ready := e.readyAgent(t, "e-ready")
	stale := e.readyAgent(t, "f-stale")
	e.clock.Add(ReadyWindow)
	e.svc.polls.seen(ready.ID, e.clock.Now())
	e.clock.Add(time.Second) // f-stale's poll is now 61 s old; e-ready's 1 s

	got, err := e.svc.Vantages(ctx)
	testdb.Must(t, err)
	want := []VantageView{
		{Kind: models.VantageSentinel, Name: "Sentinel", Ready: true},
		{Kind: models.VantageAgent, Name: "a-too-old", Reason: ReasonAgentTooOld},
		{Kind: models.VantageAgent, Name: "b-tools-off", Reason: ReasonToolsOff},
		{Kind: models.VantageAgent, Name: "C-not-on-server", Reason: ReasonNotEnabledOnServer},
		{Kind: models.VantageAgent, Name: "d-offline", Reason: ReasonOffline},
		{Kind: models.VantageAgent, Name: "e-ready", Ready: true},
		{Kind: models.VantageAgent, Name: "f-stale", Reason: ReasonOffline},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d vantages, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		g := got[i]
		g.AgentID = ""
		if g != want[i] {
			t.Errorf("vantage %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got[5].AgentID != ready.AgentID || got[6].AgentID != stale.AgentID {
		t.Errorf("agent ids %q %q, want %q %q", got[5].AgentID, got[6].AgentID, ready.AgentID, stale.AgentID)
	}

	_, err = SaveSettings(ctx, e.settings, Settings{Allowlist: []string{}, ServerEnabled: false, RetentionDays: 30})
	testdb.Must(t, err)
	got, err = e.svc.Vantages(ctx)
	testdb.Must(t, err)
	if got[0].Ready || got[0].Reason != ReasonServerDisabled {
		t.Errorf("sentinel with the server off = %+v", got[0])
	}
	if !e.svc.AllowlistEmpty(ctx) {
		t.Error("AllowlistEmpty false with an empty list")
	}
	_, err = SaveSettings(ctx, e.settings, Settings{Allowlist: []string{"10.0.0.0/24"}, ServerEnabled: true, RetentionDays: 30})
	testdb.Must(t, err)
	if e.svc.AllowlistEmpty(ctx) {
		t.Error("AllowlistEmpty true with an entry")
	}
}
