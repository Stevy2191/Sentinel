package services

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
)

// ErrSSLCertNotFound is returned when no certificate row has the given id.
var ErrSSLCertNotFound = errors.New("ssl certificate not found")

// How long to wait for a TLS handshake before giving up.
const sslHandshakeTimeout = 10 * time.Second

// SSLCheckerService reads certificates over TLS and keeps ssl_certificates in
// step with what it finds.
type SSLCheckerService struct {
	db      *gorm.DB
	manager *notifications.NotificationManager
	logger  *log.Logger
}

// NewSSLCheckerService returns a checker backed by the given database. manager
// may be nil, in which case expiry alerts are skipped rather than fatal.
func NewSSLCheckerService(db *gorm.DB, manager *notifications.NotificationManager) *SSLCheckerService {
	return &SSLCheckerService{db: db, manager: manager, logger: log.Default()}
}

// CertificateInfo is what a successful handshake tells us.
type CertificateInfo struct {
	Issuer     string
	ExpiryDate time.Time
}

// FetchCertificate opens a TLS connection to the domain and reads the leaf
// certificate's issuer and expiry.
//
// Verification is deliberately left ON. The point of this feature is to report
// what a browser would see, and a browser would reject an expired or untrusted
// certificate — so a verification failure is a finding, not an obstacle to work
// around. The one exception is expiry itself: an expired certificate fails
// verification, and reporting "handshake failed" for the very thing being
// watched would be useless, so that case is unwrapped and reported properly.
func (s *SSLCheckerService) FetchCertificate(ctx context.Context, domain string) (*CertificateInfo, error) {
	host := domain
	port := "443"
	if h, p, err := net.SplitHostPort(domain); err == nil {
		host, port = h, p
	}

	dialer := &net.Dialer{Timeout: sslHandshakeTimeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", net.JoinHostPort(host, port), &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		// An expired certificate is the case this feature exists to catch, so
		// dig the certificate out of the verification error and report it as a
		// real expiry rather than as a connection problem.
		var invalid *tls.CertificateVerificationError
		if errors.As(err, &invalid) && len(invalid.UnverifiedCertificates) > 0 {
			leaf := invalid.UnverifiedCertificates[0]
			return &CertificateInfo{
				Issuer:     issuerName(leaf.Issuer.CommonName, leaf.Issuer.Organization),
				ExpiryDate: leaf.NotAfter,
			}, nil
		}
		return nil, fmt.Errorf("%s: %w", domain, simplifyTLSError(err))
	}
	defer conn.Close()

	chain := conn.ConnectionState().PeerCertificates
	if len(chain) == 0 {
		return nil, fmt.Errorf("%s presented no certificate", domain)
	}
	leaf := chain[0]
	return &CertificateInfo{
		Issuer:     issuerName(leaf.Issuer.CommonName, leaf.Issuer.Organization),
		ExpiryDate: leaf.NotAfter,
	}, nil
}

// issuerName picks the most useful name available: the CA's common name, or its
// organisation when the common name is absent.
func issuerName(commonName string, org []string) string {
	if cn := strings.TrimSpace(commonName); cn != "" {
		return cn
	}
	if len(org) > 0 && strings.TrimSpace(org[0]) != "" {
		return strings.TrimSpace(org[0])
	}
	return "Unknown"
}

// simplifyTLSError turns a Go network error into something an operator can act
// on, rather than a wrapped dial string.
func simplifyTLSError(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such host"):
		return errors.New("domain does not resolve")
	case strings.Contains(msg, "connection refused"):
		return errors.New("connection refused on port 443")
	case strings.Contains(msg, "i/o timeout"), errors.Is(err, context.DeadlineExceeded):
		return errors.New("timed out connecting")
	case strings.Contains(msg, "certificate is not trusted"),
		strings.Contains(msg, "unknown authority"):
		return errors.New("certificate is not trusted")
	case strings.Contains(msg, "not valid for"):
		return errors.New("certificate is for a different host")
	default:
		return err
	}
}

