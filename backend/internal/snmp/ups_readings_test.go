package snmp

import (
	"context"
	"errors"
	"testing"
)

func upsPDUs(vals map[string]any) []PDU {
	out := make([]PDU, len(UPSReadingOIDs))
	for i, o := range UPSReadingOIDs {
		out[i] = PDU{OID: o, Value: vals[o]}
	}
	return out
}

func TestParseUPSReadings(t *testing.T) {
	const u = "1.3.6.1.2.1.33.1"
	r := ParseUPSReadings(upsPDUs(map[string]any{
		u + ".2.1.0": int64(2), u + ".2.2.0": int64(0), u + ".2.3.0": int64(41), u + ".2.4.0": int64(96),
		u + ".2.7.0": int64(-5), u + ".3.3.1.3.1": int64(121), u + ".4.1.0": int64(3),
		u + ".4.4.1.2.1": int64(120), u + ".4.4.1.5.1": uint64(34),
	}))
	if r.BatteryStatus != 2 || r.OutputSource != 3 || *r.SecondsOnBattery != 0 || *r.RuntimeMin != 41 ||
		*r.ChargePct != 96 || *r.BatteryTempC != -5 || *r.InputV != 121 || *r.OutputV != 120 || *r.LoadPct != 34 {
		t.Errorf("readings %+v", r)
	}
}

// noSuchObject (nil) readings are absent, never zero.
func TestParseUPSReadingsAbsent(t *testing.T) {
	const u = "1.3.6.1.2.1.33.1"
	r := ParseUPSReadings(upsPDUs(map[string]any{u + ".2.4.0": int64(80)}))
	if r.ChargePct == nil || *r.ChargePct != 80 {
		t.Fatalf("charge %+v", r.ChargePct)
	}
	if r.BatteryStatus != 0 || r.OutputSource != 0 || r.LoadPct != nil || r.BatteryTempC != nil || r.SecondsOnBattery != nil {
		t.Errorf("absent readings filled in: %+v", r)
	}
}

type upsGetFake struct {
	vals  map[string]any
	calls int
	fail  bool // a multi-OID GET fails (v1 noSuchName)
}

func (f *upsGetFake) Get(_ context.Context, _ Target, oids []string) ([]PDU, error) {
	f.calls++
	if f.fail && len(oids) > 1 {
		return nil, errors.New("agent returned NoSuchName")
	}
	out := make([]PDU, 0, len(oids))
	for _, o := range oids {
		v, ok := f.vals[o]
		if !ok && f.fail {
			return nil, errors.New("agent returned NoSuchName")
		}
		out = append(out, PDU{OID: o, Value: v})
	}
	return out, nil
}
func (f *upsGetFake) Walk(context.Context, Target, string) ([]PDU, error) { return nil, nil }

func TestReadUPSOneRequest(t *testing.T) {
	f := &upsGetFake{vals: map[string]any{"1.3.6.1.2.1.33.1.4.4.1.5.1": int64(50)}}
	r, err := ReadUPS(context.Background(), f, Target{Credential: Credential{Version: "2c"}})
	if err != nil || f.calls != 1 || r.LoadPct == nil || *r.LoadPct != 50 {
		t.Fatalf("r %+v err %v calls %d", r, err, f.calls)
	}
}

// v1 fails a whole GET when one OID is missing; each OID is then asked alone.
func TestReadUPSv1FallsBackPerOID(t *testing.T) {
	f := &upsGetFake{fail: true, vals: map[string]any{"1.3.6.1.2.1.33.1.2.4.0": int64(77)}}
	r, err := ReadUPS(context.Background(), f, Target{Credential: Credential{Version: "1"}})
	if err != nil || r.ChargePct == nil || *r.ChargePct != 77 || f.calls != 1+len(UPSReadingOIDs) {
		t.Fatalf("r %+v err %v calls %d", r, err, f.calls)
	}
}

// v2c/v3: a failed GET is an error (timeout), not retried per OID.
func TestReadUPSv2Error(t *testing.T) {
	f := &upsGetFake{fail: true}
	if _, err := ReadUPS(context.Background(), f, Target{Credential: Credential{Version: "2c"}}); err == nil || f.calls != 1 {
		t.Fatalf("err %v calls %d", err, f.calls)
	}
}
