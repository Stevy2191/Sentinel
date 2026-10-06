//go:build !windows

package nettools

import (
	"context"
	"reflect"
	"testing"
)

// Real probes to 127.0.0.1, skipped where this user may not send ICMP.

func TestPingLoopback(t *testing.T) {
	p := openProber(t)
	s := Spec{Tool: ToolPing, Target: "127.0.0.1", TargetIP: "127.0.0.1",
		Params: Params{Count: 2, IntervalMS: 200, TimeoutMS: 1000, Size: intp(56)}}
	var r recorder
	sum, err := ping(context.Background(), p, s, r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if errs := r.ofType(EventError); len(errs) > 0 {
		t.Skipf("ICMP refused here: %+v", errs[0])
	}
	if sum.Sent != 2 || sum.Received != 2 || sum.LossPct != 0 || sum.MinMS == nil {
		t.Errorf("summary %+v, events %+v", sum, r.events)
	}
}

func TestTracerouteLoopback(t *testing.T) {
	p := openProber(t)
	if !p.CanTrace() {
		t.Skip("traceroute needs raw ICMP (root or NET_RAW)")
	}
	s := Spec{Tool: ToolTraceroute, Target: "127.0.0.1", TargetIP: "127.0.0.1",
		Params: Params{MaxHops: 3, Rounds: 1, TimeoutMS: 1000}}
	var r recorder
	sum, err := traceroute(context.Background(), p, s, r.emit, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !sum.Reached || sum.HopCount != 1 || !reflect.DeepEqual(sum.Hops[0].Addrs, []string{"127.0.0.1"}) {
		t.Errorf("summary %+v", sum)
	}
}
