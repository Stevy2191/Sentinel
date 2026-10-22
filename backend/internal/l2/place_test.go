package l2

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	swA = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	swB = uuid.MustParse("00000000-0000-0000-0000-00000000000b")
	t0  = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
)

func TestClassifyPort(t *testing.T) {
	cases := []struct {
		name string
		info PortInfo
		macs int
		want PortClass
	}{
		{"LLDP AP", PortInfo{Neighbor: &Neighbor{Caps: 0x10 | 0x20}}, 20, PortAP},
		{"monitored AP", PortInfo{MonitoredAP: true}, 20, PortAP},
		{"LLDP bridge", PortInfo{Neighbor: &Neighbor{Caps: 0x20}}, 1, PortLink},
		{"LLDP router", PortInfo{Neighbor: &Neighbor{Caps: 0x08}}, 1, PortLink},
		{"port link", PortInfo{LinkedTo: true}, 1, PortLink},
		{"uplink role", PortInfo{Uplink: true}, 1, PortLink},
		{"monitored neighbour", PortInfo{MonitoredNeighbor: true}, 1, PortLink},
		{"9 MACs, nothing known", PortInfo{}, 9, PortBehind},
		{"9 MACs, LLDP host", PortInfo{Neighbor: &Neighbor{Caps: 0x80}}, 9, PortEdge},
		{"3 MACs, nothing known", PortInfo{}, 3, PortEdge},
		{"8 MACs is still edge", PortInfo{}, MaxEdgeMACs, PortEdge},
		{"IP phone (bridge+telephone)", PortInfo{Neighbor: &Neighbor{Caps: 0x24}}, 2, PortEdge},
		{"telephone and router", PortInfo{Neighbor: &Neighbor{Caps: 0x24 | 0x08}}, 2, PortLink},
	}
	for _, c := range cases {
		if got := ClassifyPort(c.info, c.macs); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// fill adds n other MACs to a port, so it counts as busy.
func fill(s []Sighting, p PortKey, n int) []Sighting {
	for i := 0; i < n; i++ {
		s = append(s, Sighting{Port: p, MAC: fmt.Sprintf("02:00:00:%02x:%02x:%02x", p.IfIndex, i/256, i%256), LastSeen: t0})
	}
	return s
}

const mac = "e4:38:83:20:f5:cf"

// CDP from several devices on one port crossed a switch that doesn't speak
// it: never an edge port, however few MACs.
func TestClassifyPortSharedCDP(t *testing.T) {
	if got := ClassifyPort(PortInfo{SharedCDP: true}, 3); got != PortBehind {
		t.Errorf("shared CDP, 3 MACs: got %v, want behind", got)
	}
	if got := ClassifyPort(PortInfo{SharedCDP: true, MonitoredNeighbor: true}, 3); got != PortLink {
		t.Errorf("shared CDP with a monitored switch: got %v, want link", got)
	}
}

// A Port-channel with a member that is a link is a link.
func TestClassifyPortLAGLink(t *testing.T) {
	if got := ClassifyPort(PortInfo{LAGLink: true}, 2); got != PortLink {
		t.Errorf("got %v, want link", got)
	}
}

func TestPlaceEdgeBeatsLink(t *testing.T) {
	a5, b49 := PortKey{swA, 5}, PortKey{swB, 49}
	s := []Sighting{{Port: b49, MAC: mac, LastSeen: t0.Add(time.Minute)}, {Port: a5, MAC: mac, VLAN: 10, LastSeen: t0}}
	got := Place(s, map[PortKey]PortInfo{b49: {Uplink: true}}, nil)[mac]
	if got.How != "wired" || got.Port == nil || *got.Port != a5 || got.VLAN != 10 {
		t.Errorf("%+v", got)
	}
}

func TestPlaceWireless(t *testing.T) {
	a7 := PortKey{swA, 7}
	ports := map[PortKey]PortInfo{a7: {Neighbor: &Neighbor{SysName: "KitchenAP", Caps: 0x10}}}
	got := Place([]Sighting{{Port: a7, MAC: mac, LastSeen: t0}}, ports, map[PortKey]string{a7: "KitchenAP"})[mac]
	if got.How != "wireless" || got.Via != "KitchenAP" || got.Port == nil || *got.Port != a7 {
		t.Errorf("%+v", got)
	}
}

func TestPlaceAPOwnMACIsWired(t *testing.T) {
	a7 := PortKey{swA, 7}
	ports := map[PortKey]PortInfo{a7: {Neighbor: &Neighbor{ChassisID: mac, SysName: "KitchenAP", Caps: 0x10}}}
	got := Place([]Sighting{{Port: a7, MAC: mac, LastSeen: t0}}, ports, map[PortKey]string{a7: "KitchenAP"})[mac]
	if got.How != "wired" {
		t.Errorf("the AP itself: %+v", got)
	}
}

func TestPlaceBehindFewest(t *testing.T) {
	a1, b2 := PortKey{swA, 1}, PortKey{swB, 2}
	s := []Sighting{{Port: a1, MAC: mac, LastSeen: t0}, {Port: b2, MAC: mac, LastSeen: t0}}
	s = fill(s, a1, 39)
	s = fill(s, b2, 11)
	got := Place(s, nil, nil)[mac]
	if got.How != "behind" || got.Port == nil || *got.Port != b2 {
		t.Errorf("%+v", got)
	}
}

func TestPlaceMostRecentEdge(t *testing.T) {
	a5, b6 := PortKey{swA, 5}, PortKey{swB, 6}
	s := []Sighting{{Port: a5, MAC: mac, LastSeen: t0}, {Port: b6, MAC: mac, LastSeen: t0.Add(time.Hour)}}
	got := Place(s, nil, nil)[mac]
	if got.How != "wired" || *got.Port != b6 || !got.LastSeen.Equal(t0.Add(time.Hour)) {
		t.Errorf("%+v", got)
	}
}

func TestPlaceLLDPHostIsEdge(t *testing.T) {
	a34 := PortKey{swA, 34}
	ports := map[PortKey]PortInfo{a34: {Neighbor: &Neighbor{SysName: "TS5410R81E", Caps: 0x80}}}
	s := fill(nil, a34, 15)
	placed := Place(s, ports, nil)
	if len(placed) != 15 {
		t.Fatalf("placed %d", len(placed))
	}
	for m, p := range placed {
		if p.How != "wired" || *p.Port != a34 {
			t.Errorf("%s: %+v", m, p)
		}
	}
}

// Review: a MAC is placed by its latest polls, not 30 days of history. A PC
// that moved from wall port 5 to behind a desk switch on port 12 is behind
// port 12 now.
func TestPlaceLatestPollWins(t *testing.T) {
	a5, a12 := PortKey{swA, 5}, PortKey{swA, 12}
	s := []Sighting{{Port: a5, MAC: mac, LastSeen: t0.Add(-72 * time.Hour)}, {Port: a12, MAC: mac, LastSeen: t0}}
	s = fill(s, a12, 11)
	got := Place(s, nil, nil)[mac]
	if got.How != "behind" || *got.Port != a12 {
		t.Errorf("%+v", got)
	}
}

// Review: an unmanaged switch's group stays a group overnight, when most of
// its PCs sleep and age out of the switch's table.
func TestPlaceBehindSurvivesAging(t *testing.T) {
	a12, a3 := PortKey{swA, 12}, PortKey{swA, 3}
	var s []Sighting
	for i := 0; i < 12; i++ {
		seen := t0
		if i >= 3 {
			seen = t0.Add(-8 * time.Hour)
		}
		s = append(s, Sighting{Port: a12, MAC: fmt.Sprintf("02:00:00:00:12:%02x", i), LastSeen: seen})
	}
	s = append(s, Sighting{Port: a3, MAC: mac, LastSeen: t0})
	placed := Place(s, nil, nil)
	for m, p := range placed {
		if m != mac && p.How != "behind" {
			t.Errorf("%s: %+v", m, p)
		}
	}
	if placed[mac].How != "wired" {
		t.Errorf("edge device: %+v", placed[mac])
	}
}

// Review: an IP phone advertises Bridge+Telephone for its PC pass-through
// port; the phone and the PC behind it are both wired on that port.
func TestPlacePhonePassThrough(t *testing.T) {
	a14 := PortKey{swA, 14}
	const phone, pc = "80:5e:c0:aa:bb:cc", "00:11:22:33:44:55"
	ports := map[PortKey]PortInfo{a14: {Neighbor: &Neighbor{ChassisID: phone, Caps: 0x24}}}
	placed := Place([]Sighting{{Port: a14, MAC: phone, LastSeen: t0}, {Port: a14, MAC: pc, LastSeen: t0}}, ports, nil)
	for _, m := range []string{phone, pc} {
		if placed[m].How != "wired" || *placed[m].Port != a14 {
			t.Errorf("%s: %+v", m, placed[m])
		}
	}
}

// Review: seen on two edge ports in the same polls (a switch with no LLDP
// uplinked to an unrecognised port), the port with fewer MACs is the
// closer one.
func TestPlaceFewestEdgeWins(t *testing.T) {
	a10, b3 := PortKey{swA, 10}, PortKey{swB, 3}
	s := []Sighting{{Port: a10, MAC: mac, LastSeen: t0.Add(time.Minute)}, {Port: b3, MAC: mac, LastSeen: t0}}
	s = fill(s, a10, 4)
	if got := Place(s, nil, nil)[mac]; *got.Port != b3 {
		t.Errorf("%+v", got)
	}
}
