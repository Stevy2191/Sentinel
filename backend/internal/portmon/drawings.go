package portmon

import (
	"regexp"
	"slices"
	"strings"
)

// Drawing keys: the realistic front panels the frontend can draw.
const (
	DrawingUDMSE       = "udm-se"
	DrawingUSWPro48PoE = "usw-pro-48-poe"
	DrawingES16XG      = "es-16-xg"
	// EdgeSwitches by product name. 150W, 250W and the 48-port 500W are the
	// newer generation's look, 24 500W and 48 750W the older's: the owner's
	// fleet splits by wattage (2026-10-18).
	DrawingES8150W  = "es-8-150w"
	DrawingES24250W = "es-24-250w"
	DrawingES24500W = "es-24-500w"
	DrawingES48500W = "es-48-500w"
	DrawingES48750W = "es-48-750w"
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
	// product matches the product name that opens sysDescr ("EdgeSwitch 24
	// 500W"), for models whose model string doesn't say which they are
	// (every 24-port EdgeSwitch reports UBNT-ES24).
	product *regexp.Regexp
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
	{Key: DrawingES8150W, Ports: portRange(1, 10), product: regexp.MustCompile(`(?i)^EDGESWITCH 8 150W$`), pattern: regexp.MustCompile(`(?i)^ES-8-150W$`)},
	{Key: DrawingES24250W, Ports: portRange(1, 26), product: regexp.MustCompile(`(?i)^EDGESWITCH 24 250W$`), pattern: regexp.MustCompile(`(?i)^ES-24-250W$`)},
	{Key: DrawingES24500W, Ports: portRange(1, 26), product: regexp.MustCompile(`(?i)^EDGESWITCH 24 500W$`), pattern: regexp.MustCompile(`(?i)^ES-24-500W$`)},
	{Key: DrawingES48500W, Ports: portRange(1, 52), product: regexp.MustCompile(`(?i)^EDGESWITCH 48 500W$`), pattern: regexp.MustCompile(`(?i)^ES-48-500W$`)},
	{Key: DrawingES48750W, Ports: portRange(1, 52), product: regexp.MustCompile(`(?i)^EDGESWITCH 48 750W$`), pattern: regexp.MustCompile(`(?i)^ES-48-750W$`)},
}

// productName is the part of sysDescr before its first comma, normalized:
// "EdgeSwitch 24 500W, 1.12.2, Linux …" gives "EDGESWITCH 24 500W".
func productName(sysDescr string) string {
	name, _, _ := strings.Cut(sysDescr, ",")
	return NormalizeModel(name)
}

// DrawingFor is the realistic drawing for a device's model (compared
// normalized), or for the product name opening its sysDescr; nil when
// neither matches.
func DrawingFor(model, sysDescr string) *Drawing {
	model, product := NormalizeModel(model), productName(sysDescr)
	for _, d := range drawings {
		if (model != "" && d.pattern != nil && d.pattern.MatchString(model)) ||
			(product != "" && d.product != nil && d.product.MatchString(product)) {
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
