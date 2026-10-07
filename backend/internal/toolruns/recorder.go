package toolruns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
)

// Messages for runs that end without the tool saying why.
const (
	msgTooMuchOutput = "too much output"
	msgNotPickedUp   = "the agent didn't pick up the job (offline?)"
	msgTimedOut      = "the run went past its time limit"
	msgInterrupted   = "Sentinel restarted while the run was in progress"
	msgToolPanicked  = "the tool stopped unexpectedly"
)

// systemActor audits the finishes nobody asked for: the sweeper's.
var systemActor = services.Actor{Username: "system"}

// ownerActor audits a run's own outcome as the user who started it.
func ownerActor(run *models.ToolRun) services.Actor {
	a := services.Actor{Username: run.Username}
	if run.UserID != nil {
		a.UserID = *run.UserID
	}
	return a
}

// insertBatchSize keeps one INSERT's parameters (5 per event) far below
// Postgres' 65535.
const insertBatchSize = 1000

// Subscribe returns a live feed of runID's frames ("event", "status",
// "end"). The SSE handler subscribes before replaying stored events, so
// nothing published in between is lost.
func (s *Service) Subscribe(runID uuid.UUID) *stream.Subscription {
	return s.hub.Subscribe(runID.String())
}

// insertEvents stores events for runID, ignoring any whose seq is already
// stored, adds the number actually inserted to the run's event_count, and
// publishes each inserted event as an "event" frame in seq order. Shared by
// local runs (the recorder) and agent posts.
func (s *Service) insertEvents(ctx context.Context, runID uuid.UUID, events []models.ToolRunEvent) (int, error) {
	inserted := 0
	for start := 0; start < len(events); start += insertBatchSize {
		batch := events[start:min(start+insertBatchSize, len(events))]
		bySeq := make(map[int]models.ToolRunEvent, len(batch))
		values := make([]string, 0, len(batch))
		args := make([]any, 0, len(batch)*5+1)
		for _, ev := range batch {
			bySeq[ev.Seq] = ev
			values = append(values, "(?, ?, ?, ?, ?)")
			args = append(args, runID, ev.Seq, ev.At, ev.Type, ev.Data)
		}
		args = append(args, runID)
		// One statement: the insert and the count bump commit together, so
		// event_count always equals the stored events.
		var seqs []int
		err := s.db.WithContext(ctx).Raw(`WITH ins AS (
				INSERT INTO tool_run_events (run_id, seq, at, type, data)
				VALUES `+strings.Join(values, ", ")+`
				ON CONFLICT (run_id, seq) DO NOTHING
				RETURNING seq
			), bump AS (
				UPDATE tool_runs SET event_count = event_count + (SELECT count(*) FROM ins) WHERE id = ?
			)
			SELECT seq FROM ins ORDER BY seq`, args...).Scan(&seqs).Error
		if err != nil {
			return inserted, fmt.Errorf("storing tool run events: %w", err)
		}
		for _, seq := range seqs {
			ev := bySeq[seq]
			ev.RunID = runID
			s.publishEvent(ev)
		}
		inserted += len(seqs)
	}
	return inserted, nil
}

// publishEvent sends one stored event to the run's live subscribers.
func (s *Service) publishEvent(ev models.ToolRunEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		log.Printf("[tools] encoding event %d of run %s: %v", ev.Seq, ev.RunID, err)
		return
	}
	s.hub.Publish(ev.RunID.String(), stream.Message{ID: strconv.Itoa(ev.Seq), Event: "event", Data: data})
}

// publishStatus tells subscribers a run's status changed (to running).
func (s *Service) publishStatus(runID uuid.UUID, status string) {
	data, _ := json.Marshal(map[string]string{"status": status})
	s.hub.Publish(runID.String(), stream.Message{Event: "status", Data: data})
}

// publishEnd sends the final run; streams close after it.
func (s *Service) publishEnd(run *models.ToolRun) {
	data, err := json.Marshal(run)
	if err != nil {
		log.Printf("[tools] encoding run %s: %v", run.ID, err)
		return
	}
	s.hub.Publish(run.ID.String(), stream.Message{Event: "end", Data: data})
}

