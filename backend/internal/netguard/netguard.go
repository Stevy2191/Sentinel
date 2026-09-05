// Package netguard provides SSRF protections for outbound connections that
// Sentinel makes on a user's behalf: monitor checks (HTTP, TCP, ping) and
// outbound notification deliveries (webhook, Slack, Discord, Telegram, ntfy).
//
// Cloud-provider instance-metadata addresses are always blocked - there is no
// legitimate reason for a monitor or notification target to be one, and
// reaching one from inside a cloud VM can leak IAM credentials or other
// secrets. The rest of the private/loopback/link-local address space is
// blocked only when ALLOW_PRIVATE_NETWORK_TARGETS is explicitly set to
// false, since monitoring devices on your own LAN is Sentinel's core use
// case and is on by default.
package netguard

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// metadataNets are cloud-provider instance-metadata endpoints. Always
// blocked, regardless of ALLOW_PRIVATE_NETWORK_TARGETS.
var metadataNets = mustParseCIDRs(
	"169.254.169.254/32", // AWS, GCP, Azure, DigitalOcean, Oracle Cloud
	"169.254.170.2/32",   // AWS ECS task metadata
	"100.100.100.200/32", // Alibaba Cloud
	"fd00:ec2::254/128",  // AWS IMDS, IPv6
)

// privateNets are RFC1918/loopback/link-local/unique-local ranges. Blocked
// only when AllowPrivateTargets() returns false.
var privateNets = mustParseCIDRs(
	"127.0.0.0/8",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"169.254.0.0/16",
	"::1/128",
	"fc00::/7",
	"fe80::/10",
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("netguard: invalid CIDR %q: %v", c, err))
		}
		nets = append(nets, n)
	}
	return nets
}

// AllowPrivateTargets reports whether monitors/notifications may target
// private/loopback/link-local addresses. Defaults to true. Set
// ALLOW_PRIVATE_NETWORK_TARGETS=false to lock this down, e.g. on a
// deployment where invited (non-admin) users are less trusted and shouldn't
// be able to use a monitor as a probe against the rest of the network the
// backend can reach.
func AllowPrivateTargets() bool {
	v := strings.TrimSpace(os.Getenv("ALLOW_PRIVATE_NETWORK_TARGETS"))
	if v == "" {
		return true
	}
	allow, err := strconv.ParseBool(v)
	if err != nil {
		return true
	}
	return allow
}

// ErrBlockedAddress is returned when a target resolves to a blocked address.
type ErrBlockedAddress struct {
	IP net.IP
}

func (e ErrBlockedAddress) Error() string {
	return fmt.Sprintf("target address %s is not allowed (metadata or internal network address)", e.IP)
}

// IsBlocked reports whether ip should never be reached by a
// Sentinel-initiated outbound request, given the current
// AllowPrivateTargets() setting.
func IsBlocked(ip net.IP) bool {
	if ip == nil {
		return true
	}
	for _, n := range metadataNets {
		if n.Contains(ip) {
			return true
		}
	}
	if !AllowPrivateTargets() {
		for _, n := range privateNets {
			if n.Contains(ip) {
				return true
			}
		}
	}
	return false
}

// DialControl is passed as net.Dialer.Control. It runs after DNS resolution
// but before the socket connects, and receives the literal address about to
// be dialed - so, unlike checking the hostname up front, it also catches DNS
// rebinding (a hostname that resolves to an allowed IP at validation time and
// a blocked one at connect time).
func DialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("netguard: could not parse resolved address %q", address)
	}
	if IsBlocked(ip) {
		return ErrBlockedAddress{IP: ip}
	}
	return nil
}

// SafeDialer returns a net.Dialer that refuses to connect to a blocked
// address, for callers that dial directly (TCP/ping checks) rather than
// through an http.Client.
func SafeDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: DialControl}
}

// NewGuardedHTTPClient returns an *http.Client whose Transport dials through
// DialControl, so it can't be used to reach a blocked address - including via
// a redirect, since each redirect is dialed through the same Transport.
func NewGuardedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: SafeDialer(timeout).DialContext,
		},
	}
}
