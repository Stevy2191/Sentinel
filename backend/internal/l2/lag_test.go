package l2

import (
	"reflect"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

func TestParseLAGDot3ad(t *testing.T) {
	r := OIDDot3adAttachedAgg
	got := ParseLAG([]snmp.PDU{pdu(r+".1", int64(0)), pdu(r+".49", int64(66)), pdu(r+".50", int64(66))}, nil, func(int) bool { return false })
	if want := map[int]int{49: 66, 50: 66}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseLAG = %v, want %v", got, want)
	}
}

func TestParseLAGFallsBackToIfStack(t *testing.T) {
	r, s := OIDDot3adAttachedAgg, OIDIfStackStatus
	dot3ad := []snmp.PDU{pdu(r+".49", int64(0)), pdu(r+".50", int64(0))}
	stack := []snmp.PDU{pdu(s+".66.49", int64(1)), pdu(s+".66.50", int64(1)), pdu(s+".66.51", int64(2)), pdu(s+".0.66", int64(1))}
	got := ParseLAG(dot3ad, stack, func(ix int) bool { return ix == 66 })
	if want := map[int]int{49: 66, 50: 66}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseLAG = %v, want %v", got, want)
	}
}
