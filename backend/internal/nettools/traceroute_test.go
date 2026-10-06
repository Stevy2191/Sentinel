package nettools

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func traceSpec(maxHops, rounds int) Spec {
	return Spec{Tool: ToolTraceroute, Target: "192.0.2.10", TargetIP: "192.0.2.10",
		Params: Params{MaxHops: maxHops, Rounds: rounds, TimeoutMS: 100}}
}

func sample(addr string, rtt float64) hopSample { return hopSample{addr: addr, rtt: ptr(rtt)} }

func TestHopStats(t *testing.T) {
	// RTTs 10, 20, 30: avg 20; stdev sqrt((100 + 0 + 100) / 3) = 8.16497 → 8.165.
	h := hopStats(4, []hopSample{sample("10.0.0.1", 10), {}, sample("10.0.0.2", 20), sample("10.0.0.1", 30)})
	if h.TTL != 4 || !reflect.DeepEqual(h.Addrs, []string{"10.0.0.1", "10.0.0.2"}) || h.Sent != 4 || h.Received != 3 ||
		h.LossPct != 25 || !eqp(h.LastMS, 30) || !eqp(h.AvgMS, 20) || !eqp(h.BestMS, 10) || !eqp(h.WorstMS, 30) ||
		!eqp(h.StdevMS, 8.165) {
		t.Errorf("mixed: %+v", h)
	}
	// Addresses sort numerically; Last is the latest answer, not a later timeout.
	// RTTs 10, 20: avg 15, stdev 5; loss 1/3 = 33.3 %.
	h = hopStats(2, []hopSample{sample("10.0.0.10", 10), sample("10.0.0.9", 20), {}})
	if !reflect.DeepEqual(h.Addrs, []string{"10.0.0.9", "10.0.0.10"}) || !eqp(h.LastMS, 20) || h.LossPct != 33.3 ||
		!eqp(h.AvgMS, 15) || !eqp(h.StdevMS, 5) {
		t.Errorf("sorting and last: %+v", h)
	}
	h = hopStats(3, []hopSample{{}, {}})
	if h.Addrs == nil || len(h.Addrs) != 0 || h.Sent != 2 || h.Received != 0 || h.LossPct != 100 ||
		h.LastMS != nil || h.AvgMS != nil || h.BestMS != nil || h.WorstMS != nil || h.StdevMS != nil {
		t.Errorf("all lost: %+v", h)
	}
	h = hopStats(1, []hopSample{sample("10.0.0.1", 4.5)})
	if !eqp(h.AvgMS, 4.5) || !eqp(h.StdevMS, 0) || h.LossPct != 0 {
		t.Errorf("one answer: %+v", h)
	}
}

func roundDones(r *recorder) []RoundDone {
	var out []RoundDone
	for _, d := range r.ofType(EventRoundDone) {
		out = append(out, d.(RoundDone))
	}
	return out
}

func hopProbes(r *recorder) []HopProbe {
	var out []HopProbe
	for _, d := range r.ofType(EventHop) {
		out = append(out, d.(HopProbe))
	}
	return out
}

func TestTracerouteReachesDestination(t *testing.T) {
	f := &fakeProber{canTrace: true, script: route("192.0.2.10", "10.0.0.1", "10.0.1.1")}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(30, 3), r.emit, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Reached || sum.HopCount != 3 || len(sum.Hops) != 3 {
		t.Fatalf("summary %+v", sum)
	}
	want := []struct {
		addr string
		avg  float64
	}{{"10.0.0.1", 1}, {"10.0.1.1", 2}, {"192.0.2.10", 3}}
	for i, w := range want {
		h := sum.Hops[i]
		if h.TTL != i+1 || !reflect.DeepEqual(h.Addrs, []string{w.addr}) || h.Sent != 3 || h.Received != 3 || !eqp(h.AvgMS, w.avg) {
			t.Errorf("hop %d: %+v", i+1, h)
		}
	}
	// Round 1 probes all 30 TTLs; once the destination is known at TTL 3,
	// rounds 2 and 3 probe only TTLs 1-3.
	if f.probesAt(3) != 3 || f.probesAt(4) != 1 || f.probesAt(30) != 1 || f.totalCalls() != 36 {
		t.Errorf("probes: ttl3 %d, ttl4 %d, ttl30 %d, total %d", f.probesAt(3), f.probesAt(4), f.probesAt(30), f.totalCalls())
	}
	rounds := roundDones(&r)
	if len(rounds) != 3 {
		t.Fatalf("%d round_done events", len(rounds))
	}
	for i, rd := range rounds {
		if rd.Round != i+1 || len(rd.Hops) != 3 || rd.Hops[0].Sent != i+1 {
			t.Errorf("round_done %d: %+v (the table must be cumulative and stop at the destination)", i+1, rd)
		}
	}
	later := 0
	for _, p := range hopProbes(&r) {
		if p.Round > 1 {
			later++
			if p.TTL > 3 {
				t.Errorf("round %d probed TTL %d past the destination", p.Round, p.TTL)
			}
		}
		if (p.TTL >= 3) != p.Reached {
			t.Errorf("probe %+v: reached is true from the destination's TTL on", p)
		}
	}
	if later != 6 {
		t.Errorf("%d hop events in rounds 2-3, want 6", later)
	}
	// Each round's hop events come before its round_done.
	round := 1
	for _, e := range r.events {
		switch d := e.Data.(type) {
		case HopProbe:
			if d.Round != round {
				t.Fatalf("hop of round %d during round %d", d.Round, round)
			}
		case RoundDone:
			round++
		}
	}
}

