package portmon

import (
	"reflect"
	"testing"
)

func numbers(ps []FacePort) []int {
	out := make([]int, len(ps))
	for i, p := range ps {
		out[i] = p.Number
	}
	return out
}

func TestPortNumber(t *testing.T) {
	cases := []struct {
		name, descr string
		idx, want   int
	}{
		{"0/12", "", 12, 12},
		{"1/0/12", "", 99, 12},
		{"Gi1/0/7", "", 10107, 7},
		{"Port 12", "", 3, 12},
		{"eth12", "", 40, 12},
		{"", "Slot: 0 Port: 12 Gigabit - Level", 5, 12},
		{"uplink", "", 7, 7},
	}
	for _, c := range cases {
		if got := PortNumber(c.name, c.descr, c.idx); got != c.want {
			t.Errorf("PortNumber(%q, %q, %d) = %d, want %d", c.name, c.descr, c.idx, got, c.want)
		}
	}
}

func uswLayoutPorts() []LayoutPort {
	var ps []LayoutPort
	for _, f := range uswPro48() {
		if IsPhysical(info(f)) {
			ps = append(ps, LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr})
		}
	}
	return ps
}

// The USW-Pro-48 draws like the real switch: four blocks of twelve with odd
// ports on top, then the four SFP+ ports as a 2x2 block.
func TestLayoutUSWPro48(t *testing.T) {
	fp := Layout(uswLayoutPorts(), nil, nil)
	if fp.Rows != 2 || len(fp.Blocks) != 5 {
		t.Fatalf("rows %d blocks %d", fp.Rows, len(fp.Blocks))
	}
	if got := numbers(fp.Blocks[0].Top); !reflect.DeepEqual(got, []int{1, 3, 5, 7, 9, 11}) {
		t.Errorf("block 1 top %v", got)
	}
	if got := numbers(fp.Blocks[3].Bottom); !reflect.DeepEqual(got, []int{38, 40, 42, 44, 46, 48}) {
		t.Errorf("block 4 bottom %v", got)
	}
	sfp := fp.Blocks[4]
	if !sfp.SFP || !reflect.DeepEqual(numbers(sfp.Top), []int{49, 51}) || !reflect.DeepEqual(numbers(sfp.Bottom), []int{50, 52}) {
		t.Errorf("sfp block %+v", sfp)
	}
}

func TestLayoutOverridesAndSmallDevices(t *testing.T) {
	one := 1
	fp := Layout(uswLayoutPorts(), &one, nil)
	if fp.Rows != 1 || len(fp.Blocks[0].Bottom) != 0 || len(fp.Blocks[0].Top) != 12 {
		t.Errorf("one-row override: %+v", fp.Blocks[0])
	}

	// Eight ports or fewer sit in one row.
	var eight []LayoutPort
	for i := 1; i <= 8; i++ {
		eight = append(eight, LayoutPort{IfIndex: i, Name: "Port " + string(rune('0'+i))})
	}
	if fp := Layout(eight, nil, nil); fp.Rows != 1 || len(fp.Blocks) != 1 || len(fp.Blocks[0].Top) != 8 {
		t.Errorf("8 ports: %+v", fp)
	}

	// The user can mark ports as SFP when the descriptions do not say so.
	fp = Layout(eight, nil, []int{7, 8})
	if len(fp.Blocks) != 2 || !fp.Blocks[1].SFP || len(fp.Blocks[0].Top) != 6 {
		t.Errorf("sfp override: %+v", fp)
	}

	// Empty input still yields well-formed JSON-able slices.
	if fp := Layout(nil, nil, nil); fp.Blocks == nil || len(fp.Blocks) != 0 {
		t.Errorf("empty layout: %+v", fp)
	}
}

// The UDM-SE: eth9/eth10 describe themselves as SFP+ and form their own block.
func TestLayoutUDMSE(t *testing.T) {
	var ps []LayoutPort
	for _, f := range udmSE() {
		if IsPhysical(info(f)) {
			ps = append(ps, LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr})
		}
	}
	fp := Layout(ps, nil, nil)
	last := fp.Blocks[len(fp.Blocks)-1]
	if !last.SFP || len(last.Top)+len(last.Bottom) != 2 {
		t.Errorf("UDM SFP block %+v", last)
	}
	copper := 0
	for _, b := range fp.Blocks[:len(fp.Blocks)-1] {
		copper += len(b.Top) + len(b.Bottom)
	}
	if copper != 9 {
		t.Errorf("UDM copper ports %d, want 9 (eth0-eth8)", copper)
	}
}
