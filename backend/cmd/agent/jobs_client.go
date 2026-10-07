package main

// The agent's half of the job routes (Sentinel's api/agent_jobs_handler.go):
// the wire types, the HTTP client and the buffer of events waiting to be
// posted.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

var (
	errToolsDisabled = errors.New("network tools are switched off for this agent in Sentinel")
	errRunGone       = errors.New("this run is no longer running in Sentinel")
	errTooMuchOutput = errors.New("too much output")
)

// agentJob is a job as jobs/next sends it (toolruns.Job in Sentinel; the
// agent cannot import that package, which pulls in the database layer).
// Deadline is on Sentinel's clock, so the agent does not go by it: it bounds
// a job by its own clock (handle).
type agentJob struct {
	RunID    string        `json:"run_id"`
	Spec     nettools.Spec `json:"spec"`
	Deadline time.Time     `json:"deadline"`
}

// jobEvent is one event of an events post (toolruns.AgentEvent).
type jobEvent struct {
	Seq  int             `json:"seq"`
	At   time.Time       `json:"at"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// jobFinish is the finish post (toolruns.AgentFinish).
type jobFinish struct {
	Status  string          `json:"status"` // done | failed | refused | timed_out
	Summary json.RawMessage `json:"summary,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// ---- the HTTP side ----------------------------------------------------------

// jobClient speaks the agent's half of the job routes.
type jobClient struct {
	baseURL string
	token   string
	agentID string
	poll    *http.Client
	post    *http.Client
}

func (c *jobClient) jobPath(runID, what string) string {
	return "/api/v1/agents/" + c.agentID + "/jobs/" + url.PathEscape(runID) + "/" + what
}

// call sends one request and returns the status and up to 1 MB of the body.
func (c *jobClient) call(ctx context.Context, hc *http.Client, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encoding request: %w", err)
		}
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rd)
	if err != nil {
		return 0, nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("User-Agent", "sentinel-agent/"+version)
	resp, err := hc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
}

// next collects one job; nil when the poll ended without one.
func (c *jobClient) next(ctx context.Context) (*agentJob, error) {
	status, body, err := c.call(ctx, c.poll, http.MethodGet, "/api/v1/agents/"+c.agentID+"/jobs/next", nil)
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusNoContent:
		return nil, nil
	case status == http.StatusOK:
		var env struct {
			Data agentJob `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil || env.Data.RunID == "" {
			return nil, fmt.Errorf("malformed job from the server: %s", bodySnippet(body))
		}
		return &env.Data, nil
	case status == http.StatusForbidden && errorCode(body) == "tools_disabled":
		return nil, errToolsDisabled
	default:
		return nil, fmt.Errorf("server answered %d: %s", status, bodySnippet(body))
	}
}

// postEvents sends a batch and reports whether Sentinel has cancelled the run.
func (c *jobClient) postEvents(ctx context.Context, runID string, events []jobEvent) (bool, error) {
	if events == nil {
		events = []jobEvent{}
	}
	status, body, err := c.call(ctx, c.post, http.MethodPost, c.jobPath(runID, "events"), map[string]any{"events": events})
	switch {
	case err != nil:
		return false, err
	case status == http.StatusOK:
		var env struct {
			Data struct {
				Cancel bool `json:"cancel"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			return false, fmt.Errorf("malformed reply from the server: %s", bodySnippet(body))
		}
		return env.Data.Cancel, nil
	case status == http.StatusConflict:
		return false, errRunGone
	case status == http.StatusRequestEntityTooLarge:
		return false, errTooMuchOutput
	default:
		return false, fmt.Errorf("server answered %d: %s", status, bodySnippet(body))
	}
}

// finish reports the run's outcome.
func (c *jobClient) finish(ctx context.Context, runID string, f jobFinish) error {
	status, body, err := c.call(ctx, c.post, http.MethodPost, c.jobPath(runID, "finish"), f)
	switch {
	case err != nil:
		return err
	case status == http.StatusOK:
		return nil
	case status == http.StatusConflict:
		return errRunGone
	default:
		return fmt.Errorf("server answered %d: %s", status, bodySnippet(body))
	}
}

// errorCode is the code of a coded error body, {"error": {"code": "…"}}.
func errorCode(body []byte) string {
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &env) != nil {
		return ""
	}
	var coded struct {
		Code any `json:"code"`
	}
	if json.Unmarshal(env.Error, &coded) != nil {
		return ""
	}
	code, _ := coded.Code.(string)
	return code
}

func bodySnippet(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ---- events waiting to be posted --------------------------------------------

// eventBuffer holds a running job's events until a post stores them. Seqs
// start at 1 and never repeat, so a post retried after a lost reply is
// stored once: Sentinel ignores seqs it already has.
type eventBuffer struct {
	mu      sync.Mutex
	seq     int
	pending []jobEvent
	sizes   []int // encoded size of each pending event
}

func (b *eventBuffer) add(ev nettools.Event, at time.Time) {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		data, _ = json.Marshal(map[string]string{"error": err.Error()})
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	e := jobEvent{Seq: b.seq, At: at.UTC(), Type: ev.Type, Data: data}
	encoded, _ := json.Marshal(e)
	b.pending = append(b.pending, e)
	b.sizes = append(b.sizes, len(encoded)+1)
}

// batch returns the oldest pending events that fit in budget bytes: at least
// one, so an event bigger than the budget still goes, on its own.
func (b *eventBuffer) batch(budget int) []jobEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, size := 0, 0
	for n < len(b.pending) && (n == 0 || size+b.sizes[n] <= budget) {
		size += b.sizes[n]
		n++
	}
	return append([]jobEvent(nil), b.pending[:n]...)
}

// drop forgets the oldest n events once a post has stored them.
func (b *eventBuffer) drop(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = b.pending[n:]
	b.sizes = b.sizes[n:]
}

func (b *eventBuffer) empty() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending) == 0
}
