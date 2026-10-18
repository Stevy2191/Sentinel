package portmon

import (
	"regexp"
	"strconv"
	"strings"
)

// Numbering is how a device's interface names map to the numbers printed on
// its case. The zero value reads the number in the name (PortNumber).
type Numbering struct {
	// ethFromOne: Linux "ethN" is port N+1 (UniFi OS consoles: eth0 is the
	// port labelled 1).
	ethFromOne bool
}

// uniFiConsole is how UniFi OS consoles (UDM, UDM Pro, UDM-SE, UDR, UNVR…)
// open sysDescr: "Ubiquiti UniFi UDM-SE 5.1.33 Linux …".
const uniFiConsole = "Ubiquiti UniFi "

var linuxEth = regexp.MustCompile(`^eth(\d+)$`)

// NumberingFor picks a device's numbering from its sysDescr.
func NumberingFor(sysDescr string) Numbering {
	return Numbering{ethFromOne: strings.HasPrefix(sysDescr, uniFiConsole)}
}

// Number is the number printed by a port (see PortNumber).
func (nb Numbering) Number(name, descr string, ifIndex int) int {
	if nb.ethFromOne {
		if m := linuxEth.FindStringSubmatch(name); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n + 1
		}
	}
	return PortNumber(name, descr, ifIndex)
}

// Label is how a port is named to people (see PortLabel).
func (nb Numbering) Label(name, descr string, ifIndex int) string {
	if nb.ethFromOne && linuxEth.MatchString(name) {
		return strconv.Itoa(nb.Number(name, descr, ifIndex))
	}
	return PortLabel(name, descr, ifIndex)
}
