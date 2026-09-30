package portmon

import "strings"

// DetectDeviceType guesses switch/router/access_point/nvr/other from the model
// (UniFi and EdgeMax naming) and falls back to the number of physical ports:
// anything with 8 or more is drawn as a switch.
func DetectDeviceType(model string, physicalPorts int) string {
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
