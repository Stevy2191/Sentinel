package snmp

import (
	"reflect"
	"testing"
)

func pdu(oid string, v any) PDU { return PDU{OID: oid, Value: v} }

func TestParseSystem(t *testing.T) {
	got := ParseSystem([]PDU{
		pdu(OIDSysDescr, []byte("EdgeSwitch 48-Port 500W, 1.9.3.5\x00")),
		pdu(OIDSysObjectID, "1.3.6.1.4.1.4413"),
		pdu(OIDSysUpTime, uint64(123456)), // hundredths of a second
		pdu(OIDSysContact, []byte("noc@example.test")),
		pdu(OIDSysName, []byte("core-sw-1")),
		pdu(OIDSysLocation, []byte("MDF rack 2")),
	})
	want := System{Descr: "EdgeSwitch 48-Port 500W, 1.9.3.5", ObjectID: "1.3.6.1.4.1.4413", UptimeSeconds: 1234,
		Contact: "noc@example.test", Name: "core-sw-1", Location: "MDF rack 2"}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	// Missing values (noSuchObject) leave fields empty instead of failing.
	if got := ParseSystem([]PDU{pdu(OIDSysName, nil)}); got != (System{}) {
		t.Errorf("noSuchObject: got %+v", got)
	}
}

func TestParseInterfaces(t *testing.T) {
	const tbl, xtbl = "1.3.6.1.2.1.2.2.1", "1.3.6.1.2.1.31.1.1.1"
	got := ParseInterfaces([]PDU{
		// ifIndex 1: gigabit port with ifXTable data and a description.
		pdu(tbl+".1.1", int64(1)), pdu(tbl+".2.1", []byte("Slot: 0 Port: 1 Gigabit - Level")),
		pdu(tbl+".3.1", int64(6)), pdu(tbl+".5.1", uint64(1000000000)),
		pdu(tbl+".6.1", []byte{0x78, 0x8a, 0x20, 0x01, 0x02, 0x03}),
		pdu(tbl+".7.1", int64(1)), pdu(tbl+".8.1", int64(1)), pdu(tbl+".9.1", uint64(4500)),
		pdu(xtbl+".1.1", []byte("0/1")), pdu(xtbl+".15.1", uint64(1000)), pdu(xtbl+".18.1", []byte("Uplink to MDF")),
		// ifIndex 49: 10G port: ifSpeed saturates at 2^32-1, ifHighSpeed is right.
		pdu(tbl+".1.49", int64(49)), pdu(tbl+".2.49", []byte("Slot: 0 Port: 49 10G - Level")),
		pdu(tbl+".5.49", uint64(4294967295)), pdu(xtbl+".15.49", uint64(10000)),
		pdu(tbl+".7.49", int64(2)), pdu(tbl+".8.49", int64(7)),
		// ifIndex 3: a v1 device with no ifXTable: name falls back to ifDescr,
		// speed to ifSpeed.
		pdu(tbl+".1.3", int64(3)), pdu(tbl+".2.3", []byte("eth0")), pdu(tbl+".5.3", uint64(100000000)),
		pdu(tbl+".7.3", int64(1)), pdu(tbl+".8.3", int64(2)),
	})
	want := []Interface{
		{Index: 1, Name: "0/1", Descr: "Slot: 0 Port: 1 Gigabit - Level", Alias: "Uplink to MDF", Type: 6,
			SpeedBps: 1_000_000_000, MAC: "78:8a:20:01:02:03", AdminStatus: "up", OperStatus: "up", LastChangeSeconds: 45},
		{Index: 3, Name: "eth0", Descr: "eth0", SpeedBps: 100_000_000, AdminStatus: "up", OperStatus: "down"},
		{Index: 49, Name: "Slot: 0 Port: 49 10G - Level", Descr: "Slot: 0 Port: 49 10G - Level",
			SpeedBps: 10_000_000_000, AdminStatus: "down", OperStatus: "lowerLayerDown"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseEntity(t *testing.T) {
	const e = "1.3.6.1.2.1.47.1.1.1.1"
	// A stack: entry 1 is a module (class 9) listed before the chassis (3).
	model, serial := ParseEntity([]PDU{
		pdu(e+".5.1", int64(9)), pdu(e+".13.1", []byte("SFP-10G")), pdu(e+".11.1", []byte("MOD1")),
		pdu(e+".5.2", int64(3)), pdu(e+".13.2", []byte("ES-48-500W")), pdu(e+".11.2", []byte("F09FC2AABBCC")),
	})
	if model != "ES-48-500W" || serial != "F09FC2AABBCC" {
		t.Errorf("chassis: got %q %q", model, serial)
	}
	// No chassis entry: the first entry with a model name.
	model, serial = ParseEntity([]PDU{pdu(e+".5.7", int64(10)), pdu(e+".13.7", []byte("CRS326"))})
	if model != "CRS326" || serial != "" {
		t.Errorf("fallback: got %q %q", model, serial)
	}
	if m, s := ParseEntity(nil); m != "" || s != "" {
		t.Errorf("no ENTITY-MIB: got %q %q", m, s)
	}
}

func TestVendorFor(t *testing.T) {
	for oid, want := range map[string]string{
		"1.3.6.1.4.1.4413":        "Ubiquiti (EdgeSwitch)",
		"1.3.6.1.4.1.9.1.2066":    "Cisco",
		"1.3.6.1.4.1.41112.1.6":   "Ubiquiti",
		"1.3.6.1.4.1.29671.2.103": "Cisco Meraki",
		"1.3.6.1.4.1.17713.21":    "Cambium Networks",
		".1.3.6.1.4.1.14988.1":    "MikroTik",
		"1.3.6.1.4.1.8072.3.2.10": "Net-SNMP (Linux)",
		"1.3.6.1.4.1.99999.1":     "Unknown (enterprise 99999)",
		"":                        "",
		"1.3.6.1.2.1.1":           "",
	} {
		if got := VendorFor(oid); got != want {
			t.Errorf("VendorFor(%q) = %q, want %q", oid, got, want)
		}
	}
}

// TestIdentity checks vendor/model refinement against real devices from the
// user's network (UDM-SE, UNVR, a USW-Pro-48-PoE switch, U7-Pro/U7-IW APs)
// plus the snmpsim EdgeSwitch, and the cases that must NOT be touched: an
// unknown enterprise and a non-Ubiquiti 8072 (stock Net-SNMP) host.
func TestIdentity(t *testing.T) {
	tests := []struct {
		name                  string
		sysObjectID, sysDescr string
		entityModel           string
		wantVendor, wantModel string
	}{
		{
			name:        "UDM-SE, no ENTITY-MIB",
			sysObjectID: "1.3.6.1.4.1.8072.3.2.10",
			sysDescr:    "Ubiquiti UniFi UDM-SE 5.1.33 Linux 4.19.152 al324",
			entityModel: "",
			wantVendor:  "Ubiquiti", wantModel: "UDM-SE",
		},
		{
			name:        "UNVR, no ENTITY-MIB",
			sysObjectID: "1.3.6.1.4.1.8072.3.2.10",
			sysDescr:    "Ubiquiti UniFi UNVR4 5.1.33 Linux 4.19.152 al324",
			entityModel: "",
			wantVendor:  "Ubiquiti", wantModel: "UNVR4",
		},
		{
			name:        "switch, ENTITY-MIB model always wins",
			sysObjectID: "1.3.6.1.4.1.4413",
			sysDescr:    "USW-Pro-48-PoE, 7.5.15.17146, Linux 3.6.5",
			entityModel: "UBNT-US48PRO-POE",
			wantVendor:  "Ubiquiti (EdgeSwitch)", wantModel: "UBNT-US48PRO-POE",
		},
		{
			name:        "switch, no ENTITY-MIB falls back to sysDescr",
			sysObjectID: "1.3.6.1.4.1.4413",
			sysDescr:    "USW-Pro-48-PoE, 7.5.15.17146, Linux 3.6.5",
			entityModel: "",
			wantVendor:  "Ubiquiti (EdgeSwitch)", wantModel: "USW-Pro-48-PoE",
		},
		{
			name:        "AP U7-Pro, no ENTITY-MIB",
			sysObjectID: "1.3.6.1.4.1.41112",
			sysDescr:    "U7-Pro 8.7.11.19419",
			entityModel: "",
			wantVendor:  "Ubiquiti", wantModel: "U7-Pro",
		},
		{
			name:        "AP U7-IW, no ENTITY-MIB",
			sysObjectID: "1.3.6.1.4.1.41112",
			sysDescr:    "U7-IW 8.7.11.19419",
			entityModel: "",
			wantVendor:  "Ubiquiti", wantModel: "U7-IW",
		},
		{
			name:        "simulator EdgeSwitch, ENTITY-MIB model always wins",
			sysObjectID: "1.3.6.1.4.1.4413",
			sysDescr:    "EdgeSwitch 48-Port 500W, 1.9.3.5372984",
			entityModel: "ES-48-500W",
			wantVendor:  "Ubiquiti (EdgeSwitch)", wantModel: "ES-48-500W",
		},
		{
			name:        "simulator EdgeSwitch, no ENTITY-MIB: 'EdgeSwitch' isn't a model code",
			sysObjectID: "1.3.6.1.4.1.4413",
			sysDescr:    "EdgeSwitch 48-Port 500W, 1.9.3.5372984",
			entityModel: "",
			wantVendor:  "Ubiquiti (EdgeSwitch)", wantModel: "",
		},
		{
			name:        "unknown enterprise, unrelated descr: unchanged",
			sysObjectID: "1.3.6.1.4.1.99999.1",
			sysDescr:    "Some Random Server v1.0",
			entityModel: "",
			wantVendor:  "Unknown (enterprise 99999)", wantModel: "",
		},
		{
			name:        "8072 non-Ubiquiti Linux host: named but no model guessed",
			sysObjectID: "1.3.6.1.4.1.8072.3.2.10",
			sysDescr:    "Linux fileserver 6.1.0",
			entityModel: "",
			wantVendor:  "Net-SNMP (Linux)", wantModel: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vendor, model := Identity(tt.sysObjectID, tt.sysDescr, tt.entityModel)
			if vendor != tt.wantVendor || model != tt.wantModel {
				t.Errorf("Identity(%q, %q, %q) = (%q, %q), want (%q, %q)",
					tt.sysObjectID, tt.sysDescr, tt.entityModel, vendor, model, tt.wantVendor, tt.wantModel)
			}
		})
	}
}

// Agents pad strings with NUL and some send Latin-1; Postgres TEXT rejects NUL
// bytes and invalid UTF-8, which would fail the whole inventory save.
func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"core\x00-sw\x00\x00": "core-sw",
		"  spaced  ":          "spaced",
		"caf\xe9":             "caf",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}
