package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

// Job is what an agent receives from GET jobs/next.
type Job struct {
	RunID    uuid.UUID     `json:"run_id"`
	Spec     nettools.Spec `json:"spec"`
	Deadline time.Time     `json:"deadline"`
}

// Agent job errors.
var (
	ErrToolsDisabled = errors.New("tools are off for this agent")
	ErrRunNotActive  = errors.New("that run is not running on this agent")
	ErrTooMuchOutput = errors.New("too much output")
	// ErrBadFinishStatus: a finish whose status is not done, failed,
	// refused or timed_out. The API answers 400.
	ErrBadFinishStatus = errors.New("status must be done, failed, refused or timed_out")
)

// NextJob claims the oldest queued run for agent, waiting up to wait for one
// to be queued (woken by Create). (nil, nil) when none arrived in time or
// the agent hung up. ErrToolsDisabled when the admin switch is off. Every
// call with tools on records a poll, which is what makes the agent count as
// ready. A tools_disabled poll records nothing: that agent then sleeps
// before asking again, so it must not count as ready the moment the switch
// goes on, or the first run would wait out the pickup timeout.
func (s *Service) NextJob(ctx context.Context, agent *models.Agent, wait time.Duration) (*Job, error) {
	if !agent.ToolsEnabled {
		return nil, ErrToolsDisabled
	}
	s.polls.seen(agent.ID, s.now())
	defer func() { s.polls.seen(agent.ID, s.now()) }()
	wake := s.wakeChan(agent.ID)
	timer := time.NewTimer(max(wait, 0))
	defer timer.Stop()
	for {
		job, err := s.claim(ctx, agent)
		if err != nil || job != nil {
			return job, err
		}
		select {
		case <-wake:
		case <-timer.C:
			// One last look: a run queued as the wait ran out.
			return s.claim(ctx, agent)
		case <-ctx.Done():
			return nil, nil
		}
	}
}

// claim moves agent's oldest queued run to running in one statement, so two
// polls can never take the same run (SKIP LOCKED makes the loser find
// nothing), and resets its deadline to now + the tool's limit.
func (s *Service) claim(ctx context.Context, agent *models.Agent) (*Job, error) {
	now := s.now()
	secs := func(t nettools.Tool) float64 { return nettools.Deadline(t).Seconds() }
	var runs []models.ToolRun
	err := s.db.WithContext(ctx).Raw(`UPDATE tool_runs
		SET status = ?, started_at = ?::timestamptz,
			deadline = ?::timestamptz + make_interval(secs => CASE tool
				WHEN 'ping' THEN ?::float8 WHEN 'traceroute' THEN ?::float8
				WHEN 'dns' THEN ?::float8 ELSE ?::float8 END)
		WHERE id = (SELECT id FROM tool_runs WHERE agent_id = ? AND status = ?
		            ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING *`,
		models.ToolRunRunning, now, now,
		secs(nettools.ToolPing), secs(nettools.ToolTraceroute), secs(nettools.ToolDNS), secs(nettools.ToolTCP),
		agent.ID, models.ToolRunQueued).Scan(&runs).Error
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil
		}
		return nil, fmt.Errorf("claiming a job for %s: %w", agent.AgentID, err)
	}
	if len(runs) == 0 {
		return nil, nil
	}
	run := &runs[0]
	var params nettools.Params
	if err := json.Unmarshal(run.Params, &params); err != nil {
		return nil, fmt.Errorf("decoding the parameters of run %s: %w", run.ID, err)
	}
	spec := nettools.Spec{Tool: nettools.Tool(run.Tool), Target: run.Target, Params: params}
	if run.TargetIP != nil {
		spec.TargetIP = *run.TargetIP
	}
	s.publishStatus(run.ID, models.ToolRunRunning)
	return &Job{RunID: run.ID, Spec: spec, Deadline: run.Deadline}, nil
}