func TestTracerouteNotReached(t *testing.T) {
	// Routers answer at TTLs 1, 2 and 4; nothing answers past 4 and the
	// target is never reached within 10 hops.
	f := &fakeProber{canTrace: true, script: route("192.0.2.10",
		"10.0.0.1", "10.0.0.2", "", "10.0.0.4", "", "", "", "", "", "")}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(10, 2), r.emit, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Rows run to the last TTL that answered (4) + 1.
	if sum.Reached || sum.HopCount != 5 || len(sum.Hops) != 5 {
		t.Fatalf("summary %+v", sum)
	}
	if h := sum.Hops[2]; h.LossPct != 100 || len(h.Addrs) != 0 || h.Sent != 2 {
		t.Errorf("silent hop 3: %+v", h)
	}
	if h := sum.Hops[4]; h.TTL != 5 || h.LossPct != 100 {
		t.Errorf("row 5: %+v", h)
	}
	// Without a destination every round probes every TTL.
	if f.probesAt(10) != 2 || f.totalCalls() != 20 {
		t.Errorf("ttl10 %d, total %d", f.probesAt(10), f.totalCalls())
	}
}

func TestTracerouteSplitPathAndNames(t *testing.T) {
	f := &fakeProber{canTrace: true, script: func(call, ttl, round int) fakeReply {
		switch {
		case ttl == 1:
			return fakeReply{from: "10.0.0.1", kind: ReplyTimeExceeded, rtt: time.Millisecond}
		case ttl == 2 && round == 1:
			return fakeReply{from: "10.0.2.2", kind: ReplyTimeExceeded, rtt: 2 * time.Millisecond}
		case ttl == 2:
			return fakeReply{from: "10.0.2.1", kind: ReplyTimeExceeded, rtt: 4 * time.Millisecond}
		}
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 5 * time.Millisecond}
	}}
	var mu sync.Mutex
	asked := map[string]int{}
	names := map[string]string{"10.0.0.1": "gw.lab", "10.0.2.1": "core-a.lab", "10.0.2.2": "core-b.lab"}
	lookup := func(_ context.Context, addr string) (string, error) {
		mu.Lock()
		asked[addr]++
		mu.Unlock()
		if n, ok := names[addr]; ok {
			return n, nil
		}
		return "", errors.New("no PTR record")
	}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(30, 2), r.emit, lookup, 0)
	if err != nil {
		t.Fatal(err)
	}
	if h := sum.Hops[1]; !reflect.DeepEqual(h.Addrs, []string{"10.0.2.1", "10.0.2.2"}) || h.Sent != 2 || h.Received != 2 || !eqp(h.AvgMS, 3) {
		t.Errorf("split hop: %+v", h)
	}
	mu.Lock()
	wantAsked := map[string]int{"10.0.0.1": 1, "10.0.2.1": 1, "10.0.2.2": 1, "192.0.2.10": 1}
	if !reflect.DeepEqual(asked, wantAsked) {
		t.Errorf("lookups %v, want one per address %v", asked, wantAsked)
	}
	mu.Unlock()
	got := map[string]HopName{}
	for _, d := range r.ofType(EventHopName) {
		n := d.(HopName)
		if _, dup := got[n.Addr]; dup {
			t.Errorf("hop_name for %s twice", n.Addr)
		}
		got[n.Addr] = n
	}
	wantNames := map[string]HopName{
		"10.0.0.1": {TTL: 1, Addr: "10.0.0.1", Name: "gw.lab"},
		"10.0.2.1": {TTL: 2, Addr: "10.0.2.1", Name: "core-a.lab"},
		"10.0.2.2": {TTL: 2, Addr: "10.0.2.2", Name: "core-b.lab"},
	}
	if !reflect.DeepEqual(got, wantNames) {
		t.Errorf("hop_name events %v, want %v", got, wantNames)
	}
	if sum.Hops[0].Name != "gw.lab" || sum.Hops[1].Name != "core-a.lab" || sum.Hops[2].Name != "" {
		t.Errorf("names in the table: %q %q %q", sum.Hops[0].Name, sum.Hops[1].Name, sum.Hops[2].Name)
	}
}

