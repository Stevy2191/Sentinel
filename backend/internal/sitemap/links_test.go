package sitemap

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

var now = time.Date(2026, 10, 19, 15, 0, 0, 0, time.UTC)

// homeNeighbor is QuantumLink's LLDP entry for port 51, as the sandbox has it.
func homeNeighbor(seen time.Time) Neighbor {
	return Neighbor{DeviceID: swID, IfIndex: 51, ChassisID: "e4:38:83:20:f5:c6", SysName: "Quantum-Gate",
		PortID: "e4:38:83:20:f5:d0", PortDescr: "eth10", LastSeen: seen}
}

func homeInput() Input {
	return Input{Devices: homeDevices(), Ports: homePorts(), Neighbors: []Neighbor{homeNeighbor(now.Add(-time.Minute))}, Now: now}
}

func sources(ss ...Source) []Source { return ss }

func TestFindLLDPOneLink(t *testing.T) {
	links := Find(homeInput())
	if len(links) != 1 {
		t.Fatalf("links = %+v, want 1", links)
	}
	l := links[0]
	ends := map[uuid.UUID]int{l.A.DeviceID: l.A.IfIndex, l.B.DeviceID: l.B.IfIndex}
	if ends[swID] != 51 || ends[gwID] != 4 {
		t.Errorf("ends = %+v, want QuantumLink:51 and Quantum-Gate:4", ends)
	}
	if !reflect.DeepEqual(l.Sources, sources(SourceLLDP)) || l.Guess || l.Stale || l.Hidden {
		t.Errorf("link = %+v", l)
	}
	if l.Key != KeyOf(End{swID, 51}, End{gwID, 4}) || KeyOf(End{swID, 51}, End{gwID, 4}) != KeyOf(End{gwID, 4}, End{swID, 51}) {
		t.Errorf("key %q isn't the ends' canonical key", l.Key)
	}
}

func TestFindBothEndsOneLink(t *testing.T) {
	in := homeInput()
	in.Neighbors = append(in.Neighbors, Neighbor{DeviceID: gwID, IfIndex: 4, ChassisID: "f4:e2:c6:58:0c:b7", SysName: "QuantumLink",
		PortID: "Port 51", LastSeen: now.Add(-2 * time.Minute)})
	if links := Find(in); len(links) != 1 {
		t.Fatalf("links = %+v, want 1", links)
	}
}

func TestFindIgnoresNonMapNeighbours(t *testing.T) {
	in := homeInput()
	in.Neighbors = append(in.Neighbors,
		Neighbor{DeviceID: swID, IfIndex: 4, ChassisID: "1c:0b:8b:76:1a:52", SysName: "MBU7IW", PortDescr: "eth0", LastSeen: now},
		Neighbor{DeviceID: swID, IfIndex: 48, ChassisID: "84:78:48:fe:d0:e8", SysName: "KitchenAP", PortDescr: "eth0", LastSeen: now})
	if links := Find(in); len(links) != 1 {
		t.Fatalf("links = %+v, want only the router link", links)
	}
}

// Review Focus 2: a LAG or a redundant pair is two cables, not one.
func TestFindTwoCablesStayTwo(t *testing.T) {
	in := homeInput()
	in.PortLinks = []PortLink{{From: End{swID, 52}, To: End{gwID, 16}}}
	if links := Find(in); len(links) != 2 {
		t.Fatalf("links = %+v, want 2", links)
	}
}

// Review Focus 3: LLDP that knows only the devices merges into a port setting
// between the same two devices.
func TestFindDeviceLevelMergesIntoPortLevel(t *testing.T) {
	in := homeInput()
	in.Neighbors = []Neighbor{{DeviceID: swID, IfIndex: 0, ChassisID: "e4:38:83:20:f5:c6", LastSeen: now}}
	in.PortLinks = []PortLink{{From: End{swID, 51}, To: End{gwID, 4}}}
	links := Find(in)
	if len(links) != 1 {
		t.Fatalf("links = %+v, want 1", links)
	}
	if !reflect.DeepEqual(links[0].Sources, sources(SourcePortSetting, SourceLLDP)) || links[0].Key != KeyOf(End{swID, 51}, End{gwID, 4}) {
		t.Errorf("link = %+v, want the port setting's ends with both sources", links[0])
	}
}

