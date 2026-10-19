// Package sitemap works out a site map's links from what Sentinel knows:
// links people drew, ports' "Network link" settings, LLDP neighbours, and
// guesses from where piece B placed a switch's MAC. It holds no database
// code; the service gathers the inputs.
package sitemap

import (
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// Device is a router or switch on the map.
type Device struct {
	ID                      uuid.UUID
	Name, SysName, SysDescr string
}

// Port is one of a map device's interfaces.
type Port struct {
	DeviceID           uuid.UUID
	IfIndex            int
	Name, Descr, Alias string
	MAC                string
	IfType             int
	ConnectorPresent   *bool
}

// End is one end of a link: a device's port, or the device itself (IfIndex 0)
// when the port isn't known.
type End struct {
	DeviceID uuid.UUID
	IfIndex  int
}

// Neighbor is an l2.neighbors row: DeviceID's port IfIndex sees ChassisID.
type Neighbor struct {
	DeviceID                              uuid.UUID
	IfIndex                               int
	ChassisID, SysName, PortID, PortDescr string
	LastSeen                              time.Time
}

// PortLink is a port's "Network link" setting.
type PortLink struct{ From, To End }

// Guess is Device's MAC placed on port At by piece B.
type Guess struct {
	Device   uuid.UUID
	At       End
	LastSeen time.Time
}

// Override kinds, the same values as site_map_links.kind.
const (
	KindManual = "manual"
	KindHidden = "hidden"
)

// Override is a site_map_links row: a link drawn by hand, or a found link
// someone hid.
type Override struct {
	ID   uuid.UUID
	Kind string
	A, B End
}

// Source is where a link was learned.
type Source string

const (
	SourceManual      Source = "manual"
	SourcePortSetting Source = "port_setting"
	SourceLLDP        Source = "lldp"
	SourceGuess       Source = "guess"
)

// Link is one cable on the map. Sources run from most to least trusted.
// Guess: only a MAC-table guess says so. Stale: only LLDP or a guess says
// so, and not for 24 hours.
type Link struct {
	Key      string
	A, B     End
	Sources  []Source
	Guess    bool
	ManualID *uuid.UUID
	LastSeen *time.Time
	Stale    bool
	Hidden   bool
}

// Input is everything Find works from.
type Input struct {
	Devices   []Device
	Ports     []Port
	PortLinks []PortLink
	Neighbors []Neighbor
	Guesses   []Guess
	Overrides []Override
	Now       time.Time
}

const (
	staleAfter = 24 * time.Hour
	dropAfter  = 7 * 24 * time.Hour
)

// KeyOf is a link's identity: its ends, ordered so either direction gives
// the same key.
func KeyOf(a, b End) string {
	if b.DeviceID.String() < a.DeviceID.String() || (a.DeviceID == b.DeviceID && b.IfIndex < a.IfIndex) {
		a, b = b, a
	}
	return fmt.Sprintf("%s:%d|%s:%d", a.DeviceID, a.IfIndex, b.DeviceID, b.IfIndex)
}

// fits reports whether a link with ends (a, b) can be the same cable as
// (x, y): the same two devices, and every port that both know agrees.
func fits(a, b, x, y End) bool {
	if a.DeviceID == y.DeviceID && b.DeviceID == x.DeviceID {
		x, y = y, x
	}
	if a.DeviceID != x.DeviceID || b.DeviceID != y.DeviceID {
		return false
	}
	agree := func(p, q int) bool { return p == 0 || q == 0 || p == q }
	return agree(a.IfIndex, x.IfIndex) && agree(b.IfIndex, y.IfIndex)
}

// fill copies ports known by (x, y) into ends of (a, b) that don't know theirs.
func fill(a, b *End, x, y End) {
	if a.DeviceID == y.DeviceID {
		x, y = y, x
	}
	if a.IfIndex == 0 {
		a.IfIndex = x.IfIndex
	}
	if b.IfIndex == 0 {
		b.IfIndex = y.IfIndex
	}
}

// Find works out the map's links. A link found two ways is one link; a
// device-level one merges into a port-level one between the same devices.
// A guess is left out for any device something firmer already links. Links
// only LLDP or a guess vouch for go stale after a day and are dropped after
// a week. Hidden links are kept, marked Hidden.
func Find(in Input) []Link {
	onMap := map[uuid.UUID]Device{}
	for _, d := range in.Devices {
		onMap[d.ID] = d
	}
	var links []*Link
	add := func(a, b End, src Source, seen *time.Time, manual *uuid.UUID) {
		if a.DeviceID == b.DeviceID {
			return
		}
		if _, ok := onMap[a.DeviceID]; !ok {
			return
		}
		if _, ok := onMap[b.DeviceID]; !ok {
			return
		}
		for _, l := range links {
			if !fits(l.A, l.B, a, b) {
				continue
			}
			fill(&l.A, &l.B, a, b)
			if !hasSource(l.Sources, src) {
				l.Sources = append(l.Sources, src)
			}
			if seen != nil && (l.LastSeen == nil || seen.After(*l.LastSeen)) {
				l.LastSeen = seen
			}
			if manual != nil && l.ManualID == nil {
				l.ManualID = manual
			}
			return
		}
		links = append(links, &Link{A: a, B: b, Sources: []Source{src}, LastSeen: seen, ManualID: manual})
	}

	for _, o := range in.Overrides {
		if o.Kind == KindManual {
			id := o.ID
			add(o.A, o.B, SourceManual, nil, &id)
		}
	}
	for _, p := range in.PortLinks {
		add(p.From, p.To, SourcePortSetting, nil, nil)
	}
	for _, n := range in.Neighbors {
		if _, ok := onMap[n.DeviceID]; !ok {
			continue
		}
		dev, ok := MatchNeighborDevice(n, in.Devices, in.Ports)
		if !ok {
			continue
		}
		port := MatchNeighborPort(n, dev, in.Ports, portmon.NumberingFor(onMap[dev].SysDescr))
		seen := n.LastSeen
		add(End{n.DeviceID, n.IfIndex}, End{dev, port}, SourceLLDP, &seen, nil)
	}
	linked := map[uuid.UUID]bool{}
	for _, l := range links {
		linked[l.A.DeviceID], linked[l.B.DeviceID] = true, true
	}
	for _, g := range in.Guesses {
		if linked[g.Device] || g.Device == g.At.DeviceID {
			continue
		}
		seen := g.LastSeen
		add(g.At, End{g.Device, 0}, SourceGuess, &seen, nil)
	}

	hidden := map[string]bool{}
	for _, o := range in.Overrides {
		if o.Kind == KindHidden {
			hidden[KeyOf(o.A, o.B)] = true
		}
	}
	out := make([]Link, 0, len(links))
	for _, l := range links {
		firm := hasSource(l.Sources, SourceManual) || hasSource(l.Sources, SourcePortSetting)
		if !firm && l.LastSeen != nil {
			age := in.Now.Sub(*l.LastSeen)
			if age > dropAfter {
				continue
			}
			l.Stale = age > staleAfter
		}
		l.Key = KeyOf(l.A, l.B)
		l.Guess = len(l.Sources) == 1 && l.Sources[0] == SourceGuess
		l.Hidden = hidden[l.Key]
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func hasSource(ss []Source, s Source) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
