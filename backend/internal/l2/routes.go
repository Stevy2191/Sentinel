package l2

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// A router's routes and addresses, for finding its internet port.
const (
	// OIDCidrRouteIfIndexDefault is ipCidrRouteIfIndex under destination
	// 0.0.0.0: walking it gives only the default routes' rows.
	OIDCidrRouteIfIndexDefault = "1.3.6.1.2.1.4.24.4.1.5.0.0.0.0"
	// OIDRouteIfIndex is the older ipRouteIfIndex, indexed by destination.
	OIDRouteIfIndex = "1.3.6.1.2.1.4.21.1.2"
	// OIDIPAdEntIfIndex is ipAdEntIfIndex, indexed by the IPv4 address.
	OIDIPAdEntIfIndex = "1.3.6.1.2.1.4.20.1.2"
)

// ParseDefaultRoutes is the ifIndexes default routes go out of: from the
// cidr table's default-route rows, or, when it has none, the old route
// table's 0.0.0.0 row. ifIndex 0 (no interface) is dropped.
func ParseDefaultRoutes(cidr, route []snmp.PDU) []int {
	set := map[int]bool{}
	if len(cidr) > 0 {
		for _, p := range cidr {
			if n, ok := p.Number(); ok && n > 0 && index(p.OID, OIDCidrRouteIfIndexDefault) != nil {
				set[int(n)] = true
			}
		}
		return sortedInts(set)
	}
	for _, p := range route {
		ix := index(p.OID, OIDRouteIfIndex)
		if n, ok := p.Number(); ok && n > 0 && len(ix) == 4 && ix[0] == 0 && ix[1] == 0 && ix[2] == 0 && ix[3] == 0 {
			set[int(n)] = true
		}
	}
	return sortedInts(set)
}

// ParseAddresses maps each ifIndex to its IPv4 addresses, sorted.
func ParseAddresses(pdus []snmp.PDU) map[int][]string {
	out := map[int][]string{}
	for _, p := range pdus {
		ix := index(p.OID, OIDIPAdEntIfIndex)
		n, ok := p.Number()
		if !ok || len(ix) != 4 {
			continue
		}
		parts := make([]string, 4)
		for i, b := range ix {
			parts[i] = strconv.Itoa(b)
		}
		out[int(n)] = append(out[int(n)], strings.Join(parts, "."))
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

var notPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/3"), // multicast and reserved, to 255.255.255.255
}

// IsPublicIPv4 reports an IPv4 address an ISP could hand a router's
// internet port: not private, loopback, link-local, "this network",
// multicast or reserved. CGNAT (100.64/10) counts: ISPs hand it out.
func IsPublicIPv4(ip string) bool {
	a, err := netip.ParseAddr(ip)
	if err != nil || !a.Is4() {
		return false
	}
	for _, p := range notPublic {
		if p.Contains(a) {
			return false
		}
	}
	return true
}
