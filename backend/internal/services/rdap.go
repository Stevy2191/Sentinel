package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
)

// rdapBaseURL is the IANA-run bootstrap service: it redirects a domain query
// to whichever registry actually holds the record, so one URL covers every TLD
// that publishes RDAP.
const rdapBaseURL = "https://rdap.org/domain/"

// rdapTimeout bounds one attempt, including the bootstrap redirect.
const rdapTimeout = 8 * time.Second

// rdapAttempts is how many times a transient failure is retried, and it is
// deliberately small. A failed lookup no longer discards what is already known
// about a domain, so patience buys little: one extra try absorbs a momentary
// stall, and anything worse is better reported quickly than waited out. Adding
// or re-checking a domain is interactive, and nobody should watch a spinner for
// a minute because a registry is down.
const rdapAttempts = 2

// rdapBudget is the ceiling on a whole lookup including retries and backoff,
// so the interactive paths have a bound that does not move if the numbers
// above are ever tuned.
const rdapBudget = 20 * time.Second

// rdapUserAgent identifies the client. Several registries throttle or refuse
// the default Go user agent, which shows up as a timeout rather than as a
// refusal, so it is worth setting explicitly.
const rdapUserAgent = "Sentinel-Uptime-Monitor/1.0 (+https://github.com/Stevy2191/Sentinel)"

// rdapClient is kept separate from http.DefaultClient so this lookup has its
// own connection pool and its own ceiling, and cannot be affected by any
// other caller's use of the default.
var rdapClient = &http.Client{
	Timeout: rdapTimeout,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

// ErrNoRDAP means the registry does not publish RDAP for this domain. Not a
// fault: plenty of country-code registries still only offer port-43 WHOIS, and
// treating that as an error would flag healthy domains as broken.
var ErrNoRDAP = errors.New("registry does not publish registration data")

// RegistrationInfo is what a registration lookup yields.
type RegistrationInfo struct {
	// Domain is the registrable domain the record belongs to.
	Domain     string
	Registrar  string
	ExpiryDate time.Time
	// Statuses are the EPP status codes, e.g. "client transfer prohibited".
	Statuses []string
}

// RegistrableDomain reduces a hostname to the domain a registration belongs
// to: sub.example.co.uk becomes example.co.uk, not co.uk.
//
// Uses the public suffix list rather than "last two labels", which would treat
// co.uk, com.au and github.io as registrable and look up records that do not
// exist.
func RegistrableDomain(host string) (string, error) {
	h := strings.ToLower(strings.TrimSpace(host))
	// A port is part of where to reach a service, never part of the name a
	// registration is held under.
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i:], "]") {
		h = h[:i]
	}
	h = strings.Trim(h, ".")
	if h == "" {
		return "", errors.New("empty host")
	}
	d, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil {
		return "", fmt.Errorf("%s has no registrable domain: %w", host, err)
	}
	return d, nil
}

// rdapResponse is the subset of an RDAP domain record we read.
type rdapResponse struct {
	LDHName string   `json:"ldhName"`
	Status  []string `json:"status"`
	Events  []struct {
		Action string `json:"eventAction"`
		Date   string `json:"eventDate"`
	} `json:"events"`
	Entities []struct {
		Roles      []string        `json:"roles"`
		VCardArray json.RawMessage `json:"vcardArray"`
	} `json:"entities"`
}

