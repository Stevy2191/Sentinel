package services

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netguard"
)

// publicDNSHTTPClient is an HTTP client that resolves through resolveHost —
// public resolvers first, the system resolver last — instead of the host's
// own configuration.
//
// Used only to second-guess a failed check. A monitor is meant to see what a
// client on this network sees, so the normal path keeps using the system
// resolver; this exists for the case where the system resolver is the thing
// that is broken.
//
// The SSRF guard still applies: addresses are dialed through netguard's
// Control, so a resolver answering with a private address cannot be used to
// reach one.
func publicDNSHTTPClient(timeout time.Duration, verify bool) *http.Client {
	dialer := netguard.SafeDialer(timeout)

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			resolved, err := resolveHost(ctx, host)
			if err != nil {
				return nil, err
			}
			// Every address is tried, not just the first: a name with several
			// records where the first is unreachable should still connect.
			var lastErr error
			for _, ip := range resolved.IPs {
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("no address for %s", host)
			}
			return nil, lastErr
		},
	}
	if !verify {
		transport.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // deliberate, per-monitor opt-in
		}
	}
	// Only the dial address is substituted, so the request keeps its original
	// Host header and TLS server name — the certificate is still verified
	// against the name being monitored, not against the IP.
	return &http.Client{Timeout: timeout, Transport: transport}
}

// worthRetryingViaPublicDNS reports whether a failed check might be a local
// name-resolution problem rather than a broken service.
//
// A check that got an HTTP response is excluded however bad the status: the
// server was reached, so resolution worked and asking a different resolver
// would only produce the same 500 more slowly. What qualifies is a failure
// with no response at all — a refused connection, a timeout, or a TLS error,
// which is what a hijacked resolver produces when it answers with some other
// machine.
func worthRetryingViaPublicDNS(check *models.Check) bool {
	if check == nil || check.Status == checkSuccess {
		return false
	}
	return check.StatusCode == 0
}
