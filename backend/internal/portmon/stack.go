package portmon

import (
	"regexp"
	"strconv"
)

var (
	threePartUnit = regexp.MustCompile(`(\d+)/\d+/\d+`)
	unitWord      = regexp.MustCompile(`(?i)\bUnit:?\s*(\d+)`)
)

// UnitNumber is a port's stack member number: the first number of a
// three-part "a/b/c" name (e.g. "1/0/12" gives 1, "Gi2/0/12" gives 2),
// otherwise "Unit: N" in the description, otherwise 0. A two-part name
// ("0/12", EdgeSwitch/UniFi) matches neither pattern and gives 0.
func UnitNumber(name, descr string) int {
	if m := threePartUnit.FindStringSubmatch(name); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	if m := unitWord.FindStringSubmatch(descr); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 0
}

// StackUnits returns each port's stack unit, in the same order as ports. A
// device counts as stacked when its physical ports carry more than one
// distinct non-zero UnitNumber; only then does every port (physical or not)
// take its own UnitNumber. Otherwise every port is unit 0, so a device whose
// port naming merely resembles a stack (e.g. a single switch using "1/0/12"
// throughout) is not drawn as one.
func StackUnits(ports []IfInfo) []int {
	units := make([]int, len(ports))
	distinct := map[int]bool{}
	for i, p := range ports {
		units[i] = UnitNumber(p.Name, p.Descr)
		if units[i] != 0 && IsPhysical(p) {
			distinct[units[i]] = true
		}
	}
	if len(distinct) <= 1 {
		for i := range units {
			units[i] = 0
		}
	}
	return units
}
