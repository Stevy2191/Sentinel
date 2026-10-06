package nettools

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func intp(n int) *int { return &n }

// paramField returns the Field of a *ParamError, or "" for nil / other errors.
func paramField(err error) string {
	var pe *ParamError
	if errors.As(err, &pe) {
		return pe.Field
	}
	return ""
}

func TestNormalizeDefaults(t *testing.T) {
	cases := []struct {
		tool Tool
		want Params
	}{
		{ToolPing, Params{Count: 5, IntervalMS: 1000, TimeoutMS: 2000, Size: intp(56)}},
		{ToolTraceroute, Params{MaxHops: 30, Rounds: 5, TimeoutMS: 1000}},
		{ToolDNS, Params{RecordType: "A"}},
		{ToolTCP, Params{Ports: "common", TimeoutMS: 1500}},
	}
	for _, c := range cases {
		got, err := Normalize(Spec{Tool: c.tool, Target: "  192.0.2.10 "})
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		if got.Target != "192.0.2.10" {
			t.Errorf("%s: target %q, want it trimmed", c.tool, got.Target)
		}
		if !reflect.DeepEqual(got.Params, c.want) {
			t.Errorf("%s: params %+v, want %+v", c.tool, got.Params, c.want)
		}
	}
}

// Every bound: min-1 and max+1 refused with the field named, min and max kept.
func TestNormalizeBounds(t *testing.T) {
	type tc struct {
		name  string
		spec  Spec
		field string // "" = accepted
	}
	ping := func(p Params) Spec { return Spec{Tool: ToolPing, Target: "h", Params: p} }
	trace := func(p Params) Spec { return Spec{Tool: ToolTraceroute, Target: "h", Params: p} }
	tcp := func(p Params) Spec { return Spec{Tool: ToolTCP, Target: "h", Params: p} }
	cases := []tc{
		{"ping count -1", ping(Params{Count: -1}), "count"},
		{"ping count 1", ping(Params{Count: 1}), ""},
		{"ping count 100", ping(Params{Count: 100}), ""},
		{"ping count 101", ping(Params{Count: 101}), "count"},
		{"ping interval 199", ping(Params{IntervalMS: 199}), "interval_ms"},
		{"ping interval 200", ping(Params{IntervalMS: 200}), ""},
		{"ping interval 5000", ping(Params{Count: 1, IntervalMS: 5000}), ""},
		{"ping interval 5001", ping(Params{Count: 1, IntervalMS: 5001}), "interval_ms"},
		{"ping timeout 499", ping(Params{TimeoutMS: 499}), "timeout_ms"},
		{"ping timeout 500", ping(Params{TimeoutMS: 500}), ""},
		{"ping timeout 5000", ping(Params{TimeoutMS: 5000}), ""},
		{"ping timeout 5001", ping(Params{TimeoutMS: 5001}), "timeout_ms"},
		{"ping size -1", ping(Params{Size: intp(-1)}), "size"},
		{"ping size 0", ping(Params{Size: intp(0)}), ""},
		{"ping size 1472", ping(Params{Size: intp(1472)}), ""},
		{"ping size 1473", ping(Params{Size: intp(1473)}), "size"},
		{"trace hops 0 is the default", trace(Params{MaxHops: 0}), ""},
		{"trace hops -1", trace(Params{MaxHops: -1}), "max_hops"},
		{"trace hops 1", trace(Params{MaxHops: 1}), ""},
		{"trace hops 30", trace(Params{MaxHops: 30}), ""},
		{"trace hops 31", trace(Params{MaxHops: 31}), "max_hops"},
		{"trace rounds -1", trace(Params{Rounds: -1}), "rounds"},
		{"trace rounds 1", trace(Params{Rounds: 1}), ""},
		{"trace rounds 10", trace(Params{Rounds: 10}), ""},
		{"trace rounds 11", trace(Params{Rounds: 11}), "rounds"},
		{"trace timeout 499", trace(Params{TimeoutMS: 499}), "timeout_ms"},
		{"trace timeout 500", trace(Params{TimeoutMS: 500}), ""},
		{"trace timeout 3000", trace(Params{TimeoutMS: 3000}), ""},
		{"trace timeout 3001", trace(Params{TimeoutMS: 3001}), "timeout_ms"},
		{"tcp timeout 499", tcp(Params{TimeoutMS: 499}), "timeout_ms"},
		{"tcp timeout 500", tcp(Params{TimeoutMS: 500}), ""},
		{"tcp timeout 5000", tcp(Params{TimeoutMS: 5000}), ""},
		{"tcp timeout 5001", tcp(Params{TimeoutMS: 5001}), "timeout_ms"},
		{"tcp ports 1-1024", tcp(Params{Ports: "1-1024"}), ""},
		{"tcp ports 1-1025", tcp(Params{Ports: "1-1025"}), "ports"},
		{"tcp ports bad", tcp(Params{Ports: "ssh"}), "ports"},
		{"unknown tool", Spec{Tool: "nmap", Target: "h"}, "tool"},
		{"empty target", Spec{Tool: ToolPing, Target: "   "}, "target"},
		{"target with a space", Spec{Tool: ToolPing, Target: "a b"}, "target"},
		{"target of 253", Spec{Tool: ToolPing, Target: string(make253())}, ""},
		{"target of 254", Spec{Tool: ToolPing, Target: string(make253()) + "a"}, "target"},
	}
	for _, c := range cases {
		_, err := Normalize(c.spec)
		if got := paramField(err); got != c.field {
			t.Errorf("%s: refused field %q (err %v), want %q", c.name, got, err, c.field)
		}
		if err != nil && paramField(err) == "" {
			t.Errorf("%s: error %v is not a *ParamError", c.name, err)
		}
	}
}

