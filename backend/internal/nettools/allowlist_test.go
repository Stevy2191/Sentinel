package nettools

import (
	"net"
	"reflect"
	"testing"
)

func TestParseAllowlistErrors(t *testing.T) {
	a, bad := ParseAllowlist([]string{
		"10.0.0.0/8",      // ok: /8 is the broadest allowed
		"10.0.0.0/7",      // too broad
		"0.0.0.0/0",       // too broad
		"192.0.2.10",      // ok
		" ",               // blank: ignored
		"fileserver",      // ok: exact name
		"*.Example.ORG.",  // ok: wildcard
		"*",               // bad
		"*.org",           // bad: the wildcard needs a dotted domain
		"bad_host.local",  // bad: underscore
		"-lead.example",   // bad: label starts with a hyphen
		"10.0.0.256",      // bad: not an address, and not a name
		"10.0.0.0/33",     // bad CIDR
		"2001:db8::1",     // IPv6
		"2001:db8::/32",   // IPv6
		"fe80::1%eth0:80", // bad
	})
	want := []EntryError{
		{"10.0.0.0/7", "too broad: use /8 or narrower"},
		{"0.0.0.0/0", "too broad: use /8 or narrower"},
		{"*", "not a valid IPv4 address, CIDR or host name"},
		{"*.org", "not a valid IPv4 address, CIDR or host name"},
		{"bad_host.local", "not a valid IPv4 address, CIDR or host name"},
		{"-lead.example", "not a valid IPv4 address, CIDR or host name"},
		{"10.0.0.256", "not a valid IPv4 address, CIDR or host name"},
		{"10.0.0.0/33", "not a valid IPv4 address, CIDR or host name"},
		{"2001:db8::1", "IPv6 is not supported yet"},
		{"2001:db8::/32", "IPv6 is not supported yet"},
		{"fe80::1%eth0:80", "not a valid IPv4 address, CIDR or host name"},
	}
	if !reflect.DeepEqual(bad, want) {
		t.Errorf("errors:\n got %v\nwant %v", bad, want)
	}
	if a.Empty() {
		t.Fatal("the good entries were dropped")
	}
	if !a.Allows("10.200.0.1", net.ParseIP("10.200.0.1")) || !a.Allows("192.0.2.10", net.ParseIP("192.0.2.10")) {
		t.Error("the good address entries do not match")
	}
	if !a.Allows("FileServer.", nil) || !a.Allows("www.example.org", nil) {
		t.Error("the good name entries do not match")
	}
}

func TestAllowlistLongNames(t *testing.T) {
	label63 := "a123456789b123456789c123456789d123456789e123456789f123456789abc"
	if _, bad := ParseAllowlist([]string{label63 + ".example"}); len(bad) != 0 {
		t.Errorf("a 63-character label: %v", bad)
	}
	if _, bad := ParseAllowlist([]string{label63 + "x.example"}); len(bad) != 1 {
		t.Errorf("a 64-character label was accepted")
	}
}

func TestAllowlistAllows(t *testing.T) {
	a, bad := ParseAllowlist([]string{"10.1.0.0/16", "192.0.2.10", "nas.lab", "*.corp.example", "*.metadata.test"})
	if len(bad) != 0 {
		t.Fatal(bad)
	}
	ip := net.ParseIP
	cases := []struct {
		name   string
		target string
		ip     net.IP
		want   bool
	}{
		{"inside the CIDR", "10.1.2.3", ip("10.1.2.3"), true},
		{"outside the CIDR", "10.2.0.1", ip("10.2.0.1"), false},
		{"single address", "192.0.2.10", ip("192.0.2.10"), true},
		{"neighbour of the single address", "192.0.2.11", ip("192.0.2.11"), false},
		{"name typed, address elsewhere", "nas.lab", ip("172.16.5.5"), true},
		{"name in another case, trailing dot", "NAS.Lab.", ip("172.16.5.5"), true},
		{"name not listed, address in the CIDR", "printer.lab", ip("10.1.9.9"), true},
		{"name not listed, address not either", "printer.lab", ip("172.16.5.6"), false},
		{"wildcard, one level", "db.corp.example", ip("172.16.0.1"), true},
		{"wildcard, two levels", "a.b.corp.example", ip("172.16.0.1"), true},
		{"wildcard does not match the bare domain", "corp.example", ip("172.16.0.1"), false},
		{"wildcard does not match a lookalike", "xcorp.example", ip("172.16.0.1"), false},
		{"pure name match without an address", "nas.lab", nil, true},
		{"no name match and no address", "other.lab", nil, false},
		{"IPv6 address", "nas.lab", ip("2001:db8::1"), false},
		// Review Focus 3: a listed name that resolves to an always-blocked address.
		{"name entry, metadata address", "x.metadata.test", ip("169.254.169.254"), false},
		{"name entry, ECS metadata", "nas.lab", ip("169.254.170.2"), false},
		{"name entry, Alibaba metadata", "nas.lab", ip("100.100.100.200"), false},
		{"name entry, multicast", "nas.lab", ip("239.1.2.3"), false},
		{"name entry, this network", "nas.lab", ip("0.1.2.3"), false},
		{"name entry, broadcast", "nas.lab", ip("255.255.255.255"), false},
	}
	for _, c := range cases {
		if got := a.Allows(c.target, c.ip); got != c.want {
			t.Errorf("%s: Allows(%q, %v) = %v, want %v", c.name, c.target, c.ip, got, c.want)
		}
	}
}

