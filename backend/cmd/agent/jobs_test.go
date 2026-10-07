package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
)

func TestJobLoopRunsAJobAndPostsItsEventsInOrder(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() { f.jobs = []agentJob{testJob("run-1", "10.0.0.5")} })
	specs := make(chan nettools.Spec, 1)
	runner := runnerFunc(func(_ context.Context, s nettools.Spec, emit nettools.Emitter) (any, error) {
		specs <- s
		for i := 1; i <= 5; i++ {
			emit(nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: i, RTTMS: 1.5, TTL: 64, From: s.TargetIP}})
			time.Sleep(3 * time.Millisecond)
		}
		return nettools.PingSummary{Sent: 5, Received: 5}, nil
	})
	startLoop(t, testLoop(f, runner, nil))

	fin := f.waitFinish(t, "run-1")
	var sum nettools.PingSummary
	if fin.Status != "done" || fin.Error != "" || json.Unmarshal(fin.Summary, &sum) != nil || sum.Sent != 5 || sum.Received != 5 {
		t.Errorf("finish = %+v (summary %s), want done with 5 of 5", fin, fin.Summary)
	}
	evs := f.eventsOf("run-1")
	if len(evs) != 5 {
		t.Fatalf("%d events stored before the finish, want 5", len(evs))
	}
	for i, ev := range evs {
		var reply nettools.PingReply
		if ev.Seq != i+1 || ev.Type != nettools.EventReply || json.Unmarshal(ev.Data, &reply) != nil || reply.Seq != i+1 || reply.From != "10.0.0.5" {
			t.Errorf("event %d = %+v (%s)", i, ev, ev.Data)
		}
	}
	// The agent ran the spec it normalized itself: the defaults are filled in.
	if s := <-specs; s.Params.Count != nettools.PingCountDefault || s.TargetIP != "10.0.0.5" {
		t.Errorf("ran %+v, want the default count and the job's address", s)
	}
}

