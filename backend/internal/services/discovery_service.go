package services

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Tuning constants for a subnet sweep. These bound how invasive and how long a
// single scan can be, since - unlike a normal monitor check - one request here
// can fan out into hundreds of probes.
const (
	discoveryProtocolICMP = 1
	discoveryPingPayload  = "SENTINEL-DISCOVERY"

	// defaultDiscoveryTimeout is used when the caller does not specify a
	// per-host timeout.
	defaultDiscoveryTimeout = 800 * time.Millisecond
	// maxDiscoveryTimeout caps a caller-supplied per-host timeout.
	maxDiscoveryTimeout = 3 * time.Second
	// discoveryConcurrency bounds how many hosts are probed at once.
	discoveryConcurrency = 40
	// maxDiscoveryHosts caps the size of subnet that can be scanned in one
	// request (1024 addresses, i.e. up to a /22) so a request can't be used to
	// sweep, say, a /8.
	maxDiscoveryHosts = 1024
	// maxDiscoveryDuration is a hard ceiling on total scan time regardless of
	// subnet size or per-host timeout, so a request can't hang indefinitely.
	maxDiscoveryDuration = 45 * time.Second
	// reverseDNSTimeout bounds the best-effort hostname lookup for each host
	// that responds; it never blocks a host from being reported if it's slow.
	reverseDNSTimeout = 500 * time.Millisecond
)

// discoveryFallbackPorts are tried, in order, when ICMP is unavailable (e.g.
// the container lacks CAP_NET_RAW and the kernel's unprivileged-ping range is
// not configured). A host that accepts or actively refuses a connection on any
// of these is considered up.
var discoveryFallbackPorts = []string{"80", "443", "22", "8080", "445"}

// DiscoveredHost is one address that answered during a subnet sweep.
type DiscoveredHost struct {
	IP             string `json:"ip"`
	Hostname       string `json:"hostname,omitempty"`
	ResponseTimeMs int    `json:"response_time_ms"`
	// Method is "icmp" or "tcp", depending on how the host was reached.
	Method string `json:"method"`
}

// DiscoveryResult summarizes one subnet sweep.
type DiscoveryResult struct {
	CIDR         string           `json:"cidr"`
	HostsScanned int              `json:"hosts_scanned"`
	HostsUp      int              `json:"hosts_up"`
	DurationMs   int64            `json:"duration_ms"`
	Hosts        []DiscoveredHost `json:"hosts"`
}

// DiscoveryService scans IPv4 subnets for hosts that respond to a ping,
// without any external dependency (no nmap binary required): it uses a raw or
// unprivileged ICMP echo where the runtime environment allows it, and falls
// back to a short list of common TCP ports otherwise.
type DiscoveryService struct{}

// NewDiscoveryService returns a ready-to-use DiscoveryService.
func NewDiscoveryService() *DiscoveryService {
	return &DiscoveryService{}
}

// ScanSubnet probes every usable host address in cidr (e.g. "192.168.1.0/24")
// and returns the ones that responded. perHostTimeout of zero uses the
// default; it is otherwise clamped to maxDiscoveryTimeout. The overall call is
// bounded by maxDiscoveryDuration regardless of subnet size.
func (s *DiscoveryService) ScanSubnet(ctx context.Context, cidr string, perHostTimeout time.Duration) (*DiscoveryResult, error) {
	ip, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	if ip.To4() == nil {
		return nil, fmt.Errorf("only IPv4 subnets are supported")
	}

	hosts, err := enumerateHosts(ipnet)
	if err != nil {
		return nil, err
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("%q has no scannable host addresses", cidr)
	}
	if len(hosts) > maxDiscoveryHosts {
		return nil, fmt.Errorf(
			"%q is too large to scan (%d addresses; max is %d - use a /22 or smaller)",
			cidr, len(hosts), maxDiscoveryHosts,
		)
	}

	if perHostTimeout <= 0 {
		perHostTimeout = defaultDiscoveryTimeout
	}
	if perHostTimeout > maxDiscoveryTimeout {
		perHostTimeout = maxDiscoveryTimeout
	}

	scanCtx, cancel := context.WithTimeout(ctx, maxDiscoveryDuration)
	defer cancel()

	// Probe once up front rather than per host: if ICMP can't be opened at all
	// (no CAP_NET_RAW and no unprivileged-ping range configured), there's no
	// point paying that failed syscall for every one of up to 1024 addresses.
	icmpUp := icmpAvailable()

	start := time.Now()
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, discoveryConcurrency)
	results := make([]DiscoveredHost, 0, len(hosts))

	for _, host := range hosts {
		if scanCtx.Err() != nil {
			break
		}
		host := host
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			if scanCtx.Err() != nil {
				return
			}
			rtt, method, up := probeHost(scanCtx, host, perHostTimeout, icmpUp)
			if !up {
				return
			}
			found := DiscoveredHost{
				IP:             host,
				Hostname:       reverseLookup(host),
				ResponseTimeMs: int(rtt.Milliseconds()),
				Method:         method,
			}
			mu.Lock()
			results = append(results, found)
			mu.Unlock()
		}()
	}
	wg.Wait()

	sort.Slice(results, func(i, j int) bool { return ipToUint32(results[i].IP) < ipToUint32(results[j].IP) })

	return &DiscoveryResult{
		CIDR:         cidr,
		HostsScanned: len(hosts),
		HostsUp:      len(results),
		DurationMs:   time.Since(start).Milliseconds(),
		Hosts:        results,
	}, nil
}

