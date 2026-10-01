package snmp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// System group (SNMPv2-MIB).
const (
	OIDSysDescr    = "1.3.6.1.2.1.1.1.0"
	OIDSysObjectID = "1.3.6.1.2.1.1.2.0"
	OIDSysUpTime   = "1.3.6.1.2.1.1.3.0"
	OIDSysContact  = "1.3.6.1.2.1.1.4.0"
	OIDSysName     = "1.3.6.1.2.1.1.5.0"
	OIDSysLocation = "1.3.6.1.2.1.1.6.0"
)

// SystemOIDs is the whole system group, in one GET.
var SystemOIDs = []string{OIDSysDescr, OIDSysObjectID, OIDSysUpTime, OIDSysContact, OIDSysName, OIDSysLocation}

const (
	oidIfEntry    = "1.3.6.1.2.1.2.2.1"      // IF-MIB ifTable
	oidIfXEntry   = "1.3.6.1.2.1.31.1.1.1"   // IF-MIB ifXTable
	oidEntPhysEnt = "1.3.6.1.2.1.47.1.1.1.1" // ENTITY-MIB entPhysicalTable
	oidUPSIdent   = "1.3.6.1.2.1.33.1.1"     // UPS-MIB upsIdent
)

// ifTableColumns and ifXTableColumns are walked one column at a time, so the
// traffic counters in the same tables (phase 2) are not fetched here.
var (
	ifTableColumns  = []int{1, 2, 3, 5, 6, 7, 8, 9} // index, descr, type, speed, physAddress, admin, oper, lastChange
	ifXTableColumns = []int{1, 15, 17, 18}          // name, highSpeed, connectorPresent, alias
	entityColumns   = []int{2, 5, 11, 13}           // descr, class, serialNum, modelName
)

