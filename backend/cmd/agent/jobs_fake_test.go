package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

const (
	testAgentID = "agent_0123456789"
	testToken   = "srv_test_token"
)

// fakeSentinel plays Sentinel's side of the job routes and the heartbeat.
type fakeSentinel struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	jobs        []agentJob // handed out by jobs/next in order, then 204
	nextStatus  int        // when set, jobs/next answers this status
	nextBody    string
	polls       int
	events      map[string][]jobEvent
	posts       int
	largestPost int
	cancelled   map[string]bool // events posts answer cancel: true
	gone        map[string]bool // events posts answer 409
	finishes    map[string]jobFinish
	heartbeats  []map[string]any
}

func newFakeSentinel(t *testing.T) *fakeSentinel {
	t.Helper()
	f := &fakeSentinel{
		t: t, events: map[string][]jobEvent{}, cancelled: map[string]bool{},
		gone: map[string]bool{}, finishes: map[string]jobFinish{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/agents/"+testAgentID+"/jobs/next", f.next)
	mux.HandleFunc("POST /api/v1/agents/"+testAgentID+"/jobs/{run}/events", f.postEvents)
	mux.HandleFunc("POST /api/v1/agents/"+testAgentID+"/jobs/{run}/finish", f.finish)
	mux.HandleFunc("POST /api/v1/agents/heartbeat", f.heartbeat)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			t.Errorf("%s %s without the agent's token", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func writeEnvelope(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": data})
}

func (f *fakeSentinel) next(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.polls++
	switch {
	case f.nextStatus != 0:
		w.WriteHeader(f.nextStatus)
		_, _ = io.WriteString(w, f.nextBody)
	case len(f.jobs) == 0:
		w.WriteHeader(http.StatusNoContent)
	default:
		job := f.jobs[0]
		f.jobs = f.jobs[1:]
		writeEnvelope(w, job)
	}
}

func (f *fakeSentinel) postEvents(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	run := r.PathValue("run")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts++
	if len(body) > f.largestPost {
		f.largestPost = len(body)
	}
	switch {
	case len(body) > 64<<10:
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	case f.gone[run]:
		w.WriteHeader(http.StatusConflict)
		return
	case f.cancelled[run]:
		writeEnvelope(w, map[string]bool{"cancel": true})
		return
	}
	var req struct {
		Events []jobEvent `json:"events"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Events == nil {
		f.t.Errorf("events post %s: %v (events must be a list, even an empty one)", body, err)
	}
	f.events[run] = append(f.events[run], req.Events...)
	writeEnvelope(w, map[string]bool{"cancel": false})
}

func (f *fakeSentinel) finish(w http.ResponseWriter, r *http.Request) {
	var req jobFinish
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Errorf("finish body: %v", err)
	}
	f.mu.Lock()
	f.finishes[r.PathValue("run")] = req
	f.mu.Unlock()
	writeEnvelope(w, map[string]bool{"ok": true})
}

func (f *fakeSentinel) heartbeat(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.t.Errorf("heartbeat body: %v", err)
	}
	f.mu.Lock()
	f.heartbeats = append(f.heartbeats, body)
	f.mu.Unlock()
	writeEnvelope(w, map[string]string{"message": "heartbeat recorded"})
}

// set changes the fake's behaviour under its lock.
func (f *fakeSentinel) set(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

func (f *fakeSentinel) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.polls
}

func (f *fakeSentinel) eventsOf(run string) []jobEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]jobEvent(nil), f.events[run]...)
}

// waitFinish waits for run's finish post and returns it.
func (f *fakeSentinel) waitFinish(t *testing.T, run string) jobFinish {
	t.Helper()
	var fin jobFinish
	waitFor(t, "the finish of "+run, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		got, ok := f.finishes[run]
		fin = got
		return ok
	})
	return fin
}

// waitFor checks cond every 5 ms for up to 3 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// runnerFunc adapts a function to toolRunner.
type runnerFunc func(ctx context.Context, s nettools.Spec, emit nettools.Emitter) (any, error)

func (f runnerFunc) Run(ctx context.Context, s nettools.Spec, emit nettools.Emitter) (any, error) {
	return f(ctx, s, emit)
}

// testLoop is a job loop against f with every wait cut to milliseconds.
func testLoop(f *fakeSentinel, runner toolRunner, allowed []*net.IPNet) *jobLoop {
	l := newJobLoop(config{ServerURL: f.srv.URL, AgentID: testAgentID, ServerToken: testToken, ToolsAllowed: allowed}, runner)
	l.flushEvery = 10 * time.Millisecond
	l.minPoll = 5 * time.Millisecond
	l.disabledWait = time.Second
	l.backoffMin = 50 * time.Millisecond
	l.backoffMax = 200 * time.Millisecond
	l.retryDelay = time.Millisecond
	return l
}

// startLoop runs l until the test ends (before the fake server closes).
func startLoop(t *testing.T, l *jobLoop) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		l.run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

// testJob is a ping job against ip with the default parameters.
func testJob(run, ip string) agentJob {
	return agentJob{
		RunID:    run,
		Spec:     nettools.Spec{Tool: nettools.ToolPing, Target: ip, TargetIP: ip},
		Deadline: time.Now().Add(time.Minute),
	}
}
