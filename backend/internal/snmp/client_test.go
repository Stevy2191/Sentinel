package snmp

import (
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
)

func TestBuildGoSNMP(t *testing.T) {
	base := Target{Host: "10.0.0.2", Port: 161, Timeout: 3 * time.Second, Retries: 1}

	v2 := base
	v2.Credential = Credential{Version: "2c", Community: "public"}
	g, err := buildGoSNMP(v2)
	if err != nil || g.Version != gosnmp.Version2c || g.Community != "public" || g.Port != 161 || g.Retries != 1 {
		t.Fatalf("v2c: %+v %v", g, err)
	}

	v1 := base
	v1.Credential = Credential{Version: "1", Community: "radios"}
	if g, err := buildGoSNMP(v1); err != nil || g.Version != gosnmp.Version1 {
		t.Fatalf("v1: %+v %v", g, err)
	}

	cases := []struct {
		cred  Credential
		flags gosnmp.SnmpV3MsgFlags
		auth  gosnmp.SnmpV3AuthProtocol
		priv  gosnmp.SnmpV3PrivProtocol
	}{
		{Credential{Version: "3", Username: "mon", AuthProtocol: "none", PrivProtocol: "none"}, gosnmp.NoAuthNoPriv, gosnmp.NoAuth, gosnmp.NoPriv},
		{Credential{Version: "3", Username: "mon", AuthProtocol: "SHA256", AuthPassword: "longenough", PrivProtocol: "none"}, gosnmp.AuthNoPriv, gosnmp.SHA256, gosnmp.NoPriv},
		{Credential{Version: "3", Username: "mon", AuthProtocol: "MD5", AuthPassword: "longenough", PrivProtocol: "DES", PrivPassword: "longenough"}, gosnmp.AuthPriv, gosnmp.MD5, gosnmp.DES},
		{Credential{Version: "3", Username: "mon", AuthProtocol: "SHA512", AuthPassword: "longenough", PrivProtocol: "AES256", PrivPassword: "longenough"}, gosnmp.AuthPriv, gosnmp.SHA512, gosnmp.AES256},
	}
	for _, tc := range cases {
		tg := base
		tg.Credential = tc.cred
		g, err := buildGoSNMP(tg)
		if err != nil {
			t.Fatalf("%+v: %v", tc.cred, err)
		}
		usm := g.SecurityParameters.(*gosnmp.UsmSecurityParameters)
		if g.Version != gosnmp.Version3 || g.MsgFlags != tc.flags || usm.UserName != "mon" ||
			usm.AuthenticationProtocol != tc.auth || usm.PrivacyProtocol != tc.priv {
			t.Errorf("%+v: flags %v auth %v priv %v", tc.cred, g.MsgFlags, usm.AuthenticationProtocol, usm.PrivacyProtocol)
		}
	}

	bad := base
	bad.Credential = Credential{Version: "3", Username: "mon", AuthProtocol: "SHA1"}
	if _, err := buildGoSNMP(bad); err == nil {
		t.Error("unknown auth protocol accepted")
	}
	bad.Credential = Credential{Version: "4"}
	if _, err := buildGoSNMP(bad); err == nil {
		t.Error("unknown version accepted")
	}
}

func TestToPDU(t *testing.T) {
	for _, tc := range []struct {
		in   gosnmp.SnmpPDU
		want any
	}{
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.1.5.0", Type: gosnmp.OctetString, Value: []byte("sw")}, []byte("sw")},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.1.2.0", Type: gosnmp.ObjectIdentifier, Value: ".1.3.6.1.4.1.9"}, "1.3.6.1.4.1.9"},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.1.3.0", Type: gosnmp.TimeTicks, Value: uint32(500)}, uint64(500)},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.2.2.1.8.1", Type: gosnmp.Integer, Value: 1}, int64(1)},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.31.1.1.1.6.1", Type: gosnmp.Counter64, Value: uint64(1 << 40)}, uint64(1 << 40)},
		{gosnmp.SnmpPDU{Name: ".1.3.6.1.2.1.1.9.0", Type: gosnmp.NoSuchObject, Value: nil}, nil},
	} {
		got := toPDU(tc.in)
		if got.OID[0] == '.' {
			t.Errorf("OID kept its leading dot: %q", got.OID)
		}
		switch want := tc.want.(type) {
		case []byte:
			if string(got.Value.([]byte)) != string(want) {
				t.Errorf("%s: %v", tc.in.Name, got.Value)
			}
		default:
			if got.Value != tc.want {
				t.Errorf("%s: got %#v want %#v", tc.in.Name, got.Value, tc.want)
			}
		}
	}
}
