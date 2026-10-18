package portmon

import (
	"fmt"
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
	fp := Layout(uswLayoutPorts(), LayoutOptions{})
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
	fp := Layout(uswLayoutPorts(), LayoutOptions{Rows: &one})
	if fp.Rows != 1 || len(fp.Blocks[0].Bottom) != 0 || len(fp.Blocks[0].Top) != 12 {
		t.Errorf("one-row override: %+v", fp.Blocks[0])
	}

	// Eight ports or fewer sit in one row.
	var eight []LayoutPort
	for i := 1; i <= 8; i++ {
		eight = append(eight, LayoutPort{IfIndex: i, Name: "Port " + string(rune('0'+i))})
	}
	if fp := Layout(eight, LayoutOptions{}); fp.Rows != 1 || len(fp.Blocks) != 1 || len(fp.Blocks[0].Top) != 8 {
		t.Errorf("8 ports: %+v", fp)
	}

	// The user can mark ports as SFP when the descriptions do not say so.
	fp = Layout(eight, LayoutOptions{SFPPorts: []int{7, 8}})
	if len(fp.Blocks) != 2 || !fp.Blocks[1].SFP || len(fp.Blocks[0].Top) != 6 {
		t.Errorf("sfp override: %+v", fp)
	}

	// Empty input still yields well-formed JSON-able slices.
	if fp := Layout(nil, LayoutOptions{}); fp.Blocks == nil || len(fp.Blocks) != 0 {
		t.Errorf("empty layout: %+v", fp)
	}
}

// stackLayoutPorts builds LayoutPorts with Unit set the way SaveInventory
// would: from StackUnits over the fixture's physical ports.
func stackLayoutPorts(fixture []fixtureIf) []LayoutPort {
	var physical []fixtureIf
	for _, f := range fixture {
		if IsPhysical(info(f)) {
			physical = append(physical, f)
		}
	}
	var infos []IfInfo
	for _, f := range physical {
		infos = append(infos, info(f))
	}
	units := StackUnits(infos)
	var ps []LayoutPort
	for i, f := range physical {
		ps = append(ps, LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr, Unit: units[i]})
	}
	return ps
}

// A two-member 48-port stack draws as two faceplates, "Switch 1" and
// "Switch 2", each with four copper blocks of twelve plus an SFP+ block.
func TestLayoutUnitsStack(t *testing.T) {
	faces := LayoutUnits(stackLayoutPorts(stackedSwitch()), LayoutOptions{})
	if len(faces) != 2 {
		t.Fatalf("faces %d, want 2", len(faces))
	}
	for i, want := range []struct {
		unit  int
		label string
	}{{1, "Switch 1"}, {2, "Switch 2"}} {
		f := faces[i]
		if f.Unit != want.unit || f.Label != want.label {
			t.Errorf("face %d: unit %d label %q, want %d %q", i, f.Unit, f.Label, want.unit, want.label)
		}
		if len(f.Blocks) != 5 {
			t.Errorf("face %d: %d blocks, want 5 (4 copper + 1 sfp)", i, len(f.Blocks))
		}
		if !f.Blocks[4].SFP {
			t.Errorf("face %d: last block should be SFP", i)
		}
	}
}

// Fix round 1, plan-mandated 4: a physical port with no unit in its name on
// an otherwise-stacked device (e.g. a Cisco FastEthernet0 management port)
// gets UnitNumber 0 same as on a non-stacked device, but must not get its
// own one-port "Switch 0" faceplate — it still shows in the port table
// (DevicePorts' Ports list is built independently of LayoutUnits).
func TestLayoutUnitsStackedDropsUnitZero(t *testing.T) {
	fixture := append(stackedSwitch(), fixtureIf{1000, "FastEthernet0", "", 6})
	faces := LayoutUnits(stackLayoutPorts(fixture), LayoutOptions{})
	if len(faces) != 2 {
		t.Fatalf("faces %d, want 2 (unit 0 dropped)", len(faces))
	}
	if faces[0].Unit != 1 || faces[0].Label != "Switch 1" || faces[1].Unit != 2 || faces[1].Label != "Switch 2" {
		t.Errorf("faces %+v", faces)
	}
}

// The USW-Pro-48 (0/N names, not a stack) yields one faceplate with no label.
func TestLayoutUnitsNotStacked(t *testing.T) {
	faces := LayoutUnits(stackLayoutPorts(uswPro48()), LayoutOptions{})
	if len(faces) != 1 || faces[0].Unit != 0 || faces[0].Label != "" {
		t.Fatalf("faces %+v", faces)
	}
}

