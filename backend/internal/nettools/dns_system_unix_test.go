//go:build !windows

package nettools

import (
	"strings"
	"testing"
)

func TestParseResolvConf(t *testing.T) {
	cases := []struct {
		name, conf, want, wantErr string
	}{
		{"first IPv4 after IPv6 ones", "# generated\nsearch lan\nnameserver fe80::1%eth0\nnameserver 2001:db8::53\n" +
			"nameserver 10.0.0.53\nnameserver 10.0.0.54\n", "10.0.0.53:53", ""},
		{"Docker's embedded DNS", "nameserver 127.0.0.11\noptions ndots:0\n", "127.0.0.11:53", ""},
		{"tabs and extra fields", "nameserver\t192.0.2.1  # office\n", "192.0.2.1:53", ""},
		{"only IPv6", "nameserver 2001:db8::53\n", "", "no IPv4 DNS server in /etc/resolv.conf"},
		{"no nameserver lines", "search lan\n", "", "no IPv4 DNS server in /etc/resolv.conf"},
		{"a bare keyword", "nameserver\n", "", "no IPv4 DNS server in /etc/resolv.conf"},
	}
	for _, c := range cases {
		got, err := parseResolvConf(strings.NewReader(c.conf))
		if got != c.want || (err == nil) != (c.wantErr == "") || err != nil && err.Error() != c.wantErr {
			t.Errorf("%s: %q, %v; want %q, %q", c.name, got, err, c.want, c.wantErr)
		}
	}
}

// The real file exists on every Unix-like test host and container.
func TestSystemResolver(t *testing.T) {
	addr, err := SystemResolver()
	if err != nil {
		t.Skipf("no usable /etc/resolv.conf here: %v", err)
	}
	if !strings.HasSuffix(addr, ":53") {
		t.Errorf("SystemResolver() = %q, want ip:53", addr)
	}
}
