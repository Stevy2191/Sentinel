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