// A single-member stack-style device (1/0/N throughout, only one unit seen)
// also yields one faceplate, Unit 0, no label.
func TestLayoutUnitsSingleMember(t *testing.T) {
	var fixture []fixtureIf
	for i := 1; i <= 24; i++ {
		fixture = append(fixture, fixtureIf{i, fmt.Sprintf("1/0/%d", i), "", 6})
	}
	faces := LayoutUnits(stackLayoutPorts(fixture), LayoutOptions{})
	if len(faces) != 1 || faces[0].Unit != 0 || faces[0].Label != "" {
		t.Fatalf("faces %+v", faces)
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
	fp := Layout(ps, LayoutOptions{})
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

// Ports named switch/slot/port are labelled slot/port, as Cisco prints them;
// everything else keeps its plain number.
func TestPortLabelAndSlot(t *testing.T) {
	cases := []struct {
		name  string
		label string
		slot  int
	}{
		{"Gi1/0/12", "0/12", 0},
		{"Te1/1/4", "1/4", 1},
		{"2/1/3", "1/3", 1},
		{"0/12", "12", 0},
		{"Gi0/0", "0", 0},
		{"eth9", "9", 0},
		{"Port 7", "7", 0},
	}
	for _, c := range cases {
		if got := PortLabel(c.name, "", 99); got != c.label {
			t.Errorf("PortLabel(%q) = %q, want %q", c.name, got, c.label)
		}
		if got := SlotNumber(c.name); got != c.slot {
			t.Errorf("SlotNumber(%q) = %d, want %d", c.name, got, c.slot)
		}
	}
}

// cisco3850Ports is the 3850-12S's physical ports, with the module's 1G
// twin of cage 2 up and everything else down.
func cisco3850Ports() []LayoutPort {
	var ps []LayoutPort
	for _, f := range cisco3850() {
		if !IsPhysical(info(f)) {
			continue
		}
		p := LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr, SpeedBps: 1_000_000_000}
		if f.Name[:2] == "Te" {
			p.SpeedBps = 10_000_000_000
		}
		p.Up = f.Name == "Gi1/1/2"
		ps = append(ps, p)
	}
	return ps
}

func ifIndexes(ps []FacePort) []int {
	out := make([]int, len(ps))
	for i, p := range ps {
		out[i] = p.IfIndex
	}
	return out
}

// The stack interfaces are not ports a cable plugs into.
func TestCiscoStackInterfacesAreVirtual(t *testing.T) {
	for _, name := range []string{"StackPort1", "StackSub-St1-1"} {
		if IsPhysical(IfInfo{Name: name, Type: 6}) {
			t.Errorf("%s counted as physical", name)
		}
	}
}

// The WS-C3850-12S-S draws as one row of twelve SFP cages, then the network
// module as its own four-cage block. Each module cage shows the interface
// that is up, else the 10G one; Gi0/0 (management) is left off.
func TestLayoutCisco3850_12S(t *testing.T) {
	fp := Layout(cisco3850Ports(), LayoutOptions{Model: "WS-C3850-12S-S"})
	if fp.Rows != 1 || len(fp.Blocks) != 2 {
		t.Fatalf("rows %d blocks %+v", fp.Rows, fp.Blocks)
	}
	main, mod := fp.Blocks[0], fp.Blocks[1]
	if !main.SFP || main.Label != "" || !reflect.DeepEqual(numbers(main.Top), []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}) {
		t.Errorf("main block %+v", main)
	}
	if !mod.SFP || mod.Label != "Module 1" || !reflect.DeepEqual(numbers(mod.Top), []int{1, 2, 3, 4}) {
		t.Errorf("module block %+v", mod)
	}
	// Te1/1/1 (25), Gi1/1/2 (22, up), Te1/1/3 (27), Te1/1/4 (28).
	if got := ifIndexes(mod.Top); !reflect.DeepEqual(got, []int{25, 22, 27, 28}) {
		t.Errorf("module cages %v", got)
	}
}

