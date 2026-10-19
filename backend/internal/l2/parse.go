// Package l2 reads what switches and routers know about the endpoints
// behind them: MAC address tables (which port a MAC was learned on), LLDP
// neighbours and ARP tables (which IP a MAC holds), and works out where
// each endpoint is plugged in.
package l2

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

const (
	oidQFdbPort      = "1.3.6.1.2.1.17.7.1.2.2.1.2"
	oidQFdbStatus    = "1.3.6.1.2.1.17.7.1.2.2.1.3"
	oidDot1dFdbPort  = "1.3.6.1.2.1.17.4.3.1.2"
	oidDot1dFdbState = "1.3.6.1.2.1.17.4.3.1.3"
	oidBasePortIf    = "1.3.6.1.2.1.17.1.4.1.2"
	oidLLDPRem       = "1.0.8802.1.1.2.1.4.1.1"
	oidIPNetToPhys   = "1.3.6.1.2.1.4.35.1.4"
	oidIPNetToMedia  = "1.3.6.1.2.1.4.22.1.2"
	fdbLearned       = 3
)

// FDBEntry is one MAC address table row: MAC learned on a bridge port, in
// a VLAN (0 when the table is not per VLAN).
type FDBEntry struct {
	VLAN       int
	MAC        string
	BridgePort int
}

// index returns the OID's sub-identifiers after root, or nil when the OID
// is not under root. Walks may or may not carry a leading dot.
func index(oid, root string) []int {
	oid = strings.TrimPrefix(oid, ".")
	rest, ok := strings.CutPrefix(oid, root+".")
	if !ok {
		return nil
	}
	parts := strings.Split(rest, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out[i] = n
	}
	return out
}

func macFromIndex(ix []int) string {
	if len(ix) != 6 {
		return ""
	}
	b := make([]byte, 6)
	for i, n := range ix {
		if n < 0 || n > 255 {
			return ""
		}
		b[i] = byte(n)
	}
	return macFromBytes(b)
}

func macFromBytes(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}

func keyOf(ix []int) string {
	parts := make([]string, len(ix))
	for i, n := range ix {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ".")
}

// statuses maps each row's index to its status. A row with no status is
// kept: some switches leave the status column out.
func statuses(pdus []snmp.PDU, root string) map[string]uint64 {
	out := map[string]uint64{}
	for _, p := range pdus {
		if ix := index(p.OID, root); ix != nil {
			if n, ok := p.Number(); ok {
				out[keyOf(ix)] = n
			}
		}
	}
	return out
}

// ParseQBridgeFDB reads dot1qTpFdbPort (index fdbId.mac) with its status
// column, keeping learned rows.
func ParseQBridgeFDB(port, status []snmp.PDU) []FDBEntry {
	st := statuses(status, oidQFdbStatus)
	var out []FDBEntry
	for _, p := range port {
		ix := index(p.OID, oidQFdbPort)
		if len(ix) != 7 {
			continue
		}
		if s, ok := st[keyOf(ix)]; ok && s != fdbLearned {
			continue
		}
		bp, ok := p.Number()
		mac := macFromIndex(ix[1:])
		if !ok || bp == 0 || mac == "" {
			continue
		}
		out = append(out, FDBEntry{VLAN: ix[0], MAC: mac, BridgePort: int(bp)})
	}
	return out
}

// ParseDot1dFDB reads the older dot1dTpFdbPort table (index mac), for
// switches without Q-BRIDGE. Its rows carry no VLAN.
func ParseDot1dFDB(port, status []snmp.PDU) []FDBEntry {
	st := statuses(status, oidDot1dFdbState)
	var out []FDBEntry
	for _, p := range port {
		ix := index(p.OID, oidDot1dFdbPort)
		if len(ix) != 6 {
			continue
		}
		if s, ok := st[keyOf(ix)]; ok && s != fdbLearned {
			continue
		}
		bp, ok := p.Number()
		mac := macFromIndex(ix)
		if !ok || bp == 0 || mac == "" {
			continue
		}
		out = append(out, FDBEntry{MAC: mac, BridgePort: int(bp)})
	}
	return out
}

// ParseBasePorts reads dot1dBasePortIfIndex: bridge port to ifIndex.
func ParseBasePorts(pdus []snmp.PDU) map[int]int {
	out := map[int]int{}
	for _, p := range pdus {
		ix := index(p.OID, oidBasePortIf)
		if len(ix) != 1 {
			continue
		}
		if n, ok := p.Number(); ok && n > 0 {
			out[ix[0]] = int(n)
		}
	}
	return out
}

// Caps is the first byte of lldpRemSysCapEnabled (a BITS value, bit 0 the
// most significant): other, repeater, bridge, wlanAccessPoint, router,
// telephone, ...
type Caps uint8

func (c Caps) Bridge() bool { return c&0x20 != 0 }
func (c Caps) AP() bool     { return c&0x10 != 0 }
func (c Caps) Router() bool { return c&0x08 != 0 }
func (c Caps) Phone() bool  { return c&0x04 != 0 }

// Neighbor is one LLDP neighbour seen on a local port. LocalPort is
// lldpRemLocalPortNum as the device reports it (a bridge port or ifIndex,
// depending on the device).
type Neighbor struct {
	LocalPort                             int
	ChassisID, PortID, PortDescr, SysName string
	Caps                                  Caps
}

