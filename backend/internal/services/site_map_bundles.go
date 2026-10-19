package services

import (
	"sort"
	"strings"

	"github.com/google/uuid"
)

// MapLag is a bundle's LAG name at each end ("lag 1", "3/1", "Po1"); "" at
// an end whose cables aren't all in one aggregator.
type MapLag struct {
	A string `json:"a"`
	B string `json:"b"`
}

const bundlePrefix = "bundle:"

// isBundleKey reports a bundle's key rather than one cable's.
func isBundleKey(k string) bool { return strings.HasPrefix(k, bundlePrefix) }

// bundleMembers is a bundle key's member keys, in member order.
func bundleMembers(k string) []string {
	return strings.Split(strings.TrimPrefix(k, bundlePrefix), ",")
}

// sourceRank orders a bundle's sources from most to least trusted, as
// sitemap.Link's are.
var sourceRank = map[string]int{"manual": 0, "port_setting": 1, "lldp": 2, "cdp": 3, "guess": 4}

// flipped is l with its ends the other way round.
func flipped(l MapLink) MapLink {
	l.A, l.B = l.B, l.A
	switch l.RateEnd {
	case "a":
		l.RateEnd = "b"
	case "b":
		l.RateEnd = "a"
	}
	return l
}

// bundleLinks draws two or more cables between the same two devices, each
// with a port at both ends and none a guess, as one bundle: its members
// oriented the same way (the first cable's A device first) and sorted by
// A's port; its key "bundle:" + the member keys. lagOf names the aggregator
// an end's port belongs to (agg 0: none). Every other link is left as it
// is; a bundle takes its first cable's place in the list.
func bundleLinks(links []MapLink, lagOf func(e MapEnd) (name string, agg int)) []MapLink {
	type pair struct{ x, y uuid.UUID }
	pairOf := func(l MapLink) pair {
		if l.B.DeviceID.String() < l.A.DeviceID.String() {
			return pair{l.B.DeviceID, l.A.DeviceID}
		}
		return pair{l.A.DeviceID, l.B.DeviceID}
	}
	bundles := func(l MapLink) bool { return !l.Guess && l.A.IfIndex != nil && l.B.IfIndex != nil }
	groups := map[pair][]MapLink{}
	for _, l := range links {
		if bundles(l) {
			groups[pairOf(l)] = append(groups[pairOf(l)], l)
		}
	}
	out := make([]MapLink, 0, len(links))
	done := map[pair]bool{}
	for _, l := range links {
		p := pairOf(l)
		g := groups[p]
		if !bundles(l) || len(g) < 2 {
			out = append(out, l)
			continue
		}
		if done[p] {
			continue
		}
		done[p] = true
		out = append(out, bundle(g, lagOf))
	}
	return out
}

// bundle is the one link that stands for cables g (two or more, between the
// same two devices, ports known at both ends).
func bundle(g []MapLink, lagOf func(e MapEnd) (string, int)) MapLink {
	aDev := g[0].A.DeviceID
	members := make([]MapLink, len(g))
	for i, l := range g {
		if l.A.DeviceID != aDev {
			l = flipped(l)
		}
		members[i] = l
	}
	sort.SliceStable(members, func(i, j int) bool { return *members[i].A.IfIndex < *members[j].A.IfIndex })

	first := members[0]
	out := MapLink{A: first.A, B: first.B, RateEnd: "b", ThresholdPct: first.ThresholdPct, Stale: true, Members: members}
	keys := make([]string, len(members))
	seen := map[string]bool{}
	for i, m := range members {
		keys[i] = m.Key
		out.SpeedBps += m.SpeedBps
		out.Stale = out.Stale && m.Stale
		if m.LastSeen != nil && (out.LastSeen == nil || m.LastSeen.After(*out.LastSeen)) {
			out.LastSeen = m.LastSeen
		}
		for _, s := range m.Sources {
			if !seen[s] {
				seen[s] = true
				out.Sources = append(out.Sources, s)
			}
		}
	}
	out.Key = bundlePrefix + strings.Join(keys, ",")
	sort.SliceStable(out.Sources, func(i, j int) bool {
		ri, ok := sourceRank[out.Sources[i]]
		if !ok {
			ri = len(sourceRank)
		}
		rj, ok := sourceRank[out.Sources[j]]
		if !ok {
			rj = len(sourceRank)
		}
		return ri < rj
	})

	// An end is a LAG when every cable there is in the same aggregator.
	lagAt := func(end func(MapLink) MapEnd) string {
		name, agg := lagOf(end(members[0]))
		if agg == 0 {
			return ""
		}
		for _, m := range members[1:] {
			if _, a := lagOf(end(m)); a != agg {
				return ""
			}
		}
		return name
	}
	lag := MapLag{A: lagAt(func(m MapLink) MapEnd { return m.A }), B: lagAt(func(m MapLink) MapEnd { return m.B })}
	if lag.A != "" || lag.B != "" {
		out.Lag = &lag
	}
	return out
}