// Port style: All RJ45 and All SFP replace what Auto works out, and Auto
// only treats the main ports as SFP on an SFP model.
func TestLayoutPortStyle(t *testing.T) {
	if fp := Layout(cisco3850Ports(), LayoutOptions{Model: "WS-C3850-12S-S", Style: PortStyleRJ45}); fp.Blocks[0].SFP || fp.Rows != 2 {
		t.Errorf("rj45: %+v", fp)
	}
	if fp := Layout(cisco3850Ports(), LayoutOptions{Model: "WS-C3850-24P-S"}); fp.Blocks[0].SFP {
		t.Errorf("copper model drawn as SFP: %+v", fp.Blocks[0])
	}
	fp := Layout(uswLayoutPorts(), LayoutOptions{Style: PortStyleSFP})
	for _, b := range fp.Blocks {
		if !b.SFP {
			t.Fatalf("sfp style left a copper block: %+v", fp)
		}
	}
	if fp.Rows != 2 || len(fp.Blocks) != 3 || len(fp.Blocks[0].Top) != 12 {
		t.Errorf("sfp style on 52 ports: rows %d blocks %d (want 24, 24, 4)", fp.Rows, len(fp.Blocks))
	}
}

func TestIsSFPModel(t *testing.T) {
	for model, want := range map[string]bool{
		"WS-C3850-12S-S": true, "WS-C3850-24S-E": true, "WS-C3850-48S": true, "WS-C3850-24XS-S": true,
		"WS-C3850-24P-S": false, "WS-C3850-12X48U": false, "USW-Pro-48-PoE": false, "": false,
	} {
		if got := IsSFPModel(model); got != want {
			t.Errorf("IsSFPModel(%q) = %v, want %v", model, got, want)
		}
	}
}

// The 4500-X has no slot 0: its built-in ports (slot 1) are the main ports,
// drawn as SFP+ in two rows of eight, and only slot 2 is a module.
func TestLayoutCisco4500X(t *testing.T) {
	var ps []LayoutPort
	for _, f := range cisco4500X() {
		ps = append(ps, LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr})
	}
	fp := Layout(ps, LayoutOptions{Model: "WS-C4500X-16"})
	if fp.Rows != 2 || len(fp.Blocks) != 2 {
		t.Fatalf("rows %d blocks %+v", fp.Rows, fp.Blocks)
	}
	main, mod := fp.Blocks[0], fp.Blocks[1]
	if main.Label != "" || !main.SFP || len(main.Top) != 8 || len(main.Bottom) != 8 {
		t.Errorf("main block %+v", main)
	}
	if mod.Label != "Module 2" || !mod.SFP || len(mod.Top)+len(mod.Bottom) != 8 {
		t.Errorf("module block %+v", mod)
	}
}

func es16XGLayoutPorts() []LayoutPort {
	var ps []LayoutPort
	for _, f := range es16XG() {
		if IsPhysical(info(f)) {
			ps = append(ps, LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr})
		}
	}
	return ps
}

// The EdgeSwitch 16 XG draws like the real switch: twelve SFP+ cages two
// high with odd ports on top, then the four RJ45 ports in one row.
func TestLayoutES16XG(t *testing.T) {
	for _, model := range []string{"UBNT-ES16XG", "ES-16-XG", "EdgeSwitch 16XG"} {
		fp := Layout(es16XGLayoutPorts(), LayoutOptions{Model: model})
		if fp.Rows != 2 || len(fp.Blocks) != 2 {
			t.Fatalf("%s: rows %d blocks %d", model, fp.Rows, len(fp.Blocks))
		}
		sfp, rj45 := fp.Blocks[0], fp.Blocks[1]
		if !sfp.SFP || sfp.Rows != 2 ||
			!reflect.DeepEqual(numbers(sfp.Top), []int{1, 3, 5, 7, 9, 11}) ||
			!reflect.DeepEqual(numbers(sfp.Bottom), []int{2, 4, 6, 8, 10, 12}) {
			t.Errorf("%s: SFP block %+v", model, sfp)
		}
		if rj45.SFP || rj45.Rows != 1 || len(rj45.Bottom) != 0 ||
			!reflect.DeepEqual(numbers(rj45.Top), []int{13, 14, 15, 16}) {
			t.Errorf("%s: RJ45 block %+v", model, rj45)
		}
	}
}

