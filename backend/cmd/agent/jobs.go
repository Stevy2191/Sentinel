package main

// Network tools. With ENABLE_TOOLS=true the agent collects jobs from
// Sentinel with a long poll, runs each with nettools, posts its events back
// while it runs and reports how it ended. Sentinel's own per-agent switch
// must be on too; while it is off, jobs/next answers tools_disabled.

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

const (
	// maxConcurrentJobs is how many jobs run at once; Sentinel queues no
	// more than this per agent either.
	maxConcurrentJobs = 2
	// jobPollTimeout: Sentinel holds a poll for up to 25 s.
	jobPollTimeout = 35 * time.Second
	jobPostTimeout = 15 * time.Second
	// jobFlushEvery is how often a running job posts its events. The reply
	// is how a cancel reaches the agent, so a post goes even when empty.
	jobFlushEvery = 250 * time.Millisecond
	// jobMinPoll: a poll that comes back sooner than this without a job (a
	// proxy cutting it short) waits out the rest, so it cannot spin.
	jobMinPoll = time.Second
	// jobDisabledWait is the wait after tools_disabled. It stays under
	// Sentinel's 30 s pickup timeout: an agent counts as ready once it polls
	// with tools on, and a run started just after the switch goes on must
	// be collected before the sweeper fails it.
	jobDisabledWait = 15 * time.Second
	jobBackoffMin   = 5 * time.Second
	jobBackoffMax   = 60 * time.Second
	// The last events post and the finish are tried this many times, this
	// far apart: without the finish a run only ends when Sentinel times it out.
	jobFinalTries = 3
	jobRetryDelay = 2 * time.Second
	// postBudget keeps an events post well inside Sentinel's 64 KB limit.
	postBudget = 48 << 10
)

// toolRunner runs one tool: *nettools.Runner, or a fake in tests.
type toolRunner interface {
	Run(ctx context.Context, s nettools.Spec, emit nettools.Emitter) (any, error)
}

// jobLoop collects and runs jobs until its context ends.
type jobLoop struct {
	client  *jobClient
	runner  toolRunner
	allowed []*net.IPNet // TOOLS_ALLOWED_TARGETS; empty means no local limit
	now     func() time.Time
	slots   chan struct{}
	jobs    sync.WaitGroup
	// toolLimit is how long a job's tool may run: nettools.Deadline, or
	// shorter in tests.
	toolLimit func(nettools.Tool) time.Duration

	// The loop's waits (the job* constants; tests shorten them).
	flushEvery   time.Duration
	minPoll      time.Duration
	disabledWait time.Duration
	backoffMin   time.Duration
	backoffMax   time.Duration
	retryDelay   time.Duration
}

func newJobLoop(cfg config, runner toolRunner) *jobLoop {
	return &jobLoop{
		client: &jobClient{
			baseURL: cfg.ServerURL,
			token:   cfg.ServerToken,
			agentID: cfg.AgentID,
			poll:    &http.Client{Timeout: jobPollTimeout},
			post:    &http.Client{Timeout: jobPostTimeout},
		},
		runner:       runner,
		allowed:      cfg.ToolsAllowed,
		now:          time.Now,
		toolLimit:    nettools.Deadline,
		slots:        make(chan struct{}, maxConcurrentJobs),
		flushEvery:   jobFlushEvery,
		minPoll:      jobMinPoll,
		disabledWait: jobDisabledWait,
		backoffMin:   jobBackoffMin,
		backoffMax:   jobBackoffMax,
		retryDelay:   jobRetryDelay,
	}
}

// run polls for jobs until ctx ends, then waits for running jobs to stop.
//
// It keeps polling while both job slots are busy: Sentinel queues no more
// than two runs per agent anyway, and an agent that stopped polling would
// show as offline there.
func (l *jobLoop) run(ctx context.Context) {
	defer l.jobs.Wait()
	backoff := l.backoffMin
	disabled := false
	for ctx.Err() == nil {
		started := time.Now()
		job, err := l.client.next(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, errToolsDisabled):
			if !disabled {
				log.Printf("network tools are switched off for this agent in Sentinel; asking again every %s", l.disabledWait)
				disabled = true
			}
			backoff = l.backoffMin
			sleepCtx(ctx, l.disabledWait)
		case err != nil:
			log.Printf("collecting tool jobs failed (next try in %s): %v", backoff, err)
			sleepCtx(ctx, backoff)
			backoff = nextBackoff(backoff, l.backoffMax)
		default:
			if disabled {
				log.Println("network tools are switched on for this agent in Sentinel")
				disabled = false
			}
			backoff = l.backoffMin
			if job != nil {
				l.jobs.Add(1)
				go func() {
					defer l.jobs.Done()
					l.handle(ctx, job)
				}()
				continue
			}
			if rest := l.minPoll - time.Since(started); rest > 0 {
				sleepCtx(ctx, rest)
			}
		}
	}
}

