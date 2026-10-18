package portmon

import (
	"regexp"
	"slices"
)

// Drawing keys: the realistic front panels the frontend can draw.
const (
	DrawingUDMSE       = "udm-se"
	DrawingUSWPro48PoE = "usw-pro-48-poe"
	DrawingES16XG      = "es-16-xg"
)

// Drawing is a model's realistic front panel: the main port numbers it
// places, and the device lights it shows beside the ports.
type Drawing struct {
	Key   string
	Ports []int
	// DriveBayDisk is the disk (diskIODevice name) in the drive bay, for a
	// model with one.
	DriveBayDisk string
	pattern      *regexp.Regexp
}

func portRange(first, last int) []int {
	out := make([]int, 0, last-first+1)
	for n := first; n <= last; n++ {
		out = append(out, n)
	}
	return out
}

var drawings = []Drawing{
	// The bay disk is sdb: the owner pulled the drive on 2026-10-18 and sdb
	// was the disk that left the disk activity table (sda is the built-in
	// SSD).
	{Key: DrawingUDMSE, Ports: portRange(1, 11), DriveBayDisk: "sdb", pattern: regexp.MustCompile(`(?i)^UDM-?SE$`)},
	// QuantumLink's ENTITY-MIB says UBNT-US48PRO-POE; the product name is
	// USW-Pro-48-PoE.
	{Key: DrawingUSWPro48PoE, Ports: portRange(1, 52), pattern: regexp.MustCompile(`(?i)^(UBNT-US48PRO-POE|USW-PRO-48-POE)$`)},
	{Key: DrawingES16XG, Ports: portRange(1, 16), pattern: regexp.MustCompile(`(?i)ES-?16-?XG|EdgeSwitch ?16 ?XG`)},
}

// DrawingFor is the realistic drawing for model (compared normalized), or nil.
func DrawingFor(model string) *Drawing {
	model = NormalizeModel(model)
	if model == "" {
		return nil
	}
	for _, d := range drawings {
		if d.pattern.MatchString(model) {
			d.Ports = slices.Clone(d.Ports)
			return &d
		}
	}
	return nil
}

// drawingKey is the drawing for a unit whose main port numbers are exactly
// the drawing's, else "": a drawing never hides or invents a port.
func drawingKey(d *Drawing, unitPorts []LayoutPort, nb Numbering) string {
	if d == nil || !slices.Equal(MainPortNumbers(unitPorts, nb), d.Ports) {
		return ""
	}
	return d.Key
}