// A port scan can emit hundreds of events between two posts; each post stays
// under Sentinel's 64 KB limit and none is lost.
func TestJobLoopSplitsBurstsUnderThePostLimit(t *testing.T) {
	f := newFakeSentinel(t)
	job := testJob("run-1", "10.0.0.5")
	job.Spec.Tool, job.Spec.Params = nettools.ToolTCP, nettools.Params{Ports: "1-1024"}
	f.set(func() { f.jobs = []agentJob{job} })
	const n = 1500
	runner := runnerFunc(func(_ context.Context, _ nettools.Spec, emit nettools.Emitter) (any, error) {
		for i := 1; i <= n; i++ {
			emit(nettools.Event{Type: nettools.EventPort, Data: nettools.PortResult{Port: i, State: nettools.PortClosed}})
		}
		return nettools.TCPSummary{Total: n, Closed: n, OpenPorts: []int{}}, nil
	})
	startLoop(t, testLoop(f, runner, nil))

	if fin := f.waitFinish(t, "run-1"); fin.Status != "done" {
		t.Fatalf("finish = %+v, want done", fin)
	}
	evs := f.eventsOf("run-1")
	if len(evs) != n {
		t.Fatalf("%d events stored, want %d", len(evs), n)
	}
	for i, ev := range evs {
		if ev.Seq != i+1 {
			t.Fatalf("event %d has seq %d: out of order or lost", i, ev.Seq)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.largestPost > 64<<10 || f.posts < 3 {
		t.Errorf("largest post %d bytes over %d posts; want every post within 64 KB, so several posts", f.largestPost, f.posts)
	}
}

func TestJobLoopRefusesJobsItWillNotRun(t *testing.T) {
	_, lan, _ := net.ParseCIDR("10.0.0.0/24")
	_, linkLocal, _ := net.ParseCIDR("169.254.0.0/16")
	tooMany := testJob("run-1", "10.0.0.5")
	tooMany.Spec.Params.Count = 1000
	noAddress := testJob("run-1", "")
	noAddress.Spec.Target = "files.example.org"
	// A lookup asking a named server contacts that server: the address rules
	// apply to it like to a probed host.
	dnsVia := func(server string) agentJob {
		return agentJob{RunID: "run-1", Deadline: time.Now().Add(time.Minute), Spec: nettools.Spec{
			Tool: nettools.ToolDNS, Target: "example.org", TargetIP: server, Params: nettools.Params{Server: server},
		}}
	}
	cases := []struct {
		name    string
		job     agentJob
		allowed []*net.IPNet
		reason  string
	}{
		{"outside TOOLS_ALLOWED_TARGETS", testJob("run-1", "192.168.1.5"), []*net.IPNet{lan}, "outside TOOLS_ALLOWED_TARGETS"},
		{"cloud metadata address", testJob("run-1", "169.254.169.254"), nil, "never probed"},
		// Defence in depth: a compromised Sentinel cannot point the agent at
		// an always-blocked address, even one TOOLS_ALLOWED_TARGETS covers.
		{"cloud metadata address inside TOOLS_ALLOWED_TARGETS", testJob("run-1", "169.254.169.254"), []*net.IPNet{linkLocal}, "never probed"},
		{"multicast DNS server", dnsVia("224.0.0.251"), nil, "never probed"},
		{"DNS server outside TOOLS_ALLOWED_TARGETS", dnsVia("8.8.8.8"), []*net.IPNet{lan}, "outside TOOLS_ALLOWED_TARGETS"},
		{"parameters past the limits", tooMany, nil, "count"},
		{"no address to probe", noAddress, nil, "no target address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSentinel(t)
			f.set(func() { f.jobs = []agentJob{tc.job} })
			var ran atomic.Int32
			runner := runnerFunc(func(context.Context, nettools.Spec, nettools.Emitter) (any, error) {
				ran.Add(1)
				return nil, nil
			})
			startLoop(t, testLoop(f, runner, tc.allowed))
			fin := f.waitFinish(t, "run-1")
			if fin.Status != "refused" || !strings.Contains(fin.Error, tc.reason) {
				t.Errorf("finish = %+v, want refused mentioning %q", fin, tc.reason)
			}
			if ran.Load() != 0 {
				t.Error("the tool ran for a refused job")
			}
		})
	}
}

// The agent bounds a job by its own clock: the tool's limit from when the job
// arrived. Sentinel's deadline is on Sentinel's clock, so an agent whose clock
// runs fast would otherwise refuse every DNS job (15 s limit) as already past
// its deadline, and cut the other tools short; Sentinel's sweeper still bounds
// the run on its side.
func TestJobLoopBoundsAJobByItsOwnClock(t *testing.T) {
	cases := []struct {
		name     string
		job      agentJob
		deadline time.Duration // Sentinel's, from now on the agent's clock
	}{
		{"a lookup whose deadline already passed here (the agent runs 1 min fast)",
			agentJob{RunID: "run-1", Spec: nettools.Spec{Tool: nettools.ToolDNS, Target: "example.org"}}, -time.Minute},
		{"a ping whose deadline is 200 ms away here", testJob("run-1", "10.0.0.5"), 200 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSentinel(t)
			job := tc.job
			job.Deadline = time.Now().Add(tc.deadline)
			f.set(func() { f.jobs = []agentJob{job} })
			deadlines := make(chan time.Time, 1)
			runner := runnerFunc(func(ctx context.Context, _ nettools.Spec, _ nettools.Emitter) (any, error) {
				d, _ := ctx.Deadline()
				deadlines <- d
				return nil, nil
			})
			before := time.Now()
			startLoop(t, testLoop(f, runner, nil))
			if fin := f.waitFinish(t, "run-1"); fin.Status != "done" {
				t.Fatalf("finish = %+v, want done: the job runs whatever Sentinel's clock says", fin)
			}
			limit := nettools.Deadline(job.Spec.Tool)
			if d := <-deadlines; d.Before(before.Add(limit)) || d.After(time.Now().Add(limit)) {
				t.Errorf("the tool's deadline is %s from the start, want the tool's limit (%s) on the agent's clock",
					d.Sub(before), limit)
			}
		})
	}
}