// System is the SNMPv2-MIB system group. Tagged for the Test connection and
// scan responses.
type System struct {
	Descr         string `json:"descr"`
	ObjectID      string `json:"object_id"`
	Contact       string `json:"contact"`
	Name          string `json:"name"`
	Location      string `json:"location"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// Interface is one IF-MIB interface, merged from ifTable and ifXTable.
type Interface struct {
	Index             int
	Name              string
	Descr             string
	Alias             string
	Type              int
	SpeedBps          int64
	MAC               string
	AdminStatus       string
	OperStatus        string
	LastChangeSeconds int64
	// HasIfX: the ifXTable answered for this interface in this walk.
	HasIfX bool
	// ConnectorPresent is ifConnectorPresent; nil when not reported.
	ConnectorPresent *bool
}

// Inventory is everything a refresh learns about a device.
type Inventory struct {
	System     System
	Vendor     string
	Model      string
	Serial     string
	Interfaces []Interface
}

// Clean makes an agent's string safe to store: no NUL bytes (Postgres TEXT
// rejects them), valid UTF-8 only, surrounding space trimmed.
func Clean(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ToValidUTF8(s, "")
	return strings.TrimSpace(s)
}

// ParseSystem reads the system group. Missing values leave fields empty.
func ParseSystem(pdus []PDU) System {
	var s System
	for _, p := range pdus {
		switch p.OID {
		case OIDSysDescr:
			s.Descr = Clean(p.Text())
		case OIDSysObjectID:
			s.ObjectID = strings.TrimPrefix(p.Text(), ".")
		case OIDSysUpTime:
			if n, ok := p.Number(); ok {
				s.UptimeSeconds = int64(n / 100)
			}
		case OIDSysContact:
			s.Contact = Clean(p.Text())
		case OIDSysName:
			s.Name = Clean(p.Text())
		case OIDSysLocation:
			s.Location = Clean(p.Text())
		}
	}
	return s
}

var adminStatusNames = map[uint64]string{1: "up", 2: "down", 3: "testing"}
var operStatusNames = map[uint64]string{1: "up", 2: "down", 3: "testing", 4: "unknown", 5: "dormant", 6: "notPresent", 7: "lowerLayerDown"}

// splitColumn splits "<table>.<column>.<index>" into column and index.
func splitColumn(oid, table string) (column, index int, ok bool) {
	rest, found := strings.CutPrefix(oid, table+".")
	if !found {
		return 0, 0, false
	}
	colStr, idxStr, found := strings.Cut(rest, ".")
	if !found || strings.Contains(idxStr, ".") {
		return 0, 0, false
	}
	c, err1 := strconv.Atoi(colStr)
	i, err2 := strconv.Atoi(idxStr)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return c, i, true
}

func formatMAC(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	parts := make([]string, len(b))
	for i, x := range b {
		parts[i] = fmt.Sprintf("%02x", x)
	}
	return strings.Join(parts, ":")
}

// ParseInterfaces merges walked ifTable and ifXTable columns by ifIndex,
// sorted by index. Name is ifName, falling back to ifDescr; speed is
// ifHighSpeed (Mb/s) when non-zero, else ifSpeed (b/s).
func ParseInterfaces(pdus []PDU) []Interface {
	byIndex := map[int]*Interface{}
	highSpeed := map[int]uint64{}
	get := func(i int) *Interface {
		if byIndex[i] == nil {
			byIndex[i] = &Interface{Index: i}
		}
		return byIndex[i]
	}
	for _, p := range pdus {
		if col, idx, ok := splitColumn(p.OID, oidIfEntry); ok {
			it := get(idx)
			n, _ := p.Number()
			switch col {
			case 2:
				it.Descr = Clean(p.Text())
			case 3:
				it.Type = int(n)
			case 5:
				if it.SpeedBps == 0 {
					it.SpeedBps = int64(n)
				}
			case 6:
				if b, ok := p.Value.([]byte); ok {
					it.MAC = formatMAC(b)
				}
			case 7:
				it.AdminStatus = adminStatusNames[n]
			case 8:
				it.OperStatus = operStatusNames[n]
			case 9:
				it.LastChangeSeconds = int64(n / 100)
			}
			continue
		}
		if col, idx, ok := splitColumn(p.OID, oidIfXEntry); ok {
			it := get(idx)
			it.HasIfX = true
			switch col {
			case 1:
				it.Name = Clean(p.Text())
			case 15:
				if n, ok := p.Number(); ok {
					highSpeed[idx] = n
				}
			case 17:
				if n, ok := p.Number(); ok {
					present := n == 1
					it.ConnectorPresent = &present
				}
			case 18:
				it.Alias = Clean(p.Text())
			}
		}
	}
	out := make([]Interface, 0, len(byIndex))
	for idx, it := range byIndex {
		if hs := highSpeed[idx]; hs > 0 {
			it.SpeedBps = int64(hs) * 1_000_000
		}
		if it.Name == "" {
			it.Name = it.Descr
		}
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// ENTITY-MIB entPhysicalClass values.
const (
	entClassChassis     = 3
	entClassPowerSupply = 6
	entClassFan         = 7
	entClassSensor      = 8
)

var (
	// modelInDescr finds a model code such as "WS-C4500X-16" inside a
	// description like "Cisco Systems, Inc. WS-C4500X-16 2 slot switch".
	modelInDescr = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(-[A-Z0-9+]+)+\b`)
	// partName is how power supplies and fans are named ("C4KX-PWR-750AC-F",
	// "PWR-C1-350WAC", "C4KX-FAN-F"), for entries whose class is unknown.
	partName = regexp.MustCompile(`(?i)(^|-)(PWR|PSU|FAN)(-|$)`)
)

