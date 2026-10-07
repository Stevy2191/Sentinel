package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBSweepPickupTimeout(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	now := e.clock.Now()
	stale := e.queueRun(t, agent, func(r *models.ToolRun) { r.CreatedAt = now.Add(-PickupTimeout - time.Second) })
	fresh := e.queueRun(t, agent, func(r *models.ToolRun) { r.CreatedAt = now.Add(-PickupTimeout + time.Second) })

	testdb.Must(t, e.svc.Sweep(context.Background()))
	got := e.reload(t, stale.ID)
	if got.Status != models.ToolRunFailed || got.Error == nil || *got.Error != "the agent didn't pick up the job (offline?)" {
		t.Errorf("stale queued run = %s %v", got.Status, got.Error)
	}
	if e.reload(t, fresh.ID).Status != models.ToolRunQueued {
		t.Error("a run queued 29 s ago was failed")
	}
	f := e.audit.byAction(models.ActionToolRunFinished)
	if len(f) != 1 || f[0].actor.Username != "system" || f[0].actor.UserID != uuid.Nil {
		t.Errorf("finish audit = %+v, want one entry by the system", f)
	}
}

// Review Focus 1: an agent claims a run and vanishes. At deadline + 15 s the
// sweeper times the run out and any open stream gets its end frame.
func TestDBSweepTimesOutVanishedAgentRun(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	run, err := e.svc.Create(ctx, e.user(t, false), agentPing(agent, "10.0.0.5"))
	testdb.Must(t, err)
	job, err := e.svc.NextJob(ctx, agent, 0)
	testdb.Must(t, err)
	if job == nil {
		t.Fatal("no job")
	}
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()

	e.clock.Add(2*time.Minute + OverdueGrace) // exactly deadline + grace: not yet
	testdb.Must(t, e.svc.Sweep(ctx))
	if e.reload(t, run.ID).Status != models.ToolRunRunning {
		t.Fatal("timed out at deadline + grace exactly, want only after it")
	}
	e.clock.Add(time.Second)
	testdb.Must(t, e.svc.Sweep(ctx))

	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunTimedOut || got.Error == nil || *got.Error != "the run went past its time limit" {
		t.Errorf("run = %s %v, want timed_out", got.Status, got.Error)
	}
	select {
	case m := <-sub.C():
		var end models.ToolRun
		testdb.Must(t, json.Unmarshal(m.Data, &end))
		if m.Event != "end" || end.Status != models.ToolRunTimedOut {
			t.Errorf("frame %s with status %s, want end / timed_out", m.Event, end.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("no end frame reached the open stream")
	}
	if f := e.audit.byAction(models.ActionToolRunFinished); len(f) != 1 || f[0].actor.Username != "system" {
		t.Errorf("finish audit = %+v, want one entry by the system", f)
	}
	// The agent's late posts are refused.
	if _, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1)); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("late events: %v, want ErrRunNotActive", err)
	}
}

func TestDBInterruptStale(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	queued := e.queueRun(t, agent, nil)
	running := e.insertRun(t, nil)
	done := e.insertRun(t, func(r *models.ToolRun) { r.Status = models.ToolRunDone })
	sub := e.svc.Subscribe(running.ID)
	defer sub.Close()

	n, err := e.svc.InterruptStale(context.Background())
	testdb.Must(t, err)
	if n != 2 {
		t.Errorf("interrupted %d runs, want 2", n)
	}
	for _, r := range []*models.ToolRun{queued, running} {
		if got := e.reload(t, r.ID); got.Status != models.ToolRunInterrupted || got.FinishedAt == nil {
			t.Errorf("run %s = %s, want interrupted", r.ID, got.Status)
		}
	}
	if e.reload(t, done.ID).Status != models.ToolRunDone {
		t.Error("a finished run was interrupted")
	}
	select {
	case m := <-sub.C():
		if m.Event != "end" {
			t.Errorf("frame %s, want end", m.Event)
		}
	default:
		t.Error("no end frame for the interrupted run")
	}
	f := e.audit.byAction(models.ActionToolRunFinished)
	if len(f) != 2 {
		t.Fatalf("finish audit = %+v, want two entries", f)
	}
	for _, entry := range f {
		if entry.actor.Username != "system" || entry.changes.Summary["status"] != models.ToolRunInterrupted {
			t.Errorf("finish audit entry = %+v, want interrupted by the system", entry)
		}
	}
}

// Prune deletes finished runs past the retention with their events, and
// nothing else.
func TestDBPrune(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	now := e.clock.Now()
	finishedAt := func(days int) func(r *models.ToolRun) {
		return func(r *models.ToolRun) {
			at := now.AddDate(0, 0, -days)
			r.Status, r.CreatedAt, r.FinishedAt = models.ToolRunDone, at, &at
		}
	}
	old := e.insertRun(t, finishedAt(31))
	recent := e.insertRun(t, finishedAt(29))
	active := e.insertRun(t, func(r *models.ToolRun) { r.CreatedAt = now.AddDate(0, 0, -40) })
	testdb.Exec(t, e.db, `INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, 1, now(), 'reply', '{}')`, old.ID)

	n, err := e.svc.Prune(ctx)
	testdb.Must(t, err)
	if n != 1 {
		t.Errorf("pruned %d, want 1", n)
	}
	var left []string
	testdb.Must(t, e.db.Raw(`SELECT id::text FROM tool_runs ORDER BY created_at`).Scan(&left).Error)
	if len(left) != 2 || left[0] != active.ID.String() || left[1] != recent.ID.String() {
		t.Errorf("left %v, want the active and the recent run", left)
	}
	var events int64
	testdb.Must(t, e.db.Raw(`SELECT count(*) FROM tool_run_events`).Scan(&events).Error)
	if events != 0 {
		t.Errorf("%d events outlived their pruned run", events)
	}

	_, err = SaveSettings(ctx, e.settings, Settings{Allowlist: testAllowlist, ServerEnabled: true, RetentionDays: 7})
	testdb.Must(t, err)
	if n, err := e.svc.Prune(ctx); err != nil || n != 1 {
		t.Errorf("with 7 days: pruned %d, %v; want the 29-day-old run", n, err)
	}
}

// Start interrupts what a previous process left and stops with its context.
func TestDBStart(t *testing.T) {
	e := newEnv(t)
	left := e.insertRun(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { e.svc.Start(ctx); close(stopped) }()
	waitFor(t, "the leftover run to be interrupted", func() bool {
		return e.reload(t, left.ID).Status == models.ToolRunInterrupted
	})
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after its context ended")
	}
}
