package portmon

import "fmt"

// Interface lists captured from the user's sandbox on 2026-09-30.

type fixtureIf struct {
	Index int
	Name  string
	Descr string
	Type  int
}

func info(f fixtureIf) IfInfo { return IfInfo{Name: f.Name, Descr: f.Descr, Type: f.Type} }

// QuantumLink: USW-Pro-48-PoE. Ports 1-48 copper, 49-52 SFP+, a CPU
// interface, and 26 link aggregates.
func uswPro48() []fixtureIf {
	var out []fixtureIf
	for i := 1; i <= 48; i++ {
		out = append(out, fixtureIf{i, fmt.Sprintf("0/%d", i), fmt.Sprintf("Slot: 0 Port: %d Gigabit - Level", i), 6})
	}
	for i := 49; i <= 52; i++ {
		out = append(out, fixtureIf{i, fmt.Sprintf("0/%d", i), fmt.Sprintf("Slot: 0 Port: %d 10G - Level", i), 6})
	}
	out = append(out, fixtureIf{65, "CPU Interface:  5/1", "CPU Interface for Slot: 5 Port: 1", 1})
	for i := 1; i <= 26; i++ {
		out = append(out, fixtureIf{65 + i, fmt.Sprintf("3/%d", i), fmt.Sprintf("Link Aggregate %d", i), 161})
	}
	return out
}

// Quantum-Gate: UDM-SE. eth0-7 LAN, eth8 2.5G WAN, eth9/eth10 SFP+; the rest
// is bridges, VLAN sub-interfaces, tunnels and the internal switch0.
func udmSE() []fixtureIf {
	out := []fixtureIf{
		{1, "lo", "lo", 24}, {2, "dummy0", "dummy0", 6},
		{3, "eth9", "Annapurna Labs Ltd. SFP+ 10G Ethernet Adapter", 6},
		{4, "eth10", "Annapurna Labs Ltd. SFP+ 10G Ethernet Adapter", 6},
		{5, "switch0", "Annapurna Labs Ltd. Gigabit Ethernet Adapter", 6},
		{6, "gre0", "gre0", 131}, {7, "gretap0", "gretap0", 6}, {8, "erspan0", "erspan0", 6},
		{9, "ip_vti0", "ip_vti0", 131}, {10, "ip6_vti0", "ip6_vti0", 131}, {11, "sit0", "sit0", 131},
		{12, "ip6tnl0", "ip6tnl0", 131},
		{13, "eth8", "Realtek Semiconductor Co., Ltd. RTL8125 2.5GbE Controller", 6},
		{14, "ifb0", "ifb0", 6}, {15, "ifb1", "ifb1", 6},
	}
	for i := 0; i <= 7; i++ {
		out = append(out, fixtureIf{16 + i, fmt.Sprintf("eth%d", i), fmt.Sprintf("eth%d", i), 6})
	}
	for i, v := range []int{10, 100, 20, 255, 30} {
		out = append(out, fixtureIf{24 + i, fmt.Sprintf("eth10.%d", v), fmt.Sprintf("eth10.%d", v), 6})
		out = append(out, fixtureIf{46 + i, fmt.Sprintf("switch0.%d", v), fmt.Sprintf("switch0.%d", v), 6})
		out = append(out, fixtureIf{58 + i, fmt.Sprintf("br%d", v), fmt.Sprintf("br%d", v), 6})
	}
	out = append(out, fixtureIf{70, "ifbeth6", "ifbeth6", 6}, fixtureIf{72, "wgsrv1", "wgsrv1", 1})
	return out
}

