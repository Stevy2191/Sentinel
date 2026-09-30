package portmon

import "regexp"

// IfInfo is what classification needs to know about an interface.
type IfInfo struct {
	Name  string
	Descr string
	Type  int // IANA ifType
	// ConnectorPresent is ifXTable's ifConnectorPresent; nil when the device
	// does not report it.
	ConnectorPresent *bool
}

const ifTypeLAG = 161

var ethernetTypes = map[int]bool{6: true, 62: true, 69: true, 117: true}

// virtualName matches what Linux-based devices (UniFi consoles and APs,
// EdgeOS) report as ifType 6 even though no cable can be plugged into it:
// loopback, bridges, VLANs, radios and their VAPs, tunnels, bonding and
// traffic-shaping devices, WireGuard, the UDM's internal switch0, and CPU
// interfaces.
var virtualName = regexp.MustCompile(`(?i)^(lo|br|vlan|wlan|wifi|ath|ra\d|rai\d|veth|docker|tun|tap|imq|ifb|gre|erspan|ip6tnl|ip6gre|ip_vti|ip6_vti|sit|teql|bond|mld-|soc\d|miireg|pd\d|dummy|wg|switch\d|cpu)`)

// subInterface matches VLAN sub-interfaces such as eth0.50.
var subInterface = regexp.MustCompile(`\.\d+$`)

// IsVirtualName reports whether an interface name is one of the virtual kinds.
func IsVirtualName(name string) bool {
	return virtualName.MatchString(name) || subInterface.MatchString(name)
}

// IsLAG reports a link aggregate.
func IsLAG(i IfInfo) bool { return i.Type == ifTypeLAG }

// IsPhysical reports whether an interface is a port a cable plugs into. A
// virtual name wins over anything the agent claims; otherwise
// ifConnectorPresent decides when reported, and an Ethernet ifType when not.
func IsPhysical(i IfInfo) bool {
	if IsLAG(i) || IsVirtualName(i.Name) {
		return false
	}
	if i.ConnectorPresent != nil {
		return *i.ConnectorPresent
	}
	return ethernetTypes[i.Type]
}

// DefaultCollect is whether the stats poll reads an interface unless the user
// says otherwise: physical ports and link aggregates.
func DefaultCollect(i IfInfo) bool { return IsLAG(i) || IsPhysical(i) }