// A lookup through the agent's own resolver contacts no job address, so
// TOOLS_ALLOWED_TARGETS does not stop it.
func TestJobLoopRunsSystemResolverLookupsWhateverTheAllowedList(t *testing.T) {
	_, lan, _ := net.ParseCIDR("10.0.0.0/24")
	f := newFakeSentinel(t)
	f.set(func() {
		f.jobs = []agentJob{{RunID: "run-1", Spec: nettools.Spec{Tool: nettools.ToolDNS, Target: "example.org"},
			Deadline: time.Now().Add(time.Minute)}}
	})
	runner := runnerFunc(func(context.Context, nettools.Spec, nettools.Emitter) (any, error) {
		return nettools.DNSSummary{Server: "127.0.0.53:53", RCode: "NOERROR", AnswerCount: 1}, nil
	})
	startLoop(t, testLoop(f, runner, []*net.IPNet{lan}))
	if fin := f.waitFinish(t, "run-1"); fin.Status != "done" {
		t.Errorf("finish = %+v, want done", fin)
	}
}

// Review Focus 5: a user cancels while the agent is mid-run. The next events
// post answers cancel: true; the agent stops the tool at once and reports
// failed/"cancelled", which Sentinel files under the cancelled run without
// changing its status.
func TestJobLoopStopsTheToolWhenSentinelCancels(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() { f.jobs = []agentJob{testJob("run-1", "10.0.0.5")} })
	stopped := make(chan error, 1)
	runner := runnerFunc(func(ctx context.Context, s nettools.Spec, emit nettools.Emitter) (any, error) {
		emit(nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: 1, RTTMS: 1, TTL: 64, From: s.TargetIP}})
		<-ctx.Done()
		stopped <- ctx.Err()
		return nettools.PingSummary{Sent: 1, Received: 1}, ctx.Err()
	})
	startLoop(t, testLoop(f, runner, nil))

	waitFor(t, "the first event", func() bool { return len(f.eventsOf("run-1")) == 1 })
	f.set(func() { f.cancelled["run-1"] = true })
	select {
	case err := <-stopped:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the tool stopped with %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the tool kept running after Sentinel cancelled the run")
	}
	fin := f.waitFinish(t, "run-1")
	var sum nettools.PingSummary
	if fin.Status != "failed" || fin.Error != "cancelled" || json.Unmarshal(fin.Summary, &sum) != nil || sum.Sent != 1 {
		t.Errorf("finish = %+v (summary %s), want failed / cancelled with the partial summary", fin, fin.Summary)
	}
}

// A 409 means Sentinel no longer runs the job (restarted, or the run was
// ended): the agent stops the tool and sends nothing more.
func TestJobLoopStopsWhenSentinelNoLongerRunsTheJob(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() {
		f.jobs = []agentJob{testJob("run-1", "10.0.0.5")}
		f.gone["run-1"] = true
	})
	stopped := make(chan struct{})
	runner := runnerFunc(func(ctx context.Context, _ nettools.Spec, _ nettools.Emitter) (any, error) {
		<-ctx.Done()
		close(stopped)
		return nil, ctx.Err()
	})
	startLoop(t, testLoop(f, runner, nil))
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("the tool kept running after a 409")
	}
	time.Sleep(100 * time.Millisecond)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.finishes) != 0 {
		t.Errorf("finish posted for a run Sentinel no longer runs: %+v", f.finishes)
	}
}

// A tool that fails for any reason but its context returns no summary
// (nettools.Runner's contract), and the finish carries none.
func TestJobLoopReportsAToolThatCannotRun(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() { f.jobs = []agentJob{testJob("run-1", "10.0.0.5")} })
	runner := runnerFunc(func(context.Context, nettools.Spec, nettools.Emitter) (any, error) {
		return nil, nettools.ErrICMPUnavailable
	})
	startLoop(t, testLoop(f, runner, nil))
	fin := f.waitFinish(t, "run-1")
	if fin.Status != "failed" || fin.Error != "ICMP isn't available here (needs root or NET_RAW)" || len(fin.Summary) != 0 {
		t.Errorf("finish = %+v (summary %s), want failed with the ICMP message and no summary", fin, fin.Summary)
	}
}

