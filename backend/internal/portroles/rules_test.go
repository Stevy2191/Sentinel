package portroles

import (
	"reflect"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func port(ix int, name string) Port {
	return Port{IfIndex: ix, Name: name, Label: name, Physical: true, Up: true, Role: "access", RoleSource: "default"}
}

func one(t *testing.T, in Input, want ...Change) {
	t.Helper()
	got := Decide(in)
	if len(want) == 0 && len(got) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Decide =\n %+v\nwant\n %+v", got, want)
	}
}

func TestDecideLLDPPeer(t *testing.T) {
	one(t, Input{Ports: []Port{port(49, "49")}, Peers: []Peer{{IfIndex: 49, Protocol: "lldp", DeviceName: "CORE-ES48-A", PortText: "49", LastSeen: now}}, Now: now},
		Change{IfIndex: 49, Role: "uplink", Source: "auto", Reason: "Found by LLDP: CORE-ES48-A port 49"})
}

func TestDecideCDPPeer(t *testing.T) {
	one(t, Input{Ports: []Port{port(1, "Gi1/0/1")}, Peers: []Peer{{IfIndex: 1, Protocol: "cdp", DeviceName: "LAB-C3850", PortText: "Gi1/0/1", LastSeen: now}}, Now: now},
		Change{IfIndex: 1, Role: "uplink", Source: "auto", Reason: "Found by CDP: LAB-C3850 port Gi1/0/1"})
}

func TestDecideStalePeerIgnored(t *testing.T) {
	stale := []Peer{{IfIndex: 49, Protocol: "lldp", DeviceName: "B", PortText: "49", LastSeen: now.Add(-8 * 24 * time.Hour)}}
	one(t, Input{Ports: []Port{port(49, "49")}, Peers: stale, Now: now})
	auto := port(49, "49")
	auto.Role, auto.RoleSource, auto.RoleReason = "uplink", "auto", "Found by LLDP: B port 49"
	one(t, Input{Ports: []Port{auto}, Peers: stale, Now: now}, Change{IfIndex: 49, Role: "access", Source: "default", Reason: ""})
}

func TestDecideLAGMember(t *testing.T) {
	p49, p50 := port(49, "49"), port(50, "50")
	p49.LagIfIndex, p50.LagIfIndex = 66, 66
	in := Input{Ports: []Port{p49, p50}, LagNames: map[int]string{66: "lag 1"}, LagPeer: map[int]string{66: "CORE-ES48-B"},
		Peers: []Peer{{IfIndex: 49, Protocol: "lldp", DeviceName: "CORE-ES48-B", PortText: "49", LastSeen: now}}, Now: now}
	one(t, in,
		Change{IfIndex: 49, Role: "uplink", Source: "auto", Reason: "Found by LLDP: CORE-ES48-B port 49"},
		Change{IfIndex: 50, Role: "uplink", Source: "auto", Reason: "Member of lag 1 to CORE-ES48-B"})
}

func TestDecideManual(t *testing.T) {
	one(t, Input{Ports: []Port{port(7, "7")}, Manual: map[int]bool{7: true}, Now: now},
		Change{IfIndex: 7, Role: "uplink", Source: "auto", Reason: "Drawn on the site map"})
}

func TestDecideWANOrder(t *testing.T) {
	p13, p58 := port(13, "eth8"), port(58, "br0")
	p13.WANEvidence, p58.WANEvidence = "public_address", "default_route"
	one(t, Input{IsRouter: true, Ports: []Port{p13, p58}, Now: now},
		Change{IfIndex: 58, Role: "wan", Source: "auto", Reason: "The default route goes out this port"})

	p58.WANEvidence = ""
	one(t, Input{IsRouter: true, Ports: []Port{p13, p58}, Now: now},
		Change{IfIndex: 13, Role: "wan", Source: "auto", Reason: "Has a public address"})

	w1, w2 := port(1, "wan1"), port(2, "wan2")
	w2.Up = false
	one(t, Input{IsRouter: true, Ports: []Port{w1, w2}, Now: now},
		Change{IfIndex: 1, Role: "wan", Source: "auto", Reason: "Named wan1"})

	virt := port(13, "ppp0")
	virt.Physical, virt.WANEvidence = false, "public_address"
	one(t, Input{IsRouter: true, Ports: []Port{virt}, Now: now})
}

func TestDecideWANNotOnSwitch(t *testing.T) {
	p := port(13, "wan")
	p.WANEvidence = "public_address"
	one(t, Input{Ports: []Port{p}, Now: now})
}

func TestDecideWANBeatsUplink(t *testing.T) {
	p := port(4, "eth3")
	p.WANEvidence = "default_route"
	one(t, Input{IsRouter: true, Ports: []Port{p}, Peers: []Peer{{IfIndex: 4, Protocol: "lldp", DeviceName: "ISP-CPE", PortText: "1", LastSeen: now}}, Now: now},
		Change{IfIndex: 4, Role: "wan", Source: "auto", Reason: "The default route goes out this port"})
}

// Review Focus 2: what a person chose stays.
func TestDecideUserWins(t *testing.T) {
	p := port(49, "49")
	p.RoleSource = "user"
	one(t, Input{Ports: []Port{p}, Peers: []Peer{{IfIndex: 49, Protocol: "lldp", DeviceName: "B", PortText: "49", LastSeen: now}}, Now: now})
}

func TestDecideNoChurn(t *testing.T) {
	p := port(49, "49")
	p.Role, p.RoleSource, p.RoleReason = "uplink", "auto", "Found by LLDP: B port 49"
	one(t, Input{Ports: []Port{p}, Peers: []Peer{{IfIndex: 49, Protocol: "lldp", DeviceName: "B", PortText: "49", LastSeen: now}}, Now: now})
}

// A port that goes down drops out of LLDP, CDP, the LAG and the routing
// table; until it's back up its automatic role stays.
func TestDecideDownPortKeepsAutoRole(t *testing.T) {
	up := port(49, "49")
	up.Role, up.RoleSource, up.RoleReason = "uplink", "auto", "Found by LLDP: B port 49"
	down := up
	down.Up = false
	wan := port(13, "eth9")
	wan.Up, wan.Role, wan.RoleSource, wan.RoleReason = false, "wan", "auto", "Default route via this port"
	one(t, Input{IsRouter: true, Ports: []Port{down, wan}, Now: now})
	one(t, Input{Ports: []Port{up}, Now: now}, Change{IfIndex: 49, Role: "access", Source: "default"})
}
