package portmon

import "testing"

func TestUnitNumber(t *testing.T) {
	cases := []struct {
		name, descr string
		want        int
	}{
		{"1/0/12", "", 1},
		{"Gi2/0/12", "", 2},
		{"0/12", "", 0},                              // two-part EdgeSwitch/UniFi name
		{"", "Unit: 2 Slot: 0 Port: 12", 2},          // ifDescr-only unit
		{"", "CPU Interface for Slot: 5 Port: 1", 0}, // no "Unit:" word
		{"eth0", "", 0},                              // no number pattern at all
		{"3/26", "Link Aggregate 26", 0},             // two-part, not a stack unit
	}
	for _, c := range cases {
		if got := UnitNumber(c.name, c.descr); got != c.want {
			t.Errorf("UnitNumber(%q, %q) = %d, want %d", c.name, c.descr, got, c.want)
		}
	}
}

func stackedIfInfos() []IfInfo {
	var out []IfInfo
	for _, f := range stackedSwitch() {
		out = append(out, info(f))
	}
	return out
}

func TestStackUnitsDetectsAStack(t *testing.T) {
	infos := stackedIfInfos()
	units := StackUnits(infos)
	if len(units) != len(infos) {
		t.Fatalf("units %d, want %d", len(units), len(infos))
	}
	// First 48 are unit 1, next 4 are unit 1's SFPs, then unit 2's 48 + 4.
	if units[0] != 1 || units[47] != 1 || units[51] != 1 {
		t.Errorf("unit 1 ports: %v", units[:52])
	}
	if units[52] != 2 || units[99] != 2 || units[103] != 2 {
		t.Errorf("unit 2 ports: %v", units[52:])
	}
}

func TestStackUnitsNotStacked(t *testing.T) {
	var infos []IfInfo
	for _, f := range uswPro48() {
		infos = append(infos, info(f))
	}
	units := StackUnits(infos)
	for i, u := range units {
		if u != 0 {
			t.Fatalf("port %d: unit %d, want 0 (not a stack)", i, u)
		}
	}
}
