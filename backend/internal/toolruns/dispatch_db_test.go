package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// queueRun inserts a queued run for agent, created at the clock's now.
func (e *env) queueRun(t *testing.T, agent *models.Agent, mutate func(r *models.ToolRun)) *models.ToolRun {
	t.Helper()
	return e.insertRun(t, func(r *models.ToolRun) {
		r.Status, r.StartedAt = models.ToolRunQueued, nil
		r.VantageKind, r.AgentUUID, r.AgentRef, r.VantageName = models.VantageAgent, &agent.ID, &agent.AgentID, agent.Name
		r.Params = models.RawJSON(`{"count":3,"interval_ms":500,"timeout_ms":1000,"size":56}`)
		if mutate != nil {
			mutate(r)
		}
	})
}

// A claimed job carries the stored spec, the run moves to running with a
// fresh deadline, and the stream hears about it.
func TestDBNextJobClaims(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	older := e.queueRun(t, agent, func(r *models.ToolRun) { r.CreatedAt = r.CreatedAt.Add(-time.Second) })
	newer := e.queueRun(t, agent, nil)
	sub := e.svc.Subscribe(older.ID)
	defer sub.Close()
	e.clock.Add(5 * time.Second)
	claimedAt := e.clock.Now()

	job, err := e.svc.NextJob(ctx, agent, time.Second)
	testdb.Must(t, err)
	if job == nil || job.RunID != older.ID {
		t.Fatalf("job = %+v, want the older run %s", job, older.ID)
	}
	if job.Spec.Tool != nettools.ToolPing || job.Spec.Target != "10.0.0.200" || job.Spec.TargetIP != "10.0.0.200" ||
		job.Spec.Params.Count != 3 || job.Spec.Params.IntervalMS != 500 || !job.Deadline.Equal(claimedAt.Add(2*time.Minute)) {
		t.Errorf("job = %+v, want the stored ping spec and deadline claim+2m", job)
	}
	got := e.reload(t, older.ID)
	if got.Status != models.ToolRunRunning || got.StartedAt == nil || !got.StartedAt.Equal(claimedAt) || !got.Deadline.Equal(job.Deadline) {
		t.Errorf("claimed run = %+v", got)
	}
	select {
	case m := <-sub.C():
		if m.Event != "status" || string(m.Data) != `{"status":"running"}` {
			t.Errorf("frame %s %s, want status running", m.Event, m.Data)
		}
	default:
		t.Error("no status frame")
	}
	if e.reload(t, newer.ID).Status != models.ToolRunQueued {
		t.Error("the newer run was claimed too")
	}
}