// ParseEntity picks the model and serial of the chassis (entPhysicalClass 3)
// from walked ENTITY-MIB columns: its model name, else a model code in its
// description (Catalyst 4500s leave the name blank). With no chassis model
// it takes the first entry with a model name that is not a power supply,
// fan or sensor, so a part is never taken for the device.
func ParseEntity(pdus []PDU) (model, serial string) {
	type ent struct {
		class                uint64
		descr, model, serial string
	}
	byIndex := map[int]*ent{}
	for _, p := range pdus {
		col, idx, ok := splitColumn(p.OID, oidEntPhysEnt)
		if !ok {
			continue
		}
		e := byIndex[idx]
		if e == nil {
			e = &ent{}
			byIndex[idx] = e
		}
		switch col {
		case 2:
			e.descr = Clean(p.Text())
		case 5:
			e.class, _ = p.Number()
		case 11:
			e.serial = Clean(p.Text())
		case 13:
			e.model = Clean(p.Text())
		}
	}
	idxs := make([]int, 0, len(byIndex))
	for i := range byIndex {
		idxs = append(idxs, i)
	}
	sort.Ints(idxs)
	for _, i := range idxs {
		if e := byIndex[i]; e.class == entClassChassis && e.model != "" {
			return e.model, e.serial
		}
	}
	for _, i := range idxs {
		if e := byIndex[i]; e.class == entClassChassis {
			if m := modelInDescr.FindString(e.descr); m != "" {
				return m, e.serial
			}
		}
	}
	for _, i := range idxs {
		e := byIndex[i]
		switch {
		case e.model == "", e.class == entClassPowerSupply, e.class == entClassFan, e.class == entClassSensor,
			partName.MatchString(e.model):
			continue
		}
		return e.model, e.serial
	}
	return "", ""
}

// ParseUPSIdent reads UPS-MIB's upsIdentManufacturer and upsIdentModel, which
// every UPS network card reports even when it has no ENTITY-MIB (Tripp Lite,
// Eaton). Both are "" on a device that is not a UPS.
func ParseUPSIdent(pdus []PDU) (manufacturer, model string) {
	for _, p := range pdus {
		switch p.OID {
		case oidUPSIdent + ".1.0":
			manufacturer = Clean(p.Text())
		case oidUPSIdent + ".2.0":
			model = Clean(p.Text())
		}
	}
	return manufacturer, model
}

// upsVendor keeps a vendor named from the enterprise number, and otherwise
// takes the manufacturer the UPS reports.
func upsVendor(vendor, manufacturer string) string {
	if manufacturer != "" && (vendor == "" || strings.HasPrefix(vendor, "Unknown")) {
		return manufacturer
	}
	return vendor
}

// vendors maps IANA private enterprise numbers to names.
var vendors = map[int]string{
	9: "Cisco", 11: "HP", 674: "Dell", 2636: "Juniper", 4413: "Ubiquiti (EdgeSwitch)",
	4526: "Netgear", 6486: "Alcatel-Lucent", 11863: "TP-Link", 12356: "Fortinet",
	14823: "Aruba", 14988: "MikroTik", 17713: "Cambium Networks", 25053: "Ruckus",
	25461: "Palo Alto Networks", 29671: "Cisco Meraki", 41112: "Ubiquiti",
	318: "APC", 534: "Eaton", 850: "Tripp Lite",
	8072: "Net-SNMP (Linux)",
}

// VendorFor names the manufacturer behind a sysObjectID, or
// "Unknown (enterprise N)"; "" when the OID is not under enterprises.
func VendorFor(sysObjectID string) string {
	rest, ok := strings.CutPrefix(strings.TrimPrefix(sysObjectID, "."), "1.3.6.1.4.1.")
	if !ok {
		return ""
	}
	numStr, _, _ := strings.Cut(rest, ".")
	n, err := strconv.Atoi(numStr)
	if err != nil {
		return ""
	}
	if v, ok := vendors[n]; ok {
		return v
	}
	return fmt.Sprintf("Unknown (enterprise %d)", n)
}

// uniFiPrefix is how UniFi OS (the UDM/UNVR family) opens sysDescr; those
// consoles run stock Net-SNMP under enterprise 8072 and have no ENTITY-MIB,
// so VendorFor alone can't tell them apart from any other Linux box.
const uniFiPrefix = "Ubiquiti UniFi "

