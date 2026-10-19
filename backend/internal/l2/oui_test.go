package l2

import (
	"strings"
	"testing"
)

func TestMakerAndKind(t *testing.T) {
	hik := Maker("44:19:b6:01:02:03")
	if !strings.Contains(hik, "Hikvision") || GuessKind(hik, 0) != KindCamera {
		t.Errorf("Hikvision: %q / %s", hik, GuessKind(hik, 0))
	}
	yea := Maker("80:5e:c0:aa:bb:cc")
	if GuessKind(yea, 0) != KindPhone {
		t.Errorf("Yealink %q: %s", yea, GuessKind(yea, 0))
	}
	if GuessKind("", Caps(0x04)) != KindPhone {
		t.Error("LLDP telephone not a phone")
	}
	if GuessKind("", Caps(0x10)) != KindAP {
		t.Error("LLDP AP not an AP")
	}
	if GuessKind("Hewlett Packard Enterprise", 0) != KindDevice || GuessKind("HP Inc.", 0) != KindPrinter {
		t.Error("HPE (Aruba network gear) must not be a printer; HP Inc. is")
	}
	if Maker("ff:ff:ff:00:00:00") != "" || GuessKind("Acme Widgets", 0) != KindDevice {
		t.Error("unknown maker")
	}
}