// Two polls racing for one queued run: exactly one gets it, every time.
func TestDBNextJobClaimRace(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	for round := 0; round < 20; round++ {
		run := e.queueRun(t, agent, nil)
		start := make(chan struct{})
		var wg sync.WaitGroup
		jobs := make([]*Job, 2)
		errs := make([]error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				jobs[i], errs[i] = e.svc.NextJob(context.Background(), agent, 0)
			}()
		}
		close(start)
		wg.Wait()
		testdb.Must(t, errors.Join(errs...))
		claimed := 0
		for _, j := range jobs {
			if j != nil {
				claimed++
				if j.RunID != run.ID {
					t.Fatalf("round %d claimed %s, want %s", round, j.RunID, run.ID)
				}
			}
		}
		if claimed != 1 {
			t.Fatalf("round %d: %d polls claimed the run, want exactly 1", round, claimed)
		}
		testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'done' WHERE id = ?`, run.ID)
	}
}

// A waiting poll answers as soon as Create queues a run for its agent.
func TestDBNextJobWakesOnCreate(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	type result struct {
		job *Job
		err error
		at  time.Time
	}
	done := make(chan result, 1)
	go func() {
		job, err := e.svc.NextJob(context.Background(), agent, 2*time.Second)
		done <- result{job, err, time.Now()}
	}()
	time.Sleep(100 * time.Millisecond) // let the poll start waiting
	select {
	case r := <-done:
		t.Fatalf("the poll returned before any run existed: %+v", r)
	default:
	}
	created := time.Now()
	run, err := e.svc.Create(context.Background(), e.user(t, false), agentPing(agent, "10.0.0.5"))
	testdb.Must(t, err)
	r := <-done
	testdb.Must(t, r.err)
	if r.job == nil || r.job.RunID != run.ID {
		t.Fatalf("job = %+v, want run %s", r.job, run.ID)
	}
	if waited := r.at.Sub(created); waited > 500*time.Millisecond {
		t.Errorf("the poll answered %v after the create, want well under the 2 s wait", waited)
	}
}

func TestDBNextJobTimesOutAndRecordsPolls(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.newAgent(t, "file-server", true, boolPtr(true))
	start := time.Now()
	job, err := e.svc.NextJob(ctx, agent, 150*time.Millisecond)
	if job != nil || err != nil || time.Since(start) < 150*time.Millisecond {
		t.Errorf("empty queue: %+v, %v after %v; want nil, nil after the full wait", job, err, time.Since(start))
	}
	if _, ok := e.svc.polls.lastSeen(agent.ID); !ok {
		t.Error("the poll was not recorded")
	}

	off := e.newAgent(t, "off-box", false, boolPtr(true))
	if _, err := e.svc.NextJob(ctx, off, time.Second); !errors.Is(err, ErrToolsDisabled) {
		t.Errorf("tools off: %v, want ErrToolsDisabled", err)
	}
	if _, ok := e.svc.polls.lastSeen(off.ID); !ok {
		t.Error("a refused poll was not recorded")
	}

	// A hung-up agent ends the wait at once.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if job, err := e.svc.NextJob(cctx, agent, 5*time.Second); job != nil || err != nil {
		t.Errorf("cancelled poll: %+v, %v", job, err)
	}
}

func agentEvents(seqs ...int) []AgentEvent {
	out := make([]AgentEvent, len(seqs))
	for i, s := range seqs {
		out[i] = AgentEvent{Seq: s, At: time.Date(2026, 10, 5, 12, 0, s, 0, time.UTC), Type: "reply",
			Data: json.RawMessage(fmt.Sprintf(`{"seq":%d}`, s))}
	}
	return out
}

func TestDBAgentEvents(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	other := e.readyAgent(t, "other-box")
	run := e.queueRun(t, agent, func(r *models.ToolRun) { r.Status = models.ToolRunRunning })
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()

	cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1, 2))
	if cancel || err != nil {
		t.Fatalf("first batch: cancel %v, %v", cancel, err)
	}
	// A retried batch is ignored.
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1, 2)); cancel || err != nil {
		t.Fatalf("retried batch: cancel %v, %v", cancel, err)
	}
	if got := e.reload(t, run.ID).EventCount; got != 2 {
		t.Errorf("event_count = %d after a retried batch, want 2", got)
	}
	stored, err := e.svc.Events(ctx, run.ID, 0)
	testdb.Must(t, err)
	if len(stored) != 2 || !stored[1].At.Equal(time.Date(2026, 10, 5, 12, 0, 2, 0, time.UTC)) {
		t.Errorf("stored = %+v, want 2 events at the agent's times", stored)
	}
	for _, want := range []string{"1", "2"} {
		if m := <-sub.C(); m.Event != "event" || m.ID != want {
			t.Errorf("frame %s %s, want event %s", m.Event, m.ID, want)
		}
	}

	if _, err := e.svc.AgentEvents(ctx, other, run.ID, agentEvents(3)); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("another agent's post: %v, want ErrRunNotActive", err)
	}
	if _, err := e.svc.AgentEvents(ctx, agent, uuid.New(), agentEvents(3)); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("an unknown run: %v, want ErrRunNotActive", err)
	}

	// Cancelled: tell the agent, store nothing.
	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'cancelled' WHERE id = ?`, run.ID)
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(3)); !cancel || err != nil {
		t.Errorf("cancelled run: cancel %v, %v; want true, nil", cancel, err)
	}
	if got := e.reload(t, run.ID).EventCount; got != 2 {
		t.Errorf("a cancelled run stored events: %d", got)
	}
	// Ended any other way: 409.
	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'interrupted' WHERE id = ?`, run.ID)
	if _, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(3)); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("interrupted run: %v, want ErrRunNotActive", err)
	}
}

// An empty batch is the agent asking whether to stop: nothing is stored or
// published, and the answer is the cancel flag (or 409 once the run ended
// another way). Even a run already at the event cap is not failed by it.
func TestDBAgentEventsEmptyBatch(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	run := e.queueRun(t, agent, func(r *models.ToolRun) { r.Status, r.EventCount = models.ToolRunRunning, MaxEventsPerRun })
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()

	for _, batch := range [][]AgentEvent{nil, {}} {
		if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, batch); cancel || err != nil {
			t.Errorf("empty batch %#v on a running run: cancel %v, %v; want false, nil", batch, cancel, err)
		}
	}
	if got := e.reload(t, run.ID); got.Status != models.ToolRunRunning || got.EventCount != MaxEventsPerRun {
		t.Errorf("run = %s with %d events after empty batches, want still running, unchanged", got.Status, got.EventCount)
	}
	select {
	case m := <-sub.C():
		t.Errorf("an empty batch published %s %s", m.Event, m.Data)
	default:
	}

	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'cancelled' WHERE id = ?`, run.ID)
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, nil); !cancel || err != nil {
		t.Errorf("empty batch on a cancelled run: cancel %v, %v; want true, nil", cancel, err)
	}
	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'timed_out' WHERE id = ?`, run.ID)
	if _, err := e.svc.AgentEvents(ctx, agent, run.ID, nil); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("empty batch on a timed-out run: %v, want ErrRunNotActive", err)
	}
}

// Within one batch a repeated seq keeps its first copy, in the table and in
// the published frame alike; events with a seq below 1 or no type are
// dropped.
func TestDBAgentEventsDuplicateSeqKeepsFirst(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	run := e.queueRun(t, agent, func(r *models.ToolRun) { r.Status = models.ToolRunRunning })
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()
	at := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	batch := []AgentEvent{
		{Seq: 1, At: at, Type: "reply", Data: json.RawMessage(`{"copy":"one"}`)},
		{Seq: 2, At: at, Type: "reply", Data: json.RawMessage(`{"copy":"first"}`)},
		{Seq: 2, At: at, Type: "timeout", Data: json.RawMessage(`{"copy":"second"}`)},
		{Seq: 0, At: at, Type: "reply", Data: json.RawMessage(`{"copy":"seq zero"}`)},
		{Seq: 3, At: at, Type: "", Data: json.RawMessage(`{"copy":"no type"}`)},
	}
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, batch); cancel || err != nil {
		t.Fatalf("cancel %v, %v", cancel, err)
	}
	stored, err := e.svc.Events(ctx, run.ID, 0)
	testdb.Must(t, err)
	if len(stored) != 2 || stored[1].Seq != 2 || stored[1].Type != "reply" || !strings.Contains(string(stored[1].Data), `"first"`) {
		t.Fatalf("stored = %+v, want seqs 1 and 2, seq 2 the first copy", stored)
	}
	if got := e.reload(t, run.ID).EventCount; got != 2 {
		t.Errorf("event_count = %d, want 2", got)
	}
	for i, want := range []string{`"one"`, `"first"`} {
		select {
		case m := <-sub.C():
			var ev models.ToolRunEvent
			testdb.Must(t, json.Unmarshal(m.Data, &ev))
			if m.Event != "event" || ev.Seq != i+1 || !strings.Contains(string(ev.Data), want) {
				t.Errorf("frame %d = %s %s, want event %d carrying %s", i, m.Event, m.Data, i+1, want)
			}
		default:
			t.Fatalf("frame %d missing", i)
		}
	}
	select {
	case m := <-sub.C():
		t.Errorf("an extra frame: %s %s", m.Event, m.Data)
	default:
	}
}

// A batch that would pass MaxEventsPerRun fails the run.
func TestDBAgentEventsCap(t *testing.T) {
	e := newEnv(t)
	agent := e.readyAgent(t, "file-server")
	run := e.queueRun(t, agent, func(r *models.ToolRun) { r.Status, r.EventCount = models.ToolRunRunning, MaxEventsPerRun-1 })
	if _, err := e.svc.AgentEvents(context.Background(), agent, run.ID, agentEvents(1, 2)); !errors.Is(err, ErrTooMuchOutput) {
		t.Fatalf("err = %v, want ErrTooMuchOutput", err)
	}
	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunFailed || got.Error == nil || *got.Error != "too much output" || got.EventCount != MaxEventsPerRun-1 {
		t.Errorf("run = %s %v %d, want failed, too much output, nothing stored", got.Status, got.Error, got.EventCount)
	}
}

// A retried batch that is already stored does not count against the cap: a
// run that reached exactly MaxEventsPerRun survives the agent resending its
// last batch, and only a genuinely new event past the cap fails it.
func TestDBAgentEventsRetryAtTheCap(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	run := e.queueRun(t, agent, func(r *models.ToolRun) { r.Status, r.EventCount = models.ToolRunRunning, MaxEventsPerRun-2 })
	if _, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1, 2)); err != nil {
		t.Fatalf("the batch reaching the cap: %v", err)
	}
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1, 2)); cancel || err != nil {
		t.Fatalf("the same batch retried: cancel %v, %v; want false, nil", cancel, err)
	}
	if got := e.reload(t, run.ID); got.Status != models.ToolRunRunning || got.EventCount != MaxEventsPerRun {
		t.Fatalf("run = %s with %d events, want running with %d", got.Status, got.EventCount, MaxEventsPerRun)
	}
	if _, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(2, 3)); !errors.Is(err, ErrTooMuchOutput) {
		t.Errorf("one new event past the cap: %v, want ErrTooMuchOutput", err)
	}
	if got := e.reload(t, run.ID); got.Status != models.ToolRunFailed || got.EventCount != MaxEventsPerRun {
		t.Errorf("run = %s with %d events, want failed with %d", got.Status, got.EventCount, MaxEventsPerRun)
	}
}

func TestDBAgentFinish(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	other := e.readyAgent(t, "other-box")
	running := func() *models.ToolRun {
		return e.queueRun(t, agent, func(r *models.ToolRun) { r.Status = models.ToolRunRunning })
	}

	run := running()
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()
	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunDone,
		Summary: json.RawMessage(`{"sent":3,"received":3,"loss_pct":0,"avg_ms":1.5}`)}))
	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunDone || got.Summary == nil || got.FinishedAt == nil || got.Error != nil {
		t.Errorf("finished run = %+v", got)
	}
	if m := <-sub.C(); m.Event != "end" || !strings.Contains(string(m.Data), `"status":"done"`) {
		t.Errorf("frame %s %s, want end with status done", m.Event, m.Data)
	}
	if f := e.audit.byAction(models.ActionToolRunFinished); len(f) != 1 || f[0].changes.Summary["result"] != "0% loss, 1.5 ms avg" {
		t.Errorf("finish audit = %+v", f)
	}

	// Review Focus 5: cancelled while the agent ran; the late finish keeps
	// it cancelled and only fills the summary.
	run = running()
	testdb.Exec(t, e.db, `UPDATE tool_runs SET status = 'cancelled', finished_at = now() WHERE id = ?`, run.ID)
	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunDone,
		Summary: json.RawMessage(`{"sent":1}`)}))
	got = e.reload(t, run.ID)
	if got.Status != models.ToolRunCancelled || got.Summary == nil || got.Error != nil {
		t.Errorf("finish after cancel = %+v, want still cancelled, summary filled, no error", got)
	}

	run = running()
	if err := e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunCancelled}); !errors.Is(err, ErrBadFinishStatus) {
		t.Errorf("status cancelled from an agent: %v, want ErrBadFinishStatus", err)
	}
	if err := e.svc.AgentFinish(ctx, other, run.ID, AgentFinish{Status: models.ToolRunDone}); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("another agent's finish: %v, want ErrRunNotActive", err)
	}
	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunRefused,
		Error: "10.0.0.200 is outside TOOLS_ALLOWED_TARGETS" + strings.Repeat("!", 600)}))
	if got := e.reload(t, run.ID); got.Status != models.ToolRunRefused || got.Error == nil || len(*got.Error) != 500 {
		t.Errorf("refused finish = %+v, want refused with the error clipped to 500 bytes", got)
	}
	if err := e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunDone}); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("a second finish: %v, want ErrRunNotActive", err)
	}
	queued := e.queueRun(t, agent, nil)
	if err := e.svc.AgentFinish(ctx, agent, queued.ID, AgentFinish{Status: models.ToolRunDone}); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("finishing an unclaimed run: %v, want ErrRunNotActive", err)
	}
	if got := e.reload(t, queued.ID); got.Status != models.ToolRunQueued {
		t.Errorf("an unclaimed run finished by the agent is %s, want still queued", got.Status)
	}
	for _, bad := range []string{"", models.ToolRunInterrupted, models.ToolRunRunning, models.ToolRunQueued, "DONE"} {
		if err := e.svc.AgentFinish(ctx, agent, uuid.New(), AgentFinish{Status: bad}); !errors.Is(err, ErrBadFinishStatus) {
			t.Errorf("status %q: %v, want ErrBadFinishStatus", bad, err)
		}
	}
}

// An agent whose tool hit the job deadline finishes timed_out, like a
// Sentinel run would: partial summary kept, the same error text when the
// agent sends none, an end frame and the owner's audit entry. A late finish
// on a run the sweeper already timed out only fills the summary.
func TestDBAgentFinishTimedOut(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	run := e.queueRun(t, agent, func(r *models.ToolRun) { r.Status, r.Username = models.ToolRunRunning, "alice" })
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()

	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunTimedOut,
		Summary: json.RawMessage(`{"sent":2,"received":2,"loss_pct":0,"avg_ms":1}`)}))
	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunTimedOut || got.Summary == nil || got.FinishedAt == nil ||
		got.Error == nil || *got.Error != "the run went past its time limit" {
		t.Errorf("timed-out finish = %+v (error %v), want timed_out, summary kept, the Sentinel run's error text", got, got.Error)
	}
	select {
	case m := <-sub.C():
		var end models.ToolRun
		testdb.Must(t, json.Unmarshal(m.Data, &end))
		if m.Event != "end" || end.Status != models.ToolRunTimedOut {
			t.Errorf("frame %s with status %s, want end / timed_out", m.Event, end.Status)
		}
	default:
		t.Error("no end frame")
	}
	if f := e.audit.byAction(models.ActionToolRunFinished); len(f) != 1 || f[0].actor.Username != "alice" ||
		f[0].changes.Summary["status"] != models.ToolRunTimedOut {
		t.Errorf("finish audit = %+v, want one timed_out entry by the run's owner", f)
	}

	// The agent's own error text, when it sends one, is kept.
	run = e.queueRun(t, agent, func(r *models.ToolRun) { r.Status = models.ToolRunRunning })
	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunTimedOut, Error: "stopped at the deadline"}))
	if got := e.reload(t, run.ID); got.Status != models.ToolRunTimedOut || got.Error == nil || *got.Error != "stopped at the deadline" {
		t.Errorf("timed-out finish with text = %s %v", got.Status, got.Error)
	}

	// The sweeper got there first: the agent's finish only fills the summary.
	run = e.queueRun(t, agent, func(r *models.ToolRun) { r.Status = models.ToolRunRunning })
	_, err := e.svc.finish(ctx, run.ID, models.ToolRunTimedOut, nil, msgTimedOut, systemActor)
	testdb.Must(t, err)
	if err := e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunTimedOut,
		Summary: json.RawMessage(`{"sent":1}`), Error: "late"}); !errors.Is(err, ErrRunNotActive) {
		t.Errorf("a finish after the sweeper's: %v, want ErrRunNotActive", err)
	}
	if got := e.reload(t, run.ID); got.Status != models.ToolRunTimedOut || got.Summary == nil ||
		got.Error == nil || *got.Error != "the run went past its time limit" {
		t.Errorf("run = %+v, want the sweeper's timed_out kept, summary filled", got)
	}
}

// Review Focus 5 end to end: the user cancels while the agent runs; the
// agent's next post is told to stop and its finish cannot undo the cancel.
func TestDBCancelAgentRun(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	agent := e.readyAgent(t, "file-server")
	who := e.user(t, false)
	run, err := e.svc.Create(ctx, who, agentPing(agent, "10.0.0.5"))
	testdb.Must(t, err)
	job, err := e.svc.NextJob(ctx, agent, 0)
	testdb.Must(t, err)
	if job == nil || job.RunID != run.ID {
		t.Fatalf("job = %+v", job)
	}
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(1)); cancel || err != nil {
		t.Fatalf("before the cancel: %v, %v", cancel, err)
	}
	got, err := e.svc.Cancel(ctx, who, run.ID)
	testdb.Must(t, err)
	if got.Status != models.ToolRunCancelled {
		t.Fatalf("Cancel left %s", got.Status)
	}
	if cancel, err := e.svc.AgentEvents(ctx, agent, run.ID, agentEvents(2)); !cancel || err != nil {
		t.Errorf("after the cancel: cancel %v, %v; want true", cancel, err)
	}
	testdb.Must(t, e.svc.AgentFinish(ctx, agent, run.ID, AgentFinish{Status: models.ToolRunFailed, Error: "cancelled"}))
	if got := e.reload(t, run.ID); got.Status != models.ToolRunCancelled || got.Error != nil || got.EventCount != 1 {
		t.Errorf("run = %+v, want cancelled, no error, the one event from before", got)
	}
}
