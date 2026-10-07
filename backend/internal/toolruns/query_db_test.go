package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A retried batch never duplicates events: seqs already stored are skipped,
// event_count counts only what was inserted, and only new events are
// published.
func TestDBInsertEventsIgnoresDuplicates(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	run := e.insertRun(t, nil)
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()
	ev := func(seq int) models.ToolRunEvent {
		return models.ToolRunEvent{RunID: run.ID, Seq: seq, At: e.clock.Now(), Type: "reply", Data: models.RawJSON(`{"seq":1}`)}
	}

	n, err := e.svc.insertEvents(ctx, run.ID, []models.ToolRunEvent{ev(1), ev(2)})
	testdb.Must(t, err)
	if n != 2 {
		t.Errorf("first batch inserted %d, want 2", n)
	}
	n, err = e.svc.insertEvents(ctx, run.ID, []models.ToolRunEvent{ev(2), ev(3)})
	testdb.Must(t, err)
	if n != 1 {
		t.Errorf("overlapping batch inserted %d, want 1", n)
	}
	if got := e.reload(t, run.ID).EventCount; got != 3 {
		t.Errorf("event_count = %d, want 3", got)
	}
	var ids []string
	for len(ids) < 3 {
		select {
		case m := <-sub.C():
			ids = append(ids, m.ID)
		case <-time.After(time.Second):
			t.Fatalf("frames %v, want 3", ids)
		}
	}
	select {
	case m := <-sub.C():
		t.Errorf("an extra frame %s %s", m.Event, m.ID)
	default:
	}
	if ids[0] != "1" || ids[1] != "2" || ids[2] != "3" {
		t.Errorf("frames %v, want [1 2 3]", ids)
	}
}

// The first finish wins (ruling 8): it sets the status, error and finish
// time, publishes one end frame and audits once. A later finish never
// changes those; it only fills the summary while that is still SQL NULL. A
// JSON null summary counts as none.
func TestDBFinishFirstWins(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	run := e.insertRun(t, nil)
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()

	moved, err := e.svc.finish(ctx, run.ID, models.ToolRunFailed, []byte(" null "), "boom", systemActor)
	testdb.Must(t, err)
	got := e.reload(t, run.ID)
	if !moved || got.Status != models.ToolRunFailed || got.Error == nil || *got.Error != "boom" || got.FinishedAt == nil ||
		!e.summaryIsNull(t, run.ID) || got.Summary != nil {
		t.Errorf("first finish: moved %v, run %+v; want failed with boom, finished, summary NULL", moved, got)
	}
	select {
	case m := <-sub.C():
		var end models.ToolRun
		testdb.Must(t, json.Unmarshal(m.Data, &end))
		if m.Event != "end" || end.ID != run.ID || end.Status != models.ToolRunFailed {
			t.Errorf("frame %s %+v, want end with the failed run", m.Event, end)
		}
	case <-time.After(time.Second):
		t.Fatal("no end frame")
	}

	for _, sum := range []string{`{"sent":1}`, `{"sent":2}`} {
		moved, err = e.svc.finish(ctx, run.ID, models.ToolRunDone, []byte(sum), "", systemActor)
		testdb.Must(t, err)
		if moved {
			t.Errorf("a finish of a finished run moved it")
		}
	}
	got = e.reload(t, run.ID)
	if got.Status != models.ToolRunFailed || got.Error == nil || *got.Error != "boom" ||
		got.Summary == nil || string(*got.Summary) != `{"sent": 1}` {
		t.Errorf("after late finishes: %+v (summary %s); want still failed with boom, the first late summary", got, ptrText(got.Summary))
	}
	select {
	case m := <-sub.C():
		t.Errorf("a late finish published %s", m.Event)
	default:
	}
	if f := e.audit.byAction(models.ActionToolRunFinished); len(f) != 1 || f[0].changes.Summary["result"] != "boom" {
		t.Errorf("finish audit = %+v, want one entry with the error as result", f)
	}
}

func ptrText(r *models.RawJSON) string {
	if r == nil {
		return "<nil>"
	}
	return string(*r)
}

