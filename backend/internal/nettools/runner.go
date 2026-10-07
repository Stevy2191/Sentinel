package nettools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Runner runs tools. Nil fields use the system's ICMP, dialer and resolvers.
type Runner struct {
	NewProber func() (Prober, error)
	Dial      func(ctx context.Context, network, addr string) (net.Conn, error)
	SystemDNS func() (string, error)
	LookupPTR func(ctx context.Context, addr string) (string, error)
}

// Run executes a normalized spec, calling emit for each event as it
// happens, and returns the tool's summary (PingSummary, TraceSummary,
// DNSSummary or TCPSummary). When ctx ends first it returns the summary of
// what completed and ctx.Err(). Any other failure returns a nil summary and
// the error (ErrICMPUnavailable as is).
func (r *Runner) Run(ctx context.Context, s Spec, emit Emitter) (any, error) {
	switch s.Tool {
	case ToolPing, ToolTraceroute, ToolTCP:
		if !isIPv4(s.TargetIP) {
			return nil, fmt.Errorf("target_ip %q is not an IPv4 address", s.TargetIP)
		}
	case ToolDNS:
	default:
		return nil, fmt.Errorf("unknown tool %q", s.Tool)
	}

	switch s.Tool {
	case ToolPing, ToolTraceroute:
		open := r.NewProber
		if open == nil {
			open = NewProber
		}
		p, err := open()
		if err != nil {
			return nil, err
		}
		defer p.Close()
		if s.Tool == ToolPing {
			return partial(ping(ctx, p, s, emit))
		}
		lookup := r.LookupPTR
		if lookup == nil {
			lookup = lookupPTR
		}
		return partial(traceroute(ctx, p, s, emit, lookup, traceRoundGap))
	case ToolTCP:
		dial := r.Dial
		if dial == nil {
			dial = (&net.Dialer{}).DialContext
		}
		return partial(tcpScan(ctx, dial, s, emit))
	}

	server, err := r.dnsServer(s)
	if err != nil {
		return nil, err
	}
	sum, err := dnsLookup(ctx, s, server, emit)
	if err != nil {
		return nil, err
	}
	return sum, nil
}

// partial passes a summary on with a nil error or ctx's error, and drops it
// for any other error.
func partial[T any](sum T, err error) (any, error) {
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	return sum, err
}

// dnsServer is the "ip:port" a lookup asks: the system resolver, or the
// named server, which the caller has checked and put in TargetIP.
func (r *Runner) dnsServer(s Spec) (string, error) {
	if s.Params.Server == "" {
		system := r.SystemDNS
		if system == nil {
			system = SystemResolver
		}
		return system()
	}
	if !isIPv4(s.TargetIP) {
		return "", fmt.Errorf("target_ip %q is not an IPv4 address", s.TargetIP)
	}
	port := "53"
	if _, p, err := net.SplitHostPort(s.Params.Server); err == nil {
		port = p
	}
	return net.JoinHostPort(s.TargetIP, port), nil
}

// isIPv4 accepts dotted-quad IPv4 only (not IPv4-mapped IPv6).
func isIPv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil && !strings.Contains(s, ":")
}

// lookupPTR is the system's reverse lookup: the first name, without its
// trailing dot.
func lookupPTR(ctx context.Context, addr string) (string, error) {
	names, err := net.DefaultResolver.LookupAddr(ctx, addr)
	if err != nil || len(names) == 0 {
		return "", err
	}
	return strings.TrimSuffix(names[0], "."), nil
}
