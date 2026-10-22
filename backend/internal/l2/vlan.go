package l2

import (
	"sort"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// A Cisco's VLANs, from CISCO-VTP-MIB vtpVlanTable (index
// managementDomain.vlan). Its default context's bridge tables are VLAN 1's
// only; each VLAN's are read in that VLAN's context.
const (
	OIDVtpVlanState = "1.3.6.1.4.1.9.9.46.1.3.1.1.2"
	OIDVtpVlanType  = "1.3.6.1.4.1.9.9.46.1.3.1.1.3"

	vlanOperational = 1
	vlanEthernet    = 1
)

// ParseVLANs is the VLANs whose bridge tables are worth reading, in order:
// operational, ethernet (when the type column answered) and not one of the
// reserved FDDI and Token Ring VLANs 1002–1005.
func ParseVLANs(state, kind []snmp.PDU) []int {
	types := map[int]uint64{}
	for _, p := range kind {
		if ix := index(p.OID, OIDVtpVlanType); len(ix) == 2 {
			types[ix[1]], _ = p.Number()
		}
	}
	seen := map[int]bool{}
	var out []int
	for _, p := range state {
		ix := index(p.OID, OIDVtpVlanState)
		if len(ix) != 2 {
			continue
		}
		vlan := ix[1]
		if s, ok := p.Number(); !ok || s != vlanOperational || seen[vlan] || (vlan >= 1002 && vlan <= 1005) {
			continue
		}
		if len(types) > 0 && types[vlan] != vlanEthernet {
			continue
		}
		seen[vlan] = true
		out = append(out, vlan)
	}
	sort.Ints(out)
	return out
}