// KitchenAP: U7-Pro. Only eth0 is a physical port.
func u7Pro() []fixtureIf {
	return []fixtureIf{
		{1, "lo", "lo", 24}, {2, "miireg", "miireg", 1}, {3, "eth0", "eth0", 6},
		{4, "ip6tnl0", "ip6tnl0", 131}, {5, "sit0", "sit0", 131}, {6, "gre0", "gre0", 131},
		{7, "gretap0", "gretap0", 6}, {8, "erspan0", "erspan0", 6}, {9, "ip6gre0", "ip6gre0", 1},
		{10, "bond0", "bond0", 6}, {11, "teql0", "teql0", 1}, {12, "mld-wifi0", "mld-wifi0", 6},
		{13, "wifi0", "wifi0", 1}, {14, "soc0", "soc0", 1}, {15, "wifi1", "Device 17cb:1109", 1},
		{18, "wifi0ap0", "wifi0ap0", 6}, {23, "wifi1ap1", "wifi1ap1", 6},
		{31, "eth0.50", "eth0.50", 6}, {32, "wifi0ap0.50", "wifi0ap0.50", 6},
		{39, "br0", "br0", 6}, {40, "br0.10", "br0.10", 6}, {44, "pd99", "pd99", 1},
	}
}

// Overwatch: UNVR. Two physical ports, no faceplate (NVR).
func unvr() []fixtureIf {
	return []fixtureIf{
		{1, "lo", "lo", 24},
		{2, "enp0s1", "Annapurna Labs Ltd. Gigabit Ethernet Adapter", 6},
		{3, "enp0s2", "Annapurna Labs Ltd. SFP+ 10G Ethernet Adapter", 6},
	}
}

// stackedSwitch: a two-member 48-port stack, Cisco-style "unit/slot/port"
// names: 1/0/1..1/0/48 and 2/0/1..2/0/48 copper, plus 1/1/1..1/1/4 and
// 2/1/1..2/1/4 as 10G SFP+.
func stackedSwitch() []fixtureIf {
	var out []fixtureIf
	idx := 1
	for _, unit := range []int{1, 2} {
		for i := 1; i <= 48; i++ {
			out = append(out, fixtureIf{idx, fmt.Sprintf("%d/0/%d", unit, i), "", 6})
			idx++
		}
		for i := 1; i <= 4; i++ {
			out = append(out, fixtureIf{idx, fmt.Sprintf("%d/1/%d", unit, i), "10G SFP+", 6})
			idx++
		}
	}
	return out
}

// cisco3850 is a Cisco WS-C3850-12S-S: twelve 1G SFP ports Gi1/0/1-12, a
// network module whose four cages show up twice (Gi1/1/1-4 and
// Te1/1/1-4), the Gi0/0 management port, the stack interfaces, a VLAN and
// Null0.
func cisco3850() []fixtureIf {
	out := []fixtureIf{{1, "Gi0/0", "GigabitEthernet0/0", 6}}
	for i := 1; i <= 12; i++ {
		out = append(out, fixtureIf{8 + i, fmt.Sprintf("Gi1/0/%d", i), fmt.Sprintf("GigabitEthernet1/0/%d", i), 6})
	}
	for i := 1; i <= 4; i++ {
		out = append(out, fixtureIf{20 + i, fmt.Sprintf("Gi1/1/%d", i), fmt.Sprintf("GigabitEthernet1/1/%d", i), 6})
		out = append(out, fixtureIf{24 + i, fmt.Sprintf("Te1/1/%d", i), fmt.Sprintf("TenGigabitEthernet1/1/%d", i), 6})
	}
	return append(out,
		fixtureIf{33, "StackPort1", "StackPort1", 6},
		fixtureIf{34, "StackSub-St1-1", "StackSub-St1-1", 6},
		fixtureIf{35, "StackSub-St1-2", "StackSub-St1-2", 6},
		fixtureIf{36, "Vl1", "Vlan1", 53},
		fixtureIf{37, "Nu0", "Null0", 1},
	)
}

// cisco4500X is one member of a Catalyst 4500-X-16 VSS pair: its sixteen
// built-in SFP+ ports are slot 1 (Te1/1/1-16) and the uplink module slot 2
// (Te1/2/1-8).
func cisco4500X() []fixtureIf {
	var out []fixtureIf
	for i := 1; i <= 16; i++ {
		out = append(out, fixtureIf{i, fmt.Sprintf("Te1/1/%d", i), fmt.Sprintf("TenGigabitEthernet1/1/%d", i), 6})
	}
	for i := 1; i <= 8; i++ {
		out = append(out, fixtureIf{16 + i, fmt.Sprintf("Te1/2/%d", i), fmt.Sprintf("TenGigabitEthernet1/2/%d", i), 6})
	}
	return out
}