// A tool that panics fails its run with a fixed message instead of taking the
// agent down; the events it emitted first are still posted, and the next job
// runs as usual.
func TestJobLoopSurvivesAToolThatPanics(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() { f.jobs = []agentJob{testJob("run-1", "10.0.0.5"), testJob("run-2", "10.0.0.6")} })
	runner := runnerFunc(func(_ context.Context, s nettools.Spec, emit nettools.Emitter) (any, error) {
		if s.TargetIP == "10.0.0.5" {
			emit(nettools.Event{Type: nettools.EventReply, Data: nettools.PingReply{Seq: 1, RTTMS: 1, TTL: 64, From: s.TargetIP}})
			panic("a bug in the tool")
		}
		return nettools.PingSummary{Sent: 1, Received: 1}, nil
	})
	startLoop(t, testLoop(f, runner, nil))

	fin := f.waitFinish(t, "run-1")
	if fin.Status != "failed" || fin.Error != "the tool stopped unexpectedly" || len(fin.Summary) != 0 {
		t.Errorf("finish = %+v (summary %s), want failed / the tool stopped unexpectedly with no summary", fin, fin.Summary)
	}
	if n := len(f.eventsOf("run-1")); n != 1 {
		t.Errorf("%d events posted before the panic's finish, want 1", n)
	}
	if fin := f.waitFinish(t, "run-2"); fin.Status != "done" {
		t.Errorf("the next job: finish = %+v, want done", fin)
	}
}

// A tool stopped by its deadline (the agent's own clock plus the tool's
// limit) reports timed_out with no error text: Sentinel then words it as it
// does its own runs that time out. A tool error once the deadline has passed
// counts as the deadline's doing.
func TestJobLoopReportsTimedOutAtTheDeadline(t *testing.T) {
	cases := []struct {
		name    string
		ret     func(ctx context.Context) (any, error)
		summary bool
	}{
		{"the context's error, with the partial summary", func(ctx context.Context) (any, error) {
			return nettools.PingSummary{Sent: 2, Received: 2}, ctx.Err()
		}, true},
		{"the tool's own error after the deadline", func(context.Context) (any, error) {
			return nil, errors.New("probe failed")
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeSentinel(t)
			f.set(func() { f.jobs = []agentJob{testJob("run-1", "10.0.0.5")} })
			runner := runnerFunc(func(ctx context.Context, _ nettools.Spec, _ nettools.Emitter) (any, error) {
				<-ctx.Done()
				return tc.ret(ctx)
			})
			l := testLoop(f, runner, nil)
			// The agent's own limit for the tool (2 minutes for ping).
			l.toolLimit = func(nettools.Tool) time.Duration { return 200 * time.Millisecond }
			startLoop(t, l)
			fin := f.waitFinish(t, "run-1")
			if fin.Status != "timed_out" || fin.Error != "" {
				t.Errorf("finish = %s / %q, want timed_out with no error text", fin.Status, fin.Error)
			}
			var sum nettools.PingSummary
			switch {
			case tc.summary && (json.Unmarshal(fin.Summary, &sum) != nil || sum.Sent != 2):
				t.Errorf("summary %s, want the partial summary (2 sent)", fin.Summary)
			case !tc.summary && len(fin.Summary) != 0:
				t.Errorf("summary %s, want none", fin.Summary)
			}
		})
	}
}

func TestJobLoopRunsAtMostTwoJobsAtOnce(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() {
		f.jobs = []agentJob{testJob("run-1", "10.0.0.5"), testJob("run-2", "10.0.0.6"), testJob("run-3", "10.0.0.7")}
	})
	var mu sync.Mutex
	running, most := 0, 0
	counts := func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return running, most
	}
	release := make(chan struct{})
	runner := runnerFunc(func(ctx context.Context, _ nettools.Spec, _ nettools.Emitter) (any, error) {
		mu.Lock()
		running++
		most = max(most, running)
		mu.Unlock()
		defer func() {
			mu.Lock()
			running--
			mu.Unlock()
		}()
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nettools.PingSummary{}, nil
	})
	startLoop(t, testLoop(f, runner, nil))

	waitFor(t, "two jobs running", func() bool { n, _ := counts(); return n == 2 })
	time.Sleep(50 * time.Millisecond)
	if n, _ := counts(); n != 2 {
		t.Errorf("%d jobs running, want 2: the third waits for a slot", n)
	}
	close(release)
	for _, run := range []string{"run-1", "run-2", "run-3"} {
		if fin := f.waitFinish(t, run); fin.Status != "done" {
			t.Errorf("%s: finish = %+v, want done", run, fin)
		}
	}
	if _, m := counts(); m != 2 {
		t.Errorf("at most %d jobs ran at once, want 2", m)
	}
}

