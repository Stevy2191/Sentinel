package nettools

import (
	"context"
	"net"
	"sync"
	"time"
)

// fakeReply scripts the answer to one probe.
type fakeReply struct {
	from  string    // who answers; "" = nobody (ErrNoReply at once)
	kind  ReplyKind // what they answer
	code  int
	rtt   time.Duration
	err   error         // returned as is when set
	block bool          // wait for the probe's context to end
	delay time.Duration // answer after this long (or when the context ends)
}

// fakeProber answers probes from a script, at once unless told otherwise.
// script gets the probe's number across the run (1-based, in call order), its
// TTL and how many probes have used that TTL so far including this one,
// which for a traceroute is the round. Like the real probers, it refuses a
// probe whose context has already ended.
type fakeProber struct {
	canTrace bool
	script   func(call, ttl, round int) fakeReply

	mu     sync.Mutex
	calls  int
	perTTL map[int]int
	starts []time.Time // when each probe was sent, in call order
	closed bool
}

func (f *fakeProber) Echo(ctx context.Context, dst net.IP, ttl, size int, timeout time.Duration) (EchoReply, error) {
	if err := ctx.Err(); err != nil {
		return EchoReply{}, err
	}
	f.mu.Lock()
	f.calls++
	if f.perTTL == nil {
		f.perTTL = map[int]int{}
	}
	f.perTTL[ttl]++
	f.starts = append(f.starts, time.Now())
	call, round := f.calls, f.perTTL[ttl]
	f.mu.Unlock()

	r := f.script(call, ttl, round)
	if r.delay > 0 {
		select {
		case <-time.After(r.delay):
		case <-ctx.Done():
			return EchoReply{}, ctx.Err()
		}
	}
	switch {
	case r.block:
		<-ctx.Done()
		return EchoReply{}, ctx.Err()
	case r.err != nil:
		return EchoReply{}, r.err
	case r.from == "":
		return EchoReply{}, ErrNoReply
	}
	return EchoReply{Kind: r.kind, Code: r.code, From: net.ParseIP(r.from).To4(), RTT: r.rtt, TTL: 57}, nil
}

func (f *fakeProber) CanTrace() bool { return f.canTrace }

func (f *fakeProber) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	return nil
}

// probesAt is how many probes used ttl.
func (f *fakeProber) probesAt(ttl int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.perTTL[ttl]
}

func (f *fakeProber) totalCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeProber) sendTimes() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.starts...)
}

// route scripts a path: routers[i] answers TTL i+1 with time exceeded ("" =
// a router that never answers) and target answers every higher TTL with an
// echo reply. Every answer at TTL n takes n ms.
func route(target string, routers ...string) func(call, ttl, round int) fakeReply {
	return func(_, ttl, _ int) fakeReply {
		rtt := time.Duration(ttl) * time.Millisecond
		if ttl <= len(routers) {
			return fakeReply{from: routers[ttl-1], kind: ReplyTimeExceeded, rtt: rtt}
		}
		return fakeReply{from: target, kind: ReplyEcho, rtt: rtt}
	}
}

// recorder collects emitted events; it is only called from the tool's
// goroutine, as the Emitter contract says.
type recorder struct {
	events []Event
	onEmit func(Event)
}

func (r *recorder) emit(e Event) {
	r.events = append(r.events, e)
	if r.onEmit != nil {
		r.onEmit(e)
	}
}

func (r *recorder) ofType(t string) []any {
	var out []any
	for _, e := range r.events {
		if e.Type == t {
			out = append(out, e.Data)
		}
	}
	return out
}