// Names are looked up for the table's rows, with the row's TTL: round 1's
// parallel probes past the destination answer first here, yet the
// destination's name carries its own TTL, and an address seen only past the
// end is never looked up.
func TestTracerouteNamesFollowTheTable(t *testing.T) {
	f := &fakeProber{canTrace: true, script: func(_, ttl, _ int) fakeReply {
		switch {
		case ttl == 1:
			return fakeReply{from: "10.0.0.1", kind: ReplyTimeExceeded, rtt: time.Millisecond}
		case ttl == 2:
			return fakeReply{from: "10.0.0.2", kind: ReplyTimeExceeded, rtt: 2 * time.Millisecond}
		case ttl == 3: // the destination's own TTL answers after the probes past it
			return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 3 * time.Millisecond, delay: 30 * time.Millisecond}
		case ttl <= 6:
			return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 3 * time.Millisecond}
		}
		// A stray answer past the destination.
		return fakeReply{from: "10.9.9.9", kind: ReplyTimeExceeded, rtt: 9 * time.Millisecond}
	}}
	var mu sync.Mutex
	asked := map[string]int{}
	names := map[string]string{"10.0.0.1": "gw.lab", "10.0.0.2": "r2.lab", "192.0.2.10": "target.lab", "10.9.9.9": "stray.lab"}
	lookup := func(_ context.Context, addr string) (string, error) {
		mu.Lock()
		asked[addr]++
		mu.Unlock()
		return names[addr], nil
	}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(8, 2), r.emit, lookup, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Reached || sum.HopCount != 3 {
		t.Fatalf("summary %+v", sum)
	}
	mu.Lock()
	wantAsked := map[string]int{"10.0.0.1": 1, "10.0.0.2": 1, "192.0.2.10": 1}
	if !reflect.DeepEqual(asked, wantAsked) {
		t.Errorf("lookups %v, want one per address in the table %v", asked, wantAsked)
	}
	mu.Unlock()
	got := map[string]HopName{}
	for _, d := range r.ofType(EventHopName) {
		n := d.(HopName)
		if n.TTL < 1 || n.TTL > sum.HopCount {
			t.Errorf("hop_name %+v: TTL outside the table's %d rows", n, sum.HopCount)
		}
		got[n.Addr] = n
	}
	if want := (HopName{TTL: 3, Addr: "192.0.2.10", Name: "target.lab"}); got["192.0.2.10"] != want {
		t.Errorf("destination's hop_name %+v, want %+v", got["192.0.2.10"], want)
	}
	if len(got) != 3 {
		t.Errorf("hop_name events %v, want the three table addresses", got)
	}
	if sum.Hops[2].Name != "target.lab" {
		t.Errorf("destination row name %q", sum.Hops[2].Name)
	}
}

// A probe that fails to send is reported with its error and counts as lost;
// the run goes on.
func TestTracerouteProbeError(t *testing.T) {
	sendErr := errors.New("sendto: no buffer space available")
	path := route("192.0.2.10", "10.0.0.1", "10.0.0.2")
	f := &fakeProber{canTrace: true, script: func(call, ttl, round int) fakeReply {
		if ttl == 2 {
			return fakeReply{err: sendErr}
		}
		return path(call, ttl, round)
	}}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(30, 2), r.emit, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Reached || sum.HopCount != 3 {
		t.Fatalf("summary %+v", sum)
	}
	if h := sum.Hops[1]; h.Sent != 2 || h.Received != 0 || h.LossPct != 100 || len(h.Addrs) != 0 {
		t.Errorf("hop 2: %+v, want 2 sent and none answered", h)
	}
	ttl2 := 0
	for _, p := range hopProbes(&r) {
		switch {
		case p.TTL == 2:
			ttl2++
			if p.Error != sendErr.Error() || p.Addr != "" || p.RTTMS != nil {
				t.Errorf("TTL 2 probe %+v, want the send error and no answer", p)
			}
		case p.Error != "":
			t.Errorf("probe %+v carries an error", p)
		}
	}
	if ttl2 != 2 {
		t.Errorf("%d hop events for TTL 2, want 2", ttl2)
	}
}

