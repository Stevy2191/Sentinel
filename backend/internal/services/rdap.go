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

// rdapTimeout bounds the whole lookup including redirects.
const rdapTimeout = 15 * time.Second

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
// registrable domain.
func LookupRegistration(ctx context.Context, host string) (*RegistrationInfo, error) {
	domain, err := RegistrableDomain(host)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, rdapTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rdapBaseURL+domain, nil)
	if err != nil {
		return nil, fmt.Errorf("building RDAP request for %s: %w", domain, err)
	}
	req.Header.Set("Accept", "application/rdap+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("looking up %s: %w", domain, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		// The bootstrap returns 404 both for "no such domain" and for a TLD it
		// cannot route, which are indistinguishable from here.
		return nil, fmt.Errorf("%s: no registration record found", domain)
	case resp.StatusCode == http.StatusNotImplemented, resp.StatusCode == http.StatusBadGateway:
		return nil, ErrNoRDAP
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
