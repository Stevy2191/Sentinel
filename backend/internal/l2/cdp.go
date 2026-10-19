package l2

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// OIDCDPCache is CISCO-CDP-MIB cdpCacheEntry, indexed ifIndex.deviceIndex.
// Columns: 3 address type, 4 address, 6 device ID, 7 device port, 8 platform.
const OIDCDPCache = "1.3.6.1.4.1.9.9.23.1.2.1.1"

// CDPNeighbor is one cdpCacheTable row: the device heard on local port
// IfIndex. SysName is DeviceID without its domain or "(serial)"; Address
// is its IPv4 address when it sent one.
type CDPNeighbor struct {
	IfIndex                                    int
	DeviceID, SysName, Port, Platform, Address string
}

// ParseCDP merges cdpCacheTable columns (keyed "3", "4", "6", "7", "8") by
// row. Rows without a device ID are dropped.
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
			}
		}
	}
	keys := make([]string, 0, len(rows))
	for k, r := range rows {
		if r.DeviceID == "" {
			continue
		}
		r.SysName = r.DeviceID
		if i := strings.IndexAny(r.SysName, ".("); i > 0 {
			r.SysName = r.SysName[:i]
		}
		if b, ok := addrs[k].([]byte); ok && types[k] == 1 && len(b) == 4 {
			r.Address = fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
		}
		keys = append(keys, k)
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