// nextBackoff doubles a wait up to limit.
func nextBackoff(cur, limit time.Duration) time.Duration {
	if cur*2 > limit {
		return limit
	}
	return cur * 2
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// check applies the agent's own rules to a job, whatever Sentinel decided:
// the tools' parameter limits (Normalize again, here) and the address rules.
// It returns the spec to run, or why the job is refused.
func (l *jobLoop) check(job *agentJob) (nettools.Spec, string) {
	spec, err := nettools.Normalize(job.Spec)
	if err != nil {
		return spec, "the agent refused the parameters: " + err.Error()
	}
	spec.TargetIP = strings.TrimSpace(job.Spec.TargetIP)
	if spec.TargetIP == "" {
		// Only a lookup through this host's own resolver has no address,
		// and that resolver is always allowed.
		if spec.Tool == nettools.ToolDNS && spec.Params.Server == "" {
			return spec, ""
		}
		return spec, "the job has no target address"
	}
	ip := net.ParseIP(spec.TargetIP).To4()
	if ip == nil {
		return spec, "the target address " + spec.TargetIP + " is not IPv4"
	}
	if nettools.AlwaysBlocked(ip) {
		return spec, spec.TargetIP + " is never probed (metadata, multicast or broadcast address)"
	}
	if len(l.allowed) > 0 && !nettools.InNets(ip, l.allowed) {
		return spec, spec.TargetIP + " is outside TOOLS_ALLOWED_TARGETS on this agent"
	}
	return spec, ""
}

// handle checks one job, runs it inside its deadline while its events are
// posted, and reports how it ended.
func (l *jobLoop) handle(ctx context.Context, job *agentJob) {
	received := l.now()
	spec, reason := l.check(job)
	if reason != "" {
		log.Printf("refused tool job %s: %s", job.RunID, reason)
		l.report(ctx, job.RunID, jobFinish{Status: "refused", Error: reason})
		return
	}
	select {
	case l.slots <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-l.slots }()

	// The agent's own clock bounds the job: the tool's limit from when the
	// job arrived. job.Deadline is on Sentinel's clock, and an agent whose
	// clock runs ahead would see it as already past (every DNS job refused)
	// or near (the other tools cut short). Sentinel's sweeper still bounds
	// the run on its side.
	runCtx, stopTool := context.WithDeadline(ctx, received.Add(l.toolLimit(spec.Tool)))
	defer stopTool()

	log.Printf("running %s job %s against %s", spec.Tool, job.RunID, spec.Target)
	buf := &eventBuffer{}
	toolDone := make(chan struct{})
	followed := make(chan flushResult, 1)
	go func() { followed <- l.follow(ctx, job.RunID, buf, stopTool, toolDone) }()

	summary, runErr := l.runTool(runCtx, job.RunID, spec, func(ev nettools.Event) { buf.add(ev, l.now()) })
	if runErr != nil && errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		// The deadline stopped the tool, whatever error it gave for it. Read
		// now: the deadline may pass while the last events are posted.
		runErr = context.DeadlineExceeded
	}
	close(toolDone)
	result := <-followed
	if result == flushOK {
		// What the tool emitted since the last tick goes before the finish.
		result = l.flushFinal(ctx, job.RunID, buf)
	}
	switch result {
	case flushGone:
		log.Printf("tool job %s: Sentinel no longer runs it; stopped", job.RunID)
	case flushTooMuch:
		l.report(ctx, job.RunID, jobFinish{Status: "failed", Summary: marshalSummary(summary), Error: errTooMuchOutput.Error()})
	case flushCancelled:
		// Sentinel keeps the run cancelled (ruling 8); this only files the
		// partial summary.
		l.report(ctx, job.RunID, jobFinish{Status: "failed", Summary: marshalSummary(summary), Error: "cancelled"})
	default:
		l.report(ctx, job.RunID, outcome(summary, runErr))
	}
}

