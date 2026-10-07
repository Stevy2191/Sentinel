package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// scriptedTool stands in for the Runner: it reports the spec it was given,
// waits for release, emits its events and returns its summary and error.
// With blockUntilCancel it then waits for its context instead.
type scriptedTool struct {
	events           []nettools.Event
	summary          any
	err              error
	blockUntilCancel bool
	started          chan nettools.Spec
	release          chan struct{}
}

func newScriptedTool(events ...nettools.Event) *scriptedTool {
	return &scriptedTool{events: events, started: make(chan nettools.Spec, 1), release: make(chan struct{})}
}

func (f *scriptedTool) run(ctx context.Context, spec nettools.Spec, emit nettools.Emitter) (any, error) {
	f.started <- spec
	select {
	case <-f.release:
	case <-ctx.Done():
		return f.summary, ctx.Err()
	}
	for _, ev := range f.events {
		emit(ev)
	}
	if f.blockUntilCancel {
		<-ctx.Done()
		return f.summary, ctx.Err()
	}
	return f.summary, f.err
}

// localEnv is newEnv with the real local launcher and tool.
func localEnv(t *testing.T, tool *scriptedTool) *env {
	t.Helper()
	e := newEnv(t)
	e.svc.launch = e.svc.launchLocal
	e.svc.runTool = tool.run
	return e
}

// waitFor polls cond every 20 ms for up to 5 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// framesUntilEnd reads a subscription up to and including the "end" frame,
// skipping "status" frames.
func framesUntilEnd(t *testing.T, sub *stream.Subscription) []stream.Message {
	t.Helper()
	var out []stream.Message
	timeout := time.After(5 * time.Second)
	for {
		select {
		case m, ok := <-sub.C():
			if !ok {
				t.Fatalf("subscription closed before end (dropped %v)", sub.Dropped())
			}
			if m.Event == "status" {
				continue
			}
			out = append(out, m)
			if m.Event == "end" {
				return out
			}
		case <-timeout:
			t.Fatalf("no end frame; got %d frames", len(out))
		}
	}
}

func f64(v float64) *float64 { return &v }

func TestDBLocalRunRecordsAndStreams(t *testing.T) {
	tool := newScriptedTool(
		nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: 1, RTTMS: 1.25, TTL: 64, From: "10.0.0.5"}},
		nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: 2, RTTMS: 1.75, TTL: 64, From: "10.0.0.5"}},
		nettools.Event{Type: "timeout", Data: map[string]int{"seq": 3}},
	)
	tool.summary = nettools.PingSummary{Sent: 3, Received: 2, LossPct: 33.3, MinMS: f64(1.25), AvgMS: f64(1.5), MaxMS: f64(1.75), JitterMS: f64(0.5)}
	e := localEnv(t, tool)
	ctx := context.Background()
	who := e.user(t, false)

	run, err := e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	testdb.Must(t, err)
	if spec := <-tool.started; spec.TargetIP != "10.0.0.5" || spec.Params.Count != 5 {
		t.Errorf("the tool got %+v", spec)
	}
	sub := e.svc.Subscribe(run.ID)
	defer sub.Close()
	close(tool.release)

	frames := framesUntilEnd(t, sub)
	if len(frames) != 4 {
		t.Fatalf("frames = %+v, want three events and end", frames)
	}
	for i, wantType := range []string{"reply", "reply", "timeout"} {
		var ev models.ToolRunEvent
		testdb.Must(t, json.Unmarshal(frames[i].Data, &ev))
		if frames[i].Event != "event" || frames[i].ID != strconv.Itoa(i+1) || ev.Seq != i+1 || ev.Type != wantType {
			t.Errorf("frame %d = %s id %s %+v, want event %d of type %s", i, frames[i].Event, frames[i].ID, ev, i+1, wantType)
		}
	}
	var end models.ToolRun
	testdb.Must(t, json.Unmarshal(frames[3].Data, &end))
	if end.ID != run.ID || end.Status != models.ToolRunDone || end.EventCount != 3 || end.Summary == nil {
		t.Errorf("end frame = %+v, want the run done with 3 events and a summary", end)
	}
	// The run Create returned is its caller's to serialise: the launcher
	// never writes to it.
	if run.Status != models.ToolRunRunning || run.FinishedAt != nil || run.EventCount != 0 || run.Summary != nil {
		t.Errorf("the run Create returned was changed to %+v", run)
	}

	got := e.reload(t, run.ID)
	var sum nettools.PingSummary
	if got.Summary != nil {
		testdb.Must(t, json.Unmarshal(*got.Summary, &sum))
	}
	if got.Status != models.ToolRunDone || got.EventCount != 3 || got.FinishedAt == nil || got.Error != nil ||
		sum.Sent != 3 || sum.AvgMS == nil || *sum.AvgMS != 1.5 {
		t.Errorf("run = %+v (summary %+v), want done, 3 events, finished, the tool's summary", got, sum)
	}
	events, err := e.svc.Events(ctx, run.ID, 0)
	testdb.Must(t, err)
	var first nettools.PingReply
	if len(events) == 3 {
		testdb.Must(t, json.Unmarshal(events[0].Data, &first))
	}
	if len(events) != 3 || events[0].Seq != 1 || events[2].Seq != 3 || events[2].Type != "timeout" ||
		first != (nettools.PingReply{Seq: 1, RTTMS: 1.25, TTL: 64, From: "10.0.0.5"}) {
		t.Errorf("stored events = %+v (first %+v)", events, first)
	}
	finished := e.audit.byAction(models.ActionToolRunFinished)
	if len(finished) != 1 || finished[0].actor.UserID != who.UserID ||
		finished[0].changes.Summary["status"] != models.ToolRunDone || finished[0].changes.Summary["result"] != "33.3% loss, 1.5 ms avg" {
		t.Errorf("finish audit = %+v", finished)
	}
	waitFor(t, "the cancel func to be forgotten", func() bool {
		e.svc.mu.Lock()
		defer e.svc.mu.Unlock()
		return len(e.svc.cancels) == 0
	})
}