func TestDBCancelRules(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	owner, other, admin := e.user(t, false), e.user(t, false), e.user(t, true)
	run := e.insertRun(t, func(r *models.ToolRun) { r.UserID = &owner.UserID })

	_, err := e.svc.Cancel(ctx, other, run.ID)
	if r := refusal(t, err); r.Status != http.StatusForbidden || r.Code != CodeForbidden {
		t.Errorf("another user's cancel: %+v, want 403 forbidden", r)
	}
	if got := e.reload(t, run.ID); got.Status != models.ToolRunRunning {
		t.Errorf("a refused cancel changed the run to %s", got.Status)
	}
	got, err := e.svc.Cancel(ctx, admin, run.ID)
	testdb.Must(t, err)
	if got.Status != models.ToolRunCancelled {
		t.Errorf("admin cancel left %s", got.Status)
	}
	_, err = e.svc.Cancel(ctx, owner, run.ID)
	if r := refusal(t, err); r.Status != http.StatusConflict || r.Code != CodeConflict {
		t.Errorf("cancelling a finished run: %+v, want 409 conflict", r)
	}
	_, err = e.svc.Cancel(ctx, admin, uuid.New())
	if r := refusal(t, err); r.Status != http.StatusNotFound || r.Code != CodeNotFound {
		t.Errorf("unknown run: %+v, want 404 not_found", r)
	}
	finished := e.audit.byAction(models.ActionToolRunFinished)
	if len(finished) != 1 || finished[0].actor.UserID != admin.UserID {
		t.Errorf("finish audit = %+v, want one entry by the admin", finished)
	}
}

func TestDBGetEventsAndList(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, false), e.user(t, false)
	agent := e.newAgent(t, "file-server", true, boolPtr(true))
	base := e.clock.Now()
	at := func(min int) time.Time { return base.Add(time.Duration(min) * time.Minute) }

	r1 := e.insertRun(t, func(r *models.ToolRun) { r.UserID, r.CreatedAt, r.Status = &alice.UserID, at(1), models.ToolRunDone })
	r2 := e.insertRun(t, func(r *models.ToolRun) {
		r.UserID, r.CreatedAt, r.Tool, r.Target = &bob.UserID, at(2), "dns", "example.com"
		r.TargetIP = nil
	})
	r3 := e.insertRun(t, func(r *models.ToolRun) {
		r.UserID, r.CreatedAt, r.Status = &alice.UserID, at(3), models.ToolRunFailed
		r.VantageKind, r.AgentUUID, r.AgentRef, r.Target = models.VantageAgent, &agent.ID, &agent.AgentID, "fileserver.example.org"
	})

	if _, err := e.svc.Get(ctx, uuid.New()); !errors.Is(err, ErrRunNotFound) {
		t.Errorf("Get(unknown) = %v, want ErrRunNotFound", err)
	}
	if got, err := e.svc.Get(ctx, r2.ID); err != nil || got.Tool != "dns" {
		t.Errorf("Get = %+v, %v", got, err)
	}

	ids := func(runs []models.ToolRun) []uuid.UUID {
		out := []uuid.UUID{}
		for _, r := range runs {
			out = append(out, r.ID)
		}
		return out
	}
	cases := []struct {
		name  string
		f     ListFilter
		want  []uuid.UUID
		total int64
	}{
		{"all, newest first", ListFilter{}, []uuid.UUID{r3.ID, r2.ID, r1.ID}, 3},
		{"by tool", ListFilter{Tool: "dns"}, []uuid.UUID{r2.ID}, 1},
		{"by user", ListFilter{UserID: &alice.UserID}, []uuid.UUID{r3.ID, r1.ID}, 2},
		{"by agent", ListFilter{AgentID: agent.AgentID}, []uuid.UUID{r3.ID}, 1},
		{"by typed target", ListFilter{Target: "fileserver.example.org"}, []uuid.UUID{r3.ID}, 1},
		{"by address", ListFilter{Target: "10.0.0.200"}, []uuid.UUID{r3.ID, r1.ID}, 2},
		{"by status", ListFilter{Status: models.ToolRunRunning}, []uuid.UUID{r2.ID}, 1},
		{"paged", ListFilter{Limit: 1, Offset: 1}, []uuid.UUID{r2.ID}, 3},
		{"past the end", ListFilter{Offset: 10}, []uuid.UUID{}, 3},
	}
	for _, c := range cases {
		runs, total, err := e.svc.List(ctx, c.f)
		testdb.Must(t, err)
		got := ids(runs)
		if total != c.total || len(got) != len(c.want) {
			t.Errorf("%s: %v (total %d), want %v (total %d)", c.name, got, total, c.want, c.total)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: %v, want %v", c.name, got, c.want)
				break
			}
		}
	}

	for seq := 1; seq <= 4; seq++ {
		testdb.Exec(t, e.db, `INSERT INTO tool_run_events (run_id, seq, at, type, data) VALUES (?, ?, now(), 'reply', '{}')`, r1.ID, seq)
	}
	events, err := e.svc.Events(ctx, r1.ID, 2)
	testdb.Must(t, err)
	if len(events) != 2 || events[0].Seq != 3 || events[1].Seq != 4 {
		t.Errorf("Events after 2 = %+v, want seqs 3 and 4", events)
	}
	if none, err := e.svc.Events(ctx, r2.ID, 0); err != nil || none == nil || len(none) != 0 {
		t.Errorf("Events of a run without any = %v, %v; want an empty, non-nil slice", none, err)
	}
}