// lldpText renders an LLDP string column: printable text as is, a 6-byte
// value as a MAC, anything else as hex.
func lldpText(v any) string {
	var b []byte
	switch x := v.(type) {
	case []byte:
		b = x
	case string:
		b = []byte(x)
	default:
		return ""
	}
	printable := len(b) > 0
	for _, r := range string(b) {
		if !unicode.IsPrint(r) {
			printable = false
			break
		}
	}
	switch {
	case printable:
		return strings.TrimSpace(string(b))
	case len(b) == 6:
		return macFromBytes(b)
	default:
		return fmt.Sprintf("%x", b)
	}
}

// ParseLLDP merges lldpRemTable columns (keyed "5" chassis ID, "7" port ID,
// "8" port description, "9" system name, "12" capabilities enabled) by row
// index timeMark.localPort.remIndex.
func ParseLLDP(rows map[string][]snmp.PDU) []Neighbor {
	byRow := map[string]*Neighbor{}
	var order []string
	for _, col := range []string{"5", "7", "8", "9", "12"} {
		root := oidLLDPRem + "." + col
		for _, p := range rows[col] {
			ix := index(p.OID, root)
			if len(ix) != 3 {
				continue
			}
			k := keyOf(ix[1:])
			n := byRow[k]
			if n == nil {
				n = &Neighbor{LocalPort: ix[1]}
				byRow[k] = n
				order = append(order, k)
			}
			switch col {
			case "5":
				n.ChassisID = chassisText(p.Value)
			case "7":
				n.PortID = lldpText(p.Value)
			case "8":
				n.PortDescr = lldpText(p.Value)
			case "9":
				n.SysName = lldpText(p.Value)
			case "12":
				if b, ok := p.Value.([]byte); ok && len(b) > 0 {
					n.Caps = Caps(b[0])
				}
			}
		}
	}
	out := make([]Neighbor, 0, len(order))
	for _, k := range order {
		out = append(out, *byRow[k])
	}
	return out
}

// chassisText prefers the MAC form for a 6-byte chassis ID, which is how
// nearly every device sends it.
func chassisText(v any) string {
	if b, ok := v.([]byte); ok && len(b) == 6 {
		return macFromBytes(b)
	}
	return lldpText(v)
}

// ARPEntry is one IPv4 address and the MAC that holds it.
type ARPEntry struct {
	IP  string
	MAC string
}

func arpRow(ip []int, v any) (ARPEntry, bool) {
	b, ok := v.([]byte)
	if !ok || len(ip) != 4 {
		return ARPEntry{}, false
	}
	mac := macFromBytes(b)
	if mac == "" || mac == "00:00:00:00:00:00" || mac == "ff:ff:ff:ff:ff:ff" {
		return ARPEntry{}, false
	}
	for _, n := range ip {
		if n < 0 || n > 255 {
			return ARPEntry{}, false
		}
	}
	return ARPEntry{IP: fmt.Sprintf("%d.%d.%d.%d", ip[0], ip[1], ip[2], ip[3]), MAC: mac}, true
}

// ParseARP reads ipNetToPhysicalPhysAddress (index ifIndex.type.len.addr,
// IPv4 rows only) or, when the device has none, the older
// ipNetToMediaPhysAddress (index ifIndex.a.b.c.d).
func ParseARP(physical, media []snmp.PDU) []ARPEntry {
	var out []ARPEntry
	seen := map[string]bool{}
	add := func(e ARPEntry) {
		if k := e.IP + " " + e.MAC; !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	for _, p := range physical {
		ix := index(p.OID, oidIPNetToPhys)
		if len(ix) != 7 || ix[1] != 1 || ix[2] != 4 {
			continue
		}
		if e, ok := arpRow(ix[3:], p.Value); ok {
			add(e)
		}
	}
	if len(physical) > 0 {
		return out
	}
	for _, p := range media {
		ix := index(p.OID, oidIPNetToMedia)
		if len(ix) != 5 {
			continue
		}
		if e, ok := arpRow(ix[1:], p.Value); ok {
			add(e)
		}
	}
	return out
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
}

// hexDigits strips MAC separators from s and returns its hex digits, or
// false if anything else is in it.
func hexDigits(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ':' || r == '-' || r == '.':
		case isHex(r):
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	return b.String(), true
}

func colonForm(hex string) string {
	var b strings.Builder
	for i, r := range hex {
		if i > 0 && i%2 == 0 {
			b.WriteByte(':')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NormalizeMAC turns a MAC in colon, dash, Cisco dot or bare hex form into
// lower-case colon form.
func NormalizeMAC(s string) (string, bool) {
	hex, ok := hexDigits(s)
	if !ok || len(hex) != 12 {
		return "", false
	}
	return colonForm(hex), true
}

// MACPrefixForSearch turns partly typed MAC input ("aa:bb:c", "AABB") into
// a lower-case colon-form prefix to match stored MACs against, or "" when
// the input is not the start of a MAC.
func MACPrefixForSearch(s string) string {
	hex, ok := hexDigits(s)
	if !ok || len(hex) < 2 || len(hex) > 12 {
		return ""
	}
	return colonForm(hex)
}