// A tool error fails the run with the tool's message.
func TestDBLocalRunToolError(t *testing.T) {
	tool := newScriptedTool()
	tool.err = nettools.ErrICMPUnavailable
	e := localEnv(t, tool)
	run, err := e.svc.Create(context.Background(), e.user(t, false), pingReq("10.0.0.5"))
	testdb.Must(t, err)
	<-tool.started
	close(tool.release)
	waitFor(t, "the run to fail", func() bool { return e.reload(t, run.ID).Status == models.ToolRunFailed })
	if got := e.reload(t, run.ID); got.Error == nil || *got.Error != "ICMP isn't available here (needs root or NET_RAW)" {
		t.Errorf("error = %v", got.Error)
	}
	// The Runner returns no summary with such an error: stored as SQL NULL.
	if !e.summaryIsNull(t, run.ID) {
		t.Error("a run without a summary stored one")
	}
}

// summaryIsNull reports whether the run's summary column is SQL NULL (not
// the JSON null).
func (e *env) summaryIsNull(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var isNull bool
	testdb.Must(t, e.db.Raw(`SELECT summary IS NULL FROM tool_runs WHERE id = ?`, id).Scan(&isNull).Error)
	return isNull
}

// Cancel wins: the run is cancelled at once, its tool is stopped, and the
// tool's late finish only fills in the summary.
func TestDBCancelLocalRun(t *testing.T) {
	tool := newScriptedTool(nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: 1, RTTMS: 2, TTL: 64, From: "10.0.0.5"}})
	tool.blockUntilCancel = true
	tool.summary = nettools.PingSummary{Sent: 1, Received: 1}
	e := localEnv(t, tool)
	ctx := context.Background()
	who := e.user(t, false)

	run, err := e.svc.Create(ctx, who, pingReq("10.0.0.5"))
	testdb.Must(t, err)
	<-tool.started
	close(tool.release)
	waitFor(t, "the first event", func() bool { return e.reload(t, run.ID).EventCount == 1 })

	got, err := e.svc.Cancel(ctx, who, run.ID)
	testdb.Must(t, err)
	if got.Status != models.ToolRunCancelled || got.FinishedAt == nil {
		t.Errorf("Cancel returned %+v, want cancelled and finished", got)
	}
	waitFor(t, "the tool's late finish", func() bool { return e.reload(t, run.ID).Summary != nil })
	got = e.reload(t, run.ID)
	if got.Status != models.ToolRunCancelled || got.Error != nil || string(*got.Summary) == "" {
		t.Errorf("after the late finish: %+v, want still cancelled with no error", got)
	}
	finished := e.audit.byAction(models.ActionToolRunFinished)
	if len(finished) != 1 || finished[0].changes.Summary["status"] != models.ToolRunCancelled ||
		finished[0].actor.UserID != who.UserID {
		t.Errorf("finish audit = %+v, want one entry, cancelled by the owner", finished)
	}
}

