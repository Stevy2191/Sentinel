// Package nettools runs the network troubleshooting tools: ping, MTR-style
// traceroute, DNS lookup and TCP port check. It is compiled into both the
// Sentinel server and the agent, so it imports only the standard library and
// golang.org/x/net and golang.org/x/sys: no database, no HTTP framework and no
// other Sentinel package.
package nettools

import "time"

// Tool names one of the network tools.
type Tool string

const (
	ToolPing       Tool = "ping"
	ToolTraceroute Tool = "traceroute"
	ToolDNS        Tool = "dns"
	ToolTCP        Tool = "tcp"
)

// Spec is one run: the tool, what it targets and its parameters.
type Spec struct {
	Tool Tool `json:"tool"`
	// Target is what the user typed: a host name or IPv4 address; for dns,
	// the name (or IPv4 address, for PTR) to look up.
	Target string `json:"target"`
	// TargetIP is the IPv4 address the tool contacts: the probed host for
	// ping, traceroute and tcp; the named DNS server for dns (empty when the
	// lookup goes through the system resolver). Set by the server after the
	// allowlist check; tools never resolve Target themselves.
	TargetIP string `json:"target_ip,omitempty"`
	Params   Params `json:"params"`
}

// Params holds every tool's parameters. Normalize fills the defaults and
// clears the fields the tool does not use.
type Params struct {
	Count      int    `json:"count,omitempty"`       // ping
	IntervalMS int    `json:"interval_ms,omitempty"` // ping
	Size       *int   `json:"size,omitempty"`        // ping payload bytes; 0 is valid
	MaxHops    int    `json:"max_hops,omitempty"`    // traceroute
	Rounds     int    `json:"rounds,omitempty"`      // traceroute
	TimeoutMS  int    `json:"timeout_ms,omitempty"`  // ping, traceroute, tcp (per probe)
	RecordType string `json:"record_type,omitempty"` // dns
	Server     string `json:"server,omitempty"`      // dns: "" = system resolver, else "a.b.c.d" or "a.b.c.d:port"
	Ports      string `json:"ports,omitempty"`       // tcp: "22,80,443,8000-8100" or "common"
}

// Event is one result a tool reports while it runs. Data is one of the event
// types below, chosen by Type.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Emitter receives a tool's events. A tool calls it from one goroutine at a
// time, in the order the results became known.
type Emitter func(Event)

// Event types.
const (
	EventReply     = "reply"      // ping: PingReply
	EventTimeout   = "timeout"    // ping: PingTimeout
	EventError     = "error"      // ping: PingError
	EventHop       = "hop"        // traceroute: HopProbe
	EventRoundDone = "round_done" // traceroute: RoundDone
	EventHopName   = "hop_name"   // traceroute: HopName
	EventAnswer    = "answer"     // dns: DNSAnswer
	EventStart     = "start"      // tcp: ScanStart
	EventPort      = "port"       // tcp: PortResult
)

// PingReply is an echo reply to probe Seq.
type PingReply struct {
	Seq   int     `json:"seq"`
	RTTMS float64 `json:"rtt_ms"`
	TTL   int     `json:"ttl"`
	From  string  `json:"from"`
}

// PingTimeout is a probe that got no reply in time.
type PingTimeout struct {
	Seq int `json:"seq"`
}

// PingError is a probe that failed, e.g. "host unreachable from 10.0.0.1".
type PingError struct {
	Seq     int    `json:"seq"`
	Message string `json:"message"`
}

// PingSummary sums up a ping run. The RTT figures are nil without replies;
// JitterMS is nil with fewer than two.
type PingSummary struct {
	Sent     int      `json:"sent"`
	Received int      `json:"received"`
	LossPct  float64  `json:"loss_pct"`
	MinMS    *float64 `json:"min_ms"`
	AvgMS    *float64 `json:"avg_ms"`
	MaxMS    *float64 `json:"max_ms"`
	JitterMS *float64 `json:"jitter_ms"`
}

