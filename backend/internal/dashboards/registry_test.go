package dashboards

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRegistryKeepsOrderAndRefusesDuplicates(t *testing.T) {
	r := NewRegistry(labelWidget{})
	if got := r.Types(); len(got) != 1 || got[0] != "label" {
		t.Fatalf("Types() = %v, want [label]", got)
	}
	if _, ok := r.Get("nope"); ok {
		t.Error("Get(nope) found a widget")
	}
	defer func() {
		if recover() == nil {
			t.Error("registering a type twice did not panic")
		}
	}()
	NewRegistry(labelWidget{}, labelWidget{})
}

func TestDecodeConfigNamesTheField(t *testing.T) {
	var c struct {
		Devices []uuid.UUID `json:"devices"`
		N       int         `json:"n"`
	}
	for raw, field := range map[string]string{
		`{"devices": "abc"}`:   "devices",
		`{"n": "five"}`:        "n",
		`{"devices": ["abc"]}`: "config",
		`{"devices": [`:        "config",
	} {
		err := decodeConfig(json.RawMessage(raw), &c)
		var fe *FieldError
		if !errors.As(err, &fe) {
			t.Errorf("%s: err = %v, want a FieldError", raw, err)
			continue
		}
		if fe.Field != field {
			t.Errorf("%s: field = %q, want %q", raw, fe.Field, field)
		}
	}
}

func TestRanges(t *testing.T) {
	for _, r := range []string{"1h", "6h", "24h", "7d", "30d", "90d", "1y"} {
		if !ValidRange(r) {
			t.Errorf("ValidRange(%q) = false", r)
		}
	}
	if ValidRange("2h") || ValidRange("") {
		t.Error("ValidRange accepts 2h or empty")
	}
	if effectiveRange("24h", "") != "24h" || effectiveRange("24h", "7d") != "7d" {
		t.Error("effectiveRange does not prefer the override")
	}
	for r, want := range map[string]time.Duration{"1h": time.Minute, "6h": time.Minute, "24h": 5 * time.Minute,
		"7d": 5 * time.Minute, "30d": 15 * time.Minute, "1y": 15 * time.Minute} {
		if got := chartRefresh(r); got != want {
			t.Errorf("chartRefresh(%s) = %v, want %v", r, got, want)
		}
	}
}

func TestDedupeAndCount(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	if got := dedupe([]uuid.UUID{a, b, a, uuid.Nil}); len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("dedupe = %v, want [a b] in order without nil", got)
	}
	if checkCount("devices", 3, 1, 50) != nil || checkCount("devices", 0, 1, 50) == nil || checkCount("devices", 51, 1, 50) == nil {
		t.Error("checkCount bounds are wrong")
	}
}
