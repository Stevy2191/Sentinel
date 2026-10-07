package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// jobsRig is the agent job routes on a real database with a short long-poll.
// agent has Sentinel's tools switch on, other has it off.
type jobsRig struct {
	db     *gorm.DB
	runs   *toolruns.Service
	router *gin.Engine
	agent  *models.Agent
	other  *models.Agent
	user   uuid.UUID
}

func newJobsRig(t *testing.T, wait time.Duration) *jobsRig {
	t.Helper()
	db := testdb.Open(t)
	runs := newTestToolRuns(db, stream.NewHub(64))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	registerAgentJobRoutes(r, services.NewAgentService(db), runs, wait)
	return &jobsRig{
		db: db, runs: runs, router: r,
		agent: registerToolAgent(t, db, "file-server", true),
		other: registerToolAgent(t, db, "print-server", false),
		user:  grantedUser(t, db),
	}
}

// call sends an agent request with token.
func (rig *jobsRig) call(token, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	rig.router.ServeHTTP(w, req)
	return w
}

func jobPath(a *models.Agent, run uuid.UUID, what string) string {
	return "/api/v1/agents/" + a.AgentID + "/jobs/" + run.String() + "/" + what
}

func nextPath(a *models.Agent) string { return "/api/v1/agents/" + a.AgentID + "/jobs/next" }

// runStatus reads a run's status and error.
func (rig *jobsRig) runStatus(t *testing.T, id uuid.UUID) (string, string) {
	t.Helper()
	var row struct {
		Status string
		Error  *string
	}
	testdb.Must(t, rig.db.Raw(`SELECT status, error FROM tool_runs WHERE id = ?`, id).Scan(&row).Error)
	if row.Error == nil {
		return row.Status, ""
	}
	return row.Status, *row.Error
}

func (rig *jobsRig) eventCount(t *testing.T, id uuid.UUID) (stored, counted int) {
	t.Helper()
	testdb.Must(t, rig.db.Raw(`SELECT count(*) FROM tool_run_events WHERE run_id = ?`, id).Scan(&stored).Error)
	testdb.Must(t, rig.db.Raw(`SELECT event_count FROM tool_runs WHERE id = ?`, id).Scan(&counted).Error)
	return stored, counted
}

const twoEvents = `{"events":[
	{"seq":1,"at":"2026-10-05T12:00:00Z","type":"reply","data":{"seq":1,"rtt_ms":1.2,"ttl":64,"from":"10.0.0.5"}},
	{"seq":2,"at":"2026-10-05T12:00:01Z","type":"reply","data":{"seq":2,"rtt_ms":1.4,"ttl":64,"from":"10.0.0.5"}}]}`

// With nothing queued, a poll is held for the wait and then answered 204.
func TestDBJobsNextAnswers204AfterTheWait(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	start := time.Now()
	w := rig.call(rig.agent.ServerToken, http.MethodGet, nextPath(rig.agent), "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204: %s", w.Code, w.Body.String())
	}
	if held := time.Since(start); held < 50*time.Millisecond {
		t.Errorf("the poll was answered after %s, want it held for the 50 ms wait", held)
	}
}

func TestDBJobsNextHandsOutTheQueuedRun(t *testing.T) {
	rig := newJobsRig(t, time.Second)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunQueued, user: rig.user, agent: rig.agent})
	w := rig.call(rig.agent.ServerToken, http.MethodGet, nextPath(rig.agent), "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	var job toolruns.Job
	toolData(t, w.Body.Bytes(), &job)
	if job.RunID != id || job.Spec.Tool != "ping" || job.Spec.Target != "10.0.0.5" || job.Spec.TargetIP != "10.0.0.5" ||
		job.Spec.Params.Count != 5 || !job.Deadline.After(time.Now()) {
		t.Errorf("job = %+v", job)
	}
	if status, _ := rig.runStatus(t, id); status != models.ToolRunRunning {
		t.Errorf("status %q after the claim, want running", status)
	}
}

