package portmon

import (
	"reflect"
	"sort"
	"testing"
)

func physicalIndexes(ifs []fixtureIf) []int {
	var out []int
	for _, f := range ifs {
		if IsPhysical(info(f)) {
			out = append(out, f.Index)
		}
	}
	sort.Ints(out)
	return out
}

func TestIsPhysicalOnRealGear(t *testing.T) {
	var usw []int
	for i := 1; i <= 52; i++ {
		usw = append(usw, i)
	}
	cases := map[string]struct {
		ifs  []fixtureIf
		want []int
	}{
		"USW-Pro-48": {uswPro48(), usw},
		"UDM-SE":     {udmSE(), []int{3, 4, 13, 16, 17, 18, 19, 20, 21, 22, 23}},
		"U7-Pro":     {u7Pro(), []int{3}},
		"UNVR":       {unvr(), []int{2, 3}},
	}
	for name, c := range cases {
		if got := physicalIndexes(c.ifs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s physical = %v, want %v", name, got, c.want)
		}
	}
}

func TestDefaultCollect(t *testing.T) {
	// Link aggregates are collected but are not physical ports.
	lag := IfInfo{Name: "3/1", Descr: "Link Aggregate 1", Type: 161}
	if !DefaultCollect(lag) || IsPhysical(lag) {
		t.Error("LAG: want collected, not physical")
	}
	// The CPU interface and virtual interfaces are not collected.
	for _, i := range []IfInfo{
		{Name: "CPU Interface:  5/1", Type: 1},
		{Name: "br0", Type: 6},
		{Name: "eth10.100", Type: 6},
		{Name: "switch0", Type: 6},
		{Name: "wifi0ap0", Type: 6},
	} {
		if DefaultCollect(i) {
			t.Errorf("%q collected by default", i.Name)
		}
	}
	// ifConnectorPresent decides for an Ethernet-typed interface with an
	// ordinary name, either way.
	yes, no := true, false
	if !IsPhysical(IfInfo{Name: "port7", Type: 1, ConnectorPresent: &yes}) {
		t.Error("connector present on a non-Ethernet type should still be physical")
	}
	if IsPhysical(IfInfo{Name: "eth3", Type: 6, ConnectorPresent: &no}) {
		t.Error("connector absent should not be physical")
	}
	// A virtual name wins even if the agent claims a connector.
	if IsPhysical(IfInfo{Name: "br0", Type: 6, ConnectorPresent: &yes}) {
		t.Error("bridge claiming a connector should not be physical")
	}
}

func TestDetectDeviceType(t *testing.T) {
	cases := []struct {
		model string
		ports int
		want  string
	}{
		{"USW-Pro-48-PoE", 52, "switch"},
		{"UBNT-US48PRO-POE", 52, "switch"}, // what the USW's ENTITY-MIB reports: falls back to port count
		{"ES-48-500W", 52, "switch"},
		{"EdgeSwitch 24", 26, "switch"},
		{"US-8-60W", 8, "switch"},
		{"UDM-SE", 11, "router"},
		{"UXG-Pro", 4, "router"},
		{"USG-3P", 3, "router"},
		{"ER-4", 4, "router"},
		{"U7-Pro", 1, "access_point"},
		{"U6-LR", 1, "access_point"},
		{"UAP-AC-Pro", 1, "access_point"},
		{"UNVR4", 2, "nvr"},
		{"WS-C2960X-24TS-L", 26, "switch"},
		{"", 2, "other"},
		{"Cambium ePMP", 2, "other"},
	}
	for _, c := range cases {
		if got := DetectDeviceType(c.model, c.ports); got != c.want {
			t.Errorf("DetectDeviceType(%q, %d) = %q, want %q", c.model, c.ports, got, c.want)
		}
	}
}

func TestUsualSpeed(t *testing.T) {
	counts := map[int64]int{1_000_000_000: 1900, 100_000_000: 116}
	if got := UsualSpeed(counts, 168); got != 1_000_000_000 {
		t.Errorf("usual = %d", got)
	}
	if got := UsualSpeed(counts, 23.9); got != 0 {
		t.Errorf("under 24 h of history must give 0, got %d", got)
	}
	// A tie picks the faster speed: an equally common slower speed is the
	// anomaly, not the norm.
	if got := UsualSpeed(map[int64]int{1e9: 5, 1e8: 5}, 48); got != 1e9 {
		t.Errorf("tie = %d", got)
	}
	if got := UsualSpeed(map[int64]int{}, 48); got != 0 {
		t.Errorf("empty = %d", got)
	}
}
