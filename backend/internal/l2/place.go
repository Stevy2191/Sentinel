package l2

import (
	"time"

	"github.com/google/uuid"
)

const (
	// MaxEdgeMACs is how many MACs a port with nothing known on its far end
	// may show before it is taken for a link to an unmanaged switch.
	MaxEdgeMACs = 8
	// RetentionDays is how long sightings, neighbours and ARP rows are kept.
	RetentionDays = 30
	// freshWindow: a MAC is placed by its sightings from about the latest
	// round of polls (every switch polls it once in 5 minutes), not by its
	// 30 days of history.
	freshWindow = 10 * time.Minute
	// countWindow: a port's MAC count covers a day, so an unmanaged switch's
	// group stays a group overnight while its PCs sleep and age out.
	countWindow = 24 * time.Hour
)

// PortKey is one switch port, by the device and its ifIndex.
type PortKey struct {
	Device  uuid.UUID
	IfIndex int
}

// PortInfo is what Sentinel knows about a port's far end. LinkedTo: the
// port has a port link to another device. MonitoredNeighbor: its LLDP
// chassis or sys name matches a monitored device.
type PortInfo struct {
	Neighbor          *Neighbor
	LinkedTo          bool
	Uplink            bool
	MonitoredAP       bool
	MonitoredNeighbor bool
	// SharedCDP: CDP from two or more devices and no LLDP neighbour, so a
	// switch that doesn't speak either is in between (Neighbor is nil).
	SharedCDP bool
	// LAGLink: a LAG one of whose members is a link.
	LAGLink bool
}

// Sighting is a MAC learned on a port, as of its last poll.
type Sighting struct {
	Port     PortKey
	VLAN     int
	MAC      string
	LastSeen time.Time
}

// PortClass is what a port is, for placing the MACs learned on it.
type PortClass int

const (
	PortEdge   PortClass = iota // an endpoint is plugged in here
	PortAP                      // an access point: its MACs are its wireless clients
	PortLink                    // another switch or router
	PortBehind                  // busy, with nothing known beyond: an unmanaged switch
)

func (c PortClass) String() string {
	return [...]string{"edge", "ap", "link", "behind"}[c]
}

// ClassifyPort classes a port: an AP first, then a link (a port link, the
// uplink role, a monitored neighbour, a LAG with a link member, an LLDP or
// CDP bridge or router), then behind when CDP from several devices crossed
// it or it has more than MaxEdgeMACs MACs with no neighbour, else edge. A
// port whose neighbour is neither bridge nor router is a host however many
// MACs it shows (containers, VMs); so is an IP phone, which announces
// bridge for its PC pass-through port.
func ClassifyPort(info PortInfo, macsOnPort int) PortClass {
	n := info.Neighbor
	switch {
	case info.MonitoredAP || (n != nil && n.Caps.AP()):
		return PortAP
	case info.LinkedTo || info.Uplink || info.MonitoredNeighbor || info.LAGLink:
		return PortLink
	case info.SharedCDP:
		return PortBehind
	case n != nil && n.Caps.Phone() && !n.Caps.Router():
		return PortEdge
	case n != nil && (n.Caps.Bridge() || n.Caps.Router()):
		return PortLink
	case n == nil && macsOnPort > MaxEdgeMACs:
		return PortBehind
	default:
		return PortEdge
	}
}

// Placement is where one MAC is. How is "wired" (on an edge port),
// "wireless" (through the AP on Port, named Via), "behind" (only seen on
// link ports; Port is the one that saw the fewest MACs) or "router" (only
// in an ARP table, added by the caller; Port nil).
type Placement struct {
	MAC      string
	Port     *PortKey
	VLAN     int
	Via      string
	How      string
	LastSeen time.Time
}

// Place works out where each MAC in sightings is, from its sightings in
// the latest round of polls: an edge port (fewest MACs, then the most
// recent), else an AP port, else the link port with the fewest MACs. ports holds what is
// known about each port's far end (ports not in it have nothing known);
// apNames names the AP on each AP port.
func Place(sightings []Sighting, ports map[PortKey]PortInfo, apNames map[PortKey]string) map[string]Placement {
	// A port's MAC count covers the last day of its switch's polls, not the
	// 30 days of history.
	latest := map[uuid.UUID]time.Time{}
	newest := map[string]time.Time{}
	for _, s := range sightings {
		if s.LastSeen.After(latest[s.Port.Device]) {
			latest[s.Port.Device] = s.LastSeen
		}
		if s.LastSeen.After(newest[s.MAC]) {
			newest[s.MAC] = s.LastSeen
		}
	}
	count := map[PortKey]map[string]bool{}
	for _, s := range sightings {
		if s.LastSeen.Before(latest[s.Port.Device].Add(-countWindow)) {
			continue
		}
		if count[s.Port] == nil {
			count[s.Port] = map[string]bool{}
		}
		count[s.Port][s.MAC] = true
	}
	class := map[PortKey]PortClass{}
	classOf := func(p PortKey) PortClass {
		c, ok := class[p]
		if !ok {
			c = ClassifyPort(ports[p], len(count[p]))
			class[p] = c
		}
		return c
	}

	type best struct {
		rank int // 0 wired, 1 wireless, 2 behind
		s    Sighting
		macs int
	}
	byMAC := map[string]best{}
	for _, s := range sightings {
		// Only the MAC's latest round of polls places it: an old port it
		// has since left doesn't count.
		if s.LastSeen.Before(newest[s.MAC].Add(-freshWindow)) {
			continue
		}
		c := classOf(s.Port)
		rank := 2
		switch {
		case c == PortEdge:
			rank = 0
		case c == PortAP:
			rank = 1
			// The AP's own MAC is the AP, plugged into this port.
			if n := ports[s.Port].Neighbor; n != nil && n.ChassisID == s.MAC {
				rank = 0
			}
		}
		cand := best{rank: rank, s: s, macs: len(count[s.Port])}
		cur, ok := byMAC[s.MAC]
		switch {
		case !ok, cand.rank < cur.rank:
		case cand.rank > cur.rank:
			continue
		// Two edge ports in the same polls (a switch with no LLDP on an
		// unrecognised uplink) or two links: the port with fewer MACs is
		// closer to the device.
		case rank != 1 && cand.macs != cur.macs:
			if cand.macs > cur.macs {
				continue
			}
		case !s.LastSeen.After(cur.s.LastSeen):
			continue
		}
		byMAC[s.MAC] = cand
	}

	out := make(map[string]Placement, len(byMAC))
	for m, b := range byMAC {
		port := b.s.Port
		p := Placement{MAC: m, Port: &port, VLAN: b.s.VLAN, LastSeen: b.s.LastSeen}
		switch b.rank {
		case 0:
			p.How = "wired"
		case 1:
			p.How = "wireless"
			p.Via = apNames[port]
			if p.Via == "" && ports[port].Neighbor != nil {
				p.Via = ports[port].Neighbor.SysName
			}
		default:
			p.How = "behind"
		}
		out[m] = p
	}
	return out
}
