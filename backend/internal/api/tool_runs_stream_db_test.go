package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
	"github.com/Stevy2191/Sentinel/backend/internal/toolruns"
)

// sseFrame is one frame as a browser's EventSource would see it.
type sseFrame struct {
	id, event, data, comment string
}

// sseStream reads a real streamed response frame by frame, as the server
// flushes them, rather than inspecting a finished recording.
type sseStream struct {
	resp   *http.Response
	frames chan sseFrame
}

// openSSE connects to a run's event stream on srv.
func openSSE(t *testing.T, srv *httptest.Server, runID uuid.UUID, lastEventID string) *sseStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/tools/runs/"+runID.String()+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	s := &sseStream{resp: resp, frames: make(chan sseFrame, 64)}
	go s.read()
	return s
}

// read parses frames until the server closes the stream.
func (s *sseStream) read() {
	defer close(s.frames)
	r := bufio.NewReader(s.resp.Body)
	var f sseFrame
	seen := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			if seen {
				s.frames <- f
			}
			f, seen = sseFrame{}, false
			continue
		}
		seen = true
		if strings.HasPrefix(line, ":") {
			f.comment = strings.TrimSpace(line[1:])
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			f.id = value
		case "event":
			f.event = value
		case "data":
			if f.data != "" {
				f.data += "\n"
			}
			f.data += value
		}
	}
}

// next returns the next frame other than a keep-alive comment.
func (s *sseStream) next(t *testing.T) sseFrame {
	t.Helper()
	for {
		f := s.nextAny(t)
		if f.comment == "" {
			return f
		}
	}
}

// nextAny returns the next frame, comments included.
func (s *sseStream) nextAny(t *testing.T) sseFrame {
	t.Helper()
	select {
	case f, ok := <-s.frames:
		if !ok {
			t.Fatal("the stream closed early")
		}
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("no frame within 3 s")
	}
	return sseFrame{}
}

// closed checks the server ends the stream with nothing more sent.
func (s *sseStream) closed(t *testing.T) {
	t.Helper()
	select {
	case f, ok := <-s.frames:
		if ok {
			t.Fatalf("frame after end: %+v", f)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream stayed open after end")
	}
}

// expectEvent checks f is the event frame for seq.
func expectEvent(t *testing.T, f sseFrame, seq int) {
	t.Helper()
	var ev struct {
		Seq  int    `json:"seq"`
		Type string `json:"type"`
	}
	if f.event != "event" || f.id != strconv.Itoa(seq) || json.Unmarshal([]byte(f.data), &ev) != nil || ev.Seq != seq {
		t.Fatalf("frame %+v, want the event frame for seq %d", f, seq)
	}
}

// expectEnd checks f is the end frame of a run in status.
func expectEnd(t *testing.T, f sseFrame, status string) {
	t.Helper()
	var run struct {
		Status string `json:"status"`
	}
	if f.event != "end" || json.Unmarshal([]byte(f.data), &run) != nil || run.Status != status {
		t.Fatalf("frame %+v, want the end frame with status %s", f, status)
	}
}

// expectStatus checks f is a status frame.
func expectStatus(t *testing.T, f sseFrame, status string) {
	t.Helper()
	var body struct {
		Status string `json:"status"`
	}
	if f.event != "status" || json.Unmarshal([]byte(f.data), &body) != nil || body.Status != status {
		t.Fatalf("frame %+v, want a status frame %s", f, status)
	}
}

// streamServer serves the rig's routes for user over a real HTTP server.
func (rig *toolsRig) streamServer(t *testing.T, user uuid.UUID) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(rig.routerFor(t, user, false))
	t.Cleanup(srv.Close)
	return srv
}

// A run that has ended replays every stored event, then its end frame, and
// the server closes the stream. The headers let the stream through nginx.
func TestDBRunStreamReplaysAnEndedRunAndCloses(t *testing.T) {
	rig := newToolsRig(t)
	user := grantedUser(t, rig.db)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: user, events: 3})
	s := openSSE(t, rig.streamServer(t, user), id, "")

	if got := s.resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
	if got := s.resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := s.resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	for seq := 1; seq <= 3; seq++ {
		expectEvent(t, s.next(t), seq)
	}
	expectEnd(t, s.next(t), models.ToolRunDone)
	s.closed(t)
}

