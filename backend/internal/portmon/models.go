package portmon

import "regexp"

// modelBlock is one group of ports on a known model's front panel: ports
// First..Last, drawn as SFP cages or RJ45, in one row or two (two rows put
// the odd ports on top).
type modelBlock struct {
	First, Last int
	SFP         bool
	Rows        int
}

// modelLayout is the real front panel of a model whose ports the automatic
// rules cannot tell apart, left to right.
type modelLayout struct {
	model  *regexp.Regexp
	blocks []modelBlock
}

var modelLayouts = []modelLayout{
	// EdgeSwitch 16 XG: twelve SFP+ cages two high, then four 10GBASE-T RJ45
	// in one row. Its firmware calls every port "10G - Level", which Auto
	// would draw as sixteen cages. ENTITY-MIB gives "UBNT-ES16XG"; the
	// product name is "EdgeSwitch 16 XG" (ES-16-XG).
	{regexp.MustCompile(`(?i)ES-?16-?XG|EdgeSwitch ?16 ?XG`), []modelBlock{{1, 12, true, 2}, {13, 16, false, 1}}},
}

// modelFaceplate lays out ports by the table entry matching model. It
// applies only when the ports are exactly the numbers the entry covers, each
// once, so a firmware that numbers them differently falls back to Auto.
func modelFaceplate(model string, ports []FacePort) (Faceplate, bool) {
	var ml *modelLayout
	for i := range modelLayouts {
		if model != "" && modelLayouts[i].model.MatchString(model) {
			ml = &modelLayouts[i]
			break
		}
	}
	if ml == nil {
		return Faceplate{}, false
	}
	byNum := map[int]FacePort{}
	for _, p := range ports {
		if _, dup := byNum[p.Number]; dup {
			return Faceplate{}, false
		}
		byNum[p.Number] = p
	}
	fp := Faceplate{Rows: 1}
	covered := 0
	for _, mb := range ml.blocks {
		var ps []FacePort
		for n := mb.First; n <= mb.Last; n++ {
			p, ok := byNum[n]
			if !ok {
				return Faceplate{}, false
			}
			ps = append(ps, p)
		}
		covered += len(ps)
		b := split(ps, mb.Rows, mb.SFP)
		b.Rows = mb.Rows
		fp.Blocks = append(fp.Blocks, b)
		fp.Rows = max(fp.Rows, mb.Rows)
	}
	if covered != len(ports) {
		return Faceplate{}, false
	}
	return fp, true
}
