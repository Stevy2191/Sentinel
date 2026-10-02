package custommetric

import (
	"reflect"
	"testing"
	"time"
)

func n(v float64) Value { return Value{Num: v, NumOK: true} }
func s(t string) Value  { return Value{Text: t} }

const (
	cpuTable   = "1.3.6.1.4.1.9.9.109.1.1.1.1"
	cpu5min    = cpuTable + ".8"
	cpuPhys    = cpuTable + ".2"
	entName    = "1.3.6.1.2.1.47.1.1.1.1.7"
	sensorType = "1.3.6.1.4.1.9.9.91.1.1.1.1.1"
	sensorPrec = "1.3.6.1.4.1.9.9.91.1.1.1.1.3"
	sensorVal  = "1.3.6.1.4.1.9.9.91.1.1.1.1.4"
	fanState   = "1.3.6.1.4.1.9.9.13.1.4.1.3"
	fanDescr   = "1.3.6.1.4.1.9.9.13.1.4.1.2"
)

// Review focus 2: a CPU with physical index 0 (no entity) still gets a label.
func TestPointerLabelWithFallback(t *testing.T) {
	d := Definition{Name: "CPU busy (5 min)", Source: "column", Kind: "gauge", Scale: 1, OID: cpu5min,
		LabelMode: "pointer", LabelPointerOID: cpuPhys, LabelTargetOID: entName}
	cols := Columns{
		cpu5min: {"1": n(12), "2": n(7)},
		cpuPhys: {"1": n(1000), "2": n(0)},
		entName: {"1000": s("Switch 1")},
	}
	rows := Evaluate(d, cols)
	want := []Row{{Instance: "1", Label: "Switch 1", Value: 12}, {Instance: "2", Label: "Row 2", Value: 7}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows %+v", rows)
	}
}

// ENTITY-SENSOR: only celsius rows, value / 10^precision, label by entPhysicalName.
func TestFilterPrecisionSameIndex(t *testing.T) {
	d := Definition{Source: "column", Kind: "gauge", Scale: 1, OID: sensorVal, PrecisionOID: sensorPrec,
		FilterOID: sensorType, FilterValues: []string{"8"}, LabelMode: "same_index", LabelOID: entName}
	cols := Columns{
		sensorVal:  {"1010": n(415), "1011": n(12000)},
		sensorPrec: {"1010": n(1), "1011": n(3)},
		sensorType: {"1010": n(8), "1011": n(4)},
		entName:    {"1010": s("Switch 1 - Inlet Temp Sensor"), "1011": s("Switch 1 - 12V")},
	}
	rows := Evaluate(d, cols)
	if len(rows) != 1 || rows[0].Instance != "1010" || rows[0].Value != 41.5 || rows[0].Label != "Switch 1 - Inlet Temp Sensor" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestStatusStates(t *testing.T) {
	d := Definition{Source: "column", Kind: "status", Scale: 1, OID: fanState, LabelMode: "column", LabelOID: fanDescr,
		OKStates: []int64{1}, StateNames: map[int64]string{1: "normal", 3: "critical"}}
	rows := Evaluate(d, Columns{fanState: {"1": n(1), "2": n(3), "3": n(9)}, fanDescr: {"1": s("Fan 1"), "2": s("Fan 2")}})
	if len(rows) != 3 || !rows[0].OK || rows[0].State != "normal" || rows[1].OK || rows[1].State != "critical" ||
		rows[2].State != "9" || rows[2].Label != "Row 3" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestUsedFreePct(t *testing.T) {
	used, free := "1.3.6.1.4.1.9.9.221.1.1.1.1.18", "1.3.6.1.4.1.9.9.221.1.1.1.1.20"
	d := Definition{Source: "used_free_pct", Kind: "gauge", Scale: 1, OID: used, OID2: free, LabelMode: "index"}
	rows := Evaluate(d, Columns{used: {"1.1": n(300), "1.2": n(0)}, free: {"1.1": n(100), "1.2": n(0)}})
	if len(rows) != 1 || rows[0].Instance != "1.1" || rows[0].Value != 75 || rows[0].Label != "1.1" {
		t.Fatalf("rows %+v", rows) // 1.2 has used+free = 0: no value
	}
}

func TestScalarAndScale(t *testing.T) {
	d := Definition{Source: "scalar", Kind: "gauge", Scale: 0.1, OID: "1.3.6.1.4.1.1.2", LabelMode: "index"}
	every, cached := Needed(d)
	if !reflect.DeepEqual(every, []string{"1.3.6.1.4.1.1.2.0"}) || len(cached) != 0 {
		t.Fatalf("needed %v %v", every, cached)
	}
	rows := Evaluate(d, Columns{"1.3.6.1.4.1.1.2.0": {"": n(235)}})
	if len(rows) != 1 || rows[0].Instance != "" || rows[0].Value != 23.5 {
		t.Fatalf("rows %+v", rows)
	}
}

func TestNeeded(t *testing.T) {
	d := Definition{Source: "column", OID: sensorVal, PrecisionOID: sensorPrec, FilterOID: sensorType, LabelMode: "pointer",
		LabelPointerOID: cpuPhys, LabelTargetOID: entName}
	every, cached := Needed(d)
	if !reflect.DeepEqual(every, []string{sensorVal}) || !reflect.DeepEqual(cached, []string{sensorPrec, sensorType, cpuPhys, entName}) {
		t.Errorf("every %v cached %v", every, cached)
	}
}

func TestRates(t *testing.T) {
	t0 := time.Unix(1000, 0)
	rows := []Row{{Instance: "1", Value: 100}, {Instance: "2", Value: 4294967000}}
	out, prev := Rates(nil, rows, t0)
	if len(out) != 0 {
		t.Fatalf("first sight produced %+v", out)
	}
	out, prev = Rates(prev, []Row{{Instance: "1", Value: 700}, {Instance: "2", Value: 704}}, t0.Add(60*time.Second))
	if len(out) != 2 || out[0].Value != 10 || out[1].Value != 1000.0/60 {
		t.Fatalf("rates %+v", out) // row 2 wrapped at 2^32: (704 + 2^32 - 4294967000) / 60
	}
	out, _ = Rates(prev, []Row{{Instance: "1", Value: 5e12}}, t0.Add(120*time.Second))
	if len(out) != 1 {
		t.Fatalf("64-bit growth %+v", out)
	}
	big := map[string]Sample{"1": {Value: 5e12, At: t0}}
	if out, _ := Rates(big, []Row{{Instance: "1", Value: 10}}, t0.Add(60*time.Second)); len(out) != 0 {
		t.Errorf("reboot produced %+v", out)
	}
}
