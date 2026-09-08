package models

import (
	"encoding/json"
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

	// ---- Read from the certificate itself -------------------------------
	// All nil until a check succeeds, and left alone by a check that fails so
	// a transient outage does not blank what the last good read found.

	SubjectCommonName *string `json:"subject_common_name" gorm:"column:subject_common_name"`
	IssuerCommonName  *string `json:"issuer_common_name" gorm:"column:issuer_common_name"`
	// SerialNumber is a string, not a number: a serial is up to 20 octets and
	// overflows both int64 and a JSON consumer's float, which renders it in
	// scientific notation and loses digits.
	SerialNumber       *string     `json:"serial_number" gorm:"column:serial_number"`
	SignatureAlgorithm *string     `json:"signature_algorithm" gorm:"column:signature_algorithm"`
	PublicKeyAlgorithm *string     `json:"public_key_algorithm" gorm:"column:public_key_algorithm"`
	SubjectAltNames    StringSlice `json:"subject_alternative_names" gorm:"column:subject_alternative_names;type:jsonb"`
	ValidFrom          *time.Time  `json:"valid_from" gorm:"column:valid_from"`
	// ResolvedIP is the address the handshake actually reached.
	ResolvedIP *string `json:"resolved_ip" gorm:"column:resolved_ip"`

	CheckInterval          int `json:"check_interval" gorm:"column:check_interval;default:86400"`
	ExpiryNotificationDays int `json:"expiry_notification_days" gorm:"column:expiry_notification_days;default:7"`

	LastChecked *time.Time `json:"last_checked" gorm:"column:last_checked"`
	// LastError explains an unknown status: unreachable host, handshake
	// failure, no certificate presented.
	LastError *string `json:"last_error" gorm:"column:last_error"`

	LastNotifiedAt   *time.Time `json:"last_notified_at" gorm:"column:last_notified_at"`
	LastNotifiedDays *int       `json:"last_notified_days" gorm:"column:last_notified_days"`

	// ---- Domain registration -------------------------------------------
	// A separate clock from the certificate: a domain can hold a freshly
	// renewed certificate and still lapse at the registrar weeks later.

	// RegistrableDomain is what the registration belongs to: example.com for
	// sub.example.com. Stored rather than derived on read so a change in the
	// public suffix list cannot silently repoint existing rows.
	RegistrableDomain     *string    `json:"registrable_domain" gorm:"column:registrable_domain"`
	Registrar             *string    `json:"registrar" gorm:"column:registrar"`
	DomainExpiryDate      *time.Time `json:"domain_expiry_date" gorm:"column:domain_expiry_date"`
	DomainDaysUntilExpiry *int       `json:"domain_days_until_expiry" gorm:"column:domain_days_until_expiry"`
	DomainStatus          string     `json:"domain_status" gorm:"column:domain_status;default:unknown"`
	RegistrationCheckedAt *time.Time `json:"registration_checked_at" gorm:"column:registration_checked_at"`
	RegistrationError     *string    `json:"registration_error" gorm:"column:registration_error"`

	DomainLastNotifiedAt   *time.Time `json:"domain_last_notified_at" gorm:"column:domain_last_notified_at"`
	DomainLastNotifiedDays *int       `json:"domain_last_notified_days" gorm:"column:domain_last_notified_days"`

	// NotifyChannels selects where expiry alerts go. nil means every enabled
	// channel, matching how a monitor reads the same field; an empty slice
	// means alert nowhere.
	NotifyChannels StringSlice `json:"notify_channels" gorm:"column:notify_channels;type:jsonb"`

	Enabled bool `json:"enabled" gorm:"column:enabled;default:true"`

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// MarshalJSON adds the two values that are derived rather than stored, so a
// client never has to recompute them and cannot disagree with the server about
// how long a certificate was issued for or when it is next due.
func (c SSLCertificate) MarshalJSON() ([]byte, error) {
	// The alias sheds the method set, so marshalling the embedded value does
	// not call back into this function forever.
	type alias SSLCertificate
	return json.Marshal(struct {
		alias
		ValidityPeriodDays int        `json:"validity_period_days"`
		NextCheck          *time.Time `json:"next_check"`
	}{
		alias:              alias(c),
		ValidityPeriodDays: c.ValidityPeriodDays(),
		NextCheck:          c.NextCheckAt(),
	})
}

// ValidityPeriodDays is how long the certificate was issued for: the whole
// window, not what is left of it. Zero when either end is unknown.
func (c *SSLCertificate) ValidityPeriodDays() int {
	if c.ValidFrom == nil || c.ExpiryDate == nil {
		return 0
	}
	d := int(c.ExpiryDate.Sub(*c.ValidFrom).Hours() / 24)
	if d < 0 {
		return 0
	}
	return d
}

// NextCheckAt is when this certificate is next due. Nil until it has been
// checked once, since there is no schedule to project from before that.
func (c *SSLCertificate) NextCheckAt() *time.Time {
	if c.LastChecked == nil {
		return nil
	}
	interval := c.CheckInterval
	if interval <= 0 {
		interval = SSLCheckIntervalSeconds
	}
	next := c.LastChecked.Add(time.Duration(interval) * time.Second)
	return &next
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

// ShouldAlertDomain is ShouldAlert for the registration clock. The two are
// tracked separately so a certificate alert already sent does not suppress the
// registration alert, which is about a different deadline entirely.
func (c *SSLCertificate) ShouldAlertDomain() bool {
	if !c.Enabled || c.DomainDaysUntilExpiry == nil {
		return false
	}
	if c.DomainStatus != SSLStatusExpiringSoon && c.DomainStatus != SSLStatusExpired {
		return false
	}
	if c.DomainLastNotifiedDays == nil {
		return true
	}
	return *c.DomainDaysUntilExpiry < *c.DomainLastNotifiedDays
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