// A poll held open is answered as soon as a run is queued for its agent, not
// when the wait runs out: Create wakes it.
func TestDBJobsNextWakesWhenARunIsQueued(t *testing.T) {
	rig := newJobsRig(t, 2*time.Second)
	ctx := context.Background()
	_, err := toolruns.SaveSettings(ctx, services.NewSettingsService(rig.db),
		toolruns.Settings{Allowlist: []string{"10.0.0.0/24"}, ServerEnabled: true, RetentionDays: 30})
	testdb.Must(t, err)
	// One poll makes the agent ready (it counts for 60 s), so Create queues
	// a run for it rather than refusing.
	if job, err := rig.runs.NextJob(ctx, rig.agent, time.Millisecond); err != nil || job != nil {
		t.Fatalf("NextJob = %v, %v; want an empty poll", job, err)
	}

	type queued struct {
		id  uuid.UUID
		err error
	}
	done := make(chan queued, 1)
	go func() {
		// Queued while the poll below is held open.
		time.Sleep(300 * time.Millisecond)
		run, err := rig.runs.Create(ctx, toolruns.Requester{UserID: rig.user, Username: "someone"},
			toolruns.CreateRequest{
				Tool:    "ping",
				Vantage: toolruns.Vantage{Kind: models.VantageAgent, AgentID: rig.agent.AgentID},
				Target:  "10.0.0.5",
			})
		if err != nil {
			done <- queued{err: err}
			return
		}
		done <- queued{id: run.ID}
	}()

	start := time.Now()
	w := rig.call(rig.agent.ServerToken, http.MethodGet, nextPath(rig.agent), "")
	held := time.Since(start)
	q := <-done
	testdb.Must(t, q.err)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", w.Code, w.Body.String())
	}
	var job toolruns.Job
	toolData(t, w.Body.Bytes(), &job)
	if job.RunID != q.id {
		t.Errorf("job for run %s, want the run just queued (%s)", job.RunID, q.id)
	}
	if held >= 1500*time.Millisecond {
		t.Errorf("the poll was answered after %s: the queued run did not wake it (the wait is 2 s)", held)
	}
}

func TestDBJobsNextRefusesWhileToolsAreOffInSentinel(t *testing.T) {
	rig := newJobsRig(t, time.Second)
	w := rig.call(rig.other.ServerToken, http.MethodGet, nextPath(rig.other), "")
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusForbidden || code != "tools_disabled" {
		t.Errorf("status %d code %q, want 403 tools_disabled", w.Code, code)
	}
}

// An agent cannot read or write another agent's runs: another agent's token
// on this agent's path is refused before any handler runs, and on its own
// path the run is not its to touch.
func TestDBJobRoutesKeepAgentsToTheirOwnRuns(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	queued := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunQueued, user: rig.user, agent: rig.agent})
	running := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, nextPath(rig.agent), ""},
		{http.MethodPost, jobPath(rig.agent, running, "events"), twoEvents},
		{http.MethodPost, jobPath(rig.agent, running, "finish"), `{"status":"done","summary":{"sent":2}}`},
	} {
		if w := rig.call(rig.other.ServerToken, tc.method, tc.path, tc.body); w.Code != http.StatusForbidden {
			t.Errorf("%s %s with another agent's token: status %d, want 403", tc.method, tc.path, w.Code)
		}
	}
	if status, _ := rig.runStatus(t, queued); status != models.ToolRunQueued {
		t.Errorf("the queued run is %q: another agent's poll claimed it", status)
	}
	if stored, _ := rig.eventCount(t, running); stored != 0 {
		t.Errorf("%d events stored from another agent's token", stored)
	}

	for _, what := range []string{"events", "finish"} {
		body := twoEvents
		if what == "finish" {
			body = `{"status":"done"}`
		}
		w := rig.call(rig.other.ServerToken, http.MethodPost, jobPath(rig.other, running, what), body)
		if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusConflict || code != "conflict" {
			t.Errorf("%s for another agent's run: status %d code %q, want 409 conflict", what, w.Code, code)
		}
	}
	if status, _ := rig.runStatus(t, running); status != models.ToolRunRunning {
		t.Errorf("status %q, want the run untouched", status)
	}
}

