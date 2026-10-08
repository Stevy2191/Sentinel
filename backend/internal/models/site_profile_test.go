package models

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// strp is defined in site_test.go.
func intp(v int) *int         { return &v }
func f64p(v float64) *float64 { return &v }

func TestNormalizeSiteNetworkInput(t *testing.T) {
	cases := []struct {
		name string
		in   SiteNetworkInput
		want SiteNetworkInput
		err  string
	}{
		{"host address becomes the network",
			SiteNetworkInput{Name: " Staff ", CIDR: " 10.20.0.5/24 ", VLAN: intp(10), Gateway: strp(" 10.20.0.1 "), Note: strp(" front desk ")},
			SiteNetworkInput{Name: "Staff", CIDR: "10.20.0.0/24", VLAN: intp(10), Gateway: strp("10.20.0.1"), Note: strp("front desk")}, ""},
		{"ipv6", SiteNetworkInput{Name: "v6", CIDR: "2001:db8::1/64"}, SiteNetworkInput{Name: "v6", CIDR: "2001:db8::/64"}, ""},
		{"blank optionals are nil", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", Gateway: strp("  "), Note: strp("")},
			SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8"}, ""},
		{"name required", SiteNetworkInput{Name: " ", CIDR: "10.0.0.0/8"}, SiteNetworkInput{}, "name is required"},
		{"name too long", SiteNetworkInput{Name: strings.Repeat("n", 101), CIDR: "10.0.0.0/8"}, SiteNetworkInput{}, "name must be 100 characters or fewer"},
		{"subnet required", SiteNetworkInput{Name: "x"}, SiteNetworkInput{}, "subnet is required"},
		{"not a subnet", SiteNetworkInput{Name: "x", CIDR: "10.20.0.0"}, SiteNetworkInput{}, `subnet "10.20.0.0" is not a network like 10.20.0.0/24`},
		{"vlan zero", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", VLAN: intp(0)}, SiteNetworkInput{}, "VLAN must be between 1 and 4094"},
		{"vlan too big", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", VLAN: intp(4095)}, SiteNetworkInput{}, "VLAN must be between 1 and 4094"},
		{"gateway not an address", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", Gateway: strp("router")}, SiteNetworkInput{}, `gateway "router" is not an IP address`},
		{"gateway outside", SiteNetworkInput{Name: "x", CIDR: "10.20.0.0/24", Gateway: strp("10.30.0.1")}, SiteNetworkInput{}, "10.30.0.1 is outside 10.20.0.0/24"},
		{"gateway other family", SiteNetworkInput{Name: "x", CIDR: "10.20.0.0/24", Gateway: strp("2001:db8::1")}, SiteNetworkInput{}, "2001:db8::1 is outside 10.20.0.0/24"},
		{"note too long", SiteNetworkInput{Name: "x", CIDR: "10.0.0.0/8", Note: strp(strings.Repeat("a", 501))}, SiteNetworkInput{}, "note must be 500 characters or fewer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeSiteNetworkInput(c.in)
			if c.err != "" {
				if err == nil || err.Error() != c.err {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestNormalizeSiteCircuitInput(t *testing.T) {
	port := uuid.New()
	cases := []struct {
		name string
		in   SiteCircuitInput
		want SiteCircuitInput
		err  string
	}{
		{"trimmed, kind lowered, decimals kept",
			SiteCircuitInput{Provider: " Spectrum ", Kind: " Fiber ", DownloadMbps: f64p(500), UploadMbps: f64p(1.5),
				CircuitRef: strp(" 12/KFGN/0391 "), SupportPhone: strp(" 1-800-892-4357 "), AccountNumber: strp(" 8347 "), Notes: strp(" static IP "), InterfaceID: &port},
			SiteCircuitInput{Provider: "Spectrum", Kind: "fiber", DownloadMbps: f64p(500), UploadMbps: f64p(1.5),
				CircuitRef: strp("12/KFGN/0391"), SupportPhone: strp("1-800-892-4357"), AccountNumber: strp("8347"), Notes: strp("static IP"), InterfaceID: &port}, ""},
		{"blank kind is other", SiteCircuitInput{Provider: "AT&T"}, SiteCircuitInput{Provider: "AT&T", Kind: "other"}, ""},
		{"provider required", SiteCircuitInput{Provider: "  "}, SiteCircuitInput{}, "provider is required"},
		{"provider too long", SiteCircuitInput{Provider: strings.Repeat("p", 101)}, SiteCircuitInput{}, "provider must be 100 characters or fewer"},
		{"unknown kind", SiteCircuitInput{Provider: "x", Kind: "satellite"}, SiteCircuitInput{}, "type must be one of fiber, cable, dsl, fixed_wireless, cellular, copper, other"},
		{"zero download", SiteCircuitInput{Provider: "x", DownloadMbps: f64p(0)}, SiteCircuitInput{}, "download speed must be more than 0 and at most 100000 Mbps"},
		{"huge upload", SiteCircuitInput{Provider: "x", UploadMbps: f64p(100001)}, SiteCircuitInput{}, "upload speed must be more than 0 and at most 100000 Mbps"},
		{"circuit id too long", SiteCircuitInput{Provider: "x", CircuitRef: strp(strings.Repeat("c", 101))}, SiteCircuitInput{}, "circuit ID must be 100 characters or fewer"},
		{"account too long", SiteCircuitInput{Provider: "x", AccountNumber: strp(strings.Repeat("a", 101))}, SiteCircuitInput{}, "account number must be 100 characters or fewer"},
		{"phone too long", SiteCircuitInput{Provider: "x", SupportPhone: strp(strings.Repeat("9", 51))}, SiteCircuitInput{}, "support phone must be 50 characters or fewer"},
		{"notes too long", SiteCircuitInput{Provider: "x", Notes: strp(strings.Repeat("n", 1001))}, SiteCircuitInput{}, "notes must be 1000 characters or fewer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeSiteCircuitInput(c.in)
			if c.err != "" {
				if err == nil || err.Error() != c.err {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestNormalizeSiteNotes(t *testing.T) {
	got, err := NormalizeSiteNotes("  Closet: room 104\nKey at the front desk  ")
	if err != nil || got == nil || *got != "Closet: room 104\nKey at the front desk" {
		t.Errorf("got %v, %v", got, err)
	}
	if got, err := NormalizeSiteNotes(" \n "); err != nil || got != nil {
		t.Errorf("blank: got %v, %v; want nil", got, err)
	}
	if _, err := NormalizeSiteNotes(strings.Repeat("n", 10001)); err == nil || err.Error() != "notes must be 10000 characters or fewer" {
		t.Errorf("long: err = %v", err)
	}
}
