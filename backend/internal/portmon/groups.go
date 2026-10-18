package portmon

import (
	"fmt"
	"sort"
	"strings"
)

// LayoutGroup is one group of ports on a front panel, left to right: ports
// First..Last (as PortNumber numbers them), drawn as SFP cages or RJ45, in
// one row or two. A built-in or saved layout is a list of them.
type LayoutGroup struct {
	First int    `json:"first"`
	Last  int    `json:"last"`
	Kind  string `json:"kind"`
	Rows  int    `json:"rows"`
	// Numbering is how a two-row group's numbers run; ignored for one row.
	Numbering string `json:"numbering,omitempty"`
}

const (
	GroupKindSFP  = "sfp"
	GroupKindRJ45 = "rj45"
	// NumberingAlternate puts the odd ports on top (1 above 2, 3 above 4).
	NumberingAlternate = "alternate"
	// NumberingAcross fills the top row first, then the bottom row.
	NumberingAcross = "across"

	maxGroups      = 16
	maxLayoutPorts = 512
	maxPortNumber  = 9999
)

// ValidateGroups checks a layout against the rules the editor shows.
func ValidateGroups(gs []LayoutGroup) error {
	if len(gs) == 0 {
		return fmt.Errorf("a layout needs at least one group")
	}
	if len(gs) > maxGroups {
		return fmt.Errorf("a layout has at most %d groups", maxGroups)
	}
	total := 0
	for i, g := range gs {
		n := i + 1
		switch {
		case g.First < 1 || g.Last < 1 || g.First > maxPortNumber || g.Last > maxPortNumber:
			return fmt.Errorf("group %d: ports are numbered 1 to %d", n, maxPortNumber)
		case g.First > g.Last:
			return fmt.Errorf("group %d: the first port is after the last", n)
		case g.Kind != GroupKindSFP && g.Kind != GroupKindRJ45:
			return fmt.Errorf("group %d: choose SFP or RJ45", n)
		case g.Rows != 1 && g.Rows != 2:
			return fmt.Errorf("group %d: a group has one or two rows", n)
		case g.Rows == 2 && g.Numbering != NumberingAlternate && g.Numbering != NumberingAcross:
			return fmt.Errorf("group %d: choose the numbering for a two-row group", n)
		}
		total += g.Last - g.First + 1
	}
	if total > maxLayoutPorts {
		return fmt.Errorf("a layout has at most %d ports", maxLayoutPorts)
	}
	idx := make([]int, len(gs))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return gs[idx[a]].First < gs[idx[b]].First })
	for k := 1; k < len(idx); k++ {
		if prev, cur := gs[idx[k-1]], gs[idx[k]]; cur.First <= prev.Last {
			lo, hi := min(idx[k-1], idx[k])+1, max(idx[k-1], idx[k])+1
			return fmt.Errorf("groups %d and %d overlap", lo, hi)
		}
	}
	return nil
}

// NormalizeModel is how models are compared: trimmed, runs of whitespace as
// one space, upper case.
func NormalizeModel(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(s), " "))
}

// Fit says how a layout and a device's main ports disagree: Missing are
// ports the device has that the layout does not place, Extra numbers the
// layout places that the device does not have (or places twice).
type Fit struct {
	Missing []int `json:"missing"`
	Extra   []int `json:"extra"`
}

// OK reports a layout that places every port exactly once.
func (f Fit) OK() bool { return len(f.Missing) == 0 && len(f.Extra) == 0 }

// CheckFit compares a layout with a device's main port numbers.
func CheckFit(gs []LayoutGroup, main []int) Fit {
	placed, have := map[int]int{}, map[int]int{}
	for _, g := range gs {
		for n := g.First; n <= g.Last; n++ {
			placed[n]++
		}
	}
	for _, n := range main {
		have[n]++
	}
	f := Fit{Missing: []int{}, Extra: []int{}}
	for n, c := range have {
		if c > placed[n] {
			f.Missing = append(f.Missing, n)
		}
	}
	for n, c := range placed {
		if c > have[n] {
			f.Extra = append(f.Extra, n)
		}
	}
	sort.Ints(f.Missing)
	sort.Ints(f.Extra)
	return f
}

// MainPortNumbers is the numbers of the ports Layout treats as main (the
// lowest slot on a switch/slot/port device), ascending.
func MainPortNumbers(ports []LayoutPort) []int {
	main, _ := mainAndModules(ports)
	out := make([]int, 0, len(main))
	for _, p := range main {
		out = append(out, PortNumber(p.Name, p.Descr, p.IfIndex))
	}
	sort.Ints(out)
	return out
}

// FitUnits checks a layout against each stack member's main ports, in
// LayoutUnits' order, and returns the first misfit, or nil when it fits all.
func FitUnits(ports []LayoutPort, gs []LayoutGroup) *Fit {
	for _, u := range unitGroups(ports) {
		if f := CheckFit(gs, MainPortNumbers(u.ports)); !f.OK() {
			return &f
		}
	}
	return nil
}

// groupBlocks draws main ports by a layout that CheckFit has passed.
func groupBlocks(gs []LayoutGroup, main []FacePort) []FaceBlock {
	byNum := make(map[int]FacePort, len(main))
	for _, p := range main {
		byNum[p.Number] = p
	}
	blocks := make([]FaceBlock, 0, len(gs))
	for _, g := range gs {
		ps := make([]FacePort, 0, g.Last-g.First+1)
		for n := g.First; n <= g.Last; n++ {
			ps = append(ps, byNum[n])
		}
		sfp := g.Kind == GroupKindSFP
		var b FaceBlock
		switch {
		case g.Rows != 2:
			b = split(ps, 1, sfp)
			b.Rows = 1
		case g.Numbering == NumberingAcross:
			top := (len(ps) + 1) / 2
			b = FaceBlock{SFP: sfp, Top: append([]FacePort{}, ps[:top]...), Bottom: append([]FacePort{}, ps[top:]...)}
			b.Rows = 2
		default:
			b = split(ps, 2, sfp)
			b.Rows = 2
		}
		blocks = append(blocks, b)
	}
	return blocks
}

func groupRows(gs []LayoutGroup) int {
	rows := 1
	for _, g := range gs {
		if g.Rows == 2 {
			rows = 2
		}
	}
	return rows
}

// GroupsFromFaceplate turns an automatic drawing's main blocks into a layout,
// the editor's starting point: one group per block (two-row blocks read odd
// on top), split wherever the numbers jump. Module blocks are left out.
func GroupsFromFaceplate(fp Faceplate) []LayoutGroup {
	out := []LayoutGroup{}
	for _, b := range fp.Blocks {
		if b.Label != "" {
			continue
		}
		rows := b.Rows
		if rows == 0 {
			rows = fp.Rows
		}
		var nums []int
		for i := range b.Top {
			nums = append(nums, b.Top[i].Number)
			if i < len(b.Bottom) {
				nums = append(nums, b.Bottom[i].Number)
			}
		}
		kind := GroupKindRJ45
		if b.SFP {
			kind = GroupKindSFP
		}
		for i := 0; i < len(nums); {
			j := i
			for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
				j++
			}
			g := LayoutGroup{First: nums[i], Last: nums[j], Kind: kind, Rows: rows}
			if rows == 2 {
				g.Numbering = NumberingAlternate
			}
			out = append(out, g)
			i = j + 1
		}
	}
	return out
}