// When every probe of round 1 fails to send, the trace fails with the error
// instead of reading as 100 % loss.
func TestTracerouteAllProbesFail(t *testing.T) {
	sendErr := errors.New("sendto: operation not permitted")
	f := &fakeProber{canTrace: true, script: func(int, int, int) fakeReply { return fakeReply{err: sendErr} }}
	var r recorder
	_, err := traceroute(context.Background(), f, traceSpec(5, 3), r.emit, nil, 0)
	if !errors.Is(err, sendErr) {
		t.Fatalf("err = %v, want %v", err, sendErr)
	}
	probes := hopProbes(&r)
	if len(probes) != 5 || f.totalCalls() != 5 {
		t.Errorf("%d hop events, %d probes; want round 1's 5 and no more rounds", len(probes), f.totalCalls())
	}
	for _, p := range probes {
		if p.Error != sendErr.Error() {
			t.Errorf("probe %+v, want the send error", p)
		}
	}
	if n := len(roundDones(&r)); n != 0 {
		t.Errorf("%d round_done events for a failed trace", n)
	}

	// Errors mixed with plain timeouts are not a failure: the path may just
	// be silent.
	f = &fakeProber{canTrace: true, script: func(_, ttl, _ int) fakeReply {
		if ttl == 1 {
			return fakeReply{err: sendErr}
		}
		return fakeReply{}
	}}
	r = recorder{}
	sum, err := traceroute(context.Background(), f, traceSpec(5, 2), r.emit, nil, 0)
	if err != nil || sum.Reached || len(roundDones(&r)) != 2 {
		t.Errorf("errors and timeouts: %+v, %v, %d rounds", sum, err, len(roundDones(&r)))
	}
}

func TestTracerouteNeedsRawICMP(t *testing.T) {
	f := &fakeProber{canTrace: false, script: route("192.0.2.10")}
	var r recorder
	_, err := traceroute(context.Background(), f, traceSpec(30, 1), r.emit, nil, 0)
	if !errors.Is(err, ErrICMPUnavailable) || len(r.events) != 0 || f.totalCalls() != 0 {
		t.Errorf("err %v, %d events, %d probes; want ErrICMPUnavailable and nothing sent", err, len(r.events), f.totalCalls())
	}
}

func TestTracerouteUnreachableEndsThePath(t *testing.T) {
	// A router at TTL 2 answers "host unreachable" for every TTL from 2 on.
	f := &fakeProber{canTrace: true, script: func(_, ttl, _ int) fakeReply {
		if ttl == 1 {
			return fakeReply{from: "10.0.0.1", kind: ReplyTimeExceeded, rtt: time.Millisecond}
		}
		return fakeReply{from: "10.0.0.2", kind: ReplyUnreachable, code: 1, rtt: 2 * time.Millisecond}
	}}
	var r recorder
	sum, err := traceroute(context.Background(), f, traceSpec(30, 2), r.emit, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Reached || sum.HopCount != 2 || !reflect.DeepEqual(sum.Hops[1].Addrs, []string{"10.0.0.2"}) || f.probesAt(3) != 1 {
		t.Errorf("router unreachable: %+v, ttl3 probed %d times", sum, f.probesAt(3))
	}

	// The destination itself answering "port unreachable" counts as reached.
	f = &fakeProber{canTrace: true, script: func(_, ttl, _ int) fakeReply {
		if ttl < 3 {
			return fakeReply{from: "10.0.0.1", kind: ReplyTimeExceeded, rtt: time.Millisecond}
		}
		return fakeReply{from: "192.0.2.10", kind: ReplyUnreachable, code: 3, rtt: time.Millisecond}
	}}
	sum, err = traceroute(context.Background(), f, traceSpec(30, 1), r.emit, nil, 0)
	if err != nil || !sum.Reached || sum.HopCount != 3 {
		t.Errorf("destination unreachable: %+v, %v", sum, err)
	}
}

func TestTracerouteRoundsAreSpaced(t *testing.T) {
	f := &fakeProber{canTrace: true, script: route("192.0.2.10", "10.0.0.1")}
	var r recorder
	start := time.Now()
	if _, err := traceroute(context.Background(), f, traceSpec(5, 3), r.emit, nil, 60*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d < 120*time.Millisecond {
		t.Errorf("3 rounds took %v, want at least 2 gaps of 60 ms", d)
	}
}

func TestTracerouteCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeProber{canTrace: true, script: route("192.0.2.10", "10.0.0.1", "10.0.0.2")}
	r := recorder{}
	r.onEmit = func(e Event) {
		if e.Type == EventRoundDone {
			cancel()
		}
	}
	sum, err := traceroute(ctx, f, traceSpec(30, 3), r.emit, nil, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if len(roundDones(&r)) != 1 || !sum.Reached || sum.HopCount != 3 || sum.Hops[0].Sent != 1 {
		t.Errorf("partial summary %+v after %d rounds, want round 1's table", sum, len(roundDones(&r)))
	}
}
