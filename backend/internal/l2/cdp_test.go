package l2

import (
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
		{IfIndex: 10101, DeviceID: "LAB-C3850.corp.example(FOC1234)", SysName: "LAB-C3850", Port: "GigabitEthernet1/0/1", Platform: "cisco WS-C3850-12S", Address: "10.20.0.5"},
		{IfIndex: 10102, DeviceID: "SEP001122334455", SysName: "SEP001122334455", Port: "Port 1", Platform: "Cisco IP Phone 7841"},
	}
	if got := ParseCDP(cols); !reflect.DeepEqual(got, want) {
		t.Errorf("ParseCDP =\n %+v\nwant\n %+v", got, want)
	}
}