func TestFindGuessSuppressedWhenLinked(t *testing.T) {
	acc := uuid.MustParse("00000000-0000-0000-0000-0000000000d4")
	in := homeInput()
	in.Devices = append(in.Devices, Device{ID: acc, Name: "Office-1"})
	in.Guesses = []Guess{
		{Device: gwID, At: End{swID, 51}, LastSeen: now}, // already linked by LLDP
		{Device: acc, At: End{swID, 49}, LastSeen: now},  // a switch that sends no LLDP
	}
	links := Find(in)
	if len(links) != 2 {
		t.Fatalf("links = %+v, want the LLDP link and one guess", links)
	}
	var g *Link
	for i := range links {
		if links[i].Guess {
			g = &links[i]
		}
	}
	if g == nil || g.Key != KeyOf(End{swID, 49}, End{acc, 0}) || !reflect.DeepEqual(g.Sources, sources(SourceGuess)) {
		t.Fatalf("guess = %+v, want QuantumLink:49 to Office-1", g)
	}
}

func TestFindManualAndHidden(t *testing.T) {
	acc := uuid.MustParse("00000000-0000-0000-0000-0000000000d4")
	manual := uuid.New()
	in := homeInput()
	in.Devices = append(in.Devices, Device{ID: acc, Name: "Office-1"})
	in.Overrides = []Override{
		{ID: manual, Kind: KindManual, A: End{swID, 49}, B: End{acc, 49}},
		{ID: uuid.New(), Kind: KindHidden, A: End{gwID, 4}, B: End{swID, 51}},
	}
	links := Find(in)
	byKey := map[string]Link{}
	for _, l := range links {
		byKey[l.Key] = l
	}
	m := byKey[KeyOf(End{swID, 49}, End{acc, 49})]
	if m.ManualID == nil || *m.ManualID != manual || !reflect.DeepEqual(m.Sources, sources(SourceManual)) {
		t.Errorf("manual link = %+v", m)
	}
	if h := byKey[KeyOf(End{swID, 51}, End{gwID, 4})]; !h.Hidden {
		t.Errorf("hidden override not applied: %+v", h)
	}
}

func TestFindStaleAndDropped(t *testing.T) {
	in := homeInput()
	in.Neighbors = []Neighbor{homeNeighbor(now.Add(-25 * time.Hour))}
	links := Find(in)
	if len(links) != 1 || !links[0].Stale || links[0].LastSeen == nil {
		t.Fatalf("25 h old = %+v, want one stale link", links)
	}
	in.Neighbors = []Neighbor{homeNeighbor(now.Add(-8 * 24 * time.Hour))}
	if links := Find(in); len(links) != 0 {
		t.Fatalf("8 days old = %+v, want dropped", links)
	}
	in.Neighbors = nil
	in.PortLinks = []PortLink{{From: End{swID, 51}, To: End{gwID, 4}}}
	if links := Find(in); len(links) != 1 || links[0].Stale {
		t.Fatalf("port setting = %+v, want one live link", links)
	}
}

// A CDP neighbour that names no MAC or name Sentinel knows still matches by
// the address it sent.
func TestMatchNeighborByAddress(t *testing.T) {
	devs := homeDevices()
	devs[1].Host = " 10.20.0.5 "
	n := Neighbor{DeviceID: gwID, IfIndex: 4, ChassisID: "LAB-C3850.corp.example", SysName: "LAB-C3850", Protocol: "cdp", Address: "10.20.0.5"}
	if got, ok := MatchNeighborDevice(n, devs, homePorts()); !ok || got != swID {
		t.Errorf("matched %v %v, want QuantumLink by address", got, ok)
	}
	n.Address = ""
	if _, ok := MatchNeighborDevice(n, devs, homePorts()); ok {
		t.Error("matched with no address, MAC or name")
	}
}

func TestFindCDPSource(t *testing.T) {
	in := homeInput()
	n := homeNeighbor(now.Add(-time.Minute))
	n.Protocol = "cdp"
	n.PortID, n.PortDescr = "eth10", ""
	in.Neighbors = []Neighbor{n}
	links := Find(in)
	if len(links) != 1 || !reflect.DeepEqual(links[0].Sources, sources(SourceCDP)) {
		t.Fatalf("links = %+v, want one found by CDP", links)
	}
	if ends := map[uuid.UUID]int{links[0].A.DeviceID: links[0].A.IfIndex, links[0].B.DeviceID: links[0].B.IfIndex}; ends[gwID] != 4 {
		t.Errorf("ends = %+v, want Quantum-Gate:4 by the CDP port name", ends)
	}
	// Both protocols on one cable: one link, both sources.
	in.Neighbors = append(in.Neighbors, homeNeighbor(now.Add(-time.Minute)))
	if links := Find(in); len(links) != 1 || !reflect.DeepEqual(links[0].Sources, sources(SourceCDP, SourceLLDP)) {
		t.Fatalf("links = %+v, want one found by CDP and LLDP", links)
	}
}
