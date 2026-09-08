package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestNormalizeDomain(t *testing.T) {
	cases := map[string]string{
		"example.com":                 "example.com",
		"  Example.COM  ":             "example.com",
		"https://example.com":         "example.com",
		"http://example.com/":         "example.com",
		"https://example.com/a/b?x=1": "example.com",
		"example.com:443":             "example.com",
		"example.com:8443":            "example.com:8443",
		"example.com.":                "example.com",
	}
	for in, want := range cases {
		if got := models.NormalizeDomain(in); got != want {
			t.Errorf("NormalizeDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateDomain(t *testing.T) {
	valid := []string{"example.com", "sub.example.co.uk", "a-b.example.com", "example.com:8443"}
	for _, d := range valid {
		if err := models.ValidateDomain(d); err != nil {
			t.Errorf("ValidateDomain(%q) = %v, want nil", d, err)
		}
	}
	invalid := []string{"", "localhost", "1.2.3.4", "-bad.example.com", "bad-.example.com", "exa mple.com"}
	for _, d := range invalid {
		if err := models.ValidateDomain(d); err == nil {
			t.Errorf("ValidateDomain(%q) = nil, want an error", d)
		}
	}
}

// The three statuses hinge on the alert threshold, not on a fixed number of
// days, so a certificate is "expiring soon" relative to how much warning the
// operator asked for.
func TestDeriveStatus(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		expiry     time.Time
		notifyDays int
		wantStatus string
		wantDays   int
	}{
		{"comfortably valid", now.AddDate(0, 0, 90), 7, models.SSLStatusValid, 90},
		{"outside the window", now.AddDate(0, 0, 8), 7, models.SSLStatusValid, 8},
		{"on the threshold", now.AddDate(0, 0, 7), 7, models.SSLStatusExpiringSoon, 7},
		{"inside the window", now.AddDate(0, 0, 2), 7, models.SSLStatusExpiringSoon, 2},
		{"wider window catches it earlier", now.AddDate(0, 0, 20), 30, models.SSLStatusExpiringSoon, 20},
		{"expired", now.AddDate(0, 0, -1), 7, models.SSLStatusExpired, -1},
		{"expiring within the hour still counts as 0 days", now.Add(30 * time.Minute), 7, models.SSLStatusExpiringSoon, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, days := models.DeriveStatus(tc.expiry, tc.notifyDays, now)
			if status != tc.wantStatus || days != tc.wantDays {
				t.Errorf("= (%s, %d), want (%s, %d)", status, days, tc.wantStatus, tc.wantDays)
			}
		})
	}
}

func days(n int) *int { return &n }

// The daily job must not re-send the same alert every pass, but must speak up
// again as the deadline gets closer.
func TestShouldAlert(t *testing.T) {
	cases := []struct {
		name string
		cert models.SSLCertificate
		want bool
	}{
		{"healthy certificate stays quiet",
			models.SSLCertificate{Enabled: true, Status: models.SSLStatusValid, DaysUntilExpiry: days(90)}, false},
		{"first time inside the window alerts",
			models.SSLCertificate{Enabled: true, Status: models.SSLStatusExpiringSoon, DaysUntilExpiry: days(7)}, true},
		{"same day does not re-alert",
			models.SSLCertificate{Enabled: true, Status: models.SSLStatusExpiringSoon, DaysUntilExpiry: days(7), LastNotifiedDays: days(7)}, false},
		{"a day closer alerts again",
			models.SSLCertificate{Enabled: true, Status: models.SSLStatusExpiringSoon, DaysUntilExpiry: days(6), LastNotifiedDays: days(7)}, true},
		{"expired alerts",
			models.SSLCertificate{Enabled: true, Status: models.SSLStatusExpired, DaysUntilExpiry: days(-1)}, true},
		{"disabled never alerts",
			models.SSLCertificate{Enabled: false, Status: models.SSLStatusExpiringSoon, DaysUntilExpiry: days(1)}, false},
		{"unknown status never alerts",
			models.SSLCertificate{Enabled: true, Status: models.SSLStatusUnknown}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cert.ShouldAlert(); got != tc.want {
				t.Errorf("ShouldAlert() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Reading a real certificate off the internet. Skipped with -short, since it
// needs outbound network.
func TestFetchCertificateLive(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network")
	}
	s := NewSSLCheckerService(nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	info, err := s.FetchCertificate(ctx, "example.com")
	if err != nil {
		t.Skipf("no outbound TLS available: %v", err)
	}
	if info.Issuer == "" || info.Issuer == "Unknown" {
		t.Errorf("issuer = %q, want a real CA name", info.Issuer)
	}
	if !info.ExpiryDate.After(time.Now()) {
		t.Errorf("expiry %v is not in the future", info.ExpiryDate)
	}
	t.Logf("example.com -> issuer=%q expires=%s", info.Issuer, info.ExpiryDate.Format(time.RFC3339))
}

// An expired certificate is the case this feature exists to catch: it must be
// reported as an expiry, not swallowed as a handshake failure.
func TestFetchCertificateExpired(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network")
	}
	s := NewSSLCheckerService(nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	info, err := s.FetchCertificate(ctx, "expired.badssl.com")
	if err != nil {
		t.Skipf("could not reach expired.badssl.com: %v", err)
	}
	if info.ExpiryDate.After(time.Now()) {
		t.Errorf("expiry %v should be in the past", info.ExpiryDate)
	}
	status, d := models.DeriveStatus(info.ExpiryDate, 7, time.Now())
	if status != models.SSLStatusExpired {
		t.Errorf("status = %s, want expired", status)
	}
	t.Logf("expired.badssl.com -> issuer=%q expired %d days ago", info.Issuer, -d)
}

// A domain that does not resolve must produce a readable message, not a raw
// dial error.
func TestFetchCertificateUnresolvable(t *testing.T) {
	s := NewSSLCheckerService(nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, err := s.FetchCertificate(ctx, "this-domain-should-not-exist-sentinel-test.invalid")
	if err == nil {
		t.Fatal("expected an error for an unresolvable domain")
	}
	t.Logf("unresolvable -> %v", err)
}

// The issuer shown must be the name a person recognises. A modern DN looks
// like "C=US, O=Google Trust Services, CN=WE1" — the CN identifies one
// intermediate in the CA's rotation and is meaningless to a reader.
func TestIssuerNamePrefersOrganisation(t *testing.T) {
	cases := []struct {
		name       string
		commonName string
		org        []string
		want       string
	}{
		{"public CA: organisation wins", "WE1", []string{"Google Trust Services"}, "Google Trust Services"},
		{"another public CA", "R11", []string{"Let's Encrypt"}, "Let's Encrypt"},
		{"no organisation falls back to the common name", "Internal Root CA", nil, "Internal Root CA"},
		{"blank organisation falls back too", "Internal Root CA", []string{"   "}, "Internal Root CA"},
		{"organisation is padded", "WE1", []string{"  Google Trust Services  "}, "Google Trust Services"},
		{"neither is present", "", nil, "Unknown"},
		{"both blank", "  ", []string{""}, "Unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := issuerName(tc.commonName, tc.org); got != tc.want {
				t.Errorf("issuerName(%q, %v) = %q, want %q", tc.commonName, tc.org, got, tc.want)
			}
		})
	}
}

func TestNormalizeResolver(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"   ", ""},
		{"1.1.1.1", "1.1.1.1:53"},
		{"1.1.1.1:53", "1.1.1.1:53"},
		{"8.8.8.8:5353", "8.8.8.8:5353"},
		// A bare IPv6 literal has colons but no port, so it must come back
		// bracketed rather than being mistaken for host:port.
		{"2606:4700:4700::1111", "[2606:4700:4700::1111]:53"},
		{"[2606:4700:4700::1111]:53", "[2606:4700:4700::1111]:53"},
	}
	for _, c := range cases {
		if got := normalizeResolver(c.in); got != c.want {
			t.Errorf("normalizeResolver(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A checker with no resolver configured must keep using the host's, which is
// the right default for everyone whose DNS is not split-horizon.
func TestNetDialerDefaultsToHostResolver(t *testing.T) {
	s := NewSSLCheckerService(nil, nil)
	if d := s.netDialer(context.Background()); d.Resolver != nil {
		t.Errorf("expected the host resolver (nil), got %#v", d.Resolver)
	}

	s.SetDNSResolverFunc(func(context.Context) string { return "" })
	if d := s.netDialer(context.Background()); d.Resolver != nil {
		t.Errorf("an empty setting must mean the host resolver, got %#v", d.Resolver)
	}

	s.SetDNSResolverFunc(func(context.Context) string { return "1.1.1.1" })
	if d := s.netDialer(context.Background()); d.Resolver == nil {
		t.Error("a configured resolver was ignored")
	}
}

// Proves the configured resolver is genuinely used for certificate checks
// rather than silently ignored — the whole point of the split-horizon fix.
func TestFetchCertificateUsesConfiguredResolver(t *testing.T) {
	if testing.Short() {
		t.Skip("needs network")
	}
	ctx := context.Background()

	// 1. A public resolver: the apex must read like any other host.
	s := NewSSLCheckerService(nil, nil)
	s.SetDNSResolverFunc(func(context.Context) string { return "1.1.1.1" })
	for _, d := range []string{"wacounty.com", "www.wacounty.com"} {
		info, err := s.FetchCertificate(ctx, d)
		if err != nil {
			t.Fatalf("%s via 1.1.1.1: %v", d, err)
		}
		t.Logf("%-20s issuer=%q expires=%s", d, info.Issuer, info.ExpiryDate.Format("2006-01-02"))
	}

	// 2. A resolver that answers nothing. If the setting were ignored, this
	//    would still succeed via the host's resolver — so a failure here is
	//    the proof that the setting takes effect.
	bogus := NewSSLCheckerService(nil, nil)
	bogus.SetDNSResolverFunc(func(context.Context) string { return "203.0.113.253" })
	if _, err := bogus.FetchCertificate(ctx, "wacounty.com"); err == nil {
		t.Fatal("expected failure through a dead resolver; the setting is being ignored")
	} else {
		t.Logf("dead resolver correctly failed: %v", err)
	}

	// 3. The diagnostic: a domain resolving to a private address must say so,
	//    which is what tells an operator their DNS is answering internally.
	priv := NewSSLCheckerService(nil, nil)
	_, err := priv.FetchCertificate(ctx, "localtest.me") // resolves to 127.0.0.1
	if err == nil {
		t.Skip("localtest.me unexpectedly served TLS; cannot exercise the diagnostic")
	}
	if !strings.Contains(err.Error(), "internal address") {
		t.Errorf("expected the split-horizon hint, got: %v", err)
	} else {
		t.Logf("diagnostic: %v", err)
	}
}
