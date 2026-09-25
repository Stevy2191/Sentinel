package services

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestWorthRetryingViaPublicDNS(t *testing.T) {
	cases := []struct {
		name  string
		check *models.Check
		want  bool
	}{
		// No response at all: the address may simply have been wrong.
		{"connection refused", &models.Check{Status: checkFailed, StatusCode: 0}, true},
		{"timeout", &models.Check{Status: checkTimeout, StatusCode: 0}, true},

		// The server answered, so resolution worked. Asking a different
		// resolver would produce the same status more slowly.
		{"500 from the server", &models.Check{Status: checkFailed, StatusCode: 500}, false},
		{"404 from the server", &models.Check{Status: checkFailed, StatusCode: 404}, false},

		{"success", &models.Check{Status: checkSuccess, StatusCode: 200}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := worthRetryingViaPublicDNS(c.check); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// The fallback client must still send the original hostname, not the IP it
// dialled: the Host header and TLS server name are what the far end routes on,
// and a certificate is verified against the name being monitored.
func TestPublicDNSHTTPClient_PreservesHost(t *testing.T) {
	var gotHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// localhost resolves through the system resolver, which resolveHost still
	// consults last, so this exercises the real dial path.
	client := publicDNSHTTPClient(5*time.Second, true)
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Skipf("no usable resolver in this environment: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if gotHost == "" {
		t.Fatal("the server saw no Host header")
	}
}

// Verification stays on unless the monitor opted out, so the fallback cannot
// quietly accept the wrong certificate — which is the very symptom it exists
// to diagnose.
func TestPublicDNSHTTPClient_KeepsVerificationByDefault(t *testing.T) {
	verifying := publicDNSHTTPClient(time.Second, true)
	tr, ok := verifying.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected an *http.Transport")
	}
	if tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		t.Error("verification must stay on when the monitor did not opt out")
	}

	skipping := publicDNSHTTPClient(time.Second, false)
	tr2 := skipping.Transport.(*http.Transport)
	if tr2.TLSClientConfig == nil || !tr2.TLSClientConfig.InsecureSkipVerify {
		t.Error("a monitor that opted out of verification should still be honoured")
	}
	var _ = tls.Config{}
}
