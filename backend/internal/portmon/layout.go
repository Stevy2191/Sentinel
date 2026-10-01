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
}

// FacePort is one port's place: its ifIndex and the number printed by it.
type FacePort struct {
	IfIndex int `json:"if_index"`
	Number  int `json:"number"`
}

// FaceBlock is a group of ports drawn together. With two rows, Top holds the
// 1st, 3rd, 5th... port of the block and Bottom the 2nd, 4th...
type FaceBlock struct {
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
)

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

// Layout places physical ports. Copper ports are ordered by number and split
// into blocks of 12; SFP ports (by description, or listed in sfpOverride by
// number) form a last block on the right. Eight copper ports or fewer sit in
// one row, more in two; rowsOverride (1 or 2) replaces that choice.
func Layout(ports []LayoutPort, rowsOverride *int, sfpOverride []int) Faceplate {
	sfpSet := map[int]bool{}
	for _, n := range sfpOverride {
		sfpSet[n] = true
	}
	copper, sfp := []FacePort{}, []FacePort{}
	for _, p := range ports {
		fp := FacePort{IfIndex: p.IfIndex, Number: PortNumber(p.Name, p.Descr, p.IfIndex)}
		if sfpSet[fp.Number] || sfpWord.MatchString(p.Name+" "+p.Descr) {
			sfp = append(sfp, fp)
		} else {
			copper = append(copper, fp)
		}
	}
	byNumber := func(ps []FacePort) {
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].Number != ps[j].Number {
				return ps[i].Number < ps[j].Number
			}
			return ps[i].IfIndex < ps[j].IfIndex
		})
	}
	byNumber(copper)
	byNumber(sfp)

	rows := 1
	if len(copper) > maxOneRowPorts {
		rows = 2
	}
	if rowsOverride != nil && (*rowsOverride == 1 || *rowsOverride == 2) {
		rows = *rowsOverride
	}

	blocks := []FaceBlock{}
	for i := 0; i < len(copper); i += blockSize {
		blocks = append(blocks, split(copper[i:min(i+blockSize, len(copper))], rows, false))
	}
	if len(sfp) > 0 {
		blocks = append(blocks, split(sfp, rows, true))
	}
	return Faceplate{Rows: rows, Blocks: blocks}
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
func LayoutUnits(ports []LayoutPort, rowsOverride *int, sfpOverride []int) []UnitFaceplate {
	byUnit := map[int][]LayoutPort{}
	var units []int
	for _, p := range ports {
		if _, ok := byUnit[p.Unit]; !ok {
			units = append(units, p.Unit)
		}
		byUnit[p.Unit] = append(byUnit[p.Unit], p)
	}
	sort.Ints(units)
	out := make([]UnitFaceplate, 0, len(units))
	for _, u := range units {
		label := ""
		if len(units) > 1 {
			label = fmt.Sprintf("Switch %d", u)
		}
		out = append(out, UnitFaceplate{Unit: u, Label: label, Faceplate: Layout(byUnit[u], rowsOverride, sfpOverride)})
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
