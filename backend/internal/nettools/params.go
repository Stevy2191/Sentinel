package nettools

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Limits and defaults. Normalize enforces every one of them.
const (
	PingCountDefault, PingCountMin, PingCountMax                = 5, 1, 100
	PingIntervalDefaultMS, PingIntervalMinMS, PingIntervalMaxMS = 1000, 200, 5000
	PingTimeoutDefaultMS, PingTimeoutMinMS, PingTimeoutMaxMS    = 2000, 500, 5000
	PingSizeDefault, PingSizeMax                                = 56, 1472
	PingMaxTotalMS                                              = 115000 // count × interval + timeout
	TraceMaxHopsDefault, TraceMaxHopsMin, TraceMaxHopsMax       = 30, 1, 30
	TraceRoundsDefault, TraceRoundsMin, TraceRoundsMax          = 5, 1, 10
	TraceTimeoutDefaultMS, TraceTimeoutMinMS, TraceTimeoutMaxMS = 1000, 500, 3000
	TCPTimeoutDefaultMS, TCPTimeoutMinMS, TCPTimeoutMaxMS       = 1500, 500, 5000
	TCPConcurrency                                              = 50
	MaxPorts                                                    = 1024
	MaxPortsLength                                              = 8 << 10 // bytes of the ports list
	MaxTargetLength                                             = 253
	DNSTimeout                                                  = 5 * time.Second
)

// DNSRecordTypes in display order; RecordType default "A".
var DNSRecordTypes = []string{"A", "AAAA", "CNAME", "MX", "NS", "TXT", "SOA", "SRV", "PTR", "CAA"}

// ParamError is a parameter Normalize refused. Field is the parameter's JSON
// name ("count", "server", …), or "tool" / "target".
type ParamError struct {
	Field   string
	Message string
}

func (e *ParamError) Error() string { return e.Field + ": " + e.Message }

// Normalize checks a spec against the limits and returns it with defaults
// filled in and unused parameters cleared. It does not resolve anything or
// consult the allowlist. Errors are *ParamError.
func Normalize(s Spec) (Spec, error) {
	s.Target = strings.TrimSpace(s.Target)
	s.TargetIP = strings.TrimSpace(s.TargetIP)
	var norm func(Params) (Params, error)
	switch s.Tool {
	case ToolPing:
		norm = normalizePing
	case ToolTraceroute:
		norm = normalizeTraceroute
	case ToolDNS:
		norm = normalizeDNS
	case ToolTCP:
		norm = normalizeTCP
	default:
		return s, &ParamError{Field: "tool", Message: fmt.Sprintf("unknown tool %q", s.Tool)}
	}
	if err := checkTarget(s.Target); err != nil {
		return s, err
	}
	p, err := norm(s.Params)
	if err != nil {
		return s, err
	}
	s.Params = p
	return s, nil
}

func checkTarget(t string) error {
	switch {
	case t == "":
		return &ParamError{Field: "target", Message: "target is required"}
	case len(t) > MaxTargetLength:
		return &ParamError{Field: "target", Message: fmt.Sprintf("target is too long: at most %d characters", MaxTargetLength)}
	case strings.IndexFunc(t, unicode.IsSpace) >= 0:
		return &ParamError{Field: "target", Message: "target must not contain spaces"}
	}
	return nil
}

// intParam returns v, or def when v is 0, and checks it lies in [lo, hi].
func intParam(field string, v, def, lo, hi int) (int, error) {
	if v == 0 {
		v = def
	}
	if v < lo || v > hi {
		return 0, &ParamError{Field: field, Message: fmt.Sprintf("%s must be between %d and %d", field, lo, hi)}
	}
	return v, nil
}

func normalizePing(in Params) (Params, error) {
	var out Params
	var err error
	if out.Count, err = intParam("count", in.Count, PingCountDefault, PingCountMin, PingCountMax); err != nil {
		return out, err
	}
	if out.IntervalMS, err = intParam("interval_ms", in.IntervalMS, PingIntervalDefaultMS, PingIntervalMinMS, PingIntervalMaxMS); err != nil {
		return out, err
	}
	if out.TimeoutMS, err = intParam("timeout_ms", in.TimeoutMS, PingTimeoutDefaultMS, PingTimeoutMinMS, PingTimeoutMaxMS); err != nil {
		return out, err
	}
	size := PingSizeDefault
	if in.Size != nil {
		size = *in.Size
	}
	if size < 0 || size > PingSizeMax {
		return out, &ParamError{Field: "size", Message: fmt.Sprintf("size must be between 0 and %d", PingSizeMax)}
	}
	out.Size = &size
	if out.Count*out.IntervalMS+out.TimeoutMS > PingMaxTotalMS {
		return out, &ParamError{Field: "count", Message: "count × interval is too long: at most 115 seconds"}
	}
	return out, nil
}

func normalizeTraceroute(in Params) (Params, error) {
	var out Params
	var err error
	if out.MaxHops, err = intParam("max_hops", in.MaxHops, TraceMaxHopsDefault, TraceMaxHopsMin, TraceMaxHopsMax); err != nil {
		return out, err
	}
	if out.Rounds, err = intParam("rounds", in.Rounds, TraceRoundsDefault, TraceRoundsMin, TraceRoundsMax); err != nil {
		return out, err
	}
	if out.TimeoutMS, err = intParam("timeout_ms", in.TimeoutMS, TraceTimeoutDefaultMS, TraceTimeoutMinMS, TraceTimeoutMaxMS); err != nil {
		return out, err
	}
	return out, nil
}

func normalizeDNS(in Params) (Params, error) {
	var out Params
	out.RecordType = strings.ToUpper(strings.TrimSpace(in.RecordType))
	if out.RecordType == "" {
		out.RecordType = "A"
	}
	known := false
	for _, t := range DNSRecordTypes {
		if t == out.RecordType {
			known = true
		}
	}
	if !known {
		return out, &ParamError{Field: "record_type", Message: "record_type must be one of " + strings.Join(DNSRecordTypes, ", ")}
	}
	server, err := normalizeServer(in.Server)
	if err != nil {
		return out, err
	}
	out.Server = server
	return out, nil
}

// normalizeServer accepts "", "a.b.c.d" or "a.b.c.d:port" and writes the
// address in canonical form.
func normalizeServer(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	bad := &ParamError{Field: "server", Message: "server must be an IPv4 address, optionally with :port"}
	host, port, hasPort := s, "", false
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, port, hasPort = h, p, true
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || strings.Contains(host, ":") {
		return "", bad
	}
	if !hasPort {
		return ip.To4().String(), nil
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", bad
	}
	return net.JoinHostPort(ip.To4().String(), strconv.Itoa(n)), nil
}

func normalizeTCP(in Params) (Params, error) {
	var out Params
	var err error
	// Before parsing: the list is stored and audited as typed, and a long
	// one of repeated ranges names few ports but costs a pass per range.
	if len(in.Ports) > MaxPortsLength {
		return out, &ParamError{Field: "ports", Message: "the port list is too long"}
	}
	out.Ports = strings.TrimSpace(in.Ports)
	if out.Ports == "" || strings.EqualFold(out.Ports, "common") {
		out.Ports = "common"
	}
	if _, perr := ParsePorts(out.Ports); perr != nil {
		return out, &ParamError{Field: "ports", Message: perr.Error()}
	}
	if out.TimeoutMS, err = intParam("timeout_ms", in.TimeoutMS, TCPTimeoutDefaultMS, TCPTimeoutMinMS, TCPTimeoutMaxMS); err != nil {
		return out, err
	}
	return out, nil
}
