package services

import (
	"context"
	"errors"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type walkFake struct {
	walks map[string][]snmp.PDU
	gets  map[string]any
	fail  map[string]bool
}

func (f *walkFake) Get(_ context.Context, _ snmp.Target, oids []string) ([]snmp.PDU, error) {
	out := make([]snmp.PDU, len(oids))
	for i, o := range oids {
		out[i] = snmp.PDU{OID: o, Value: f.gets[o]}
	}
	return out, nil
}
func (f *walkFake) Walk(_ context.Context, _ snmp.Target, root string) ([]snmp.PDU, error) {
	if f.fail[root] {
		return nil, errors.New("request timeout")
	}
	return f.walks[root], nil
}

func TestReadColumns(t *testing.T) {
	cpu := "1.3.6.1.4.1.9.9.109.1.1.1.1.8"
	f := &walkFake{
		walks: map[string][]snmp.PDU{cpu: {{OID: cpu + ".1", Value: uint64(12)}, {OID: cpu + ".2", Value: uint64(7)}}},
		gets:  map[string]any{"1.3.6.1.2.1.1.5.0": []byte("core")},
		fail:  map[string]bool{"1.3.6.1.4.1.9.9.13.1.4.1.3": true},
	}
	cols, errs := ReadColumns(context.Background(), f, snmp.Target{}, []string{cpu, "1.3.6.1.2.1.1.5.0", "1.3.6.1.4.1.9.9.13.1.4.1.3", "1.3.6.1.4.1.9.9.48.1.1.1.5"})
	if cols[cpu]["1"].Num != 12 || cols[cpu]["2"].Num != 7 || cols["1.3.6.1.2.1.1.5.0"][""].Text != "core" {
		t.Errorf("cols %+v", cols)
	}
	if errs["1.3.6.1.4.1.9.9.13.1.4.1.3"] == nil || len(errs) != 1 {
		t.Errorf("errs %v", errs)
	}
	if v, ok := cols["1.3.6.1.4.1.9.9.48.1.1.1.5"]; !ok || len(v) != 0 {
		t.Errorf("unsupported table should be empty, got %v", v)
	}
}

func TestValueOf(t *testing.T) {
	if v := ValueOf(snmp.PDU{Value: int64(-5)}); !v.NumOK || v.Num != -5 {
		t.Errorf("int %+v", v)
	}
	if v := ValueOf(snmp.PDU{Value: []byte{0x00, 0x1a, 0xff}}); v.Text != "00:1a:ff" {
		t.Errorf("binary %+v", v)
	}
	if v := ValueOf(snmp.PDU{Value: nil}); v.NumOK || v.Text != "" {
		t.Errorf("nil %+v", v)
	}
}