// A reconnecting EventSource sends Last-Event-ID; the replay starts after it.
func TestDBRunStreamResumesAfterLastEventID(t *testing.T) {
	rig := newToolsRig(t)
	user := grantedUser(t, rig.db)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunDone, user: user, events: 4})
	s := openSSE(t, rig.streamServer(t, user), id, "2")

	expectEvent(t, s.next(t), 3)
	expectEvent(t, s.next(t), 4)
	expectEnd(t, s.next(t), models.ToolRunDone)
	s.closed(t)
}

func TestDBRunStreamAnswers404ForAnUnknownRun(t *testing.T) {
	rig := newToolsRig(t)
	user := grantedUser(t, rig.db)
	srv := rig.streamServer(t, user)
	resp, err := srv.Client().Get(srv.URL + "/api/v1/tools/runs/" + uuid.NewString() + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status %d, want 404", resp.StatusCode)
	}
}

// Review Focus 2: a second tab, or a reconnect, mid-run. Events land in the
// window between the replay query and the switch to live events: event 2
// was stored before the replay but its publish arrives only now, and event 3
// is stored and published after the replay. The browser sees 1, 2, 3, then
// the live 4 and the end, each exactly once and in order.
func TestDBRunStreamHasNoGapOrDuplicateAcrossTheReplay(t *testing.T) {
	rig := newToolsRig(t)
	ctx := context.Background()
	user := grantedUser(t, rig.db)
	agent := registerToolAgent(t, rig.db, "file-server", true)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: user, agent: agent, events: 2})
	event := func(seq int) toolruns.AgentEvent {
		return toolruns.AgentEvent{Seq: seq, At: time.Now().UTC(), Type: "reply",
			Data: json.RawMessage(`{"seq":` + strconv.Itoa(seq) + `,"rtt_ms":1.5,"ttl":64,"from":"10.0.0.5"}`)}
	}
	rig.h.afterReplay = func(runID uuid.UUID) {
		late, err := json.Marshal(models.ToolRunEvent{Seq: 2, At: time.Now().UTC(), Type: "reply",
			Data: models.RawJSON(`{"seq":2,"rtt_ms":1.5,"ttl":64,"from":"10.0.0.5"}`)})
		if err != nil {
			t.Error(err)
		}
		rig.hub.Publish(runID.String(), stream.Message{ID: "2", Event: "event", Data: late})
		if _, err := rig.runs.AgentEvents(ctx, agent, runID, []toolruns.AgentEvent{event(3)}); err != nil {
			t.Errorf("storing event 3: %v", err)
		}
	}
	s := openSSE(t, rig.streamServer(t, user), id, "")

	expectEvent(t, s.next(t), 1)
	expectEvent(t, s.next(t), 2)
	expectStatus(t, s.next(t), models.ToolRunRunning)
	expectEvent(t, s.next(t), 3)

	if _, err := rig.runs.AgentEvents(ctx, agent, id, []toolruns.AgentEvent{event(4)}); err != nil {
		t.Fatal(err)
	}
	expectEvent(t, s.next(t), 4)
	if err := rig.runs.AgentFinish(ctx, agent, id, toolruns.AgentFinish{Status: models.ToolRunDone,
		Summary: json.RawMessage(`{"sent":4,"received":4,"loss_pct":0}`)}); err != nil {
		t.Fatal(err)
	}
	expectEnd(t, s.next(t), models.ToolRunDone)
	s.closed(t)
}

// Review Focus 1: an agent vanishes after claiming a run. The sweeper ends
// the run as timed_out at deadline + 15 s, and the end frame reaches the
// open stream, which then closes, so the page stops showing "Running".
func TestDBRunStreamEndsWhenTheSweeperTimesTheRunOut(t *testing.T) {
	rig := newToolsRig(t)
	rig.h.keepAlive = 20 * time.Millisecond
	user := grantedUser(t, rig.db)
	agent := registerToolAgent(t, rig.db, "file-server", true)
	id := insertToolRun(t, rig.db, toolRunRow{status: models.ToolRunRunning, user: user, agent: agent,
		deadline: time.Now().UTC().Add(-time.Minute)})
	s := openSSE(t, rig.streamServer(t, user), id, "")

	// The status frame is sent after the handler re-read the run as running,
	// so from here on only the hub can end the stream.
	expectStatus(t, s.next(t), models.ToolRunRunning)
	if f := s.nextAny(t); f.comment != "keep-alive" {
		t.Fatalf("frame %+v, want a keep-alive comment while the run is quiet", f)
	}
	if err := rig.runs.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	expectEnd(t, s.next(t), models.ToolRunTimedOut)
	s.closed(t)
}