// AgentEvent is one event as an agent posts it.
type AgentEvent struct {
	Seq  int             `json:"seq"`
	At   time.Time       `json:"at"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// agentRun loads runID and checks it is agent's.
func (s *Service) agentRun(ctx context.Context, agent *models.Agent, runID uuid.UUID) (*models.ToolRun, error) {
	run, err := s.Get(ctx, runID)
	if errors.Is(err, ErrRunNotFound) {
		return nil, ErrRunNotActive
	}
	if err != nil {
		return nil, err
	}
	if run.AgentUUID == nil || *run.AgentUUID != agent.ID {
		return nil, ErrRunNotActive
	}
	return run, nil
}

// AgentEvents stores a batch from agent for runID (seqs already stored are
// ignored, so a retried post never duplicates) and publishes it. cancel is
// true when the run has been cancelled: nothing is stored and the agent
// stops the tool. ErrRunNotActive when the run is not this agent's or has
// ended otherwise. ErrTooMuchOutput when the batch would take the run past
// MaxEventsPerRun; the run is then failed. Events with a seq below 1 or no
// type are dropped, and a seq repeated within the batch keeps its first
// copy. An empty batch stores nothing: the agent posts one every flush to
// hear about a cancel.
func (s *Service) AgentEvents(ctx context.Context, agent *models.Agent, runID uuid.UUID, events []AgentEvent) (bool, error) {
	run, err := s.agentRun(ctx, agent, runID)
	if err != nil {
		return false, err
	}
	switch run.Status {
	case models.ToolRunCancelled:
		return true, nil
	case models.ToolRunRunning:
	default:
		return false, ErrRunNotActive
	}
	rows := s.agentRows(runID, events)
	if len(rows) == 0 {
		return false, nil
	}
	if run.EventCount+len(rows) > MaxEventsPerRun {
		// Over the cap only if the batch is new: a retry of a batch already
		// stored adds nothing.
		fresh, err := s.unstoredCount(ctx, runID, rows)
		if err != nil {
			return false, err
		}
		if run.EventCount+fresh > MaxEventsPerRun {
			if _, err := s.finish(ctx, runID, models.ToolRunFailed, nil, msgTooMuchOutput, ownerActor(run)); err != nil {
				return false, err
			}
			return false, ErrTooMuchOutput
		}
	}
	_, err = s.insertEvents(ctx, runID, rows)
	return false, err
}

// agentRows turns an agent's batch into rows: invalid events dropped, a
// repeated seq kept as its first copy (insertEvents' ON CONFLICT keeps the
// first too, so the stored row and the published frame agree), a missing
// time set to now and missing data to JSON null.
func (s *Service) agentRows(runID uuid.UUID, events []AgentEvent) []models.ToolRunEvent {
	rows := make([]models.ToolRunEvent, 0, len(events))
	seen := make(map[int]bool, len(events))
	for _, ev := range events {
		if ev.Seq < 1 || ev.Type == "" || seen[ev.Seq] {
			continue
		}
		seen[ev.Seq] = true
		at := ev.At.UTC()
		if ev.At.IsZero() {
			at = s.now()
		}
		data := models.RawJSON(ev.Data)
		if len(data) == 0 {
			data = models.RawJSON(`null`)
		}
		rows = append(rows, models.ToolRunEvent{RunID: runID, Seq: ev.Seq, At: at, Type: ev.Type, Data: data})
	}
	return rows
}

// unstoredCount is how many of rows (distinct seqs) runID has not stored yet.
func (s *Service) unstoredCount(ctx context.Context, runID uuid.UUID, rows []models.ToolRunEvent) (int, error) {
	seqs := make([]int, len(rows))
	for i, r := range rows {
		seqs[i] = r.Seq
	}
	var stored int64
	if err := s.db.WithContext(ctx).Model(&models.ToolRunEvent{}).
		Where("run_id = ? AND seq IN ?", runID, seqs).Count(&stored).Error; err != nil {
		return 0, fmt.Errorf("counting stored events of tool run %s: %w", runID, err)
	}
	return len(rows) - int(stored), nil
}

// maxAgentErrorLen bounds the error text an agent may store on a run.
const maxAgentErrorLen = 500

// clip shortens s to at most n bytes without splitting a UTF-8 character.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// AgentFinish is the body of POST jobs/:run_id/finish.
type AgentFinish struct {
	Status  string          `json:"status"`  // done | failed | refused | timed_out
	Summary json.RawMessage `json:"summary"` // nil or JSON null: no summary
	Error   string          `json:"error"`
}

// AgentFinish records agent's outcome for runID. timed_out is the agent's
// tool reaching the job deadline; without error text it gets the same text
// as a Sentinel run that timed out. A run cancelled meanwhile stays
// cancelled and only gains the summary (ruling 8); that is not an error.
// ErrBadFinishStatus for another status; ErrRunNotActive when the run is
// not this agent's, was never claimed, or ended for another reason (a
// missing summary is still filled in).
func (s *Service) AgentFinish(ctx context.Context, agent *models.Agent, runID uuid.UUID, f AgentFinish) error {
	switch f.Status {
	case models.ToolRunDone, models.ToolRunFailed, models.ToolRunRefused, models.ToolRunTimedOut:
	default:
		return ErrBadFinishStatus
	}
	run, err := s.agentRun(ctx, agent, runID)
	if err != nil {
		return err
	}
	if run.Status == models.ToolRunQueued {
		return ErrRunNotActive
	}
	errText := clip(f.Error, maxAgentErrorLen)
	if errText == "" && f.Status == models.ToolRunTimedOut {
		errText = msgTimedOut
	}
	moved, err := s.finish(ctx, runID, f.Status, f.Summary, errText, ownerActor(run))
	if err != nil || moved {
		return err
	}
	cur, err := s.Get(ctx, runID)
	switch {
	case errors.Is(err, ErrRunNotFound):
		return ErrRunNotActive
	case err != nil:
		return err
	case cur.Status == models.ToolRunCancelled:
		return nil
	}
	return ErrRunNotActive
}