// Events are stored once however often a post is retried; an empty batch is
// answered too (it is how the agent hears of a cancel); after a cancel the
// reply says so, and the agent's late finish leaves the run cancelled.
func TestDBJobEventsStoreOnceAndCarryTheCancel(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})
	cancelOf := func(w *httptest.ResponseRecorder) bool {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var reply struct {
			Cancel bool `json:"cancel"`
		}
		toolData(t, w.Body.Bytes(), &reply)
		return reply.Cancel
	}

	for i := 0; i < 2; i++ {
		if cancelOf(rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), twoEvents)) {
			t.Fatal("cancel before anyone cancelled")
		}
	}
	if stored, counted := rig.eventCount(t, id); stored != 2 || counted != 2 {
		t.Errorf("after a retried post: %d stored, event_count %d; want 2 and 2", stored, counted)
	}
	if cancelOf(rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), `{"events":[]}`)) {
		t.Error("an empty batch answered cancel for a running run")
	}

	_, err := rig.runs.Cancel(context.Background(), toolruns.Requester{UserID: rig.user, Username: "someone"}, id)
	testdb.Must(t, err)
	if !cancelOf(rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), `{"events":[]}`)) {
		t.Error("the reply after a cancel did not say cancel")
	}
	w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "finish"),
		`{"status":"failed","summary":{"sent":2,"received":2},"error":"cancelled"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("finish after cancel: status %d: %s", w.Code, w.Body.String())
	}
	if status, _ := rig.runStatus(t, id); status != models.ToolRunCancelled {
		t.Errorf("status %q after the agent's late finish, want cancelled", status)
	}
}

func TestDBJobEventsRefuseAPostOver64KB(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})
	big := `{"events":[{"seq":1,"at":"2026-10-05T12:00:00Z","type":"reply","data":"` + strings.Repeat("x", 70<<10) + `"}]}`
	w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), big)
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusRequestEntityTooLarge || code != "limit" {
		t.Errorf("status %d code %q, want 413 limit", w.Code, code)
	}
	if stored, _ := rig.eventCount(t, id); stored != 0 {
		t.Errorf("%d events stored from an oversized post", stored)
	}
}

// Reaching 5,000 events ends the run as failed, "too much output", and the
// agent is told with a 413.
func TestDBJobEventsEndTheRunAtTheEventCap(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})
	testdb.Exec(t, rig.db, `UPDATE tool_runs SET event_count = 5000 WHERE id = ?`, id)
	w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), twoEvents)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d, want 413", w.Code)
	}
	if status, errText := rig.runStatus(t, id); status != models.ToolRunFailed || errText != "too much output" {
		t.Errorf("run is %q %q, want failed / too much output", status, errText)
	}
}

// A run that is not running (still queued, or already ended) takes no events.
func TestDBJobEventsConflictUnlessTheRunIsRunning(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	for _, status := range []string{models.ToolRunQueued, models.ToolRunDone} {
		id := insertToolRun(t, rig.db, toolRunRow{status: status, user: rig.user, agent: rig.agent})
		if w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "events"), twoEvents); w.Code != http.StatusConflict {
			t.Errorf("events for a %s run: status %d, want 409", status, w.Code)
		}
	}
}

func TestDBJobFinishRecordsTheOutcome(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})

	w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "finish"), `{"status":"running"}`)
	if code, _ := toolErrorBody(t, w.Body.Bytes()); w.Code != http.StatusBadRequest || code != "invalid_params" {
		t.Errorf("finish as running: status %d code %q, want 400 invalid_params", w.Code, code)
	}
	if w := rig.call(rig.agent.ServerToken, http.MethodPost, "/api/v1/agents/"+rig.agent.AgentID+"/jobs/not-a-run/finish",
		`{"status":"done"}`); w.Code != http.StatusBadRequest {
		t.Errorf("finish for a malformed run id: status %d, want 400", w.Code)
	}

	w = rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "finish"),
		`{"status":"done","summary":{"sent":5,"received":5,"loss_pct":0}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var sent int
	testdb.Must(t, rig.db.Raw(`SELECT (summary->>'sent')::int FROM tool_runs WHERE id = ?`, id).Scan(&sent).Error)
	if status, _ := rig.runStatus(t, id); status != models.ToolRunDone || sent != 5 {
		t.Errorf("run is %q with summary sent %d, want done and 5", status, sent)
	}
}

// An agent whose tool hit the job's deadline reports timed_out; with no error
// text the run gets the same text as a Sentinel run that timed out. The
// agent's error text is bounded once, by the service (500 bytes).
func TestDBJobFinishAcceptsTimedOut(t *testing.T) {
	rig := newJobsRig(t, 50*time.Millisecond)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})
	w := rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, id, "finish"),
		`{"status":"timed_out","summary":{"sent":3,"received":3,"loss_pct":0}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("finish as timed_out: status %d, want 200: %s", w.Code, w.Body.String())
	}
	var sent int
	testdb.Must(t, rig.db.Raw(`SELECT (summary->>'sent')::int FROM tool_runs WHERE id = ?`, id).Scan(&sent).Error)
	if status, errText := rig.runStatus(t, id); status != models.ToolRunTimedOut ||
		errText != "the run went past its time limit" || sent != 3 {
		t.Errorf("run is %q %q with summary sent %d, want timed_out / the run went past its time limit / 3",
			status, errText, sent)
	}

	long := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: rig.user, agent: rig.agent})
	w = rig.call(rig.agent.ServerToken, http.MethodPost, jobPath(rig.agent, long, "finish"),
		`{"status":"failed","error":"`+strings.Repeat("x", 3000)+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("finish with a long error: status %d: %s", w.Code, w.Body.String())
	}
	if status, errText := rig.runStatus(t, long); status != models.ToolRunFailed || len(errText) != 500 {
		t.Errorf("run is %q with a %d-byte error, want failed and 500 bytes", status, len(errText))
	}
}
