package nettools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"time"
)

// pingTTL is the IP TTL of ping probes.
const pingTTL = 64

// ping sends s.Params.Count echo requests to s.TargetIP, probe i at
// start + (i-1) × interval whatever happened to the earlier ones, each
// waiting up to the probe timeout. Probes run concurrently; each result is
// emitted as soon as it is known, so events arrive in completion order and
// carry their seq. s must be normalized and TargetIP an IPv4 address.
func ping(ctx context.Context, p Prober, s Spec, emit Emitter) (PingSummary, error) {
	dst := net.ParseIP(s.TargetIP).To4()
	count := s.Params.Count
	interval := time.Duration(s.Params.IntervalMS) * time.Millisecond
	timeout := time.Duration(s.Params.TimeoutMS) * time.Millisecond
	size := PingSizeDefault
	if s.Params.Size != nil {
		size = *s.Params.Size
	}

	type result struct {
		seq   int
		reply EchoReply
		err   error
	}
	results := make(chan result, count) // never blocks a probe, even after we return
	rtts := make([]*float64, count+1)   // by seq; nil = no echo reply
	completed, pending, next := 0, 0, 1
	start := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()

	for (next <= count || pending > 0) && ctx.Err() == nil {
		var tick <-chan time.Time
		if next <= count {
			tick = timer.C
		}
		select {
		case <-ctx.Done():
		case <-tick:
			seq := next
			next++
			pending++
			go func() {
				r, err := p.Echo(ctx, dst, pingTTL, size, timeout)
				results <- result{seq, r, err}
			}()
			if next <= count {
				timer.Reset(time.Until(start.Add(time.Duration(next-1) * interval)))
			}
		case res := <-results:
			pending--
			if ctx.Err() != nil && res.err != nil {
				continue // cut short by the cancellation: not a real outcome
			}
			completed++
			switch {
			case res.err == nil && res.reply.Kind == ReplyEcho:
				ms := durationMS(res.reply.RTT)
				rtts[res.seq] = &ms
				emit(Event{Type: EventReply, Data: PingReply{Seq: res.seq, RTTMS: ms, TTL: res.reply.TTL, From: res.reply.From.String()}})
			case res.err == nil:
				emit(Event{Type: EventError, Data: PingError{Seq: res.seq, Message: replyProblem(res.reply)}})
			case errors.Is(res.err, ErrNoReply):
				emit(Event{Type: EventTimeout, Data: PingTimeout{Seq: res.seq}})
			default:
				emit(Event{Type: EventError, Data: PingError{Seq: res.seq, Message: res.err.Error()}})
			}
		}
	}
	var inOrder []float64
	for _, r := range rtts {
		if r != nil {
			inOrder = append(inOrder, *r)
		}
	}
	return pingSummary(completed, inOrder), ctx.Err()
}

// replyProblem words an ICMP error that answered a ping probe.
func replyProblem(r EchoReply) string {
	if r.Kind == ReplyTimeExceeded {
		return fmt.Sprintf("TTL exceeded in transit from %s", r.From)
	}
	what := "destination unreachable"
	switch r.Code {
	case 0:
		what = "network unreachable"
	case 1:
		what = "host unreachable"
	case 2:
		what = "protocol unreachable"
	case 3:
		what = "port unreachable"
	case 9, 10, 13:
		what = "communication administratively prohibited"
	}
	return fmt.Sprintf("%s from %s", what, r.From)
}

// pingSummary sums up sent completed probes whose echo replies took rtts
// (milliseconds, in seq order).
func pingSummary(sent int, rtts []float64) PingSummary {
	s := PingSummary{Sent: sent, Received: len(rtts)}
	if sent > 0 {
		s.LossPct = round1(float64(sent-len(rtts)) / float64(sent) * 100)
	}
	if len(rtts) == 0 {
		return s
	}
	lo, hi, sum := rtts[0], rtts[0], 0.0
	for _, v := range rtts {
		lo, hi, sum = math.Min(lo, v), math.Max(hi, v), sum+v
	}
	s.MinMS, s.MaxMS, s.AvgMS = ptr(round3(lo)), ptr(round3(hi)), ptr(round3(sum/float64(len(rtts))))
	if len(rtts) >= 2 {
		var diffs float64
		for i := 1; i < len(rtts); i++ {
			diffs += math.Abs(rtts[i] - rtts[i-1])
		}
		s.JitterMS = ptr(round3(diffs / float64(len(rtts)-1)))
	}
	return s
}

// Helpers shared by the tools.

// durationMS is d in milliseconds, to the microsecond.
func durationMS(d time.Duration) float64 { return round3(float64(d) / float64(time.Millisecond)) }

func round1(x float64) float64 { return math.Round(x*10) / 10 }
func round3(x float64) float64 { return math.Round(x*1000) / 1000 }
func ptr(x float64) *float64   { return &x }