func make253() []byte {
	b := make([]byte, 253)
	for i := range b {
		b[i] = 'a'
	}
	return b
}

// Review Focus 4: a ping must fit its 2-minute deadline.
func TestNormalizePingTotal(t *testing.T) {
	cases := []struct {
		count, interval, timeout int
		ok                       bool
	}{
		{100, 1000, 2000, true},  // 102,000 ms
		{100, 5000, 2000, false}, // 502,000 ms
		{23, 5000, 2000, false},  // 117,000 ms
		{22, 5000, 2000, true},   // 112,000 ms
		{22, 5000, 5000, true},   // 115,000 ms: exactly the limit
	}
	for _, c := range cases {
		_, err := Normalize(Spec{Tool: ToolPing, Target: "h", Params: Params{Count: c.count, IntervalMS: c.interval, TimeoutMS: c.timeout}})
		if (err == nil) != c.ok {
			t.Errorf("%d × %d + %d: err %v, want ok=%v", c.count, c.interval, c.timeout, err, c.ok)
		}
	}
	_, err := Normalize(Spec{Tool: ToolPing, Target: "h", Params: Params{Count: 23, IntervalMS: 5000}})
	var pe *ParamError
	if !errors.As(err, &pe) || pe.Field != "count" || pe.Message != "count × interval is too long: at most 115 seconds" {
		t.Errorf("23 × 5000: %v, want count: count × interval is too long: at most 115 seconds", err)
	}
}

func TestNormalizeClearsOtherToolsFields(t *testing.T) {
	all := Params{Count: 3, IntervalMS: 300, Size: intp(10), MaxHops: 4, Rounds: 2, TimeoutMS: 900,
		RecordType: "mx", Server: "192.0.2.53", Ports: "22"}
	cases := []struct {
		tool Tool
		want Params
	}{
		{ToolPing, Params{Count: 3, IntervalMS: 300, Size: intp(10), TimeoutMS: 900}},
		{ToolTraceroute, Params{MaxHops: 4, Rounds: 2, TimeoutMS: 900}},
		{ToolDNS, Params{RecordType: "MX", Server: "192.0.2.53"}},
		{ToolTCP, Params{Ports: "22", TimeoutMS: 900}},
	}
	for _, c := range cases {
		got, err := Normalize(Spec{Tool: c.tool, Target: "example.org", Params: all})
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		if !reflect.DeepEqual(got.Params, c.want) {
			t.Errorf("%s: %+v, want %+v", c.tool, got.Params, c.want)
		}
	}
}

func TestNormalizeDNS(t *testing.T) {
	cases := []struct {
		recordType, server string
		wantType, wantSrv  string
		field              string
	}{
		{"", "", "A", "", ""},
		{" aaaa ", "", "AAAA", "", ""},
		{"caa", "", "CAA", "", ""},
		{"AXFR", "", "", "", "record_type"},
		{"A", "192.0.2.53", "A", "192.0.2.53", ""},
		{"A", " 192.0.2.53:5353 ", "A", "192.0.2.53:5353", ""},
		{"A", "192.0.2.53:0", "", "", "server"},
		{"A", "192.0.2.53:65536", "", "", "server"},
		{"A", "192.0.2.53:", "", "", "server"},
		{"A", "dns.example.org", "", "", "server"},
		{"A", "2001:db8::53", "", "", "server"},
		{"A", "[2001:db8::53]:53", "", "", "server"},
		{"A", "::ffff:192.0.2.53", "", "", "server"},
	}
	for _, c := range cases {
		got, err := Normalize(Spec{Tool: ToolDNS, Target: "example.org", Params: Params{RecordType: c.recordType, Server: c.server}})
		if f := paramField(err); f != c.field {
			t.Errorf("%q %q: field %q (%v), want %q", c.recordType, c.server, f, err, c.field)
			continue
		}
		if err == nil && (got.Params.RecordType != c.wantType || got.Params.Server != c.wantSrv) {
			t.Errorf("%q %q: got %q %q, want %q %q", c.recordType, c.server, got.Params.RecordType, got.Params.Server, c.wantType, c.wantSrv)
		}
	}
	_, err := Normalize(Spec{Tool: ToolDNS, Target: "x", Params: Params{Server: "nope"}})
	var pe *ParamError
	if !errors.As(err, &pe) || pe.Message != "server must be an IPv4 address, optionally with :port" {
		t.Errorf("bad server message: %v", err)
	}
	// PTR of a name is looked up as typed.
	if _, err := Normalize(Spec{Tool: ToolDNS, Target: "10.in-addr.arpa", Params: Params{RecordType: "PTR"}}); err != nil {
		t.Errorf("PTR of a name: %v", err)
	}
}

func TestNormalizeTCPPorts(t *testing.T) {
	for in, want := range map[string]string{"": "common", " COMMON ": "common", " 22, 80 ": "22, 80"} {
		got, err := Normalize(Spec{Tool: ToolTCP, Target: "h", Params: Params{Ports: in}})
		if err != nil || got.Params.Ports != want {
			t.Errorf("ports %q: %q, %v; want %q", in, got.Params.Ports, err, want)
		}
	}
}

func TestDeadline(t *testing.T) {
	want := map[Tool]time.Duration{ToolDNS: 15 * time.Second, ToolPing: 2 * time.Minute,
		ToolTraceroute: 3 * time.Minute, ToolTCP: 5 * time.Minute, "nmap": 0}
	for tool, d := range want {
		if got := Deadline(tool); got != d {
			t.Errorf("Deadline(%s) = %v, want %v", tool, got, d)
		}
	}
}
