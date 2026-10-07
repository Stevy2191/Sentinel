package toolruns

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// finalWriteTimeout bounds the last flush and the finish of a local run,
// which use a fresh context because the tool's may already be over.
const finalWriteTimeout = 10 * time.Second

// recorder buffers one local run's events, numbering them from 1, until
// the next flush. It stops accepting events at MaxEventsPerRun and stops
// the tool.
type recorder struct {
	s     *Service
	runID uuid.UUID
	stop  context.CancelFunc

	mu   sync.Mutex
	buf  []models.ToolRunEvent
	next int
	full bool
}

// emit is the tool's Emitter. The tool calls it from one goroutine; the
// mutex guards the buffer against the flusher.
func (r *recorder) emit(ev nettools.Event) {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		data = []byte(`null`)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.full {
		return
	}
	if r.next >= MaxEventsPerRun {
		r.full = true
		r.stop()
		return
	}
	r.next++
	r.buf = append(r.buf, models.ToolRunEvent{RunID: r.runID, Seq: r.next, At: r.s.now(), Type: ev.Type, Data: data})
}

// flush stores what is buffered.
func (r *recorder) flush(ctx context.Context) {
	r.mu.Lock()
	batch := r.buf
	r.buf = nil
	r.mu.Unlock()
	if len(batch) == 0 {
		return
	}
	if _, err := r.s.insertEvents(ctx, r.runID, batch); err != nil {
		log.Printf("[tools] run %s: %v", r.runID, err)
	}
}

func (r *recorder) isFull() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.full
}

// localRun is what the launcher keeps of a run: copies, because the
// *models.ToolRun Create returns is also its caller's to serialise.
type localRun struct {
	id    uuid.UUID
	owner services.Actor
}

// launchLocal starts a Sentinel-server run in a goroutine, with the run's
// deadline on its context (the Runner applies none of its own). Cancel and
// the sweeper stop it through s.cancels.
func (s *Service) launchLocal(run *models.ToolRun, spec nettools.Spec) {
	lr := localRun{id: run.ID, owner: ownerActor(run)}
	ctx, cancel := context.WithDeadline(context.Background(), run.Deadline)
	s.mu.Lock()
	s.cancels[lr.id] = cancel
	s.mu.Unlock()
	go s.runLocal(ctx, cancel, lr, spec)
}

// runLocal runs the tool, flushing its events every FlushEvery, then
// records the outcome through finish, the same path agent runs take.
func (s *Service) runLocal(ctx context.Context, cancel context.CancelFunc, run localRun, spec nettools.Spec) {
	defer func() {
		s.mu.Lock()
		delete(s.cancels, run.id)
		s.mu.Unlock()
		cancel()
	}()
	s.publishStatus(run.id, models.ToolRunRunning)
	rec := &recorder{s: s, runID: run.id, stop: cancel}

	stopFlush, flushed := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(flushed)
		tick := time.NewTicker(FlushEvery)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				rec.flush(context.Background())
			case <-stopFlush:
				return
			}
		}
	}()
	summary, err := s.runToolSafely(ctx, run.id, spec, rec.emit)
	close(stopFlush)
	<-flushed

	wctx, wcancel := context.WithTimeout(context.Background(), finalWriteTimeout)
	defer wcancel()
	rec.flush(wctx)
	status, errText := localOutcome(rec.isFull(), err)
	var sum []byte
	if summary != nil {
		if sum, err = json.Marshal(summary); err != nil {
			log.Printf("[tools] run %s: encoding the summary: %v", run.id, err)
			sum = nil
		}
	}
	if _, err := s.finish(wctx, run.id, status, sum, errText, run.owner); err != nil {
		log.Printf("[tools] %v", err)
	}
}

// errToolPanicked is a tool that panicked; its run ends failed with this.
var errToolPanicked = errors.New(msgToolPanicked)

// runToolSafely runs the tool, turning a panic into errToolPanicked (and a
// log entry with the stack), so the run still finishes and the server stays
// up. The emitter runs on the tool's goroutine, so it is covered too.
func (s *Service) runToolSafely(ctx context.Context, runID uuid.UUID, spec nettools.Spec, emit nettools.Emitter) (summary any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[tools] run %s: the %s tool panicked: %v\n%s", runID, spec.Tool, r, debug.Stack())
			summary, err = nil, errToolPanicked
		}
	}()
	return s.runTool(ctx, spec, emit)
}

// localOutcome maps how a local tool ended to a final status and error
// text. Hitting the event cap wins: the recorder cancelled the tool itself.
func localOutcome(full bool, err error) (status, errText string) {
	switch {
	case full:
		return models.ToolRunFailed, msgTooMuchOutput
	case err == nil:
		return models.ToolRunDone, ""
	case errors.Is(err, context.DeadlineExceeded):
		return models.ToolRunTimedOut, msgTimedOut
	case errors.Is(err, context.Canceled):
		return models.ToolRunCancelled, ""
	default:
		return models.ToolRunFailed, err.Error()
	}
}
