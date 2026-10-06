package nettools

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"
)

// eqp reports whether p holds want (to 1e-9).
func eqp(p *float64, want float64) bool { return p != nil && math.Abs(*p-want) < 1e-9 }

// pingSpec is a ping of count probes to 192.0.2.10. ping runs the spec it is
// given, so the tests use intervals below the 200 ms minimum to finish at once.
func pingSpec(count, intervalMS int) Spec {
	return Spec{Tool: ToolPing, Target: "192.0.2.10", TargetIP: "192.0.2.10",
		Params: Params{Count: count, IntervalMS: intervalMS, TimeoutMS: 100, Size: intp(56)}}
}

// seqs lists the seq of every ping event, ascending.
func seqs(r *recorder) []int {
	var out []int
	for _, e := range r.events {
		switch d := e.Data.(type) {
		case PingReply:
			out = append(out, d.Seq)
		case PingTimeout:
			out = append(out, d.Seq)
		case PingError:
			out = append(out, d.Seq)
		}
	}
	sort.Ints(out)
	return out
}

func TestPingSummary(t *testing.T) {
	s := pingSummary(4, []float64{10, 20, 15, 30})
	// Jitter: (|20-10| + |15-20| + |30-15|) / 3 = 30 / 3 = 10.
	if s.Sent != 4 || s.Received != 4 || s.LossPct != 0 || !eqp(s.MinMS, 10) || !eqp(s.AvgMS, 18.75) ||
		!eqp(s.MaxMS, 30) || !eqp(s.JitterMS, 10) {
		t.Errorf("all replies: %+v", s)
	}
	s = pingSummary(5, []float64{10.5, 12.25, 11})
	// Loss 2/5; avg 33.75/3 = 11.25; jitter (1.75 + 1.25) / 2 = 1.5.
	if s.Received != 3 || s.LossPct != 40 || !eqp(s.MinMS, 10.5) || !eqp(s.AvgMS, 11.25) || !eqp(s.MaxMS, 12.25) || !eqp(s.JitterMS, 1.5) {
		t.Errorf("some lost: %+v", s)
	}
	s = pingSummary(3, []float64{1, 2, 2})
	// Avg 5/3 rounds to 1.667; jitter (1 + 0) / 2 = 0.5; loss 0.
	if !eqp(s.AvgMS, 1.667) || !eqp(s.JitterMS, 0.5) || s.LossPct != 0 {
		t.Errorf("rounding: %+v", s)
	}
	s = pingSummary(3, []float64{7})
	// Loss 2/3 = 66.67 % rounds to 66.7; one reply has no jitter.
	if s.LossPct != 66.7 || !eqp(s.MinMS, 7) || !eqp(s.AvgMS, 7) || !eqp(s.MaxMS, 7) || s.JitterMS != nil {
		t.Errorf("one reply: %+v", s)
	}
	if s := pingSummary(2, nil); !reflect.DeepEqual(s, PingSummary{Sent: 2, LossPct: 100}) {
		t.Errorf("no replies: %+v", s)
	}
	if s := pingSummary(0, nil); !reflect.DeepEqual(s, PingSummary{}) {
		t.Errorf("nothing sent: %+v", s)
	}
}

func TestPingAllReplies(t *testing.T) {
	// The n-th probe sent takes n ms: RTTs 1, 2, 3, 4 in some order.
	f := &fakeProber{script: func(call, _, _ int) fakeReply {
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: time.Duration(call) * time.Millisecond}
	}}
	var r recorder
	sum, err := ping(context.Background(), f, pingSpec(4, 1), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Sent != 4 || sum.Received != 4 || sum.LossPct != 0 || !eqp(sum.MinMS, 1) || !eqp(sum.AvgMS, 2.5) ||
		!eqp(sum.MaxMS, 4) || sum.JitterMS == nil {
		t.Errorf("summary %+v", sum)
	}
	if got := seqs(&r); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Errorf("seqs %v, want 1..4 once each", got)
	}
	for _, d := range r.ofType(EventReply) {
		if rep := d.(PingReply); rep.From != "192.0.2.10" || rep.TTL != 57 || rep.RTTMS < 1 || rep.RTTMS > 4 {
			t.Errorf("reply %+v", rep)
		}
	}
}

