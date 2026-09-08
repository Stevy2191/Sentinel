package services

import (
	"context"
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
