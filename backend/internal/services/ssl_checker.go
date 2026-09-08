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

	// resolverFunc reports the DNS server certificate checks should use, or ""
	// for the host's own. Resolved per check rather than captured at
	// construction so an admin's edit takes effect without a restart — the same
	// reason BaseURLFunc works this way.
	resolverFunc func(context.Context) string
}

// SetDNSResolverFunc supplies the resolver lookup. Safe to leave unset: the
// host resolver is then used, which is the right default for everyone whose
// DNS is not split-horizon.
func (s *SSLCheckerService) SetDNSResolverFunc(fn func(context.Context) string) {
	s.resolverFunc = fn
}

// netDialer returns the TCP dialer for certificate checks, pointed at the
// configured resolver when there is one.
//
// The problem this solves: an Active Directory domain often shares its name
// with the organisation's public domain, so the internal resolver answers for
// the apex with a domain controller's address. A check for "example.com" then
// opens TLS against a domain controller — the wrong host entirely — while
// "www.example.com" works, because only the apex is overridden. Asking a public
// resolver makes the check see what a visitor sees.
func (s *SSLCheckerService) netDialer(ctx context.Context) *net.Dialer {
	dialer := &net.Dialer{Timeout: sslHandshakeTimeout}
	if s.resolverFunc != nil {
		if addr := normalizeResolver(s.resolverFunc(ctx)); addr != "" {
			dialer.Resolver = &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
					// network is inherited so UDP stays UDP and a truncated
					// answer can still be retried over TCP.
					return (&net.Dialer{Timeout: sslHandshakeTimeout}).DialContext(ctx, network, addr)
				},
			}
		}
	}
	return dialer
}

// normalizeResolver accepts "1.1.1.1" or "1.1.1.1:53" and returns a dialable
// address, or "" if the value is not a usable resolver.
func normalizeResolver(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(raw); err == nil {
		return raw
	}
	// A bare IPv6 literal has colons but no port, so it must be bracketed
	// before a port is appended.
	return net.JoinHostPort(raw, "53")
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

	handshakeCtx, cancel := context.WithTimeout(ctx, sslHandshakeTimeout)
	defer cancel()
	tlsDialer := &tls.Dialer{
		NetDialer: s.netDialer(ctx),
		Config: &tls.Config{
			ServerName: host,
			MinVersion: tls.VersionTLS12,
		},
	}
	rawConn, err := tlsDialer.DialContext(handshakeCtx, "tcp", net.JoinHostPort(host, port))
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
		return nil, fmt.Errorf("%s: %w", domain, s.explainDialFailure(ctx, host, err))
	}
	conn := rawConn.(*tls.Conn)
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

// issuerName picks the most recognisable name for the issuing CA.
//
// Organisation first, common name second. A modern issuer DN looks like
// "C=US, O=Google Trust Services, CN=WE1": the CN is an opaque label for one
// intermediate in the CA's rotation and means nothing to a reader, while the O
// is the name people know the CA by. Preferring the CN — as this did — showed
// "WE1" where "Google Trust Services" belonged.
func issuerName(commonName string, org []string) string {
	if len(org) > 0 {
		if o := strings.TrimSpace(org[0]); o != "" {
			return o
		}
	}
	// Self-signed and internal CAs often carry no organisation at all, so the
	// common name is the only name there is.
	if cn := strings.TrimSpace(commonName); cn != "" {
		return cn
	}
	return "Unknown"
}

// simplifyTLSError turns a Go network error into something an operator can act
// on, rather than a wrapped dial string.
// explainDialFailure turns a dial error into something an operator can act on,
// naming the address the check actually reached.
//
// The address is the whole point. When an internal resolver answers for a
// public apex domain, the failure looks like a plain refused connection or a
// certificate for the wrong host, and nothing on screen says why the subdomain
// works while the apex does not. Seeing a private address next to a public
// domain names the cause immediately.
func (s *SSLCheckerService) explainDialFailure(ctx context.Context, host string, err error) error {
	simple := simplifyTLSError(err)

	// Only worth resolving when the name resolved at all; "no such host" has
	// no address to report.
	if strings.Contains(err.Error(), "no such host") {
		return simple
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resolver := s.netDialer(ctx).Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, lookupErr := resolver.LookupIPAddr(lookupCtx, host)
	if lookupErr != nil || len(addrs) == 0 {
		return simple
	}

	ip := addrs[0].IP
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return fmt.Errorf("%w (resolved to the internal address %s — DNS here answers for this domain "+
			"privately, so the check reached an internal host instead of the public one; "+
			"set a public DNS resolver in Settings to check what visitors see)", simple, ip)
	}
	return fmt.Errorf("%w (resolved to %s)", simple, ip)
}

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
		// nil means every enabled channel, an empty slice means none — the
		// same contract a monitor's notify_channels carries.
		Channels: cert.NotifyChannels,
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

