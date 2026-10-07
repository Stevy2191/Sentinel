package nettools

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// normalized is Normalize(s) with TargetIP set, failing the test on error.
func normalized(t *testing.T, s Spec, targetIP string) Spec {
	t.Helper()
	n, err := Normalize(s)
	if err != nil {
		t.Fatal(err)
	}
	n.TargetIP = targetIP
	return n
}

func proberOf(p Prober) func() (Prober, error) { return func() (Prober, error) { return p, nil } }

func TestRunnerPing(t *testing.T) {
	f := &fakeProber{script: func(int, int, int) fakeReply {
		return fakeReply{from: "192.0.2.10", kind: ReplyEcho, rtt: 3 * time.Millisecond}
	}}
	var r recorder
	sum, err := (&Runner{NewProber: proberOf(f)}).Run(context.Background(),
		normalized(t, Spec{Tool: ToolPing, Target: "server1", Params: Params{Count: 1}}, "192.0.2.10"), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	ps, ok := sum.(PingSummary)
	if !ok || ps.Sent != 1 || ps.Received != 1 || !eqp(ps.AvgMS, 3) {
		t.Errorf("summary %#v", sum)
	}
	if !f.closed {
		t.Error("the prober was not closed")
	}
}

func TestRunnerTraceroute(t *testing.T) {
	f := &fakeProber{canTrace: true, script: route("192.0.2.10", "10.0.0.1")}
	lookup := func(_ context.Context, addr string) (string, error) {
		if addr == "10.0.0.1" {
			return "gw.lab", nil
		}
		return "", nil
	}
	var r recorder
	sum, err := (&Runner{NewProber: proberOf(f), LookupPTR: lookup}).Run(context.Background(),
		normalized(t, Spec{Tool: ToolTraceroute, Target: "server1", Params: Params{MaxHops: 5, Rounds: 1}}, "192.0.2.10"), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	ts, ok := sum.(TraceSummary)
	if !ok || !ts.Reached || ts.HopCount != 2 || ts.Hops[0].Name != "gw.lab" {
		t.Errorf("summary %#v", sum)
	}
	if !f.closed {
		t.Error("the prober was not closed")
	}
}

func aRecordServer(t *testing.T) *dnsTestServer {
	return newDNSTestServer(t, func(q dnsmessage.Message, _ bool) []dnsmessage.Message {
		return []dnsmessage.Message{dnsReply(q, dnsmessage.RCodeSuccess,
			[]dnsmessage.Resource{rr(q.Questions[0].Name.String(), 60, &dnsmessage.AResource{A: [4]byte{192, 0, 2, 80}})}, nil, nil)}
	})
}

func TestRunnerDNSSystemResolver(t *testing.T) {
	srv := aRecordServer(t)
	run := &Runner{SystemDNS: func() (string, error) { return srv.addr, nil }}
	var r recorder
	sum, err := run.Run(context.Background(), normalized(t, Spec{Tool: ToolDNS, Target: "www.example.org"}, ""), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if sum != (DNSSummary{Server: srv.addr, RCode: "NOERROR", AnswerCount: 1, RTTMS: sum.(DNSSummary).RTTMS}) {
		t.Errorf("summary %#v", sum)
	}
	if len(r.ofType(EventAnswer)) != 1 {
		t.Errorf("events %+v", r.events)
	}
}

func TestRunnerDNSNamedServer(t *testing.T) {
	srv := aRecordServer(t)
	_, port, _ := net.SplitHostPort(srv.addr)
	run := &Runner{SystemDNS: func() (string, error) {
		t.Error("the system resolver was asked")
		return "", errors.New("unused")
	}}
	var r recorder
	sum, err := run.Run(context.Background(),
		normalized(t, Spec{Tool: ToolDNS, Target: "www.example.org", Params: Params{Server: "127.0.0.1:" + port}}, "127.0.0.1"), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if ds := sum.(DNSSummary); ds.Server != srv.addr || ds.AnswerCount != 1 {
		t.Errorf("summary %#v", sum)
	}
	// The named server's port defaults to 53; the address is TargetIP.
	if got, err := (&Runner{}).dnsServer(Spec{TargetIP: "192.0.2.53", Params: Params{Server: "192.0.2.53"}}); got != "192.0.2.53:53" || err != nil {
		t.Errorf("default port: %q, %v", got, err)
	}
	if _, err := (&Runner{}).dnsServer(Spec{Params: Params{Server: "192.0.2.53"}}); err == nil {
		t.Error("a named server without target_ip was accepted")
	}
}

func TestRunnerTCP(t *testing.T) {
	dial := func(_ context.Context, _, addr string) (net.Conn, error) {
		if addr == "192.0.2.10:443" {
			return openConn(), nil
		}
		return nil, refused()
	}
	var r recorder
	sum, err := (&Runner{Dial: dial}).Run(context.Background(),
		normalized(t, Spec{Tool: ToolTCP, Target: "server1", Params: Params{Ports: "443,444"}}, "192.0.2.10"), r.emit)
	if err != nil {
		t.Fatal(err)
	}
	if want := (TCPSummary{Total: 2, Open: 1, Closed: 1, OpenPorts: []int{443}}); !reflect.DeepEqual(sum, want) {
		t.Errorf("summary %#v, want %#v", sum, want)
	}
}

func TestRunnerRefusesNonIPv4Targets(t *testing.T) {
	opened, dialed := false, false
	run := &Runner{
		NewProber: func() (Prober, error) { opened = true; return &fakeProber{}, nil },
		Dial: func(context.Context, string, string) (net.Conn, error) {
			dialed = true
			return nil, refused()
		},
	}
	for _, tool := range []Tool{ToolPing, ToolTraceroute, ToolTCP} {
		for _, ip := range []string{"", "server1", "2001:db8::1", "::ffff:192.0.2.1"} {
			var r recorder
			sum, err := run.Run(context.Background(), Spec{Tool: tool, Target: "server1", TargetIP: ip, Params: Params{Count: 1, Ports: "22"}}, r.emit)
			if err == nil || sum != nil || len(r.events) != 0 {
				t.Errorf("%s to %q: %v, %v, %d events", tool, ip, sum, err, len(r.events))
			}
		}
	}
	if opened || dialed {
		t.Error("a refused run still opened a prober or dialed")
	}
	if _, err := run.Run(context.Background(), Spec{Tool: "nmap", TargetIP: "192.0.2.10"}, func(Event) {}); err == nil {
		t.Error("an unknown tool ran")
	}
}

func TestRunnerICMPUnavailable(t *testing.T) {
	run := &Runner{NewProber: func() (Prober, error) { return nil, ErrICMPUnavailable }}
	var r recorder
	sum, err := run.Run(context.Background(), normalized(t, Spec{Tool: ToolPing, Target: "h"}, "192.0.2.10"), r.emit)
	if !errors.Is(err, ErrICMPUnavailable) || sum != nil || len(r.events) != 0 {
		t.Errorf("%v, %v, %d events", sum, err, len(r.events))
	}
	if err.Error() != "ICMP isn't available here (needs root or NET_RAW)" {
		t.Errorf("message %q", err.Error())
	}
	f := &fakeProber{canTrace: false}
	sum, err = (&Runner{NewProber: proberOf(f)}).Run(context.Background(),
		normalized(t, Spec{Tool: ToolTraceroute, Target: "h"}, "192.0.2.10"), r.emit)
	if !errors.Is(err, ErrICMPUnavailable) || sum != nil || !f.closed {
		t.Errorf("traceroute without raw ICMP: %v, %v, closed %v", sum, err, f.closed)
	}
}

func TestRunnerCancelledKeepsThePartialSummary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var r recorder
	sum, err := (&Runner{Dial: func(context.Context, string, string) (net.Conn, error) { return nil, refused() }}).Run(ctx,
		normalized(t, Spec{Tool: ToolTCP, Target: "h", Params: Params{Ports: "22,80"}}, "192.0.2.10"), r.emit)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := sum.(TCPSummary); !ok {
		t.Errorf("summary %#v, want the TCPSummary so far", sum)
	}
}