func TestPingSomeLost(t *testing.T) {
	f := &fakeProber{script: func(call, _, _ int) fakeReply {
		if call == 2 || call == 4 {
			return fakeReply{} // nobody answers
		}
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 10 * time.Millisecond}
	}}
	var r recorder
	sum, err := ping(context.Background(), f, pingSpec(5, 1), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Sent != 5 || sum.Received != 3 || sum.LossPct != 40 || !eqp(sum.MinMS, 10) || !eqp(sum.AvgMS, 10) ||
		!eqp(sum.MaxMS, 10) || !eqp(sum.JitterMS, 0) {
		t.Errorf("summary %+v", sum)
	}
	if n, m := len(r.ofType(EventReply)), len(r.ofType(EventTimeout)); n != 3 || m != 2 {
		t.Errorf("%d replies and %d timeouts, want 3 and 2", n, m)
	}
	if got := seqs(&r); !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5}) {
		t.Errorf("seqs %v", got)
	}
}

func TestPingNoReplies(t *testing.T) {
	f := &fakeProber{script: func(int, int, int) fakeReply { return fakeReply{} }}
	var r recorder
	sum, err := ping(context.Background(), f, pingSpec(3, 1), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sum, PingSummary{Sent: 3, LossPct: 100}) {
		t.Errorf("summary %+v", sum)
	}
	if len(r.ofType(EventTimeout)) != 3 {
		t.Errorf("events %+v", r.events)
	}
}

func TestPingErrors(t *testing.T) {
	f := &fakeProber{script: func(call, _, _ int) fakeReply {
		switch call {
		case 1:
			return fakeReply{from: "10.0.0.1", kind: ReplyUnreachable, code: 1}
		case 2:
			return fakeReply{from: "10.0.0.9", kind: ReplyTimeExceeded}
		}
		return fakeReply{err: errors.New("sendto: network is unreachable")}
	}}
	var r recorder
	sum, err := ping(context.Background(), f, pingSpec(3, 1), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, d := range r.ofType(EventError) {
		msgs = append(msgs, d.(PingError).Message)
	}
	sort.Strings(msgs)
	want := []string{"TTL exceeded in transit from 10.0.0.9", "host unreachable from 10.0.0.1", "sendto: network is unreachable"}
	if !reflect.DeepEqual(msgs, want) {
		t.Errorf("messages %q, want %q", msgs, want)
	}
	if sum.Sent != 3 || sum.Received != 0 || sum.LossPct != 100 {
		t.Errorf("summary %+v", sum)
	}
}

// Probes go out on the interval, not after the previous reply.
func TestPingKeepsTheInterval(t *testing.T) {
	f := &fakeProber{script: func(int, int, int) fakeReply {
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: time.Millisecond, delay: 120 * time.Millisecond}
	}}
	var r recorder
	if _, err := ping(context.Background(), f, pingSpec(3, 30), r.emit); err != nil {
		t.Fatal(err)
	}
	sent := f.sendTimes()
	if len(sent) != 3 {
		t.Fatalf("%d probes", len(sent))
	}
	// On the interval: sent at 0, 30 and 60 ms. After each reply: 0, 120, 240.
	if gap := sent[2].Sub(sent[0]); gap < 55*time.Millisecond || gap > 200*time.Millisecond {
		t.Errorf("third probe sent %v after the first, want about 60 ms", gap)
	}
}

func TestPingCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &fakeProber{script: func(call, _, _ int) fakeReply {
		if call <= 2 {
			return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 5 * time.Millisecond}
		}
		return fakeReply{block: true}
	}}
	r := recorder{}
	r.onEmit = func(Event) {
		if len(r.ofType(EventReply)) == 2 {
			cancel()
		}
	}
	sum, err := ping(ctx, f, pingSpec(5, 1), r.emit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if sum.Sent != 2 || sum.Received != 2 || sum.LossPct != 0 || !eqp(sum.AvgMS, 5) {
		t.Errorf("partial summary %+v, want the two answered probes", sum)
	}
	if len(r.ofType(EventTimeout))+len(r.ofType(EventError)) != 0 {
		t.Errorf("cancelled probes were reported: %+v", r.events)
	}
}
