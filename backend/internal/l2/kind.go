package l2

import "strings"

// What kind of thing an endpoint is, for its icon.
const (
	KindCamera  = "camera"
	KindPhone   = "phone"
	KindPrinter = "printer"
	KindAP      = "ap"
	KindDevice  = "device"
)

var kindMakers = []struct {
	kind   string
	makers []string
}{
	{KindCamera, []string{"hikvision", "dahua", "axis", "hanwha", "uniview"}},
	{KindPhone, []string{"yealink", "polycom", "poly", "grandstream", "mitel", "avaya"}},
	{KindPrinter, []string{"hewlett packard", "hp inc", "brother", "lexmark", "canon", "seiko epson", "xerox", "ricoh", "kyocera"}},
}

// GuessKind guesses an endpoint's kind from its maker and the LLDP
// capabilities it announces (an AP or phone says so itself).
func GuessKind(maker string, caps Caps) string {
	if caps.AP() {
		return KindAP
	}
	if caps.Phone() {
		return KindPhone
	}
	m := strings.ToLower(maker)
	// HPE is Aruba's switches and APs, not HP's printers.
	if strings.Contains(m, "hewlett packard enterprise") {
		return KindDevice
	}
	for _, k := range kindMakers {
		for _, name := range k.makers {
			if hasWord(m, name) {
				return k.kind
			}
		}
	}
	return KindDevice
}

// hasWord reports whether name appears in s as whole words, so "poly"
// matches "Poly" and "Polycom" is its own entry, but "Axis" does not match
// "Taxis Ltd".
func hasWord(s, name string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		before := start == 0 || !isLetter(s[start-1])
		after := end == len(s) || !isLetter(s[end])
		if before && after {
			return true
		}
		i = start + 1
	}
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') }
