package nettools

import (
	"bytes"
	"context"
	"math"
	"net"
	"sort"
	"sync"
	"time"
)

const (
	// traceRoundGap is the least time between the starts of two rounds.
	traceRoundGap = time.Second
	// hopNameTimeout bounds each reverse lookup of a hop address.
	hopNameTimeout = time.Second
	// traceSize is the payload size of traceroute probes.
	traceSize = 32
)

// lookupFunc finds the name of an address ("" when it has none).
type lookupFunc func(ctx context.Context, addr string) (string, error)

// hopSample is one probe's outcome at one TTL: who answered and how fast.
// addr is "" and rtt nil when nothing answered.
type hopSample struct {
	addr string
	rtt  *float64
}

// traceState is what a traceroute has learned so far.
type traceState struct {
	maxHops      int
	samples      map[int][]hopSample // by TTL, in round order
	stopTTL      int                 // lowest TTL whose probe went no further (0 = none yet)
	reached      bool                // the destination itself answered
	lastAnswered int                 // highest TTL that ever answered
	names        map[string]string   // addr → reverse name
}

// traceroute runs an MTR-style trace to s.TargetIP: each round sends one
// echo request per TTL, all in parallel, and ends when every probe has
// answered or timed out; rounds start at least gap apart. The path ends at
// the first TTL answered by the destination (or by an unreachable message,
// which means the probe went no further); later rounds probe only up to it
// and rows past it are dropped. Without an end, rows run to the last TTL
// that ever answered + 1 (at most max hops). After each round a RoundDone
// carries the cumulative table. Hop names are looked up once per address in
// the background. s must be normalized and TargetIP an IPv4 address.
func traceroute(ctx context.Context, p Prober, s Spec, emit Emitter, lookup lookupFunc, gap time.Duration) (TraceSummary, error) {
	if !p.CanTrace() {
		return TraceSummary{}, ErrICMPUnavailable
	}
	dst := net.ParseIP(s.TargetIP).To4()
	timeout := time.Duration(s.Params.TimeoutMS) * time.Millisecond
	st := &traceState{maxHops: s.Params.MaxHops, samples: map[int][]hopSample{}, names: map[string]string{}}

	// Reverse lookups run in the background and report here; the buffer
	// holds one name per possible probe, so a lookup never blocks.
	names := make(chan HopName, s.Params.MaxHops*s.Params.Rounds)
	var lookups sync.WaitGroup
	asked := map[string]bool{}
	lookUp := func(ttl int, addr string) {
		if lookup == nil || addr == "" || asked[addr] {
			return
		}
		asked[addr] = true
		lookups.Add(1)
		go func() {
			defer lookups.Done()
			lctx, cancel := context.WithTimeout(ctx, hopNameTimeout)
			defer cancel()
			if name, err := lookup(lctx, addr); err == nil && name != "" {
				names <- HopName{TTL: ttl, Addr: addr, Name: name}
			}
		}()
	}
	gotName := func(n HopName) {
		st.names[n.Addr] = n.Name
		emit(Event{Type: EventHopName, Data: n})
	}

	type result struct {
		ttl   int
		reply EchoReply
		err   error
	}
	for round := 1; round <= s.Params.Rounds; round++ {
		roundStart := time.Now()
		limit := st.maxHops
		if st.stopTTL > 0 {
			limit = st.stopTTL
		}
		results := make(chan result, limit)
		for ttl := 1; ttl <= limit; ttl++ {
			go func() {
				r, err := p.Echo(ctx, dst, ttl, traceSize, timeout)
				results <- result{ttl, r, err}
			}()
		}
		for left := limit; left > 0; {
			select {
			case n := <-names:
				gotName(n)
			case res := <-results:
				left--
				if ctx.Err() != nil && res.err != nil {
					continue // cut short by the cancellation
				}
				probe := HopProbe{Round: round, TTL: res.ttl}
				sample := hopSample{}
				if res.err == nil {
					ms := durationMS(res.reply.RTT)
					sample = hopSample{addr: res.reply.From.String(), rtt: &ms}
					probe.Addr, probe.RTTMS = sample.addr, sample.rtt
					probe.Reached = res.reply.Kind == ReplyEcho || res.reply.From.Equal(dst)
					st.answered(res.ttl, probe.Reached || res.reply.Kind == ReplyUnreachable, probe.Reached)
				}
				st.samples[res.ttl] = append(st.samples[res.ttl], sample)
				if st.stopTTL == 0 || res.ttl <= st.stopTTL {
					emit(Event{Type: EventHop, Data: probe})
				}
				lookUp(res.ttl, sample.addr)
			}
		}
		if err := ctx.Err(); err != nil {
			return st.summary(), err
		}
		emit(Event{Type: EventRoundDone, Data: RoundDone{Round: round, Hops: st.rows()}})
		if round < s.Params.Rounds {
			wait := time.NewTimer(time.Until(roundStart.Add(gap)))
			for waiting := true; waiting; {
				select {
				case n := <-names:
					gotName(n)
				case <-wait.C:
					waiting = false
				case <-ctx.Done():
					wait.Stop()
					return st.summary(), ctx.Err()
				}
			}
		}
	}

	// Let the last lookups finish (each is bounded by hopNameTimeout).
	lookupsDone := make(chan struct{})
	go func() {
		lookups.Wait()
		close(lookupsDone)
	}()
	for waiting := true; waiting; {
		select {
		case n := <-names:
			gotName(n)
		case <-lookupsDone:
			waiting = false
		case <-ctx.Done():
			return st.summary(), ctx.Err()
		}
	}
	for len(names) > 0 {
		gotName(<-names)
	}
	return st.summary(), nil
}