// enumerateHosts lists every usable host address in an IPv4 network, excluding
// the network and broadcast addresses for subnets larger than a /31.
func enumerateHosts(ipnet *net.IPNet) ([]string, error) {
	ones, bits := ipnet.Mask.Size()
	if bits != 32 {
		return nil, fmt.Errorf("only IPv4 is supported")
	}
	base := ipnet.IP.To4()
	if base == nil {
		return nil, fmt.Errorf("invalid IPv4 network")
	}
	baseInt := binary.BigEndian.Uint32(base)

	total := 1 << uint(bits-ones)
	first, last := 0, total-1
	if total > 2 {
		first, last = 1, total-2 // skip network + broadcast
	}

	hosts := make([]string, 0, last-first+1)
	for i := first; i <= last; i++ {
		addr := make(net.IP, 4)
		binary.BigEndian.PutUint32(addr, baseInt+uint32(i))
		hosts = append(hosts, addr.String())
	}
	return hosts, nil
}

// icmpAvailable reports whether this process can open an ICMP socket at all,
// either unprivileged (Linux ping_group_range) or raw (CAP_NET_RAW/root).
func icmpAvailable() bool {
	if conn, err := icmp.ListenPacket("udp4", "0.0.0.0"); err == nil {
		_ = conn.Close()
		return true
	}
	if conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0"); err == nil {
		_ = conn.Close()
		return true
	}
	return false
}

// probeHost tries ICMP first (when available) and falls back to a handful of
// common TCP ports. It reports the round-trip time, which method answered,
// and whether the host is considered up at all.
func probeHost(ctx context.Context, ipAddr string, timeout time.Duration, icmpUp bool) (time.Duration, string, bool) {
	if icmpUp {
		if rtt, ok := icmpPingOnce(ctx, ipAddr, timeout); ok {
			return rtt, "icmp", true
		}
	}
	for _, port := range discoveryFallbackPorts {
		start := time.Now()
		dialer := net.Dialer{Timeout: timeout}
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(ipAddr, port))
		if err == nil {
			_ = conn.Close()
			return time.Since(start), "tcp", true
		}
		// A "connection refused" still proves something is listening on the
		// network stack at that address, which is enough to call it up even
		// though nothing is actually served on that port.
		if opErr, ok := err.(*net.OpError); ok && strings.Contains(opErr.Err.Error(), "refused") {
			return time.Since(start), "tcp", true
		}
	}
	return 0, "", false
}

// icmpPingOnce sends a single ICMP echo request to ipAddr and waits for a
// reply, mirroring CheckService's ping check but scoped to a single attempt
// with no monitor context, since a discovery sweep has no monitor to log
// against.
func icmpPingOnce(ctx context.Context, ipAddr string, timeout time.Duration) (time.Duration, bool) {
	privileged := false
	conn, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		conn, err = icmp.ListenPacket("ip4:icmp", "0.0.0.0")
		if err != nil {
			return 0, false
		}
		privileged = true
	}
	defer conn.Close()

	ip := net.ParseIP(ipAddr)
	if ip == nil {
		return 0, false
	}

	var dst net.Addr = &net.UDPAddr{IP: ip}
	if privileged {
		dst = &net.IPAddr{IP: ip}
	}

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte(discoveryPingPayload),
		},
	}
	wb, err := msg.Marshal(nil)
	if err != nil {
		return 0, false
	}

	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return 0, false
	}

	start := time.Now()
	if _, err := conn.WriteTo(wb, dst); err != nil {
		return 0, false
	}

	reply := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(reply)
		if err != nil {
			return 0, false // timeout, or the socket was closed: treat as down
		}
		parsed, err := icmp.ParseMessage(discoveryProtocolICMP, reply[:n])
		if err != nil {
			continue
		}
		if parsed.Type == ipv4.ICMPTypeEchoReply {
			return time.Since(start), true
		}
		// Unrelated ICMP traffic (e.g. a reply meant for another probe sharing
		// the same broadcast domain); keep waiting until the deadline.
	}
}

// reverseLookup best-effort resolves a PTR record for ip. It returns "" on any
// failure or timeout rather than letting a slow/absent PTR record hold up the
// scan or fail an otherwise-successful probe.
func reverseLookup(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), reverseDNSTimeout)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

// ipToUint32 converts a dotted-quad IPv4 address into its numeric form for
// sorting; malformed input sorts as 0.
func ipToUint32(s string) uint32 {
	ip := net.ParseIP(s)
	if ip == nil {
		return 0
	}
	v4 := ip.To4()
	if v4 == nil {
		return 0
	}
	return binary.BigEndian.Uint32(v4)
}
