package portmon

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func es16XGGroups() []LayoutGroup {
	return []LayoutGroup{
		{First: 1, Last: 12, Kind: GroupKindSFP, Rows: 2, Numbering: NumberingAlternate},
		{First: 13, Last: 16, Kind: GroupKindRJ45, Rows: 1},
	}
}

func seq(from, to int) []int {
	var out []int
	for n := from; n <= to; n++ {
		out = append(out, n)
	}
	return out
}

func TestValidateGroups(t *testing.T) {
	many := make([]LayoutGroup, 17)
	for i := range many {
		many[i] = LayoutGroup{First: i + 1, Last: i + 1, Kind: GroupKindRJ45, Rows: 1}
	}
	cases := []struct {
		name string
		gs   []LayoutGroup
		want string // "" = valid
	}{
		{"es-16-xg", es16XGGroups(), ""},
		{"none", []LayoutGroup{}, "at least one group"},
		{"seventeen", many, "at most 16 groups"},
		{"backwards", []LayoutGroup{{First: 5, Last: 3, Kind: GroupKindRJ45, Rows: 1}}, "first port"},
		{"zero", []LayoutGroup{{First: 0, Last: 3, Kind: GroupKindRJ45, Rows: 1}}, "1 to 9999"},
		{"too high", []LayoutGroup{{First: 1, Last: 10000, Kind: GroupKindRJ45, Rows: 1}}, "1 to 9999"},
		{"overlap", []LayoutGroup{{First: 1, Last: 8, Kind: GroupKindRJ45, Rows: 1}, {First: 8, Last: 12, Kind: GroupKindSFP, Rows: 1}}, "overlap"},
		{"too many ports", []LayoutGroup{{First: 1, Last: 513, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAlternate}}, "at most 512 ports"},
		{"no kind", []LayoutGroup{{First: 1, Last: 4, Rows: 1}}, "SFP or RJ45"},
		{"three rows", []LayoutGroup{{First: 1, Last: 4, Kind: GroupKindRJ45, Rows: 3}}, "one or two rows"},
		{"two rows no numbering", []LayoutGroup{{First: 1, Last: 4, Kind: GroupKindRJ45, Rows: 2}}, "numbering"},
		{"one row numbering ignored", []LayoutGroup{{First: 1, Last: 4, Kind: GroupKindRJ45, Rows: 1, Numbering: NumberingAcross}}, ""},
	}
	for _, c := range cases {
		err := ValidateGroups(c.gs)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: error %v, want one containing %q", c.name, err, c.want)
		}
	}
}

