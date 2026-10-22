package l2

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// OIDCDPCache is CISCO-CDP-MIB cdpCacheEntry, indexed ifIndex.deviceIndex.
// Columns: 3 address type, 4 address, 6 device ID, 7 device port, 8 platform,
// 9 capabilities.
const OIDCDPCache = "1.3.6.1.4.1.9.9.23.1.2.1.1"

// CDPNeighbor is one cdpCacheTable row: the device heard on local port
// IfIndex. SysName is DeviceID without its domain or "(serial)", unless the
// ID is an IP address; Address is its IPv4 address when it sent one. Key is
// what the row is stored under: DeviceID, or DeviceID@Address when another
// row on the same port has the same ID from another address (every UniFi
// device says "UBNT"). Caps are its capabilities on LLDP's bits.
type CDPNeighbor struct {
	IfIndex                                         int
	DeviceID, Key, SysName, Port, Platform, Address string
	Caps                                            Caps
}

// cdpCaps maps cdpCacheCapabilities (CDP's bits, big-endian) onto LLDP's:
// router; trans-bridge, source-route bridge and switch as bridge; repeater;
// phone as telephone; host as station.
func cdpCaps(b []byte) Caps {
	var v uint32
	for _, x := range b {
		v = v<<8 | uint32(x)
	}
	var c Caps
	for _, m := range []struct {
		cdp  uint32
		lldp Caps
	}{{0x01, 0x08}, {0x02, 0x20}, {0x04, 0x20}, {0x08, 0x20}, {0x40, 0x40}, {0x80, 0x04}, {0x10, 0x01}} {
		if v&m.cdp != 0 {
			c |= m.lldp
		}
	}
	return c
}

// ParseCDP merges cdpCacheTable columns (keyed "3", "4", "6", "7", "8", "9")
// by row. Rows without a device ID are dropped.
func ParseCDP(cols map[string][]snmp.PDU) []CDPNeighbor {
	rows := map[string]*CDPNeighbor{}
	types := map[string]uint64{}
	addrs := map[string]any{}
	for col, pdus := range cols {
		root := OIDCDPCache + "." + col
		for _, p := range pdus {
			ix := index(p.OID, root)
			if len(ix) != 2 {
				continue
			}
			k := keyOf(ix)
			r := rows[k]
			if r == nil {
				r = &CDPNeighbor{IfIndex: ix[0]}
				rows[k] = r
			}
			switch col {
			case "3":
				types[k], _ = p.Number()
			case "4":
				addrs[k] = p.Value
			case "6":
				r.DeviceID = lldpText(p.Value)
			case "7":
				r.Port = lldpText(p.Value)
			case "8":
				r.Platform = lldpText(p.Value)
			case "9":
				if b, ok := p.Value.([]byte); ok {
					r.Caps = cdpCaps(b)
				}
			}
		}
	}
	keys := make([]string, 0, len(rows))
	from := map[[2]any]map[string]bool{} // port and device ID → addresses
	for k, r := range rows {
		if r.DeviceID == "" {
			continue
		}
		r.SysName = r.DeviceID
		if i := strings.IndexAny(r.SysName, ".("); i > 0 && net.ParseIP(r.SysName) == nil {
			r.SysName = r.SysName[:i]
		}
		if b, ok := addrs[k].([]byte); ok && types[k] == 1 && len(b) == 4 {
			r.Address = fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
		}
		id := [2]any{r.IfIndex, r.DeviceID}
		if from[id] == nil {
			from[id] = map[string]bool{}
		}
		from[id][r.Address] = true
		keys = append(keys, k)
	}
	for _, k := range keys {
		r := rows[k]
		r.Key = r.DeviceID
		if len(from[[2]any{r.IfIndex, r.DeviceID}]) > 1 && r.Address != "" {
			r.Key = r.DeviceID + "@" + r.Address
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := rows[keys[i]], rows[keys[j]]
		return a.IfIndex < b.IfIndex || (a.IfIndex == b.IfIndex && keys[i] < keys[j])
	})
	out := make([]CDPNeighbor, len(keys))
	for i, k := range keys {
		out[i] = *rows[k]
	}
	return out
}
