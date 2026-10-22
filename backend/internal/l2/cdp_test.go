package l2

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

func TestParseCDP(t *testing.T) {
	c := OIDCDPCache
	cols := map[string][]snmp.PDU{
		"3": {pdu(c+".3.10101.3", int64(1)), pdu(c+".3.10102.1", int64(20))},
		"4": {pdu(c+".4.10101.3", []byte{10, 20, 0, 5}), pdu(c+".4.10102.1", []byte{1, 2})},
		"6": {pdu(c+".6.10101.3", []byte("LAB-C3850.corp.example(FOC1234)")), pdu(c+".6.10102.1", []byte("SEP001122334455"))},
		"7": {pdu(c+".7.10101.3", []byte("GigabitEthernet1/0/1")), pdu(c+".7.10102.1", []byte("Port 1"))},
		"8": {pdu(c+".8.10101.3", []byte("cisco WS-C3850-12S")), pdu(c+".8.10102.1", []byte("Cisco IP Phone 7841"))},
	}
	want := []CDPNeighbor{
		{IfIndex: 10101, DeviceID: "LAB-C3850.corp.example(FOC1234)", Key: "LAB-C3850.corp.example(FOC1234)", SysName: "LAB-C3850", Port: "GigabitEthernet1/0/1", Platform: "cisco WS-C3850-12S", Address: "10.20.0.5"},
		{IfIndex: 10102, DeviceID: "SEP001122334455", Key: "SEP001122334455", SysName: "SEP001122334455", Port: "Port 1", Platform: "Cisco IP Phone 7841"},
	}
	if got := ParseCDP(cols); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseCDP =\n %+v\nwant\n %+v", got, want)
	}
}

// cdpCacheCapabilities as the walk showed them, onto LLDP's bits.
func TestCDPCaps(t *testing.T) {
	phone := cdpCaps([]byte{0, 0, 4, 0x90}) // host, phone, two-port MAC relay
	if phone != 0x05 || !phone.Phone() || phone.Router() || phone.Bridge() {
		t.Errorf("phone = %#x", phone)
	}
	catalyst := cdpCaps([]byte{0, 0, 0, 0x29}) // router, switch, IGMP
	if catalyst != 0x28 || !catalyst.Bridge() || !catalyst.Router() {
		t.Errorf("catalyst = %#x", catalyst)
	}
	if got := cdpCaps([]byte{0, 0, 0, 0x10}); got != 0x01 { // UniFi: host
		t.Errorf("host = %#x, want station 0x01", got)
	}
	if got := cdpCaps([]byte{0, 0, 0, 0x01}); got != 0x08 { // MikroTik: router
		t.Errorf("router = %#x, want 0x08", got)
	}
	if got := cdpCaps(nil); got != 0 {
		t.Errorf("none = %#x", got)
	}
}

func cdpRow(ifIndex, n int, deviceID string, addr []byte, caps []byte) map[string][]snmp.PDU {
	c, ix := OIDCDPCache, fmt.Sprintf(".%d.%d", ifIndex, n)
	return map[string][]snmp.PDU{
		"3": {pdu(c+".3"+ix, int64(1))}, "4": {pdu(c+".4"+ix, addr)},
		"6": {pdu(c+".6"+ix, []byte(deviceID))}, "9": {pdu(c+".9"+ix, caps)},
	}
}

func mergeCols(rows ...map[string][]snmp.PDU) map[string][]snmp.PDU {
	out := map[string][]snmp.PDU{}
	for _, r := range rows {
		for col, pdus := range r {
			out[col] = append(out[col], pdus...)
		}
	}
	return out
}

func TestParseCDPIPDeviceID(t *testing.T) {
	cols := mergeCols(cdpRow(1, 1, "10.99.1.1", nil, nil), cdpRow(2, 1, "core.corp.example", nil, nil), cdpRow(3, 1, "SW(FOC123)", nil, nil))
	got := map[int]string{}
	for _, n := range ParseCDP(cols) {
		got[n.IfIndex] = n.SysName
	}
	if want := map[int]string{1: "10.99.1.1", 2: "core", 3: "SW"}; !reflect.DeepEqual(got, want) {
		t.Errorf("system names = %v, want %v", got, want)
	}
}

// Every UniFi device says it is "UBNT": two on one port are kept apart by
// their address; one alone on its port keeps the plain key.
func TestParseCDPUniFiApart(t *testing.T) {
	host := []byte{0, 0, 0, 0x10}
	cols := mergeCols(cdpRow(5, 1, "UBNT", []byte{10, 0, 5, 1}, host), cdpRow(5, 2, "UBNT", []byte{10, 0, 5, 2}, host), cdpRow(6, 1, "UBNT", []byte{10, 0, 5, 3}, host))
	var keys []string
	for _, n := range ParseCDP(cols) {
		keys = append(keys, n.Key)
		if n.SysName != "UBNT" || n.Caps != 0x01 {
			t.Errorf("%s: sys name %q caps %#x", n.Key, n.SysName, n.Caps)
		}
	}
	if want := []string{"UBNT@10.0.5.1", "UBNT@10.0.5.2", "UBNT"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
}