func TestNormalizeModel(t *testing.T) {
	for in, want := range map[string]string{
		" ubnt-es16xg ":       "UBNT-ES16XG",
		"EdgeSwitch  16   XG": "EDGESWITCH 16 XG",
		"\tUSW-24\n":          "USW-24",
		"":                    "",
	} {
		if got := NormalizeModel(in); got != want {
			t.Errorf("NormalizeModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckFit(t *testing.T) {
	if f := CheckFit(es16XGGroups(), seq(1, 16)); !f.OK() {
		t.Errorf("1..16: %+v", f)
	}
	if f := CheckFit(es16XGGroups(), seq(1, 15)); !reflect.DeepEqual(f.Extra, []int{16}) || len(f.Missing) != 0 {
		t.Errorf("1..15: %+v", f)
	}
	if f := CheckFit(es16XGGroups(), seq(1, 17)); !reflect.DeepEqual(f.Missing, []int{17}) || len(f.Extra) != 0 {
		t.Errorf("1..17: %+v", f)
	}
	overlap := []LayoutGroup{{First: 1, Last: 8, Kind: GroupKindRJ45, Rows: 1}, {First: 8, Last: 8, Kind: GroupKindRJ45, Rows: 1}}
	if f := CheckFit(overlap, seq(1, 8)); !reflect.DeepEqual(f.Extra, []int{8}) {
		t.Errorf("overlap: %+v", f)
	}
	if f := CheckFit(es16XGGroups(), seq(1, 16)); f.Missing == nil || f.Extra == nil {
		t.Errorf("Missing and Extra must be empty lists, not nil: %+v", f)
	}
}

func onePlaneLayout(gs []LayoutGroup, ports []int) Faceplate {
	var ps []LayoutPort
	for _, n := range ports {
		ps = append(ps, LayoutPort{IfIndex: n, Name: "0/" + strconv.Itoa(n)})
	}
	return Layout(ps, LayoutOptions{Saved: gs})
}

func TestGroupNumbering(t *testing.T) {
	alt := onePlaneLayout([]LayoutGroup{{First: 1, Last: 12, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAlternate}}, seq(1, 12))
	if b := alt.Blocks[0]; !reflect.DeepEqual(numbers(b.Top), []int{1, 3, 5, 7, 9, 11}) || !reflect.DeepEqual(numbers(b.Bottom), []int{2, 4, 6, 8, 10, 12}) {
		t.Errorf("alternate: %+v", b)
	}
	across := onePlaneLayout([]LayoutGroup{{First: 1, Last: 12, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAcross}}, seq(1, 12))
	if b := across.Blocks[0]; !reflect.DeepEqual(numbers(b.Top), seq(1, 6)) || !reflect.DeepEqual(numbers(b.Bottom), seq(7, 12)) {
		t.Errorf("across: %+v", b)
	}
	odd := onePlaneLayout([]LayoutGroup{{First: 1, Last: 7, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAcross}}, seq(1, 7))
	if b := odd.Blocks[0]; !reflect.DeepEqual(numbers(b.Top), seq(1, 4)) || !reflect.DeepEqual(numbers(b.Bottom), seq(5, 7)) {
		t.Errorf("across, odd: %+v", b)
	}
	if b := odd.Blocks[0]; b.Rows != 2 || odd.Rows != 2 {
		t.Errorf("rows: block %d panel %d", b.Rows, odd.Rows)
	}
}

func TestGroupsFromFaceplate(t *testing.T) {
	got := GroupsFromFaceplate(Layout(uswLayoutPorts(), LayoutOptions{}))
	want := []LayoutGroup{
		{First: 1, Last: 12, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAlternate},
		{First: 13, Last: 24, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAlternate},
		{First: 25, Last: 36, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAlternate},
		{First: 37, Last: 48, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAlternate},
		{First: 49, Last: 52, Kind: GroupKindSFP, Rows: 2, Numbering: NumberingAlternate},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestGroupsFromFaceplateSplitsJumps(t *testing.T) {
	fp := Faceplate{Rows: 1, Blocks: []FaceBlock{{Top: []FacePort{{1, 1}, {2, 2}, {3, 3}, {7, 7}, {8, 8}}, Bottom: []FacePort{}}}}
	want := []LayoutGroup{
		{First: 1, Last: 3, Kind: GroupKindRJ45, Rows: 1},
		{First: 7, Last: 8, Kind: GroupKindRJ45, Rows: 1},
	}
	if got := GroupsFromFaceplate(fp); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestMainPortNumbersAndFitUnits(t *testing.T) {
	if got := MainPortNumbers(es16XGLayoutPorts(), Numbering{}); !reflect.DeepEqual(got, seq(1, 16)) {
		t.Errorf("ES-16-XG main ports %v", got)
	}
	var stack []LayoutPort
	for u := 1; u <= 2; u++ {
		for n := 1; n <= 16; n++ {
			if u == 2 && n == 16 {
				continue
			}
			stack = append(stack, LayoutPort{IfIndex: u*100 + n, Name: strconv.Itoa(u) + "/0/" + strconv.Itoa(n), Unit: u})
		}
	}
	f := FitUnits(stack, es16XGGroups(), Numbering{})
	if f == nil || !reflect.DeepEqual(f.Extra, []int{16}) {
		t.Errorf("stack misfit: %+v", f)
	}
	if f := FitUnits(es16XGLayoutPorts(), es16XGGroups(), Numbering{}); f != nil {
		t.Errorf("ES-16-XG fits, got %+v", f)
	}
}

// A device with no ports to check against never "fits": every number the
// layout places is reported as not on the device.
func TestFitUnitsNoPorts(t *testing.T) {
	f := FitUnits(nil, es16XGGroups(), Numbering{})
	if f == nil || !reflect.DeepEqual(f.Extra, seq(1, 16)) || len(f.Missing) != 0 {
		t.Errorf("no ports: %+v", f)
	}
}