// CheckRegistration looks up the domain's registration and saves what it finds.
//
// Independent of the TLS check on purpose: RDAP is an HTTPS call to the
// registry and never resolves the monitored domain, so it keeps working where
// an internal resolver answers for a public domain and points the TLS check at
// the wrong host. For an apex domain behind split-horizon DNS this is often the
// only clock that can be read at all.
func (s *SSLCheckerService) CheckRegistration(ctx context.Context, cert *models.SSLCertificate) error {
	now := time.Now()
	info, err := LookupRegistration(ctx, cert.Domain)

	updates := map[string]interface{}{"registration_checked_at": now, "updated_at": now}
	if err != nil {
		msg := err.Error()
		// Previous values are kept: a lookup that failed today has not changed
		// when the domain expires.
		updates["domain_status"] = models.SSLStatusUnknown
		updates["registration_error"] = msg
		cert.DomainStatus = models.SSLStatusUnknown
		cert.RegistrationError = &msg
	} else {
		status, days := models.DeriveStatus(info.ExpiryDate, cert.ExpiryNotificationDays, now)
		updates["registrable_domain"] = info.Domain
		updates["registrar"] = info.Registrar
		updates["domain_expiry_date"] = info.ExpiryDate
		updates["domain_days_until_expiry"] = days
		updates["domain_status"] = status
		updates["registration_error"] = nil

		cert.RegistrableDomain = &info.Domain
		cert.Registrar = &info.Registrar
		cert.DomainExpiryDate = &info.ExpiryDate
		cert.DomainDaysUntilExpiry = &days
		cert.DomainStatus = status
		cert.RegistrationError = nil
	}
	cert.RegistrationCheckedAt = &now

	if dbErr := s.db.WithContext(ctx).Model(&models.SSLCertificate{}).
		Where("id = ?", cert.ID).Updates(updates).Error; dbErr != nil {
		return fmt.Errorf("saving registration check for %s: %w", cert.Domain, dbErr)
	}

	if err == nil && cert.ShouldAlertDomain() {
		s.alertDomainExpiring(ctx, cert, now)
	}
	return err
}

// alertDomainExpiring warns that the registration, not the certificate, is
// running out. Worth saying plainly: a lapsed registration takes the whole
// domain down, which is a bigger outage than an expired certificate and is
// fixed somewhere else entirely — at the registrar.
func (s *SSLCheckerService) alertDomainExpiring(ctx context.Context, cert *models.SSLCertificate, now time.Time) {
	if s.manager == nil {
		return
	}
	days := 0
	if cert.DomainDaysUntilExpiry != nil {
		days = *cert.DomainDaysUntilExpiry
	}
	name := cert.Domain
	if cert.RegistrableDomain != nil {
		name = *cert.RegistrableDomain
	}

	var message string
	if cert.DomainStatus == models.SSLStatusExpired {
		message = fmt.Sprintf("The domain registration for %s has EXPIRED.", name)
	} else {
		message = fmt.Sprintf("The domain registration for %s expires in %d day(s).", name, days)
	}
	if cert.DomainExpiryDate != nil {
		message += fmt.Sprintf(" Expires %s.", cert.DomainExpiryDate.UTC().Format("2 Jan 2006"))
	}
	if cert.Registrar != nil && *cert.Registrar != "" {
		message += fmt.Sprintf(" Renew at %s.", *cert.Registrar)
	}

	msg := &notifications.NotificationMessage{
		MonitorName: name + " (domain registration)",
		MonitorURL:  "https://" + name,
		Status:      "down",
		Message:     message,
		Timestamp:   now,
		Channels:    cert.NotifyChannels,
	}
	if err := s.manager.SendNotification(ctx, msg); err != nil {
		s.logger.Printf("[ssl] registration alert for %s failed: %v", name, err)
		return
	}
	if err := s.db.WithContext(ctx).Model(&models.SSLCertificate{}).
		Where("id = ?", cert.ID).Updates(map[string]interface{}{
		"domain_last_notified_at":   now,
		"domain_last_notified_days": days,
		"updated_at":                now,
	}).Error; err != nil {
		s.logger.Printf("[ssl] WARNING: could not record the registration alert for %s; it may repeat: %v", name, err)
		return
	}
	s.logger.Printf("[ssl] registration alert sent for %s (%d day(s) left)", name, days)
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
		// Both clocks, independently. A certificate that cannot be read must
		// not stop the registration lookup: for an apex domain behind an
		// internal resolver, registration is often the only one that works.
		certErr := s.CheckCertificate(ctx, &certs[i])
		regErr := s.CheckRegistration(ctx, &certs[i])
		if certErr != nil {
			failed++
			s.logger.Printf("[ssl] %s certificate: %v", certs[i].Domain, certErr)
		}
		if regErr != nil {
			s.logger.Printf("[ssl] %s registration: %v", certs[i].Domain, regErr)
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
	// This is what lets an apex domain be added even where an internal
	// resolver breaks the TLS check — the registration clock still reads.
	if err := s.CheckCertificate(ctx, cert); err != nil {
		s.logger.Printf("[ssl] first certificate check of %s failed: %v", cert.Domain, err)
	}
	if err := s.CheckRegistration(ctx, cert); err != nil {
		s.logger.Printf("[ssl] first registration check of %s failed: %v", cert.Domain, err)
	}
	return nil
}

// Update changes only the two fields that are the operator's to choose. The
// domain and the check interval are fixed after creation: changing the domain
// would silently repoint the history, and the interval is not configurable.
func (s *SSLCheckerService) Update(ctx context.Context, id uuid.UUID, notifyDays int, enabled bool, channels models.StringSlice) (*models.SSLCertificate, error) {
	cert, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		"expiry_notification_days": notifyDays,
		"enabled":                  enabled,
		"notify_channels":          channels,
		"updated_at":               time.Now(),
	}
	// The threshold decides what counts as "expiring soon", so a change to it
	// has to re-derive the status; otherwise a row keeps a classification made
	// under the old rule until the next daily sweep.
	now := time.Now()
	if cert.ExpiryDate != nil {
		status, days := models.DeriveStatus(*cert.ExpiryDate, notifyDays, now)
		updates["status"] = status
		updates["days_until_expiry"] = days
	}
	// The same threshold governs both clocks, so both are re-derived.
	if cert.DomainExpiryDate != nil {
		status, days := models.DeriveStatus(*cert.DomainExpiryDate, notifyDays, now)
		updates["domain_status"] = status
		updates["domain_days_until_expiry"] = days
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
