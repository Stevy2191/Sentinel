// Package portroles decides which of a device's ports are network links and
// which are its internet (WAN) ports, from what the L2 poll found: LLDP and
// CDP neighbours that are the user's own routers and switches, LAG
// membership, links drawn on the site map, and a router's default routes,
// public addresses and port names. A port a person set is never changed. It
// holds no database code; the service gathers the inputs and writes the
// changes.
package portroles

import (
	"fmt"
	"regexp"
	"sort"
	"time"
)

// Roles and sources, the same values as device_interfaces.role / role_source.
const (
	RoleAccess = "access"
	RoleUplink = "uplink"
	RoleWAN    = "wan"

	SourceDefault = "default"
	SourceUser    = "user"
	SourceAuto    = "auto"

	EvidenceDefaultRoute  = "default_route"
	EvidencePublicAddress = "public_address"
)

// Fresh is how long a neighbour still counts after it was last heard: the
// same week after which the site map drops a link.
const Fresh = 7 * 24 * time.Hour

// Port is one of the device's ports as it stands.
type Port struct {
	IfIndex     int
	Name        string // ifName
	Label       string // faceplate label
	Physical    bool
	Up          bool
	Role        string
	RoleSource  string
	RoleReason  string
	LagIfIndex  int // the aggregator it belongs to, 0 for none
	WANEvidence string
}

// Peer is a neighbour on IfIndex already matched to one of the user's
// routers or switches. PortText is its port as the reason shows it.
type Peer struct {
	IfIndex    int
	Protocol   string // lldp or cdp
	DeviceName string
	PortText   string
	LastSeen   time.Time
}

// Input is everything Decide works from, for one device.
type Input struct {
	IsRouter bool
	Ports    []Port
	Peers    []Peer
	LagNames map[int]string // aggregator ifIndex → its ifName
	LagPeer  map[int]string // aggregator ifIndex → a device reached through any member
	Manual   map[int]bool   // ports at an end of a link drawn on the site map
	Now      time.Time
}

// Change is a port's new role, who set it, and why.
type Change struct {
	IfIndex              int
	Role, Source, Reason string
}

var wanName = regexp.MustCompile(`(?i)^wan\d*$`)

// Decide is the role every port not set by a person should have, as the
// changes from what it has now, in port order.
func Decide(in Input) []Change {
	wan := wanPorts(in)
	peers := map[int]Peer{}
	for _, p := range in.Peers {
		if in.Now.Sub(p.LastSeen) > Fresh {
			continue
		}
		cur, ok := peers[p.IfIndex]
		if !ok || better(p, cur) {
			peers[p.IfIndex] = p
		}
	}
	var out []Change
	for _, p := range in.Ports {
		if p.RoleSource == SourceUser {
			continue
		}
		c := Change{IfIndex: p.IfIndex, Role: RoleAccess, Source: SourceDefault}
		if reason, ok := wan[p.IfIndex]; ok {
			c.Role, c.Source, c.Reason = RoleWAN, SourceAuto, reason
		} else if peer, ok := peers[p.IfIndex]; ok {
			c.Role, c.Source, c.Reason = RoleUplink, SourceAuto, peerReason(peer)
		} else if to := in.LagPeer[p.LagIfIndex]; p.LagIfIndex > 0 && to != "" {
			name := in.LagNames[p.LagIfIndex]
			if name == "" {
				name = "a LAG"
			}
			c.Role, c.Source, c.Reason = RoleUplink, SourceAuto, fmt.Sprintf("Member of %s to %s", name, to)
		} else if in.Manual[p.IfIndex] {
			c.Role, c.Source, c.Reason = RoleUplink, SourceAuto, "Drawn on the site map"
		}
		if c.Role != p.Role || c.Source != p.RoleSource || c.Reason != p.RoleReason {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IfIndex < out[j].IfIndex })
	return out
}

// better prefers LLDP to CDP, then the newer, then the name, so one
// neighbour gives the reason whatever order they come in.
func better(a, b Peer) bool {
	if a.Protocol != b.Protocol {
		return a.Protocol == "lldp"
	}
	if !a.LastSeen.Equal(b.LastSeen) {
		return a.LastSeen.After(b.LastSeen)
	}
	return a.DeviceName < b.DeviceName
}

func peerReason(p Peer) string {
	if p.Protocol == "cdp" {
		return fmt.Sprintf("Found by CDP: %s %s", p.DeviceName, p.PortText)
	}
	return fmt.Sprintf("Found by LLDP: %s port %s", p.DeviceName, p.PortText)
}

// wanPorts is a router's internet ports by the first rule that finds any:
// the default route's port, then a port with a public address, then a
// port named wan, wan1, …; only physical ports, the last two only when up.
func wanPorts(in Input) map[int]string {
	out := map[int]string{}
	if !in.IsRouter {
		return out
	}
	rules := []struct {
		reason func(Port) string
		match  func(Port) bool
	}{
		{func(Port) string { return "The default route goes out this port" }, func(p Port) bool { return p.WANEvidence == EvidenceDefaultRoute }},
		{func(Port) string { return "Has a public address" }, func(p Port) bool { return p.Up && p.WANEvidence == EvidencePublicAddress }},
		{func(p Port) string { return "Named " + p.Name }, func(p Port) bool { return p.Up && wanName.MatchString(p.Name) }},
	}
	for _, r := range rules {
		for _, p := range in.Ports {
			if p.Physical && r.match(p) {
				out[p.IfIndex] = r.reason(p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return out
}