func TestAllowlistBlockedEvenWhenListed(t *testing.T) {
	a, bad := ParseAllowlist([]string{"169.254.0.0/16", "224.0.0.0/8", "0.0.0.0/8"})
	if len(bad) != 0 {
		t.Fatal(bad)
	}
	for _, s := range []string{"169.254.169.254", "224.0.0.1", "0.0.0.0"} {
		if a.Allows(s, net.ParseIP(s)) {
			t.Errorf("%s allowed", s)
		}
	}
	if !a.Allows("169.254.1.1", net.ParseIP("169.254.1.1")) {
		t.Error("an ordinary link-local address in a listed CIDR was refused")
	}
}

func TestAllowlistEmpty(t *testing.T) {
	for _, entries := range [][]string{nil, {}, {"", "  "}, {"*"}} {
		a, _ := ParseAllowlist(entries)
		if !a.Empty() {
			t.Errorf("%q: not empty", entries)
		}
		if a.Allows("10.0.0.1", net.ParseIP("10.0.0.1")) || a.Allows("fileserver", nil) {
			t.Errorf("%q: an empty list allowed something", entries)
		}
	}
	var none *Allowlist
	if !none.Empty() || none.Allows("x", net.ParseIP("10.0.0.1")) {
		t.Error("a nil list allowed something")
	}
}

func TestAlwaysBlocked(t *testing.T) {
	cases := map[string]bool{
		"169.254.169.254": true, "169.254.170.2": true, "100.100.100.200": true,
		"0.0.0.0": true, "0.255.255.255": true, "224.0.0.1": true, "239.255.255.255": true,
		"255.255.255.255": true, "2001:db8::1": true,
		"169.254.169.253": false, "1.0.0.0": false, "223.255.255.255": false, "240.0.0.1": false,
		"10.0.0.1": false, "127.0.0.1": false, "255.255.255.254": false,
	}
	for s, want := range cases {
		if got := AlwaysBlocked(net.ParseIP(s)); got != want {
			t.Errorf("AlwaysBlocked(%s) = %v, want %v", s, got, want)
		}
	}
	if !AlwaysBlocked(nil) {
		t.Error("AlwaysBlocked(nil) = false")
	}
}

func TestParseAddressList(t *testing.T) {
	nets, err := ParseAddressList(" 10.0.0.0/24, 192.0.2.7 ,,")
	if err != nil || len(nets) != 2 {
		t.Fatalf("got %v, %v", nets, err)
	}
	for s, want := range map[string]bool{"10.0.0.255": true, "10.0.1.0": false, "192.0.2.7": true, "192.0.2.8": false} {
		if got := InNets(net.ParseIP(s), nets); got != want {
			t.Errorf("InNets(%s) = %v, want %v", s, got, want)
		}
	}
	if nets, err := ParseAddressList(""); nets != nil || err != nil {
		t.Errorf(`"" = %v, %v; want nil, nil`, nets, err)
	}
	for _, s := range []string{"10.0.0.0/33", "fileserver", "2001:db8::/32", "10.0.0.1,nope"} {
		if _, err := ParseAddressList(s); err == nil {
			t.Errorf("%q accepted", s)
		}
	}
	if InNets(net.ParseIP("2001:db8::1"), nets) || InNets(nil, nets) {
		t.Error("InNets matched a non-IPv4 address")
	}
}