// errToolPanicked is a tool that panicked; its run is reported failed with it.
var errToolPanicked = errors.New("the tool stopped unexpectedly")

// runTool runs the job's tool, turning a panic into errToolPanicked (and a
// log entry with the stack): the run is reported failed, its events still
// go out, and the agent carries on with its other jobs. The emitter runs on
// the tool's goroutine, so it is covered too.
func (l *jobLoop) runTool(ctx context.Context, runID string, spec nettools.Spec, emit nettools.Emitter) (summary any, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("tool job %s: the %s tool panicked: %v\n%s", runID, spec.Tool, r, debug.Stack())
			summary, err = nil, errToolPanicked
		}
	}()
	return l.runner.Run(ctx, spec, emit)
}

// flushResult is how a round of events posts went.
type flushResult int

const (
	flushOK        flushResult = iota // everything pending was stored
	flushRetry                        // a post failed; the events wait for the next one
	flushCancelled                    // Sentinel says the run was cancelled
	flushGone                         // Sentinel no longer runs it (409)
	flushTooMuch                      // Sentinel refused more output (413)
)

// follow posts the job's events every flushEvery until the tool returns
// (done closes). When Sentinel says stop, it stops the tool and says why.
func (l *jobLoop) follow(ctx context.Context, runID string, buf *eventBuffer, stopTool context.CancelFunc, done <-chan struct{}) flushResult {
	tick := time.NewTicker(l.flushEvery)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return flushOK
		case <-ctx.Done():
			return flushOK
		case <-tick.C:
			switch r := l.flush(ctx, runID, buf, true); r {
			case flushCancelled, flushGone, flushTooMuch:
				stopTool()
				return r
			}
		}
	}
}

// flush posts what is pending in batches under postBudget. With always set
// it posts even when nothing is pending, to hear about a cancel.
func (l *jobLoop) flush(ctx context.Context, runID string, buf *eventBuffer, always bool) flushResult {
	for {
		batch := buf.batch(postBudget)
		if len(batch) == 0 && !always {
			return flushOK
		}
		cancel, err := l.client.postEvents(ctx, runID, batch)
		switch {
		case errors.Is(err, errRunGone):
			return flushGone
		case errors.Is(err, errTooMuchOutput):
			return flushTooMuch
		case err != nil:
			log.Printf("tool job %s: posting events failed (will retry): %v", runID, err)
			return flushRetry
		}
		buf.drop(len(batch))
		if cancel {
			return flushCancelled
		}
		if buf.empty() {
			return flushOK
		}
		always = false
	}
}

// flushFinal posts what is left after the tool returned, trying a few times.
func (l *jobLoop) flushFinal(ctx context.Context, runID string, buf *eventBuffer) flushResult {
	r := l.flush(ctx, runID, buf, false)
	for try := 1; try < jobFinalTries && r == flushRetry && ctx.Err() == nil; try++ {
		sleepCtx(ctx, l.retryDelay)
		r = l.flush(ctx, runID, buf, false)
	}
	return r
}

// report sends the finish, trying a few times.
func (l *jobLoop) report(ctx context.Context, runID string, f jobFinish) {
	for try := 1; ; try++ {
		err := l.client.finish(ctx, runID, f)
		if err == nil {
			return
		}
		if errors.Is(err, errRunGone) || ctx.Err() != nil || try == jobFinalTries {
			log.Printf("tool job %s: reporting the result failed: %v", runID, err)
			return
		}
		sleepCtx(ctx, l.retryDelay)
	}
}

// outcome turns what the tool returned into the finish post. A tool the
// deadline stopped is timed_out with no error text: Sentinel words it as it
// does its own runs that time out. A nil summary (any failure but the
// context's) is left out, which Sentinel stores as no summary.
func outcome(summary any, err error) jobFinish {
	f := jobFinish{Status: "done", Summary: marshalSummary(summary)}
	switch {
	case err == nil:
	case errors.Is(err, context.DeadlineExceeded):
		f.Status = "timed_out"
	default:
		f.Status, f.Error = "failed", err.Error()
	}
	return f
}

func marshalSummary(summary any) json.RawMessage {
	if summary == nil {
		return nil
	}
	data, err := json.Marshal(summary)
	if err != nil {
		return nil
	}
	return data
}
