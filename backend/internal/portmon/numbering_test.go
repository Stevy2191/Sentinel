package portmon

import (
	"reflect"
	"testing"
)

const udmSEDescr = "Ubiquiti UniFi UDM-SE 5.1.33 Linux 4.19.152 al324"

// UniFi OS consoles name their ports eth0, eth1… but print 1, 2… on the
// case; other devices keep the number in the name.
func TestNumberingFor(t *testing.T) {
	unifi := NumberingFor(udmSEDescr)
	for name, want := range map[string]int{"eth0": 1, "eth7": 8, "eth8": 9, "eth10": 11, "eth10.10": 10, "0/5": 5} {
		if got := unifi.Number(name, name, 99); got != want {
			t.Errorf("UniFi console %s = %d, want %d", name, got, want)
		}
	}
	if got := unifi.Label("eth9", "", 3); got != "10" {
		t.Errorf("UniFi console eth9 label %q, want 10", got)
	}
	for _, descr := range []string{"EdgeOS v2.0.9 ER-4", "USW-Pro-48-PoE, 7.5.15.17146, Linux 3.6.5", ""} {
		nb := NumberingFor(descr)
		if got := nb.Number("eth0", "eth0", 16); got != 0 {
			t.Errorf("%q: eth0 = %d, want 0 (the name's own number)", descr, got)
		}
		if got := nb.Label("Gi1/0/12", "", 5); got != "0/12" {
			t.Errorf("%q: Gi1/0/12 label %q", descr, got)
		}
	}
}

func udmSELayoutPorts() []LayoutPort {
	var ps []LayoutPort
	for _, f := range udmSE() {
		if IsPhysical(info(f)) {
			ps = append(ps, LayoutPort{IfIndex: f.Index, Name: f.Name, Descr: f.Descr})
		}
	}
	return ps
}

// The UDM-SE draws as the real one: ports 1-8 two high, the 2.5G WAN port 9
// on its own, then the two SFP+ cages stacked (10 over 11).
func TestLayoutUDMSEBuiltin(t *testing.T) {
	fp := Layout(udmSELayoutPorts(), LayoutOptions{Model: "UDM-SE", Numbering: NumberingFor(udmSEDescr)})
	if fp.Rows != 2 || len(fp.Blocks) != 3 {
		t.Fatalf("rows %d blocks %+v", fp.Rows, fp.Blocks)
	}
	lan, wan, sfp := fp.Blocks[0], fp.Blocks[1], fp.Blocks[2]
	if lan.SFP || !reflect.DeepEqual(numbers(lan.Top), []int{1, 3, 5, 7}) || !reflect.DeepEqual(numbers(lan.Bottom), []int{2, 4, 6, 8}) {
		t.Errorf("LAN block %+v", lan)
	}
	if wan.SFP || wan.Rows != 1 || !reflect.DeepEqual(numbers(wan.Top), []int{9}) {
		t.Errorf("WAN block %+v", wan)
	}
	if !sfp.SFP || !reflect.DeepEqual(numbers(sfp.Top), []int{10}) || !reflect.DeepEqual(numbers(sfp.Bottom), []int{11}) {
		t.Errorf("SFP block %+v", sfp)
	}
	if got := MainPortNumbers(udmSELayoutPorts(), NumberingFor(udmSEDescr)); !reflect.DeepEqual(got, seq(1, 11)) {
		t.Errorf("main port numbers %v", got)
	}
}