// LookupRegistration fetches the registration record for a hostname's
// registrable domain, retrying transient failures.
func LookupRegistration(ctx context.Context, host string) (*RegistrationInfo, error) {
	domain, err := RegistrableDomain(host)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, rdapBudget)
	defer cancel()

	var lastErr error
	for attempt := 0; attempt < rdapAttempts; attempt++ {
		if attempt > 0 {
			// Linear backoff. The failures worth retrying are rate limits and
			// momentary stalls, both of which clear in seconds.
			delay := time.Duration(attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("looking up %s: %w", domain, ctx.Err())
			case <-time.After(delay):
			}
		}

		info, err := lookupRegistrationOnce(ctx, domain)
		if err == nil {
			return info, nil
		}
		lastErr = err
		// A definitive answer is not worth asking again: the record is
		// absent, unparseable, or the registry does not publish RDAP at all.
		if !isTransientRDAPError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// transientRDAPError marks a failure worth retrying.
type transientRDAPError struct{ err error }

func (e transientRDAPError) Error() string { return e.err.Error() }
func (e transientRDAPError) Unwrap() error { return e.err }

func isTransientRDAPError(err error) bool {
	var t transientRDAPError
	return errors.As(err, &t)
}

func lookupRegistrationOnce(ctx context.Context, domain string) (*RegistrationInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, rdapTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rdapBaseURL+domain, nil)
	if err != nil {
		return nil, fmt.Errorf("building RDAP request for %s: %w", domain, err)
	}
	req.Header.Set("Accept", "application/rdap+json")
	req.Header.Set("User-Agent", rdapUserAgent)

	resp, err := rdapClient.Do(req)
	if err != nil {
		// Network failures and timeouts are the retryable case.
		return nil, transientRDAPError{fmt.Errorf("looking up %s: %w", domain, err)}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		// The bootstrap returns 404 both for "no such domain" and for a TLD it
		// cannot route, which are indistinguishable from here.
		return nil, fmt.Errorf("%s: no registration record found", domain)
	case resp.StatusCode == http.StatusNotImplemented:
		return nil, ErrNoRDAP
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusBadGateway,
		resp.StatusCode == http.StatusServiceUnavailable,
		resp.StatusCode == http.StatusGatewayTimeout:
		// 502 used to be read as "no RDAP here". It is more often the
		// bootstrap failing to reach the registry, which clears on a retry.
		return nil, transientRDAPError{fmt.Errorf("%s: registry returned %d", domain, resp.StatusCode)}
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s: registry returned %d", domain, resp.StatusCode)
	}

	var rec rdapResponse
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		return nil, fmt.Errorf("reading registration record for %s: %w", domain, err)
	}

	info := &RegistrationInfo{Domain: domain, Statuses: rec.Status}
	for _, e := range rec.Events {
		if strings.EqualFold(e.Action, "expiration") {
			t, err := parseRDAPTime(e.Date)
			if err != nil {
				return nil, fmt.Errorf("%s: unreadable expiry date %q", domain, e.Date)
			}
			info.ExpiryDate = t
			break
		}
	}
	if info.ExpiryDate.IsZero() {
		// Some registries withhold the expiry date. Without it there is nothing
		// to count down to, so this is reported rather than stored as "valid".
		return nil, fmt.Errorf("%s: registry does not publish an expiry date", domain)
	}
	info.Registrar = registrarName(rec)
	return info, nil
}

// parseRDAPTime accepts the formats registries actually emit. RFC 3339 is what
// the spec requires, but a plain UTC timestamp without a zone is common enough
// to be worth handling rather than failing the whole lookup over.
func parseRDAPTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z0700",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised time %q", raw)
}

// registrarName digs the registrar's display name out of the entity list. RDAP
// nests it in a jCard, which is a JSON array of arrays rather than an object,
// so it is walked loosely instead of modelled.
func registrarName(rec rdapResponse) string {
	for _, ent := range rec.Entities {
		isRegistrar := false
		for _, r := range ent.Roles {
			if strings.EqualFold(r, "registrar") {
				isRegistrar = true
				break
			}
		}
		if !isRegistrar || len(ent.VCardArray) == 0 {
			continue
		}
		var card []json.RawMessage
		if err := json.Unmarshal(ent.VCardArray, &card); err != nil || len(card) < 2 {
			continue
		}
		var props [][]json.RawMessage
		if err := json.Unmarshal(card[1], &props); err != nil {
			continue
		}
		for _, p := range props {
			if len(p) < 4 {
				continue
			}
			var key string
			if err := json.Unmarshal(p[0], &key); err != nil || !strings.EqualFold(key, "fn") {
				continue
			}
			var name string
			if err := json.Unmarshal(p[3], &name); err == nil && strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name)
			}
		}
	}
	return ""
}