// A model layout yields to the user's own faceplate settings, and to a port
// list it does not match exactly (a firmware that numbers ports differently).
func TestLayoutModelFallsBack(t *testing.T) {
	two := 2
	for name, o := range map[string]LayoutOptions{
		"rows set":  {Model: "UBNT-ES16XG", Rows: &two},
		"style set": {Model: "UBNT-ES16XG", Style: PortStyleRJ45},
		"sfp ports": {Model: "UBNT-ES16XG", SFPPorts: []int{1}},
	} {
		fp := Layout(es16XGLayoutPorts(), o)
		for _, b := range fp.Blocks {
			if b.Rows != 0 {
				t.Errorf("%s: model layout used: %+v", name, fp)
			}
		}
	}
	short := es16XGLayoutPorts()[:15]
	if fp := Layout(short, LayoutOptions{Model: "UBNT-ES16XG"}); len(fp.Blocks) != 1 || fp.Blocks[0].Rows != 0 {
		t.Errorf("15 ports: want the automatic layout, got %+v", fp)
	}
	// Other models keep the automatic layout, with no per-block rows.
	if fp := Layout(uswLayoutPorts(), LayoutOptions{Model: "UBNT-US48PRO-POE"}); fp.Blocks[0].Rows != 0 {
		t.Errorf("USW-Pro-48 block rows %d", fp.Blocks[0].Rows)
	}
}

func cisco4500XLayoutPorts() []LayoutPort {
	var ps []LayoutPort
	for _, f := range cisco4500X() {
		ps = append(ps, LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr})
	}
	return ps
}

// A saved layout replaces the built-in one for the same model.
func TestLayoutSavedOverridesBuiltin(t *testing.T) {
	saved := []LayoutGroup{{First: 1, Last: 16, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAlternate}}
	fp := Layout(es16XGLayoutPorts(), LayoutOptions{Model: "UBNT-ES16XG", Saved: saved})
	if len(fp.Blocks) != 1 || fp.Blocks[0].SFP || len(fp.Blocks[0].Top) != 8 {
		t.Errorf("saved layout not used: %+v", fp)
	}
}

// A saved layout that does not place the ports exactly is skipped for the
// built-in one.
func TestLayoutSavedMisfitFallsBack(t *testing.T) {
	saved := []LayoutGroup{{First: 1, Last: 20, Kind: GroupKindRJ45, Rows: 2, Numbering: NumberingAlternate}}
	fp := Layout(es16XGLayoutPorts(), LayoutOptions{Model: "UBNT-ES16XG", Saved: saved})
	if len(fp.Blocks) != 2 || !fp.Blocks[0].SFP || fp.Blocks[1].SFP {
		t.Errorf("want the built-in ES-16-XG drawing, got %+v", fp)
	}
}

// The device's own settings beat a saved layout, unless told to ignore them
// (the editor's preview).
func TestLayoutDeviceSettingsBeatSaved(t *testing.T) {
	two := 2
	saved := []LayoutGroup{{First: 1, Last: 16, Kind: GroupKindRJ45, Rows: 1}}
	fp := Layout(es16XGLayoutPorts(), LayoutOptions{Model: "UBNT-ES16XG", Rows: &two, Saved: saved})
	for _, b := range fp.Blocks {
		if b.Rows != 0 {
			t.Errorf("device settings ignored: %+v", fp)
		}
	}
	fp = Layout(es16XGLayoutPorts(), LayoutOptions{Model: "UBNT-ES16XG", Rows: &two, Saved: saved, IgnoreDeviceSettings: true})
	if len(fp.Blocks) != 1 || fp.Blocks[0].Rows != 1 || len(fp.Blocks[0].Top) != 16 {
		t.Errorf("preview should draw the saved layout: %+v", fp)
	}
}

// Network modules are still drawn after a saved layout of the main ports.
func TestLayoutModulesAfterLayout(t *testing.T) {
	saved := []LayoutGroup{{First: 1, Last: 16, Kind: GroupKindSFP, Rows: 2, Numbering: NumberingAcross}}
	fp := Layout(cisco4500XLayoutPorts(), LayoutOptions{Model: "WS-C4500X-16", Saved: saved})
	if len(fp.Blocks) != 2 || fp.Blocks[0].Rows != 2 || !reflect.DeepEqual(numbers(fp.Blocks[0].Top), seq(1, 8)) {
		t.Fatalf("main block: %+v", fp.Blocks)
	}
	if mod := fp.Blocks[1]; mod.Label != "Module 2" || len(mod.Top)+len(mod.Bottom) != 8 {
		t.Errorf("module block %+v", mod)
	}
}
