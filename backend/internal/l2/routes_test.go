package l2

import (
	"reflect"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

func TestParseDefaultRoutesCidr(t *testing.T) {
	c := OIDCidrRouteIfIndexDefault
	got := ParseDefaultRoutes([]snmp.PDU{pdu(c+".0.0.0.0.0.10.0.0.1", int64(13))}, nil)
	if want := []int{13}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseDefaultRoutes = %v, want %v", got, want)
	}
}

func TestParseDefaultRoutesOldTable(t *testing.T) {
	r := OIDRouteIfIndex
	udm := []snmp.PDU{pdu(r+".10.1.1.0", int64(58)), pdu(r+".108.204.168.0", int64(13))}
	if got := ParseDefaultRoutes(nil, udm); len(got) != 0 {
		t.Errorf("UDM-SE's connected routes gave default routes %v", got)
	}
	withDefault := append(udm, pdu(r+".0.0.0.0", int64(13)))
	if got, want := ParseDefaultRoutes(nil, withDefault), []int{13}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseDefaultRoutes = %v, want %v", got, want)
	}
	if got := ParseDefaultRoutes([]snmp.PDU{pdu(OIDCidrRouteIfIndexDefault+".0.0.0.0.0.1.1.1.1", int64(0))}, withDefault); len(got) != 0 {
		t.Errorf("a cidr default route on ifIndex 0 gave %v; the old table is only read when cidr has no rows", got)
	}
}

func TestParseAddresses(t *testing.T) {
	a := OIDIPAdEntIfIndex
	got := ParseAddresses([]snmp.PDU{pdu(a+".108.204.170.224", int64(13)), pdu(a+".10.99.1.1", int64(58)), pdu(a+".127.0.0.1", int64(1))})
	want := map[int][]string{13: {"108.204.170.224"}, 58: {"10.99.1.1"}, 1: {"127.0.0.1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseAddresses = %v, want %v", got, want)
	}
}

func TestIsPublicIPv4(t *testing.T) {
	for ip, want := range map[string]bool{
		"108.204.170.224": true, "100.64.3.4": true, "8.8.8.8": true,
		"10.99.1.1": false, "172.20.0.1": false, "192.168.8.1": false, "127.0.0.1": false,
		"169.254.1.1": false, "0.0.0.0": false, "224.0.0.1": false, "255.255.255.255": false, "x": false, "::1": false,
	} {
		if got := IsPublicIPv4(ip); got != want {
			t.Errorf("IsPublicIPv4(%q) = %v, want %v", ip, got, want)
		}
	}
}
