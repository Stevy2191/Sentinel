package l2

import (
	"sort"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// The tables that say which ports belong to a LAG.
const (
	// OIDDot3adAttachedAgg is IEEE8023-LAG-MIB dot3adAggPortAttachedAggID:
	// per member ifIndex, the aggregator's ifIndex (0 when not attached).
	OIDDot3adAttachedAgg = "1.2.840.10006.300.43.1.2.1.1.13"
	// OIDIfStackStatus is IF-MIB ifStackStatus, indexed higher.lower.
	OIDIfStackStatus = "1.3.6.1.2.1.31.1.2.1.3"
)

// ParseLAG maps each LAG member's ifIndex to its aggregator's, from
// dot3adAggPortAttachedAggID (LACP), then, for the aggregators that names
// no member of (a Cisco's static and PAgP channels, or a device without
// dot3ad), from the active ifStackTable rows whose higher layer is one.
func ParseLAG(dot3ad, stack []snmp.PDU, isLAG func(ifIndex int) bool) map[int]int {
	out := map[int]int{}
	named := map[int]bool{}
	for _, p := range dot3ad {
		ix := index(p.OID, OIDDot3adAttachedAgg)
		if n, ok := p.Number(); ok && len(ix) == 1 && n > 0 {
			out[ix[0]] = int(n)
			named[int(n)] = true
		}
	}
	for _, p := range stack {
		ix := index(p.OID, OIDIfStackStatus)
		n, ok := p.Number()
		if !ok || n != 1 || len(ix) != 2 {
			continue
		}
		higher, lower := ix[0], ix[1]
		if _, taken := out[lower]; higher > 0 && lower > 0 && !named[higher] && !taken && isLAG(higher) && !isLAG(lower) {
			out[lower] = higher
		}
	}
	return out
}

// sortedInts is the set's members in order.
func sortedInts(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}
