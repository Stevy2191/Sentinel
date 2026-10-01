package portmon

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

// LayoutPort is a physical port to place on the faceplate.
type LayoutPort struct {
	IfIndex int
	Name    string
	Descr   string
	// Unit is the stack member this port belongs to (0 when the device is
	// not a stack), as stored in device_interfaces.stack_unit.
	Unit int
	// Up and SpeedBps pick which of two interfaces sharing one module cage
	// is drawn (see Layout).
	Up       bool
	SpeedBps int64
}

// FacePort is one port's place: its ifIndex and the number printed by it.
type FacePort struct {
	IfIndex int `json:"if_index"`
	Number  int `json:"number"`
}

// FaceBlock is a group of ports drawn together. With two rows, Top holds the
// 1st, 3rd, 5th... port of the block and Bottom the 2nd, 4th...
type FaceBlock struct {
	// Label names a module block ("Module 1"); empty for the main ports.
	Label  string     `json:"label,omitempty"`
	SFP    bool       `json:"sfp"`
	Top    []FacePort `json:"top"`
	Bottom []FacePort `json:"bottom"`
}

// Faceplate is the whole front panel, left to right.
type Faceplate struct {
	Rows   int         `json:"rows"`
	Blocks []FaceBlock `json:"blocks"`
}

const (
	blockSize      = 12
	maxOneRowPorts = 8
)

var (
	trailingNumber = regexp.MustCompile(`(\d+)\s*$`)
	portWord       = regexp.MustCompile(`(?i)\bport:?\s*(\d+)`)
	sfpWord        = regexp.MustCompile(`(?i)sfp|\b(10|25|40|100)g\b`)
	// slotName is a Cisco-style switch/slot/port name: "Gi1/0/12", "Te1/1/4".
	slotName = regexp.MustCompile(`(\d+)/(\d+)/(\d+)$`)
	// sfpModel is Cisco's naming for an all-SFP switch: WS-C3850-12S-S,
	// -24S, -48S, -24XS.
	sfpModel = regexp.MustCompile(`(?i)-(12|24|48)X?S(-|$)`)
	// fastCisco is a Cisco 10G-and-up interface (Te1/1/1, TenGigabitEthernet,
	// TwentyFiveGigE, FortyGigabitEthernet, HundredGigE), almost always a
	// cage; Auto draws it as SFP. ("Tw" alone is TwoGigabitEthernet, copper.)
	fastCisco = regexp.MustCompile(`(?i)^(Te|Twe|Fo|Hu)\d|^(TenGigabitEthernet|TwentyFiveGigE|FortyGigabitEthernet|HundredGigE)`)
)

// Port styles a user can set for the faceplate's main ports. Auto (empty)
// draws a port as SFP when its description says so or the model is an
// all-SFP one.
const (
	PortStyleAuto = ""
	PortStyleSFP  = "sfp"
	PortStyleRJ45 = "rj45"
)

// IsSFPModel reports a model name that Cisco uses for all-SFP switches.
func IsSFPModel(model string) bool { return sfpModel.MatchString(model) }

// SlotNumber is the slot of a switch/slot/port name ("Te1/1/4" gives 1):
// 0 for the switch's own ports, N for network module N. Any other name is 0.
func SlotNumber(name string) int {
	if m := slotName.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[2])
		return n
	}
	return 0
}

// PortLabel is how a port is named to people: "slot/port" for a
// switch/slot/port name ("Gi1/0/12" gives "0/12", "Te1/1/4" gives "1/4"),
// otherwise its PortNumber.
func PortLabel(name, descr string, ifIndex int) string {
	if m := slotName.FindStringSubmatch(name); m != nil {
		slot, _ := strconv.Atoi(m[2])
		port, _ := strconv.Atoi(m[3])
		return fmt.Sprintf("%d/%d", slot, port)
	}
	return strconv.Itoa(PortNumber(name, descr, ifIndex))
}

// PortNumber is the number printed next to a port: "Port N" in the name, else
// the trailing number of the name ("0/12", "Gi1/0/12", "eth12"), else "Port: N"
// in the description, else the ifIndex.
func PortNumber(name, descr string, ifIndex int) int {
	for _, m := range [][]string{portWord.FindStringSubmatch(name), trailingNumber.FindStringSubmatch(name), portWord.FindStringSubmatch(descr)} {
		if m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil {
				return n
			}
		}
	}
	return ifIndex
}

// LayoutOptions are the user's faceplate settings and what Auto needs.
type LayoutOptions struct {
	// Rows (1 or 2) replaces the automatic choice.
	Rows *int
	// SFPPorts lists port numbers to draw as SFP whatever the style.
	SFPPorts []int
	// Style is PortStyleAuto, PortStyleSFP or PortStyleRJ45.
	Style string
	// Model is the device's model, for Auto's IsSFPModel check.
	Model string
}

