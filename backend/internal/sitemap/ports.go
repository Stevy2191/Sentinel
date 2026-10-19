package sitemap

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/l2"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// portWordN is an LLDP port reported as "Port 5": TS5410R81E names
// QuantumLink's port so, while QuantumLink itself calls it "0/5".
var portWordN = regexp.MustCompile(`(?i)^port\s*(\d+)$`)

func physical(p Port) bool {
	return portmon.IsPhysical(portmon.IfInfo{Name: p.Name, Descr: p.Descr, Type: p.IfType, ConnectorPresent: p.ConnectorPresent})
}

// MatchNeighborDevice finds the map device an LLDP neighbour is: by its
// chassis ID among the devices' interface MACs, else by its system name
// against a device's sysName or name, ignoring case. The device reporting
// the neighbour never matches itself.
func MatchNeighborDevice(n Neighbor, devices []Device, ports []Port) (uuid.UUID, bool) {
	onMap := map[uuid.UUID]bool{}
	for _, d := range devices {
		onMap[d.ID] = true
	}
	if mac, ok := l2.NormalizeMAC(n.ChassisID); ok {
		for _, p := range ports {
			if pm, ok := l2.NormalizeMAC(p.MAC); ok && pm == mac && onMap[p.DeviceID] && p.DeviceID != n.DeviceID {
				return p.DeviceID, true
			}
		}
	}
	name := strings.ToLower(strings.TrimSpace(n.SysName))
	if name == "" {
		return uuid.Nil, false
	}
	for _, d := range devices {
		if d.ID == n.DeviceID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(d.SysName), name) || strings.EqualFold(strings.TrimSpace(d.Name), name) {
			return d.ID, true
		}
	}
	return uuid.Nil, false
}

// MatchNeighborPort finds which of dev's ports an LLDP neighbour's port is,
// in this order: the port ID or description equals the port's ifName,
// ifDescr or ifAlias; it reads "Port N" and N is the port's faceplate label;
// the port ID is a MAC held by exactly one physical port. 0 when none says.
func MatchNeighborPort(n Neighbor, dev uuid.UUID, ports []Port, numbering portmon.Numbering) int {
	var mine []Port
	for _, p := range ports {
		if p.DeviceID == dev {
			mine = append(mine, p)
		}
	}
	says := []string{strings.TrimSpace(n.PortID), strings.TrimSpace(n.PortDescr)}

	// 1. By name: physical ports first, so "eth10" never lands on a bridge.
	for _, wantPhysical := range []bool{true, false} {
		for _, s := range says {
			if s == "" {
				continue
			}
			for _, p := range mine {
				if physical(p) != wantPhysical {
					continue
				}
				for _, v := range []string{p.Name, p.Descr, p.Alias} {
					if v != "" && strings.EqualFold(strings.TrimSpace(v), s) {
						return p.IfIndex
					}
				}
			}
		}
	}
	// 2. "Port N".
	for _, s := range says {
		m := portWordN.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		want, _ := strconv.Atoi(m[1])
		for _, p := range mine {
			if physical(p) && numbering.Label(p.Name, p.Descr, p.IfIndex) == strconv.Itoa(want) {
				return p.IfIndex
			}
		}
	}
	// 3. A MAC on exactly one physical port.
	if mac, ok := l2.NormalizeMAC(n.PortID); ok {
		found, count := 0, 0
		for _, p := range mine {
			if pm, ok := l2.NormalizeMAC(p.MAC); ok && pm == mac && physical(p) {
				found, count = p.IfIndex, count+1
			}
		}
		if count == 1 {
			return found
		}
	}
	return 0
}
