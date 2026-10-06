package nettools

import (
	"fmt"
	"net"
	"strings"
)

// Messages for bad allowlist entries.
const (
	msgBadEntry = "not a valid IPv4 address, CIDR or host name"
	msgTooBroad = "too broad: use /8 or narrower"
	msgIPv6     = "IPv6 is not supported yet"
)

// EntryError is one allowlist entry ParseAllowlist refused.
type EntryError struct {
	Entry   string `json:"entry"`
	Message string `json:"message"`
}

// Allowlist is the set of targets the tools may contact.
type Allowlist struct {
	nets     []*net.IPNet
	hosts    map[string]bool // exact names, lower case, no trailing dot
	suffixes []string        // ".example.org" for "*.example.org"
}

// ParseAllowlist accepts IPv4 addresses, IPv4 CIDRs (no broader than /8),
// exact host names and "*.domain" wildcards. Blank entries are ignored.
// Every bad entry is reported; the returned list holds the good ones.
func ParseAllowlist(entries []string) (*Allowlist, []EntryError) {
	a := &Allowlist{hosts: map[string]bool{}}
	var bad []EntryError
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		if msg := a.add(e); msg != "" {
			bad = append(bad, EntryError{Entry: e, Message: msg})
		}
	}
	return a, bad
}

// add parses one trimmed entry into a and returns "" or why it is refused.
func (a *Allowlist) add(e string) string {
	if strings.Contains(e, ":") {
		if net.ParseIP(e) != nil {
			return msgIPv6
		}
		if _, _, err := net.ParseCIDR(e); err == nil {
			return msgIPv6
		}
		return msgBadEntry
	}
	if strings.Contains(e, "/") {
		ip, n, err := net.ParseCIDR(e)
		if err != nil || ip.To4() == nil {
			return msgBadEntry
		}
		if ones, _ := n.Mask.Size(); ones < 8 {
			return msgTooBroad
		}
		a.nets = append(a.nets, n)
		return ""
	}
	if ip := net.ParseIP(e); ip != nil {
		a.nets = append(a.nets, &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)})
		return ""
	}
	name := normalizeHost(e)
	if rest, ok := strings.CutPrefix(name, "*."); ok {
		if !strings.Contains(rest, ".") || !validHostName(rest) {
			return msgBadEntry
		}
		a.suffixes = append(a.suffixes, "."+rest)
		return ""
	}
	if !validHostName(name) {
		return msgBadEntry
	}
	a.hosts[name] = true
	return ""
}

// normalizeHost lower-cases a host name and drops one trailing dot.
func normalizeHost(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// validHostName: letters, digits and hyphens in dot-separated labels of
// 1–63 characters, no label starting or ending with a hyphen, at most 253
// characters, and a last label that is not all digits (so "10.0.0.256" is
// not taken for a name).
func validHostName(s string) bool {
	if s == "" || len(s) > MaxTargetLength {
		return false
	}
	labels := strings.Split(s, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	last := labels[len(labels)-1]
	return strings.Trim(last, "0123456789") != ""
}

// Empty reports whether the list allows nothing.
func (a *Allowlist) Empty() bool {
	return a == nil || len(a.nets) == 0 && len(a.hosts) == 0 && len(a.suffixes) == 0
}

// Allows reports whether a run may contact ip for the typed target: the
// target matches a host-name entry, or ip lies in an address/CIDR entry —
// and ip is never AlwaysBlocked. ip may be nil only for a pure host-name match.
func (a *Allowlist) Allows(target string, ip net.IP) bool {
	if a == nil {
		return false
	}
	if ip != nil && AlwaysBlocked(ip) {
		return false
	}
	if a.matchesHost(target) {
		return true
	}
	return ip != nil && InNets(ip, a.nets)
}

func (a *Allowlist) matchesHost(target string) bool {
	t := normalizeHost(target)
	if t == "" {
		return false
	}
	if a.hosts[t] {
		return true
	}
	for _, suffix := range a.suffixes {
		if len(t) > len(suffix) && strings.HasSuffix(t, suffix) {
			return true
		}
	}
	return false
}

// alwaysBlockedNets: cloud metadata addresses (the ones netguard blocks),
// "this network", multicast and the limited broadcast address.
var alwaysBlockedNets = mustCIDRs(
	"169.254.169.254/32", // AWS, GCP, Azure, DigitalOcean, Oracle Cloud metadata
	"169.254.170.2/32",   // AWS ECS task metadata
	"100.100.100.200/32", // Alibaba Cloud metadata
	"0.0.0.0/8",
	"224.0.0.0/4",
	"255.255.255.255/32",
)

func mustCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("nettools: bad CIDR %q: %v", c, err))
		}
		out = append(out, n)
	}
	return out
}

// AlwaysBlocked reports whether ip may never be contacted, whatever the
// allowlist says: the cloud metadata addresses, 0.0.0.0/8, multicast
// (224.0.0.0/4) and 255.255.255.255. A nil or non-IPv4 address is blocked
// too, since the tools reach IPv4 only.
func AlwaysBlocked(ip net.IP) bool {
	if ip.To4() == nil {
		return true
	}
	return InNets(ip, alwaysBlockedNets)
}

// ParseAddressList parses the agent's TOOLS_ALLOWED_TARGETS: comma-separated
// IPv4 addresses and CIDRs ("" → nil, nil).
func ParseAddressList(s string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, part := range strings.Split(s, ",") {
		e := strings.TrimSpace(part)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			ip, n, err := net.ParseCIDR(e)
			if err != nil || ip.To4() == nil || strings.Contains(e, ":") {
				return nil, fmt.Errorf("%q is not an IPv4 address or CIDR", e)
			}
			nets = append(nets, n)
			continue
		}
		ip := net.ParseIP(e)
		if ip == nil || ip.To4() == nil || strings.Contains(e, ":") {
			return nil, fmt.Errorf("%q is not an IPv4 address or CIDR", e)
		}
		nets = append(nets, &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)})
	}
	return nets, nil
}

// InNets reports whether the IPv4 address ip lies in one of nets.
func InNets(ip net.IP, nets []*net.IPNet) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip4) {
			return true
		}
	}
	return false
}