// answered records an answer at ttl; ends says the path stops there.
func (st *traceState) answered(ttl int, ends, reached bool) {
	if ttl > st.lastAnswered {
		st.lastAnswered = ttl
	}
	if ends && (st.stopTTL == 0 || ttl < st.stopTTL) {
		st.stopTTL = ttl
	}
	if reached {
		st.reached = true
	}
}

// rows is the MTR table: TTL 1 to the end of the path, or, without an end,
// to the last TTL that ever answered + 1, at most max hops.
func (st *traceState) rows() []HopStats {
	n := st.stopTTL
	if n == 0 {
		n = min(st.maxHops, st.lastAnswered+1)
	}
	n = max(n, 1)
	out := make([]HopStats, 0, n)
	for ttl := 1; ttl <= n; ttl++ {
		h := hopStats(ttl, st.samples[ttl])
		for _, a := range h.Addrs {
			if name := st.names[a]; name != "" {
				h.Name = name
				break
			}
		}
		out = append(out, h)
	}
	return out
}

func (st *traceState) summary() TraceSummary {
	rows := st.rows()
	return TraceSummary{Reached: st.reached, HopCount: len(rows), Hops: rows}
}

// hopStats sums up the probes sent with one TTL (in round order): the
// addresses that answered (sorted), loss, and RTT figures over the answers.
// Last is the most recent answer; Stdev is the population standard deviation.
func hopStats(ttl int, probes []hopSample) HopStats {
	h := HopStats{TTL: ttl, Addrs: []string{}, Sent: len(probes)}
	seen := map[string]bool{}
	var rtts []float64
	for _, p := range probes {
		if p.addr != "" && !seen[p.addr] {
			seen[p.addr] = true
			h.Addrs = append(h.Addrs, p.addr)
		}
		if p.rtt != nil {
			rtts = append(rtts, *p.rtt)
		}
	}
	sort.Slice(h.Addrs, func(i, j int) bool {
		a, b := net.ParseIP(h.Addrs[i]).To4(), net.ParseIP(h.Addrs[j]).To4()
		if a == nil || b == nil {
			return h.Addrs[i] < h.Addrs[j]
		}
		return bytes.Compare(a, b) < 0
	})
	h.Received = len(rtts)
	if h.Sent > 0 {
		h.LossPct = round1(float64(h.Sent-h.Received) / float64(h.Sent) * 100)
	}
	if len(rtts) == 0 {
		return h
	}
	best, worst, sum := rtts[0], rtts[0], 0.0
	for _, v := range rtts {
		best, worst, sum = math.Min(best, v), math.Max(worst, v), sum+v
	}
	avg := sum / float64(len(rtts))
	var sq float64
	for _, v := range rtts {
		sq += (v - avg) * (v - avg)
	}
	h.LastMS = ptr(rtts[len(rtts)-1])
	h.AvgMS, h.BestMS, h.WorstMS = ptr(round3(avg)), ptr(best), ptr(worst)
	h.StdevMS = ptr(round3(math.Sqrt(sq / float64(len(rtts)))))
	return h
}
