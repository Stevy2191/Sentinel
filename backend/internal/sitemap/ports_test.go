package sitemap

import (
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// The owner's home network, as the sandbox records it.
var (
	gwID = uuid.MustParse("00000000-0000-0000-0000-0000000000a1") // Quantum-Gate, UDM-SE
	swID = uuid.MustParse("00000000-0000-0000-0000-0000000000b2") // QuantumLink, USW-Pro-48-PoE
	tsID = uuid.MustParse("00000000-0000-0000-0000-0000000000c3") // TS5410R81E (not a switch)

	gwDescr = "Ubiquiti UniFi UDM-SE 5.1.33 Linux 5.4.213 al324"
	swDescr = "USW-Pro-48-PoE, 7.2.123.16565, Linux 3.6.5"
)

func homeDevices() []Device {
	return []Device{
		{ID: gwID, Name: "Quantum-Gate", SysName: "Quantum-Gate", SysDescr: gwDescr},
		{ID: swID, Name: "QuantumLink", SysName: "QuantumLink", SysDescr: swDescr},
	}
}

func homePorts() []Port {
	ps := []Port{
		{DeviceID: gwID, IfIndex: 4, Name: "eth10", MAC: "e4:38:83:20:f5:d0", IfType: 6},
		{DeviceID: gwID, IfIndex: 24, Name: "eth10.10", MAC: "e4:38:83:20:f5:d0", IfType: 135},
		{DeviceID: gwID, IfIndex: 16, Name: "eth0", MAC: "e4:38:83:20:f5:c6", IfType: 6},
	}
	for _, n := range []int{4, 5, 48, 49, 50, 51, 52} {
		ps = append(ps, Port{DeviceID: swID, IfIndex: n, Name: "0/" + strconv.Itoa(n), MAC: "f4:e2:c6:58:0c:b8", IfType: 6})
	}
	return ps
}

func TestMatchPortByName(t *testing.T) {
	n := Neighbor{DeviceID: swID, IfIndex: 51, ChassisID: "e4:38:83:20:f5:c6", SysName: "Quantum-Gate", PortDescr: "eth10"}
	if got := MatchNeighborPort(n, gwID, homePorts(), portmon.NumberingFor(gwDescr)); got != 4 {
		t.Fatalf("port = %d, want 4 (eth10)", got)
	}
}

func TestMatchPortNumberN(t *testing.T) {
	n := Neighbor{DeviceID: tsID, IfIndex: 4, ChassisID: "f4:e2:c6:58:0c:b7", SysName: "QuantumLink", PortID: "Port 5", PortDescr: "TS5410R"}
	if got := MatchNeighborPort(n, swID, homePorts(), portmon.NumberingFor(swDescr)); got != 5 {
		t.Fatalf("port = %d, want 5", got)
	}
}

func TestMatchPortByUniqueMAC(t *testing.T) {
	// eth10.10 shares eth10's MAC but is a VLAN sub-interface, not a port.
	n := Neighbor{DeviceID: swID, IfIndex: 51, ChassisID: "e4:38:83:20:f5:c6", PortID: "e4:38:83:20:f5:d0"}
	if got := MatchNeighborPort(n, gwID, homePorts(), portmon.NumberingFor(gwDescr)); got != 4 {
		t.Fatalf("port = %d, want 4", got)
	}
}

func TestMatchPortSharedMACUnknown(t *testing.T) {
	// QuantumLink gives every port the same MAC.
	n := Neighbor{DeviceID: gwID, IfIndex: 4, ChassisID: "f4:e2:c6:58:0c:b7", PortID: "f4:e2:c6:58:0c:b8"}
	if got := MatchNeighborPort(n, swID, homePorts(), portmon.NumberingFor(swDescr)); got != 0 {
		t.Fatalf("port = %d, want 0 (unknown)", got)
	}
}

func TestMatchDeviceBySysName(t *testing.T) {
	n := Neighbor{DeviceID: swID, IfIndex: 51, ChassisID: "00:00:00:00:00:01", SysName: "quantum-gate "}
	id, ok := MatchNeighborDevice(n, homeDevices(), homePorts())
	if !ok || id != gwID {
		t.Fatalf("device = %v %v, want Quantum-Gate", id, ok)
	}
	n.SysName = "MBU7IW"
	if _, ok := MatchNeighborDevice(n, homeDevices(), homePorts()); ok {
		t.Fatal("a neighbour that isn't a map device matched")
	}
}

var (
	xID = uuid.MustParse("00000000-0000-0000-0000-0000000000d4")
	yID = uuid.MustParse("00000000-0000-0000-0000-0000000000e5")
	zID = uuid.MustParse("00000000-0000-0000-0000-0000000000f6")
)

// CDP sends a Cisco's name without its domain; Sentinel may know it with
// one, and an LLDP neighbour the other way round.
func TestMatchDeviceByNameWithoutDomain(t *testing.T) {
	devices := []Device{{ID: xID, Name: "core.corp.example", SysName: "core.corp.example"}, {ID: yID, Name: "edge"}}
	if id, ok := MatchNeighborDevice(Neighbor{DeviceID: swID, SysName: "core"}, devices, nil); !ok || id != xID {
		t.Errorf("core = %v %v, want core.corp.example", id, ok)
	}
	if id, ok := MatchNeighborDevice(Neighbor{DeviceID: swID, SysName: "edge.corp.example"}, devices, nil); !ok || id != yID {
		t.Errorf("edge.corp.example = %v %v, want edge", id, ok)
	}
}

func TestMatchDeviceShortNameAmbiguous(t *testing.T) {
	devices := []Device{{ID: xID, Name: "Switch.a.example"}, {ID: yID, Name: "Switch.b.example"}}
	if id, ok := MatchNeighborDevice(Neighbor{DeviceID: swID, SysName: "Switch"}, devices, nil); ok {
		t.Errorf("Switch matched %v, want no match (two devices are Switch)", id)
	}
}

// The reporting device neither matches itself nor makes a match ambiguous.
func TestMatchDeviceShortNameNotSelf(t *testing.T) {
	devices := []Device{{ID: xID, Name: "Switch.corp.example"}, {ID: zID, Name: "Switch.other.example"}}
	n := Neighbor{DeviceID: xID, SysName: "Switch"}
	if id, ok := MatchNeighborDevice(n, devices, nil); !ok || id != zID {
		t.Errorf("Switch = %v %v, want Switch.other.example", id, ok)
	}
	if id, ok := MatchNeighborDevice(n, devices[:1], nil); ok {
		t.Errorf("Switch matched %v, want no match (only the reporter is Switch)", id)
	}
}

func TestMatchDeviceIPNameNotShortened(t *testing.T) {
	devices := []Device{{ID: xID, Name: "10.99.255.1"}}
	if id, ok := MatchNeighborDevice(Neighbor{DeviceID: swID, SysName: "10"}, devices, nil); ok {
		t.Errorf("10 matched %v, want no match", id)
	}
}

func TestMatchDeviceByChassisMAC(t *testing.T) {
	n := Neighbor{DeviceID: swID, IfIndex: 51, ChassisID: "e4:38:83:20:f5:c6"}
	if id, ok := MatchNeighborDevice(n, homeDevices(), homePorts()); !ok || id != gwID {
		t.Fatalf("device = %v %v, want Quantum-Gate by its eth0 MAC", id, ok)
	}
}