// HopProbe is one traceroute probe: what answered the probe sent with TTL in
// Round. Addr is empty and RTTMS nil when nothing answered; Error is set when
// the probe itself failed (e.g. it could not be sent) rather than timing out.
type HopProbe struct {
	Round   int      `json:"round"`
	TTL     int      `json:"ttl"`
	Addr    string   `json:"addr,omitempty"`
	RTTMS   *float64 `json:"rtt_ms,omitempty"`
	Reached bool     `json:"reached"`
	Error   string   `json:"error,omitempty"`
}

// HopStats is one row of the MTR table: every probe sent with one TTL.
type HopStats struct {
	TTL      int      `json:"ttl"`
	Addrs    []string `json:"addrs"`
	Name     string   `json:"name,omitempty"`
	Sent     int      `json:"sent"`
	Received int      `json:"received"`
	LossPct  float64  `json:"loss_pct"`
	LastMS   *float64 `json:"last_ms"`
	AvgMS    *float64 `json:"avg_ms"`
	BestMS   *float64 `json:"best_ms"`
	WorstMS  *float64 `json:"worst_ms"`
	StdevMS  *float64 `json:"stdev_ms"`
}

// RoundDone closes a traceroute round with the table so far.
type RoundDone struct {
	Round int        `json:"round"`
	Hops  []HopStats `json:"hops"` // cumulative after this round
}

// HopName is the reverse-DNS name of a hop address.
type HopName struct {
	TTL  int    `json:"ttl"`
	Addr string `json:"addr"`
	Name string `json:"name"`
}

// TraceSummary sums up a traceroute run.
type TraceSummary struct {
	Reached  bool       `json:"reached"`
	HopCount int        `json:"hop_count"`
	Hops     []HopStats `json:"hops"`
}

// DNSRecord is one resource record, with its data written out as text.
type DNSRecord struct {
	Name string `json:"name"`
	Type string `json:"type"`
	TTL  uint32 `json:"ttl"`
	Data string `json:"data"`
}

// DNSAnswer is the reply to a DNS lookup.
type DNSAnswer struct {
	Server        string      `json:"server"` // ip:port asked
	RCode         string      `json:"rcode"`  // "NOERROR", "NXDOMAIN", …
	Authoritative bool        `json:"authoritative"`
	Truncated     bool        `json:"truncated"` // the UDP answer was truncated
	TCP           bool        `json:"tcp"`       // the answer shown came over TCP
	RTTMS         float64     `json:"rtt_ms"`
	Answer        []DNSRecord `json:"answer"`
	Authority     []DNSRecord `json:"authority"`
	Additional    []DNSRecord `json:"additional"`
}

// DNSSummary sums up a DNS lookup.
type DNSSummary struct {
	Server      string  `json:"server"`
	RCode       string  `json:"rcode"`
	AnswerCount int     `json:"answer_count"`
	RTTMS       float64 `json:"rtt_ms"`
}

// TCP port states.
const (
	PortOpen     = "open"
	PortClosed   = "closed"
	PortFiltered = "filtered"
)

// ScanStart opens a port scan with the number of ports it will try.
type ScanStart struct {
	Total int `json:"total"`
}

// PortResult is the state of one port. RTTMS is set for open ports.
type PortResult struct {
	Port    int      `json:"port"`
	State   string   `json:"state"`
	RTTMS   *float64 `json:"rtt_ms,omitempty"`
	Service string   `json:"service,omitempty"`
}

// TCPSummary sums up a port scan.
type TCPSummary struct {
	Total     int   `json:"total"`
	Open      int   `json:"open"`
	Closed    int   `json:"closed"`
	Filtered  int   `json:"filtered"`
	OpenPorts []int `json:"open_ports"`
}

// Deadline is the tool's hard limit: dns 15s, ping 2m, traceroute 3m, tcp 5m.
// It is 0 for an unknown tool.
func Deadline(t Tool) time.Duration {
	switch t {
	case ToolDNS:
		return 15 * time.Second
	case ToolPing:
		return 2 * time.Minute
	case ToolTraceroute:
		return 3 * time.Minute
	case ToolTCP:
		return 5 * time.Minute
	}
	return 0
}
