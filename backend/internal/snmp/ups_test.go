package snmp

import (
	"context"
	"strings"
	"testing"
)

// tripplite answers like the user's Tripp Lite SMART1500RM2UN card: a system
// group and UPS-MIB identity, but no ifTable and no ENTITY-MIB.
type tripplite struct{}

func (tripplite) Get(_ context.Context, _ Target, oids []string) ([]PDU, error) {
	vals := map[string]any{
		OIDSysDescr:          []byte("Ubuntu 18.04 Linux 4.4.31 software: PowerAlert 20.2.1 (Build 942)"),
		OIDSysObjectID:       "1.3.6.1.4.1.850.1.1.1",
		OIDSysName:           []byte("EXP-BEB-SMART1500"),
		oidUPSIdent + ".1.0": []byte("TRIPP LITE"),
		oidUPSIdent + ".2.0": []byte("SMART1500RM2UN"),
	}
	out := make([]PDU, len(oids))
	for i, o := range oids {
		out[i] = PDU{OID: o, Value: vals[o]}
	}
	return out, nil
}

func (tripplite) Walk(context.Context, Target, string) ([]PDU, error) { return nil, nil }

// A UPS with no ENTITY-MIB takes its model from UPS-MIB, and Tripp Lite's
// enterprise number is named.
func TestReadInventoryUPSIdent(t *testing.T) {
	inv, err := ReadInventory(context.Background(), tripplite{}, Target{})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Vendor != "Tripp Lite" || inv.Model != "SMART1500RM2UN" || len(inv.Interfaces) != 0 {
		t.Errorf("vendor %q model %q interfaces %d", inv.Vendor, inv.Model, len(inv.Interfaces))
	}
}

func TestParseUPSIdent(t *testing.T) {
	maker, model := ParseUPSIdent([]PDU{{OID: oidUPSIdent + ".1.0", Value: []byte(" EATON ")},
		{OID: oidUPSIdent + ".2.0", Value: []byte("9PX2000RT")}})
	if maker != "EATON" || model != "9PX2000RT" {
		t.Errorf("got %q %q", maker, model)
	}
	// Not a UPS: the agent answers noSuchObject (nil).
	if m, mo := ParseUPSIdent([]PDU{{OID: oidUPSIdent + ".1.0"}, {OID: oidUPSIdent + ".2.0"}}); m != "" || mo != "" {
		t.Errorf("not a UPS: got %q %q", m, mo)
	}
	// An unknown enterprise falls back to the manufacturer UPS-MIB reports.
	if v := upsVendor("Unknown (enterprise 99999)", "ACME POWER"); !strings.EqualFold(v, "ACME POWER") {
		t.Errorf("unknown enterprise vendor %q", v)
	}
	if v := upsVendor("Eaton", "EATON"); v != "Eaton" {
		t.Errorf("known vendor replaced: %q", v)
	}
}
