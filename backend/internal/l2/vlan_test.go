package l2

import (
	"reflect"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

func vlanStates() []snmp.PDU {
	s := OIDVtpVlanState
	return []snmp.PDU{pdu(s+".1.1", int64(1)), pdu(s+".1.20", int64(1)), pdu(s+".1.30", int64(2)), pdu(s+".1.1002", int64(1)), pdu(s+".1.60", int64(1))}
}

// Operational ethernet VLANs only: 30 is suspended, 1002 is the reserved
// FDDI default, 60 isn't ethernet.
func TestParseVLANs(t *testing.T) {
	k := OIDVtpVlanType
	kind := []snmp.PDU{pdu(k+".1.1", int64(1)), pdu(k+".1.20", int64(1)), pdu(k+".1.30", int64(1)), pdu(k+".1.1002", int64(2)), pdu(k+".1.60", int64(4))}
	if got, want := ParseVLANs(vlanStates(), kind), []int{1, 20}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseVLANs = %v, want %v", got, want)
	}
}

func TestParseVLANsWithoutTypes(t *testing.T) {
	if got, want := ParseVLANs(vlanStates(), nil), []int{1, 20, 60}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseVLANs = %v, want %v", got, want)
	}
}

func TestParseVLANsEmpty(t *testing.T) {
	if got := ParseVLANs(nil, nil); len(got) != 0 {
		t.Errorf("ParseVLANs = %v, want none", got)
	}
}