// While Sentinel's switch is off the agent asks again only every
// disabledWait (15 s; 1 s here), not in a tight loop.
func TestJobLoopWaitsWhileToolsAreDisabled(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() {
		f.nextStatus = http.StatusForbidden
		f.nextBody = `{"success":false,"error":{"code":"tools_disabled","message":"network tools are switched off for this agent in Sentinel"}}`
	})
	startLoop(t, testLoop(f, runnerFunc(nil), nil))
	waitFor(t, "the first poll", func() bool { return f.pollCount() >= 1 })
	time.Sleep(300 * time.Millisecond)
	if n := f.pollCount(); n != 1 {
		t.Errorf("%d polls in 300 ms after tools_disabled, want 1", n)
	}
}

// The wait after tools_disabled is 15 s, under Sentinel's 30 s pickup
// timeout: a run started just after an admin switches tools on is collected
// on the agent's next poll, before the sweeper fails it as not picked up.
func TestJobLoopAsksAgainWithinThePickupTimeoutAfterToolsDisabled(t *testing.T) {
	if jobDisabledWait != 15*time.Second {
		t.Errorf("jobDisabledWait = %s, want 15s (below Sentinel's 30 s pickup timeout)", jobDisabledWait)
	}
	if l := newJobLoop(config{}, nil); l.disabledWait != jobDisabledWait {
		t.Errorf("the loop waits %s after tools_disabled, want jobDisabledWait (%s)", l.disabledWait, jobDisabledWait)
	}

	// Tools switched on during the wait: the next poll collects the job.
	f := newFakeSentinel(t)
	f.set(func() {
		f.nextStatus = http.StatusForbidden
		f.nextBody = `{"success":false,"error":{"code":"tools_disabled","message":"network tools are switched off for this agent in Sentinel"}}`
	})
	runner := runnerFunc(func(context.Context, nettools.Spec, nettools.Emitter) (any, error) {
		return nettools.PingSummary{Sent: 1, Received: 1}, nil
	})
	l := testLoop(f, runner, nil)
	l.disabledWait = 100 * time.Millisecond
	startLoop(t, l)
	waitFor(t, "the refused poll", func() bool { return f.pollCount() >= 1 })
	f.set(func() {
		f.nextStatus, f.nextBody = 0, ""
		f.jobs = []agentJob{testJob("run-1", "10.0.0.5")}
	})
	if fin := f.waitFinish(t, "run-1"); fin.Status != "done" {
		t.Errorf("finish = %+v, want done once tools are on", fin)
	}
}

// After a failed poll the agent waits backoffMin (5 s; 50 ms here), doubling.
func TestJobLoopBacksOffAfterFailedPolls(t *testing.T) {
	f := newFakeSentinel(t)
	f.set(func() { f.nextStatus = http.StatusBadGateway })
	startLoop(t, testLoop(f, runnerFunc(nil), nil))
	time.Sleep(120 * time.Millisecond)
	// Polls go at 0, after at least 50 ms, then after at least 100 ms more:
	// no more than two fit in the first 120 ms.
	if n := f.pollCount(); n < 1 || n > 2 {
		t.Errorf("%d polls in 120 ms against a failing server, want 1 or 2", n)
	}
}

func TestNextBackoffDoublesUpToTheCap(t *testing.T) {
	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second}
	d := jobBackoffMin
	for i, w := range want {
		if d != w {
			t.Errorf("wait %d = %s, want %s", i+1, d, w)
		}
		d = nextBackoff(d, jobBackoffMax)
	}
}
