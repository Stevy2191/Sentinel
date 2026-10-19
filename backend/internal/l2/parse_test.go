package l2

import (
	"reflect"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

const (
	qPort   = "1.3.6.1.2.1.17.7.1.2.2.1.2"
	qStatus = "1.3.6.1.2.1.17.7.1.2.2.1.3"
	dPort   = "1.3.6.1.2.1.17.4.3.1.2"
	dStatus = "1.3.6.1.2.1.17.4.3.1.3"
)

func pdu(oid string, v any) snmp.PDU { return snmp.PDU{OID: oid, Value: v} }

func TestParseQBridgeFDB(t *testing.T) {
	ports := []snmp.PDU{pdu(qPort+".1.228.56.131.32.245.207", int64(51)), pdu(qPort+".10.10.86.198.140.145.152", int64(48))}
	status := []snmp.PDU{pdu(qStatus+".1.228.56.131.32.245.207", int64(3)), pdu(qStatus+".10.10.86.198.140.145.152", int64(1))}
	got := ParseQBridgeFDB(ports, status)
	want := []FDBEntry{{VLAN: 1, MAC: "e4:38:83:20:f5:cf", BridgePort: 51}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseDot1dFDB(t *testing.T) {
	got := ParseDot1dFDB([]snmp.PDU{pdu(dPort+".0.17.34.51.68.85", int64(7))}, []snmp.PDU{pdu(dStatus+".0.17.34.51.68.85", int64(3))})
	want := []FDBEntry{{VLAN: 0, MAC: "00:11:22:33:44:55", BridgePort: 7}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseBasePorts(t *testing.T) {
	got := ParseBasePorts([]snmp.PDU{pdu("1.3.6.1.2.1.17.1.4.1.2.5", int64(5)), pdu("1.3.6.1.2.1.17.1.4.1.2.78", int64(52))})
	if !reflect.DeepEqual(got, map[int]int{5: 5, 78: 52}) {
		t.Errorf("got %v", got)
	}
}

func TestParseLLDP(t *testing.T) {
	base := "1.0.8802.1.1.2.1.4.1.1"
	rows := map[string][]snmp.PDU{
		"5":  {pdu(base+".5.0.47.2", []byte{0x24, 0x5a, 0x4c, 0x01, 0x02, 0x03})},
		"7":  {pdu(base+".7.0.47.2", []byte("eth0"))},
		"8":  {pdu(base+".8.0.47.2", []byte("uplink"))},
		"9":  {pdu(base+".9.0.47.2", []byte("MBU7IW")), pdu(base+".9.0.34.1", []byte("TS5410R81E"))},
		"12": {pdu(base+".12.0.47.2", []byte{0x10}), pdu(base+".12.0.34.1", []byte{0x80})},
	}
	got := ParseLLDP(rows)
	if len(got) != 2 {
		t.Fatalf("neighbours %+v", got)
	}
	byName := map[string]Neighbor{}
	for _, n := range got {
		byName[n.SysName] = n
	}
	ap := byName["MBU7IW"]
	if ap.LocalPort != 47 || !ap.Caps.AP() || ap.Caps.Bridge() || ap.PortID != "eth0" || ap.PortDescr != "uplink" || ap.ChassisID != "24:5a:4c:01:02:03" {
		t.Errorf("AP %+v", ap)
	}
	if nas := byName["TS5410R81E"]; nas.LocalPort != 34 || nas.Caps.Bridge() || nas.Caps.Router() || nas.Caps.AP() {
		t.Errorf("NAS %+v", nas)
	}
	// Found on the sandbox: UniFi sends its port ID as raw MAC bytes, which
	// are not UTF-8 and must not be stored as text.
	bin := ParseLLDP(map[string][]snmp.PDU{
		"5": {pdu(base+".5.0.48.4", []byte("84:78:48:fe:d0:e8"))},
		"7": {pdu(base+".7.0.48.4", []byte{0x84, 0x78, 0x48, 0xfe, 0xd0, 0xe8})},
		"8": {pdu(base+".8.0.48.4", []byte{0x84, 0xff, 0x01})},
	})
	if len(bin) != 1 || bin[0].PortID != "84:78:48:fe:d0:e8" || bin[0].PortDescr != "84ff01" {
		t.Errorf("binary port ID: %+v", bin)
	}
	// UniFi sends its chassis ID as MAC text.
	text := ParseLLDP(map[string][]snmp.PDU{"5": {pdu(base+".5.0.48.4", []byte("84:78:48:FE:D0:E8"))}})
	if len(text) != 1 || text[0].ChassisID != "84:78:48:fe:d0:e8" || text[0].LocalPort != 48 {
		t.Errorf("text chassis: %+v", text)
	}
	for b, check := range map[byte]func(Caps) bool{0x20: Caps.Bridge, 0x08: Caps.Router, 0x04: Caps.Phone} {
		if !check(Caps(b)) {
			t.Errorf("caps 0x%02x not recognised", b)
		}
	}
}

func TestParseARP(t *testing.T) {
	mac := []byte{0x3c, 0xef, 0x8c, 0x12, 0x9a, 0x41}
	phys := []snmp.PDU{
		pdu("1.3.6.1.2.1.4.35.1.4.13.1.4.10.1.10.60", mac),
		pdu("1.3.6.1.2.1.4.35.1.4.13.2.16.254.128.0.0.0.0.0.0.42.37.95.255.254.204.73.241", mac),
	}
	got := ParseARP(phys, nil)
	want := []ARPEntry{{IP: "10.1.10.60", MAC: "3c:ef:8c:12:9a:41"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("physical: %+v", got)
	}
	got = ParseARP(nil, []snmp.PDU{pdu("1.3.6.1.2.1.4.22.1.2.59.10.1.10.61", mac)})
	if len(got) != 1 || got[0].IP != "10.1.10.61" {
		t.Errorf("media fallback: %+v", got)
	}
	// Review: a physical table holding only IPv6 rows (older Cisco IOS) still
	// falls back to the media table for IPv4.
	got = ParseARP(phys[1:], []snmp.PDU{pdu("1.3.6.1.2.1.4.22.1.2.59.10.1.10.61", mac)})
	if len(got) != 1 || got[0].IP != "10.1.10.61" {
		t.Errorf("IPv6-only physical: %+v", got)
	}
}

func TestNormalizeMAC(t *testing.T) {
	for _, in := range []string{"AA-BB-CC-DD-EE-FF", "aabb.ccdd.eeff", "aabbccddeeff", " aa:bb:cc:dd:ee:ff "} {
		if got, ok := NormalizeMAC(in); !ok || got != "aa:bb:cc:dd:ee:ff" {
			t.Errorf("NormalizeMAC(%q) = %q, %v", in, got, ok)
		}
	}
	if _, ok := NormalizeMAC("zz"); ok {
		t.Error("zz accepted")
	}
	for in, want := range map[string]string{"aa:bb:c": "aa:bb:c", "AABB": "aa:bb", "aa-bb-cc": "aa:bb:cc", "camera": "", "a": ""} {
		if got := MACPrefixForSearch(in); got != want {
			t.Errorf("MACPrefixForSearch(%q) = %q, want %q", in, got, want)
		}
	}
}
