package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/netguard"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// SentinelURLs are the two addresses an install command needs.
//
// They are separate because they answer different questions. External is where
// a person's browser reached Sentinel, and is what an install command should
// download from. Internal is where a monitored host should send its metrics.
// Behind a reverse proxy those differ: the browser arrives at
// https://sentinel.example.com while the agent has a direct route to
// http://10.1.20.10:3001 and no path through the proxy at all.
type SentinelURLs struct {
	External string `json:"external_url"`
	Internal string `json:"internal_url"`
}

// resolveSentinelURLs works out both addresses for this request.
//
// External, in order of preference:
//  1. the sentinel_external_url setting, when an administrator has set one
//  2. base_url, which already exists for email links and is usually right
//  3. the request itself, honouring the forwarding headers a proxy adds
//
// The third is what makes a fresh install work at all. Before it, an unset
// base_url produced "/scripts/server-agent.sh" with no host, and curl refused
// it with "No host part in the URL" — the install failing on its first line
// with an error that says nothing about the setting behind it.
//
// Internal falls back to external, which is correct whenever there is no
// proxy: the address that reached Sentinel is the address that can reach it.
func resolveSentinelURLs(c *gin.Context, settings *services.SettingsService) SentinelURLs {
	ctx := c.Request.Context()

	external := trimURL(settings.GetString(ctx, models.SettingSentinelExternalURL, ""))
	if external == "" {
		external = trimURL(settings.GetString(ctx, models.SettingBaseURL, ""))
	}
	if external == "" {
		external = requestBaseURL(c)
	}

	internal := trimURL(settings.GetString(ctx, models.SettingSentinelInternalURL, ""))
	if internal == "" {
		internal = external
	}

	return SentinelURLs{External: external, Internal: internal}
}

// requestBaseURL reconstructs the address the client used to reach us.
//
// The forwarding headers come first because behind a proxy the request's own
// scheme and host describe the hop from the proxy to us, not the address the
// browser typed — which would hand out an internal address, or http for a site
// served over https.
func requestBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := firstHeaderValue(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}

	host := firstHeaderValue(c.GetHeader("X-Forwarded-Host"))
	if host == "" {
		host = c.Request.Host
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + host
}

// firstHeaderValue takes the first entry of a comma-separated header.
//
// A request through more than one proxy arrives with a list, and the first
// entry is the one nearest the client.
func firstHeaderValue(raw string) string {
	first, _, _ := strings.Cut(raw, ",")
	return strings.TrimSpace(first)
}

func trimURL(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

// validateSentinelURL checks an operator-supplied override.
//
// Empty is valid and means "work it out", which is the default and the right
// answer for an install that is not behind a proxy.
func validateSentinelURL(raw string) error {
	raw = trimURL(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("must be an absolute URL including the host, e.g. https://sentinel.example.com or http://10.1.20.10:3001")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must start with http:// or https://")
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("must not include a path")
	}
	return nil
}

// urlProbeTimeout bounds one reachability check. Short: this runs while an
// administrator waits on a button.
const urlProbeTimeout = 6 * time.Second

// urlProbe is the outcome of testing one address.
type urlProbe struct {
	URL       string `json:"url"`
	Reachable bool   `json:"reachable"`
	Status    int    `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
	// Source says where the address came from, so a surprising result can be
	// traced to the setting or the header that produced it.
	Source string `json:"source"`
}

// TestSentinelURLsHandler handles POST /api/v1/agents/urls/test.
//
// Checks that each address actually answers, which is the question an operator
// has when configuring these. Getting them wrong is silent otherwise: the
// agent installs, reports into the void, and the host simply never appears.
//
// Each URL is fetched from the server rather than the browser, because the
// internal address is often one only the server and the monitored hosts can
// reach. Testing it from the browser would report a failure for an address
// that works perfectly well where it matters.
func TestSentinelURLsHandler(settings *services.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			ExternalURL *string `json:"external_url"`
			InternalURL *string `json:"internal_url"`
		}
		// A body is optional: with none, the stored configuration is tested.
		_ = c.ShouldBindJSON(&req)

		stored := resolveSentinelURLs(c, settings)
		external, externalSource := stored.External, "configured"
		internal, internalSource := stored.Internal, "configured"
		if req.ExternalURL != nil {
			external, externalSource = trimURL(*req.ExternalURL), "submitted"
		}
		if req.InternalURL != nil {
			internal, internalSource = trimURL(*req.InternalURL), "submitted"
		}
		// An empty override means "derive", so test what would be derived.
		if external == "" {
			external, externalSource = requestBaseURL(c), "derived from this request"
		}
		if internal == "" {
			internal, internalSource = external, "same as external"
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"external": probeURL(c.Request.Context(), external, externalSource, "/scripts/server-agent.sh"),
			"internal": probeURL(c.Request.Context(), internal, internalSource, "/health"),
		})
	}
}

// probeURL fetches a path under the base address and reports what happened.
//
// The external address is checked against the install script and the internal
// one against the health endpoint, because those are the things each is
// actually used for. A URL that serves a homepage but not the script would
// otherwise test clean and still fail the install.
func probeURL(ctx context.Context, base, source, path string) urlProbe {
	probe := urlProbe{URL: base, Source: source}
	if base == "" {
		probe.Error = "no address could be determined"
		return probe
	}
	if err := validateSentinelURL(base); err != nil {
		probe.Error = err.Error()
		return probe
	}

	reqCtx, cancel := context.WithTimeout(ctx, urlProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, base+path, nil)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	// Guarded like every other outbound request: the address is operator
	// input, so it is exactly what an SSRF lockdown is meant to constrain.
	client := netguard.NewGuardedHTTPClient(urlProbeTimeout)
	resp, err := client.Do(req)
	if err != nil {
		probe.Error = simplifyProbeError(err)
		return probe
	}
	defer resp.Body.Close()

	probe.Status = resp.StatusCode
	// Any answer at all proves the address routes here, which is the question.
	// A 401 from the API is a perfectly good result: something is listening
	// and it is Sentinel.
	probe.Reachable = resp.StatusCode < 500
	if !probe.Reachable {
		probe.Error = fmt.Sprintf("responded %d", resp.StatusCode)
	}
	return probe
}

func simplifyProbeError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such host"):
		return "the host name does not resolve from the Sentinel server"
	case strings.Contains(msg, "connection refused"):
		return "nothing is listening at that address"
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "timeout"):
		return "timed out; the address may be unreachable from the Sentinel server"
	case strings.Contains(msg, "certificate"):
		return "the TLS certificate could not be verified"
	default:
		return msg
	}
}
