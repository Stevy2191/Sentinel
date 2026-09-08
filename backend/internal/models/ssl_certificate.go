package models

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Certificate status values.
const (
	SSLStatusUnknown      = "unknown"
	SSLStatusValid        = "valid"
	SSLStatusExpiringSoon = "expiring_soon"
	SSLStatusExpired      = "expired"
)

const (
	// SSLCheckIntervalSeconds is how often certificates are re-checked. Fixed:
	// an expiry date moves once a day at most, so a configurable interval would
	// only ever be a way to check less often than is useful.
	SSLCheckIntervalSeconds = 86400
	// Bounds for the expiry alert lead time.
	MinExpiryNotificationDays = 1
	MaxExpiryNotificationDays = 365
	// DefaultExpiryNotificationDays is a week's warning, enough to renew.
	DefaultExpiryNotificationDays = 7
)

// SSLCertificate is a watched domain and the state of its TLS certificate.
type SSLCertificate struct {
	ID     uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Domain string    `json:"domain" gorm:"column:domain;not null"`

	// Read from the certificate; nil until a check has succeeded.
	Issuer     *string    `json:"issuer" gorm:"column:issuer"`
	ExpiryDate *time.Time `json:"expiry_date" gorm:"column:expiry_date"`
	// DaysUntilExpiry is stored rather than computed on read so the list can be
	// sorted by it in the database. Negative once expired.
	DaysUntilExpiry *int   `json:"days_until_expiry" gorm:"column:days_until_expiry"`
	Status          string `json:"status" gorm:"column:status;not null;default:unknown"`

	CheckInterval          int `json:"check_interval" gorm:"column:check_interval;default:86400"`
	ExpiryNotificationDays int `json:"expiry_notification_days" gorm:"column:expiry_notification_days;default:7"`

	LastChecked *time.Time `json:"last_checked" gorm:"column:last_checked"`
	// LastError explains an unknown status: unreachable host, handshake
	// failure, no certificate presented.
	LastError *string `json:"last_error" gorm:"column:last_error"`

	LastNotifiedAt   *time.Time `json:"last_notified_at" gorm:"column:last_notified_at"`
	LastNotifiedDays *int       `json:"last_notified_days" gorm:"column:last_notified_days"`

	Enabled bool `json:"enabled" gorm:"column:enabled;default:true"`

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the model.
func (SSLCertificate) TableName() string { return "ssl_certificates" }

// A hostname label: alphanumerics and hyphens, not starting or ending with one.
var domainLabel = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// NormalizeDomain trims a user-supplied domain into the host to connect to.
//
// People paste what they have: a full URL, a host with a trailing slash, mixed
// case. Accepting those and reducing them to a hostname is friendlier than
// rejecting them, and prevents "example.com" and "https://example.com/" being
// stored as two different rows for one certificate.
func NormalizeDomain(raw string) string {
	d := strings.TrimSpace(strings.ToLower(raw))
	d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	// Strip an explicit :443 but keep any other port: a certificate can be
	// served on a non-standard port and that is part of where to find it.
	d = strings.TrimSuffix(d, ":443")
	return strings.Trim(d, ".")
}

// ValidateDomain checks that a normalised domain is a plausible host to open a
// TLS connection to. It deliberately does not resolve DNS — a domain that does
// not resolve yet is still worth watching, and the check will report that.
func ValidateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain is required")
	}
	host := domain
	if h, port, err := net.SplitHostPort(domain); err == nil {
		host = h
		if port == "" {
			return fmt.Errorf("port is empty in %q", domain)
		}
	}
	if len(host) > 253 {
		return fmt.Errorf("domain is too long")
	}
	if net.ParseIP(host) != nil {
		// An IP has no name for a certificate to match, so a check against one
		// almost always fails verification. Rejecting it up front is clearer
		// than storing a row that can only ever error.
		return fmt.Errorf("enter a domain name rather than an IP address")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return fmt.Errorf("enter a full domain, e.g. example.com")
	}
	for _, l := range labels {
		if !domainLabel.MatchString(l) {
			return fmt.Errorf("%q is not a valid domain", domain)
		}
	}
	return nil
}

// DeriveStatus classifies a certificate from its expiry and the alert lead
// time. Exported so the checker and any backfill agree on one definition.
func DeriveStatus(expiry time.Time, notifyDays int, now time.Time) (status string, daysLeft int) {
	// Whole days remaining, rounded down: with 36 hours left, "1 day" is the
	// honest answer and "2" would be a day of false comfort.
	daysLeft = int(expiry.Sub(now).Hours() / 24)
	switch {
	case !expiry.After(now):
		return SSLStatusExpired, daysLeft
	case daysLeft <= notifyDays:
		return SSLStatusExpiringSoon, daysLeft
	default:
		return SSLStatusValid, daysLeft
	}
}

// ShouldAlert reports whether an expiry alert is due for this certificate.
//
// It fires when the certificate is inside its warning window and either has
// never been alerted or has lost a day since the last alert. Without the second
// condition the daily job would re-send every day for the whole window; without
// the first, a certificate would go quiet as it got closer to expiring.
func (c *SSLCertificate) ShouldAlert() bool {
	if !c.Enabled || c.DaysUntilExpiry == nil {
		return false
	}
	if c.Status != SSLStatusExpiringSoon && c.Status != SSLStatusExpired {
		return false
	}
	if c.LastNotifiedDays == nil {
		return true
	}
	return *c.DaysUntilExpiry < *c.LastNotifiedDays
}