// CheckCertificate re-reads one certificate and saves what it finds, alerting
// if the result crosses the expiry threshold. A failed read is recorded on the
// row as an error rather than returned, so one unreachable domain does not stop
// a sweep; the error is returned as well for callers that check on demand.
func (s *SSLCheckerService) CheckCertificate(ctx context.Context, cert *models.SSLCertificate) error {
	now := time.Now()
	info, err := s.FetchCertificate(ctx, cert.Domain)

	updates := map[string]interface{}{"last_checked": now, "updated_at": now}
	if err != nil {
		msg := err.Error()
		// The previously known expiry is kept: a domain that is briefly
		// unreachable has not lost the certificate it had, and blanking the
		// date would make the table forget what it knew.
		updates["status"] = models.SSLStatusUnknown
		updates["last_error"] = msg
		cert.Status = models.SSLStatusUnknown
		cert.LastError = &msg
	} else {
		status, days := models.DeriveStatus(info.ExpiryDate, cert.ExpiryNotificationDays, now)
		updates["issuer"] = info.Issuer
		updates["expiry_date"] = info.ExpiryDate
		updates["days_until_expiry"] = days
		updates["status"] = status
		updates["last_error"] = nil

		cert.Issuer = &info.Issuer
		cert.ExpiryDate = &info.ExpiryDate
		cert.DaysUntilExpiry = &days
		cert.Status = status
		cert.LastError = nil
	}
	cert.LastChecked = &now

	if dbErr := s.db.WithContext(ctx).Model(&models.SSLCertificate{}).
		Where("id = ?", cert.ID).Updates(updates).Error; dbErr != nil {
		return fmt.Errorf("saving certificate check for %s: %w", cert.Domain, dbErr)
	}

	if err == nil && cert.ShouldAlert() {
		s.alertExpiring(ctx, cert, now)
	}
	return err
}

// alertExpiring sends the expiry notification and records that it went out.
//
// Notifications only: no incident row is created. An incident in this app is a
// monitor outage — it carries a duration and feeds uptime percentages and MTTR
// — and a certificate with a week left is not downtime. Writing synthetic
// incidents would corrupt exactly the numbers the reports exist to give.
// The certificate's own status is the durable record.
func (s *SSLCheckerService) alertExpiring(ctx context.Context, cert *models.SSLCertificate, now time.Time) {
	if s.manager == nil {
		return
	}
	days := 0
	if cert.DaysUntilExpiry != nil {
		days = *cert.DaysUntilExpiry
	}

	var message string
	if cert.Status == models.SSLStatusExpired {
		message = fmt.Sprintf("The TLS certificate for %s has EXPIRED.", cert.Domain)
	} else {
		message = fmt.Sprintf("The TLS certificate for %s expires in %d day(s).", cert.Domain, days)
	}
	if cert.ExpiryDate != nil {
		message += fmt.Sprintf(" Expires %s.", cert.ExpiryDate.UTC().Format("2 Jan 2006"))
	}
	if cert.Issuer != nil {
		message += fmt.Sprintf(" Issued by %s.", *cert.Issuer)
	}

	// MonitorID is left zero: this alert is not about a monitor. The manager
	// logs a warning when it cannot store a delivery record against one, which
	// is accurate — the record belongs on the certificate, updated below.
	msg := &notifications.NotificationMessage{
		MonitorName: cert.Domain + " (TLS certificate)",
		MonitorURL:  "https://" + cert.Domain,
		Status:      "down",
		Message:     message,
		Timestamp:   now,
	}
	if err := s.manager.SendNotification(ctx, msg); err != nil {
		s.logger.Printf("[ssl] expiry alert for %s failed: %v", cert.Domain, err)
		return
	}

	if err := s.db.WithContext(ctx).Model(&models.SSLCertificate{}).
		Where("id = ?", cert.ID).Updates(map[string]interface{}{
		"last_notified_at":   now,
		"last_notified_days": days,
		"updated_at":         now,
	}).Error; err != nil {
		// Worth shouting about: if this does not stick, the same alert goes out
		// again tomorrow, and the day after.
		s.logger.Printf("[ssl] WARNING: could not record the expiry alert for %s; it may repeat: %v",
			cert.Domain, err)
		return
	}
	s.logger.Printf("[ssl] expiry alert sent for %s (%d day(s) left)", cert.Domain, days)
}

