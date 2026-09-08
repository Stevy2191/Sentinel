package services

import (
	"context"
	"testing"
	"time"
)

// The registrable domain is what a registration is held under. "Last two
// labels" would treat co.uk and github.io as registrable and look up records
// that do not exist, so the public suffix list decides.
func TestRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"example.com":        "example.com",
		"www.example.com":    "example.com",
		"a.b.c.example.com":  "example.com",
		"EXAMPLE.COM":        "example.com",
		"example.com:8443":   "example.com",
		"example.com.":       "example.com",
		"example.co.uk":      "example.co.uk",
		"sub.example.co.uk":  "example.co.uk",
		"thing.github.io":    "thing.github.io",
		"wacounty.com":       "wacounty.com",
		"media.wacounty.com": "wacounty.com",
	}
	for in, want := range cases {
		got, err := RegistrableDomain(in)
		if err != nil {
			t.Errorf("RegistrableDomain(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("RegistrableDomain(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "   ", "."} {
		if _, err := RegistrableDomain(bad); err == nil {
			t.Errorf("RegistrableDomain(%q) should have errored", bad)
		}
	}
}

func TestParseRDAPTime(t *testing.T) {
	for _, raw := range []string{
		"2026-09-29T17:06:52Z",
		"2026-09-29T17:06:52-0500",
		"2026-09-29T17:06:52",
		"2026-09-29 17:06:52",
		"2026-09-29",
	} {
		if _, err := parseRDAPTime(raw); err != nil {
			t.Errorf("parseRDAPTime(%q) errored: %v", raw, err)
		}
	}
	if _, err := parseRDAPTime("not a date"); err == nil {
		t.Error("parseRDAPTime should reject nonsense")
	}
}

// A live lookup. Skipped with -short since it needs outbound HTTPS.
func TestLookupRegistrationLive(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, host := range []string{"wacounty.com", "media.wacounty.com", "github.com"} {
		info, err := LookupRegistration(ctx, host)
		if err != nil {
			t.Logf("%-22s -> %v", host, err)
			continue
		}
		if info.ExpiryDate.IsZero() {
			t.Errorf("%s returned no expiry date", host)
		}
		t.Logf("%-22s -> domain=%s registrar=%q expires=%s",
			host, info.Domain, info.Registrar, info.ExpiryDate.Format("2006-01-02"))
	}
}