// Identity refines VendorFor's vendor with sysDescr, and fills in a model
// when ENTITY-MIB (entityModel) gave none. An ENTITY-MIB model always wins.
func Identity(sysObjectID, sysDescr, entityModel string) (vendor, model string) {
	vendor = VendorFor(sysObjectID)
	model = entityModel
	descr := strings.TrimSpace(sysDescr)
	switch {
	case strings.HasPrefix(descr, uniFiPrefix):
		// UniFi OS console: "Ubiquiti UniFi UDM-SE 5.1.33 Linux ...".
		vendor = "Ubiquiti"
		if model == "" {
			rest := strings.TrimPrefix(descr, uniFiPrefix)
			model, _, _ = strings.Cut(rest, " ")
		}
	case (vendor == "Ubiquiti" || vendor == "Ubiquiti (EdgeSwitch)") && model == "":
		// EdgeOS/UniFi device firmware: "USW-Pro-48-PoE, 7.5.15.17146, ..."
		// or "U7-Pro 8.7.11.19419" — the model code leads sysDescr.
		token := descr
		if i := strings.IndexAny(descr, ", \t"); i >= 0 {
			token = descr[:i]
		}
		if looksLikeModelCode(token) {
			model = token
		}
	}
	return vendor, model
}

// looksLikeModelCode reports whether s reads like a hardware model ("U7-Pro",
// "USW-Pro-48-PoE") rather than a product family name ("EdgeSwitch"): it has
// no spaces, is a plausible length, and mixes letters with digits or a dash.
func looksLikeModelCode(s string) bool {
	if len(s) < 2 || len(s) > 40 {
		return false
	}
	var hasLetter, hasDigitOrDash bool
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9' || r == '-':
			hasDigitOrDash = true
		case r == ' ' || r == '\t':
			return false
		}
	}
	return hasLetter && hasDigitOrDash
}

// Identify reads a device's system group.
func Identify(ctx context.Context, c Client, t Target) (System, error) {
	pdus, err := c.Get(ctx, t, SystemOIDs)
	if err != nil {
		return System{}, err
	}
	return ParseSystem(pdus), nil
}

// ReadInventory reads identity, model/serial and interfaces. Only a failure
// of the system group or ifTable is an error: ifXTable (absent on v1 and some
// radios), ENTITY-MIB (absent on many small devices) and UPS-MIB are optional.
func ReadInventory(ctx context.Context, c Client, t Target) (Inventory, error) {
	sys, err := Identify(ctx, c, t)
	if err != nil {
		return Inventory{}, fmt.Errorf("reading system group: %w", err)
	}
	inv := Inventory{System: sys}

	var ifPDUs []PDU
	for _, col := range ifTableColumns {
		p, err := c.Walk(ctx, t, fmt.Sprintf("%s.%d", oidIfEntry, col))
		if err != nil {
			return Inventory{}, fmt.Errorf("walking ifTable: %w", err)
		}
		ifPDUs = append(ifPDUs, p...)
	}
	for _, col := range ifXTableColumns {
		if p, err := c.Walk(ctx, t, fmt.Sprintf("%s.%d", oidIfXEntry, col)); err == nil {
			ifPDUs = append(ifPDUs, p...)
		}
	}
	inv.Interfaces = ParseInterfaces(ifPDUs)

	var entPDUs []PDU
	for _, col := range entityColumns {
		if p, err := c.Walk(ctx, t, fmt.Sprintf("%s.%d", oidEntPhysEnt, col)); err == nil {
			entPDUs = append(entPDUs, p...)
		}
	}
	entityModel, serial := ParseEntity(entPDUs)
	// No ENTITY-MIB model: a UPS still names itself in UPS-MIB. Optional, so
	// an agent that refuses the request is simply not a UPS.
	var upsMaker string
	if entityModel == "" {
		if p, err := c.Get(ctx, t, []string{oidUPSIdent + ".1.0", oidUPSIdent + ".2.0"}); err == nil {
			upsMaker, entityModel = ParseUPSIdent(p)
		}
	}
	inv.Serial = serial
	inv.Vendor, inv.Model = Identity(sys.ObjectID, sys.Descr, entityModel)
	inv.Vendor = upsVendor(inv.Vendor, upsMaker)
	return inv, nil
}