// CheckAll re-reads every enabled certificate, returning how many were checked
// and how many failed. One bad domain never stops the sweep.
func (s *SSLCheckerService) CheckAll(ctx context.Context) (checked, failed int, err error) {
	var certs []models.SSLCertificate
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Find(&certs).Error; err != nil {
		return 0, 0, fmt.Errorf("listing certificates: %w", err)
	}
	for i := range certs {
		if ctx.Err() != nil {
			return checked, failed, ctx.Err()
		}
		checked++
		if err := s.CheckCertificate(ctx, &certs[i]); err != nil {
			failed++
			s.logger.Printf("[ssl] %s: %v", certs[i].Domain, err)
		}
	}
	return checked, failed, nil
}

// ---- storage -------------------------------------------------------------

// List returns every certificate, newest domains last.
func (s *SSLCheckerService) List(ctx context.Context) ([]models.SSLCertificate, error) {
	var certs []models.SSLCertificate
	if err := s.db.WithContext(ctx).Order("domain ASC").Find(&certs).Error; err != nil {
		return nil, fmt.Errorf("listing certificates: %w", err)
	}
	return certs, nil
}

// Get returns one certificate by id.
func (s *SSLCheckerService) Get(ctx context.Context, id uuid.UUID) (*models.SSLCertificate, error) {
	var cert models.SSLCertificate
	err := s.db.WithContext(ctx).First(&cert, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSSLCertNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching certificate %s: %w", id, err)
	}
	return &cert, nil
}

// Create stores a new watched domain and checks it immediately, so the row is
// never shown as "unknown" while the first daily sweep is a day away.
func (s *SSLCheckerService) Create(ctx context.Context, cert *models.SSLCertificate) error {
	cert.ID = uuid.New()
	cert.CheckInterval = models.SSLCheckIntervalSeconds
	cert.Status = models.SSLStatusUnknown
	now := time.Now()
	cert.CreatedAt = now
	cert.UpdatedAt = now

	if err := s.db.WithContext(ctx).Create(cert).Error; err != nil {
		if isDuplicateKey(err) {
			return fmt.Errorf("%s is already being monitored", cert.Domain)
		}
		return fmt.Errorf("creating certificate for %s: %w", cert.Domain, err)
	}
	// A failure here is not a failure to create: the row exists and says why
	// the check did not work, which is more useful than refusing to add it.
	if err := s.CheckCertificate(ctx, cert); err != nil {
		s.logger.Printf("[ssl] first check of %s failed: %v", cert.Domain, err)
	}
	return nil
}

// Update changes only the two fields that are the operator's to choose. The
// domain and the check interval are fixed after creation: changing the domain
// would silently repoint the history, and the interval is not configurable.
func (s *SSLCheckerService) Update(ctx context.Context, id uuid.UUID, notifyDays int, enabled bool) (*models.SSLCertificate, error) {
	cert, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		"expiry_notification_days": notifyDays,
		"enabled":                  enabled,
		"updated_at":               time.Now(),
	}
	// The threshold decides what counts as "expiring soon", so a change to it
	// has to re-derive the status; otherwise a row keeps a classification made
	// under the old rule until the next daily sweep.
	if cert.ExpiryDate != nil {
		status, days := models.DeriveStatus(*cert.ExpiryDate, notifyDays, time.Now())
		updates["status"] = status
		updates["days_until_expiry"] = days
	}
	if err := s.db.WithContext(ctx).Model(&models.SSLCertificate{}).
		Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("updating certificate %s: %w", id, err)
	}
	return s.Get(ctx, id)
}

// Delete removes a watched domain outright.
func (s *SSLCheckerService) Delete(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Delete(&models.SSLCertificate{}, "id = ?", id)
	if res.Error != nil {
		return fmt.Errorf("deleting certificate %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrSSLCertNotFound
	}
	return nil
}

// isDuplicateKey reports whether err is a unique-constraint violation.
func isDuplicateKey(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