// Reaching MaxEventsPerRun stops the tool and fails the run.
func TestDBLocalRunEventCap(t *testing.T) {
	events := make([]nettools.Event, MaxEventsPerRun+10)
	for i := range events {
		events[i] = nettools.Event{Type: nettools.EventPort, Data: map[string]int{"port": i + 1}}
	}
	tool := newScriptedTool(events...)
	e := localEnv(t, tool)
	run, err := e.svc.Create(context.Background(), e.user(t, false), pingReq("10.0.0.5"))
	testdb.Must(t, err)
	<-tool.started
	close(tool.release)
	waitFor(t, "the run to end", func() bool { return !models.ToolRunActive(e.reload(t, run.ID).Status) })
	got := e.reload(t, run.ID)
	if got.Status != models.ToolRunFailed || got.Error == nil || *got.Error != "too much output" || got.EventCount != MaxEventsPerRun {
		t.Errorf("run = status %s, error %v, %d events; want failed, too much output, 5000", got.Status, got.Error, got.EventCount)
	}
}

// The real Runner, with a fake dialer: a two-port scan from the Sentinel
// server is stored and finished like any other run.
func TestDBLocalRunThroughRunner(t *testing.T) {
	e := newEnv(t)
	e.svc.launch = e.svc.launchLocal
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr == "10.0.0.5:22" {
			c, s := net.Pipe()
			go s.Close()
			return c, nil
		}
		return nil, syscall.ECONNREFUSED
	}
	e.svc.runTool = (&nettools.Runner{Dial: dial}).Run
	run, err := e.svc.Create(context.Background(), e.user(t, false), CreateRequest{
		Tool: nettools.ToolTCP, Vantage: Vantage{Kind: models.VantageSentinel}, Target: "10.0.0.5",
		Params: nettools.Params{Ports: "22,80"},
	})
	testdb.Must(t, err)
	waitFor(t, "the scan to finish", func() bool { return e.reload(t, run.ID).Status == models.ToolRunDone })
	got := e.reload(t, run.ID)
	var sum nettools.TCPSummary
	testdb.Must(t, json.Unmarshal(*got.Summary, &sum))
	events, err := e.svc.Events(context.Background(), run.ID, 0)
	testdb.Must(t, err)
	if got.EventCount != 3 || len(events) != 3 || events[0].Type != nettools.EventStart ||
		sum.Total != 2 || sum.Open != 1 || sum.Closed != 1 || fmt.Sprint(sum.OpenPorts) != "[22]" {
		t.Errorf("run %+v, events %+v, summary %+v; want start + 2 ports, 1 open (22), 1 closed", got, events, sum)
	}
}

func TestLocalOutcome(t *testing.T) {
	cases := []struct {
		full          bool
		err           error
		status, error string
	}{
		{true, context.Canceled, models.ToolRunFailed, "too much output"},
		{false, nil, models.ToolRunDone, ""},
		{false, fmt.Errorf("probe: %w", context.DeadlineExceeded), models.ToolRunTimedOut, "the run went past its time limit"},
		{false, context.Canceled, models.ToolRunCancelled, ""},
		{false, nettools.ErrICMPUnavailable, models.ToolRunFailed, "ICMP isn't available here (needs root or NET_RAW)"},
		{false, errors.New("boom"), models.ToolRunFailed, "boom"},
	}
	for _, c := range cases {
		status, text := localOutcome(c.full, c.err)
		if status != c.status || text != c.error {
			t.Errorf("localOutcome(%v, %v) = %s %q, want %s %q", c.full, c.err, status, text, c.status, c.error)
		}
	}
}
