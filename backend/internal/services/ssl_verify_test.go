package services

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netguard"
)

func boolPtr(b bool) *bool { return &b }

// Unset must mean "verify". A monitor created without the field — every monitor
// that existed before this column did — must not silently stop verifying.
func TestVerifyTLSDefaultsToOn(t *testing.T) {
	cases := []struct {
		name string
		m    models.Monitor
		want bool
	}{
		{"unset", models.Monitor{}, true},
		{"explicitly true", models.Monitor{SSLVerify: boolPtr(true)}, true},
		{"explicitly false", models.Monitor{SSLVerify: boolPtr(false)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.m.VerifyTLS(); got != tc.want {
				t.Errorf("VerifyTLS() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The toggle has to change what actually happens on the wire, not just what is
// stored. httptest's TLS server uses a certificate no system root trusts, so a
// verifying client must fail on it and a non-verifying one must succeed.
func TestGuardedClientHonoursCertificateVerification(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Run("verify=true rejects an untrusted certificate", func(t *testing.T) {
		client := netguard.NewGuardedHTTPClientTLS(5*time.Second, true)
		resp, err := client.Get(srv.URL)
		if err == nil {
			resp.Body.Close()
			t.Fatal("expected the request to fail on an untrusted certificate")
		}
	})

	t.Run("verify=false accepts it", func(t *testing.T) {
		client := netguard.NewGuardedHTTPClientTLS(5*time.Second, false)
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Fatalf("expected the request to succeed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})

	// The default constructor must behave like verify=true.
	t.Run("the plain constructor still verifies", func(t *testing.T) {
		client := netguard.NewGuardedHTTPClient(5 * time.Second)
		if resp, err := client.Get(srv.URL); err == nil {
			resp.Body.Close()
			t.Fatal("NewGuardedHTTPClient must verify certificates")
		}
	})
}

// Skipping verification must not also drop the SSRF dialer guard: the two are
// separate protections and turning one off must not quietly disable the other.
func TestSkippingVerificationKeepsTheDialerGuard(t *testing.T) {
	client := netguard.NewGuardedHTTPClientTLS(2*time.Second, false)
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("transport is not *http.Transport")
	}
	if tr.DialContext == nil {
		t.Error("the guarded dialer was dropped when verification was disabled")
	}
	if tr.TLSClientConfig == nil || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("InsecureSkipVerify was not set")
	}
	var _ *tls.Config = tr.TLSClientConfig
}
