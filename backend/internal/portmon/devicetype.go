package portmon

import "strings"

// upsEnterprises are the IANA enterprise numbers of UPS makers: APC (318),
// Eaton/Powerware (534), Eaton/MGE (705), Vertiv/Liebert (476), Tripp Lite
// (850) and CyberPower (3808). A device whose sysObjectID sits under one of
// these is a UPS.
var upsEnterprises = map[string]bool{"318": true, "534": true, "705": true, "476": true, "850": true, "3808": true}

// upsMIB is the standard UPS-MIB (RFC 1628). Some generic UPS network cards
// report it as their sysObjectID instead of a vendor number.
const upsMIB = "1.3.6.1.2.1.33"

// DetectDeviceType guesses switch/router/access_point/nvr/ups/other: UPS makers
// by the sysObjectID's enterprise number, then the model (UniFi and EdgeMax
// naming), then the number of physical ports (8 or more is drawn as a switch).
func DetectDeviceType(model, sysObjectID string, physicalPorts int) string {
	oid := strings.TrimPrefix(sysObjectID, ".")
	if oid == upsMIB || strings.HasPrefix(oid, upsMIB+".") {
		return "ups"
	}
	if rest, ok := strings.CutPrefix(oid, "1.3.6.1.4.1."); ok {
		enterprise, _, _ := strings.Cut(rest, ".")
		if upsEnterprises[enterprise] {
			return "ups"
		}
	}
	m := strings.ToUpper(strings.TrimSpace(model))
	has := func(prefixes ...string) bool {
		for _, p := range prefixes {
			if strings.HasPrefix(m, p) {
				return true
			}
		}
		return false
	}
	switch {
	case has("USW", "US-", "EDGESWITCH", "ES-"):
		return "switch"
	case has("UDM", "UXG", "USG", "ER-"):
		return "router"
	case has("U6", "U7", "UAP", "UAL", "UK"):
		return "access_point"
	case has("UNVR"):
		return "nvr"
	case physicalPorts >= 8:
		return "switch"
	default:
		return "other"
	}
}