// Layout places physical ports. The main ports are ordered by number: copper
// ones split into blocks of 12, then SFP ones (see LayoutOptions.Style) in
// blocks of 12 columns on the right. Eight copper ports or fewer sit in one
// row, more in two; with no copper at all, twelve SFP ports or fewer sit in
// one row.
//
// On a device with switch/slot/port names, the lowest slot holds the main
// ports (slot 0 on most Catalysts, slot 1 on a 4500-X), each other slot is a
// network module drawn as its own SFP block after them, labelled "Module N",
// and a
// port without such a name (the Gi0/0 management port) is left off. Where
// two module interfaces share a number (Cisco lists each cage of a 1G/10G
// module as both Gi1/1/N and Te1/1/N), one cage is drawn, for the interface
// that is up, else the faster one.
func Layout(ports []LayoutPort, o LayoutOptions) Faceplate {
	sfpSet := map[int]bool{}
	for _, n := range o.SFPPorts {
		sfpSet[n] = true
	}
	slotted, mainSlot := false, 0
	for _, p := range ports {
		if slotName.MatchString(p.Name) {
			if slot := SlotNumber(p.Name); !slotted || slot < mainSlot {
				mainSlot = slot
			}
			slotted = true
		}
	}
	copper, sfp := []FacePort{}, []FacePort{}
	modules := map[int]map[int]LayoutPort{}
	for _, p := range ports {
		if slotted && !slotName.MatchString(p.Name) {
			continue
		}
		n := PortNumber(p.Name, p.Descr, p.IfIndex)
		if slot := SlotNumber(p.Name); slot != mainSlot {
			if modules[slot] == nil {
				modules[slot] = map[int]LayoutPort{}
			}
			if cur, ok := modules[slot][n]; !ok || drawnBefore(p, cur) {
				modules[slot][n] = p
			}
			continue
		}
		fp := FacePort{IfIndex: p.IfIndex, Number: n}
		isSFP := sfpSet[n]
		switch o.Style {
		case PortStyleSFP:
			isSFP = true
		case PortStyleRJ45:
		default:
			isSFP = isSFP || sfpWord.MatchString(p.Name+" "+p.Descr) || IsSFPModel(o.Model) ||
				fastCisco.MatchString(p.Name) || fastCisco.MatchString(p.Descr)
		}
		if isSFP {
			sfp = append(sfp, fp)
		} else {
			copper = append(copper, fp)
		}
	}
	byNumber(copper)
	byNumber(sfp)

	rows := 1
	if len(copper) > maxOneRowPorts || (len(copper) == 0 && len(sfp) > blockSize) {
		rows = 2
	}
	if o.Rows != nil && (*o.Rows == 1 || *o.Rows == 2) {
		rows = *o.Rows
	}

	// Copper ports come in blocks of twelve; SFP cages in blocks of twelve
	// columns (24 cages over two rows), so a 4500-X's sixteen stay together.
	blocks := []FaceBlock{}
	for _, group := range []struct {
		ps   []FacePort
		size int
		sfp  bool
	}{{copper, blockSize, false}, {sfp, blockSize * rows, true}} {
		for i := 0; i < len(group.ps); i += group.size {
			blocks = append(blocks, split(group.ps[i:min(i+group.size, len(group.ps))], rows, group.sfp))
		}
	}
	slots := make([]int, 0, len(modules))
	for slot := range modules {
		slots = append(slots, slot)
	}
	sort.Ints(slots)
	for _, slot := range slots {
		var ps []FacePort
		for n, p := range modules[slot] {
			ps = append(ps, FacePort{IfIndex: p.IfIndex, Number: n})
		}
		byNumber(ps)
		b := split(ps, rows, true)
		b.Label = fmt.Sprintf("Module %d", slot)
		blocks = append(blocks, b)
	}
	return Faceplate{Rows: rows, Blocks: blocks}
}

// drawnBefore reports whether a should be drawn instead of b in one cage: an
// interface that is up, else the faster one, else the lower ifIndex.
func drawnBefore(a, b LayoutPort) bool {
	if a.Up != b.Up {
		return a.Up
	}
	if a.SpeedBps != b.SpeedBps {
		return a.SpeedBps > b.SpeedBps
	}
	return a.IfIndex < b.IfIndex
}

func byNumber(ps []FacePort) {
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].Number != ps[j].Number {
			return ps[i].Number < ps[j].Number
		}
		return ps[i].IfIndex < ps[j].IfIndex
	})
}

// UnitFaceplate is one stack member's faceplate.
type UnitFaceplate struct {
	Unit  int    `json:"unit"`
	Label string `json:"label"`
	Faceplate
}

// LayoutUnits groups ports by their stack Unit and lays out each group with
// Layout, ordered by unit. Label is "Switch N" when there is more than one
// group, else "" — a non-stacked device (every port's Unit is 0) always
// yields exactly one entry, Unit 0 with an empty Label.
//
// On a stacked device, a physical port whose name carries no unit (e.g. a
// Cisco FastEthernet0 management port) still gets UnitNumber 0 — same as on
// a non-stacked device, since UnitNumber alone cannot tell the two apart —
// but drawing it as its own one-port "Switch 0" chassis would be wrong: it
// is not a stack member, just a port LayoutUnits otherwise has nowhere to
// put. It is dropped from the faceplate entirely here (it still shows in
// the port table, which is built independently of this function).
func LayoutUnits(ports []LayoutPort, o LayoutOptions) []UnitFaceplate {
	byUnit := map[int][]LayoutPort{}
	var units []int
	for _, p := range ports {
		if _, ok := byUnit[p.Unit]; !ok {
			units = append(units, p.Unit)
		}
		byUnit[p.Unit] = append(byUnit[p.Unit], p)
	}
	sort.Ints(units)
	stacked := len(units) > 1
	out := make([]UnitFaceplate, 0, len(units))
	for _, u := range units {
		if stacked && u == 0 {
			continue
		}
		label := ""
		if stacked {
			label = fmt.Sprintf("Switch %d", u)
		}
		out = append(out, UnitFaceplate{Unit: u, Label: label, Faceplate: Layout(byUnit[u], o)})
	}
	return out
}

func split(ps []FacePort, rows int, sfp bool) FaceBlock {
	b := FaceBlock{SFP: sfp, Top: []FacePort{}, Bottom: []FacePort{}}
	if rows == 1 {
		b.Top = append(b.Top, ps...)
		return b
	}
	for i, p := range ps {
		if i%2 == 0 {
			b.Top = append(b.Top, p)
		} else {
			b.Bottom = append(b.Bottom, p)
		}
	}
	return b
}