// finish moves a queued or running run to a final status and reports
// whether this call did. The first finish wins (cancel wins, ruling 8):
// when the run is already final, a later finish only fills the summary if
// it is still empty and never changes the status, error or end frame. On
// the transition it stops a local run's tool, publishes "end" and audits
// tool_run_finished as actor. A nil, empty or JSON null summary leaves the
// column SQL NULL; errText "" leaves error NULL.
func (s *Service) finish(ctx context.Context, runID uuid.UUID, status string, summary []byte, errText string, actor services.Actor) (bool, error) {
	var sum, errVal any
	if trimmed := bytes.TrimSpace(summary); len(trimmed) > 0 && string(trimmed) != "null" {
		sum = string(trimmed)
	}
	if errText != "" {
		errVal = errText
	}
	var runs []models.ToolRun
	err := s.db.WithContext(ctx).Raw(`UPDATE tool_runs
		SET status = ?, finished_at = ?, summary = COALESCE(summary, ?::jsonb), error = COALESCE(error, ?::text)
		WHERE id = ? AND status IN (?, ?)
		RETURNING *`, status, s.now(), sum, errVal, runID, models.ToolRunQueued, models.ToolRunRunning).Scan(&runs).Error
	if err != nil {
		return false, fmt.Errorf("finishing tool run %s: %w", runID, err)
	}
	if len(runs) == 0 {
		if sum != nil {
			if err := s.db.WithContext(ctx).Exec(`UPDATE tool_runs SET summary = ?::jsonb WHERE id = ? AND summary IS NULL`,
				sum, runID).Error; err != nil {
				return false, fmt.Errorf("filling the summary of tool run %s: %w", runID, err)
			}
		}
		return false, nil
	}
	run := &runs[0]
	s.cancelLocal(runID)
	s.publishEnd(run)
	s.audit.Record(ctx, actor, models.ActionToolRunFinished, models.ResourceToolRun, &run.ID,
		models.AuditChanges{Summary: map[string]any{
			"tool": run.Tool, "target": run.Target, "status": run.Status,
			"duration_ms": runDuration(run).Milliseconds(), "result": resultLine(run),
		}})
	return true, nil
}

// cancelLocal stops runID's tool if it is running on this server.
func (s *Service) cancelLocal(runID uuid.UUID) {
	s.mu.Lock()
	cancel := s.cancels[runID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// runDuration is how long a finished run took, from its start (or its
// creation, if it never started).
func runDuration(run *models.ToolRun) time.Duration {
	if run.FinishedAt == nil {
		return 0
	}
	from := run.CreatedAt
	if run.StartedAt != nil {
		from = *run.StartedAt
	}
	return run.FinishedAt.Sub(from)
}

// resultLine is a finished run's one-line result for the audit log:
// "0% loss, 2.1 ms avg", "reached in 9 hops", "NOERROR, 2 answers",
// "3 open of 100"; for a run that did not finish done, its error or status.
func resultLine(run *models.ToolRun) string {
	if run.Status != models.ToolRunDone {
		if run.Error != nil && *run.Error != "" {
			return *run.Error
		}
		return run.Status
	}
	if run.Summary == nil {
		return run.Status
	}
	raw := []byte(*run.Summary)
	switch nettools.Tool(run.Tool) {
	case nettools.ToolPing:
		var p nettools.PingSummary
		if json.Unmarshal(raw, &p) == nil {
			loss := strconv.FormatFloat(p.LossPct, 'f', -1, 64) + "% loss"
			if p.AvgMS == nil {
				return loss
			}
			return fmt.Sprintf("%s, %.1f ms avg", loss, *p.AvgMS)
		}
	case nettools.ToolTraceroute:
		var tr nettools.TraceSummary
		if json.Unmarshal(raw, &tr) == nil {
			if tr.Reached {
				return fmt.Sprintf("reached in %d hops", tr.HopCount)
			}
			return fmt.Sprintf("not reached (%d hops)", tr.HopCount)
		}
	case nettools.ToolDNS:
		var d nettools.DNSSummary
		if json.Unmarshal(raw, &d) == nil {
			if d.AnswerCount == 1 {
				return d.RCode + ", 1 answer"
			}
			return fmt.Sprintf("%s, %d answers", d.RCode, d.AnswerCount)
		}
	case nettools.ToolTCP:
		var tc nettools.TCPSummary
		if json.Unmarshal(raw, &tc) == nil {
			return fmt.Sprintf("%d open of %d", tc.Open, tc.Total)
		}
	}
	return run.Status
}
