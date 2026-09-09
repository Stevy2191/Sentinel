package services

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestOrderIPsPutsV4First(t *testing.T) {
	got := orderIPs([]string{"2606:4700::1111", "1.2.3.4", "2606:4700::1112", "5.6.7.8"})
	want := []string{"1.2.3.4", "5.6.7.8", "2606:4700::1111", "2606:4700::1112"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// An address literal has nothing to resolve, and asking a resolver about one
// fails, so it must be passed straight through.
func TestResolveHostAcceptsLiterals(t *testing.T) {
	for _, ip := range []string{"1.2.3.4", "::1"} {
		got, err := resolveHost(context.Background(), ip)
		if err != nil {
			t.Fatalf("%s: %v", ip, err)
		}
		if len(got.IPs) != 1 || got.IPs[0] != ip {
			t.Errorf("%s: got %v", ip, got.IPs)
		}
	}
}

func TestResolveHostUsesPublicDNSFirst(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network")
	}
	got, err := resolveHost(context.Background(), "github.com")
	if err != nil {
		t.Fatalf("github.com: %v", err)
	}
	if got.Via != "Google Public DNS" {
		t.Errorf("expected the first resolver to answer, got %q", got.Via)
	}
	if len(got.IPs) == 0 {
		t.Fatal("no addresses returned")
	}
	if net.ParseIP(got.IPs[0]) == nil {
		t.Errorf("not an address: %q", got.IPs[0])
	}
	t.Logf("github.com -> %v via %s", got.IPs, got.Via)
}

// A name no resolver knows must fail rather than hang, and must say that both
// public and system DNS were tried.
func TestResolveHostFailsClearly(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network")
	}
	start := time.Now()
	_, err := resolveHost(context.Background(), "definitely-not-a-real-host.invalid")
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), "public or system DNS") {
		t.Errorf("unhelpful error: %v", err)
	}
	// Three resolvers at five seconds each is the ceiling.
	if elapsed := time.Since(start); elapsed > 3*resolverTimeout+2*time.Second {
		t.Errorf("took %v, longer than the resolver budget allows", elapsed)
	}
	t.Logf("after %v: %v", time.Since(start).Round(time.Millisecond), err)
}

// The fallback is the whole point of the chain: where a network blocks
// outbound DNS, or the public resolvers are unreachable, checks must keep
// working through the host's own resolver rather than failing.
func TestResolveHostFallsBackToSystemDNS(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network")
	}
	original := certResolvers
	t.Cleanup(func() { certResolvers = original })

	// Both public resolvers replaced with addresses that answer nothing.
	certResolvers = []struct {
		name string
		addr string
	}{
		{"blackhole A", "203.0.113.253:53"},
		{"blackhole B", "203.0.113.254:53"},
		{"system DNS", ""},
	}

	start := time.Now()
	got, err := resolveHost(context.Background(), "github.com")
	if err != nil {
		t.Fatalf("expected the system resolver to answer: %v", err)
	}
	if got.Via != "system DNS" {
		t.Errorf("expected the fallback to answer, got %q", got.Via)
	}
	if len(got.IPs) == 0 {
		t.Fatal("no addresses returned")
	}
	// Two dead resolvers at five seconds each, then the system one.
	if elapsed := time.Since(start); elapsed > 2*resolverTimeout+3*time.Second {
		t.Errorf("fallback took %v, longer than the budget allows", elapsed)
	}
	t.Logf("fell back to %s in %v -> %v", got.Via, time.Since(start).Round(time.Millisecond), got.IPs)
}
