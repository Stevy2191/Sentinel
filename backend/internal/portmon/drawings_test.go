package portmon

import (
	"fmt"
	"testing"
)

func TestDrawingFor(t *testing.T) {
	for model, want := range map[string]string{
		"UDM-SE": DrawingUDMSE, " udm-se ": DrawingUDMSE,
		"UBNT-US48PRO-POE": DrawingUSWPro48PoE, "USW-Pro-48-PoE": DrawingUSWPro48PoE,
		"UBNT-ES16XG": DrawingES16XG,
		"USW-Pro-48":  "", "UDM-Pro": "", "": "",
	} {
		got := ""
		if d := DrawingFor(model); d != nil {
			got = d.Key
		}
		if got != want {
			t.Errorf("DrawingFor(%q) = %q, want %q", model, got, want)
		}
	}
	if d := DrawingFor("UDM-SE"); d == nil || d.DriveBayDisk != "sdb" {
		t.Errorf("UDM-SE drive bay disk: %+v", d)
	}
	if d := DrawingFor("UBNT-ES16XG"); d == nil || d.DriveBayDisk != "" {
		t.Errorf("ES-16-XG has no drive bay: %+v", d)
	}
}

func TestLayoutUnitsDrawing(t *testing.T) {
	cases := []struct {
		name  string
		ports []LayoutPort
		o     LayoutOptions
		want  string
	}{
		{"udm-se", udmSELayoutPorts(), LayoutOptions{Model: "UDM-SE", Numbering: NumberingFor(udmSEDescr)}, DrawingUDMSE},
		{"udm-se numbered from 0", udmSELayoutPorts(), LayoutOptions{Model: "UDM-SE"}, ""},
		{"usw-pro-48", uswLayoutPorts(), LayoutOptions{Model: "UBNT-US48PRO-POE"}, DrawingUSWPro48PoE},
		{"es-16-xg", es16XGLayoutPorts(), LayoutOptions{Model: "UBNT-ES16XG"}, DrawingES16XG},
		{"other model", uswLayoutPorts(), LayoutOptions{Model: "USW-48"}, ""},
	}
	for _, c := range cases {
		units := LayoutUnits(c.ports, c.o)
		if len(units) != 1 || units[0].Drawing != c.want || len(units[0].Blocks) == 0 {
			t.Errorf("%s: %+v", c.name, units)
		}
	}
}

// A model match is not enough: the ports must be exactly the drawing's.
func TestLayoutUnitsDrawingNeedsExactPorts(t *testing.T) {
	es := es16XGLayoutPorts()
	if u := LayoutUnits(es[:15], LayoutOptions{Model: "UBNT-ES16XG"}); u[0].Drawing != "" {
		t.Error("15 ports drawn as an ES-16-XG")
	}
	extra := append(append([]LayoutPort{}, es...), LayoutPort{IfIndex: 99, Name: "0/17"})
	if u := LayoutUnits(extra, LayoutOptions{Model: "UBNT-ES16XG"}); u[0].Drawing != "" {
		t.Error("17 ports drawn as an ES-16-XG")
	}
	var stack []LayoutPort
	for unit := 1; unit <= 2; unit++ {
		for n := 1; n <= 16; n++ {
			if unit == 2 && n == 3 {
				continue
			}
			stack = append(stack, LayoutPort{IfIndex: unit*100 + n, Name: fmt.Sprintf("%d/0/%d", unit, n), Unit: unit})
		}
	}
	u := LayoutUnits(stack, LayoutOptions{Model: "UBNT-ES16XG"})
	if len(u) != 2 || u[0].Drawing != DrawingES16XG || u[1].Drawing != "" {
		t.Errorf("stack: %+v / %+v", u[0].Drawing, u[1].Drawing)
	}
}
