package models

import (
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"
)

// NOT NULL JSONB columns must never receive SQL NULL from a zero value.
func TestJSONTypesStoreEmptyNotNull(t *testing.T) {
	for name, v := range map[string]driver.Valuer{
		"ConditionSet": ConditionSet(nil),
		"TimeMap":      TimeMap(nil),
		"JSONMap":      JSONMap(nil),
	} {
		got, err := v.Value()
		if err != nil || got == nil {
			t.Errorf("%s(nil).Value() = %v, %v; want empty JSON", name, got, err)
		}
	}
	if got, _ := IntSlice(nil).Value(); got != nil {
		t.Errorf("IntSlice(nil) should store NULL (no override), got %v", got)
	}
}

func TestTimeMapRoundTrip(t *testing.T) {
	at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	raw, err := TimeMap{"errors": at}.Value()
	if err != nil {
		t.Fatal(err)
	}
	var back TimeMap
	if err := back.Scan(raw); err != nil || !back["errors"].Equal(at) {
		t.Fatalf("round trip: %v %v", back, err)
	}
}

// Opt distinguishes "absent" from "null" from a value.
func TestOptDecoding(t *testing.T) {
	var body struct {
		A Opt[int] `json:"a"`
		B Opt[int] `json:"b"`
		C Opt[int] `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"b": null, "c": 7}`), &body); err != nil {
		t.Fatal(err)
	}
	if body.A.Set {
		t.Error("absent field reported as set")
	}
	if !body.B.Set || body.B.Value != nil {
		t.Errorf("null field: %+v", body.B)
	}
	if !body.C.Set || body.C.Value == nil || *body.C.Value != 7 {
		t.Errorf("value field: %+v", body.C)
	}
}
