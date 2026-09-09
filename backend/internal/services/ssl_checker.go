package services

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
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

	SubjectCommonName  string
	IssuerCommonName   string
	SerialNumber       string
	SignatureAlgorithm string
	PublicKeyAlgorithm string
	SubjectAltNames    []string
	ValidFrom          time.Time
	// ResolvedIP is the address the handshake reached, empty if the connection
	// never got that far.
	ResolvedIP string
}

// describeCertificate reads the fields of a leaf certificate that the detail
// view shows.
func describeCertificate(leaf *x509.Certificate) CertificateInfo {
	return CertificateInfo{
		Issuer:             issuerName(leaf.Issuer.CommonName, leaf.Issuer.Organization),
		ExpiryDate:         leaf.NotAfter,
		ValidFrom:          leaf.NotBefore,
		SubjectCommonName:  strings.TrimSpace(leaf.Subject.CommonName),
		IssuerCommonName:   strings.TrimSpace(leaf.Issuer.CommonName),
		SerialNumber:       serialString(leaf),
		SignatureAlgorithm: leaf.SignatureAlgorithm.String(),
		PublicKeyAlgorithm: publicKeyDescription(leaf),
		SubjectAltNames:    leaf.DNSNames,
	}
}

// serialString renders the serial number as text.
//
// A serial is up to 20 octets, so it fits neither an int64 nor a JSON number:
// a consumer that parses it as a float renders it in scientific notation and
// silently drops digits. Hex is also how openssl and browsers print it, which
// makes it comparable with what an operator sees elsewhere.
func serialString(leaf *x509.Certificate) string {
	if leaf.SerialNumber == nil {
		return ""
	}
	h := strings.ToUpper(leaf.SerialNumber.Text(16))
	if len(h)%2 == 1 {
		h = "0" + h
	}
	// Grouped in octets, as openssl prints it.
	var b strings.Builder
	for i := 0; i < len(h); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(h[i : i+2])
	}
	return b.String()
}

// publicKeyDescription names the key type and its size, e.g. "ECDSA 256-bit".
func publicKeyDescription(leaf *x509.Certificate) string {
	switch key := leaf.PublicKey.(type) {
	case *rsa.PublicKey:
		return fmt.Sprintf("RSA %d-bit", key.N.BitLen())
	case *ecdsa.PublicKey:
		return fmt.Sprintf("ECDSA %d-bit", key.Curve.Params().BitSize)
	case ed25519.PublicKey:
		return "Ed25519"
	default:
		return leaf.PublicKeyAlgorithm.String()
	}
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
	// The address the check actually reached, recorded even when the
	// handshake then fails: it is what shows a lookup landed on the wrong host.
	var resolvedIP string

	resolved, err := resolveHost(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", domain, err)
	}

	// Every address the winning resolver returned is tried, not just the
	// first. A host commonly publishes several, and one of them being
	// unreachable is not the same as the service being down.
	var conn *tls.Conn
	var dialErr error
	for _, ip := range resolved.IPs {
		// Recorded before the attempt, not after it succeeds: the address is
		// most useful precisely when the connection fails, since naming it is
		// what shows the lookup landed somewhere unexpected.
		resolvedIP = ip
		handshakeCtx, cancel := context.WithTimeout(ctx, sslHandshakeTimeout)
		dialer := &tls.Dialer{
			NetDialer: &net.Dialer{Timeout: sslHandshakeTimeout},
			Config: &tls.Config{
				// The name, not the address: SNI and certificate verification
				// are both about the host being checked, while the connection
				// goes to the address DNS gave us.
				ServerName: host,
				MinVersion: tls.VersionTLS12,
			},
		}
		raw, err := dialer.DialContext(handshakeCtx, "tcp", net.JoinHostPort(ip, port))
		cancel()
		if err == nil {
			conn = raw.(*tls.Conn)
			dialErr = nil
			break
		}
		dialErr = err
		// A certificate that fails verification is a finding about that host,
		// not a reason to try another address: the next one would report the
		// same thing.
		var invalid *tls.CertificateVerificationError
		if errors.As(err, &invalid) {
			break
		}
	}

	if conn == nil {
		err := dialErr
		// An expired certificate is the case this feature exists to catch, so
		// dig the certificate out of the verification error and report it as a
		// real expiry rather than as a connection problem.
		var invalid *tls.CertificateVerificationError
		if errors.As(err, &invalid) && len(invalid.UnverifiedCertificates) > 0 {
			info := describeCertificate(invalid.UnverifiedCertificates[0])
			info.ResolvedIP = resolvedIP
			return &info, nil
		}
		return nil, fmt.Errorf("%s: %w", domain, explainDialFailure(host, resolvedIP, resolved.Via, err))
	}
	defer conn.Close()

	chain := conn.ConnectionState().PeerCertificates
	if len(chain) == 0 {
		return nil, fmt.Errorf("%s presented no certificate", domain)
	}
	info := describeCertificate(chain[0])
	info.ResolvedIP = resolvedIP
	return &info, nil
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
// naming the address the check reached and which resolver supplied it.
//
// The address is the point. A refused connection or a certificate for the wrong
// host says nothing about why, and a private address next to a public domain
// names the cause immediately: the answer came from local DNS. Since public
// resolvers are tried first, reaching that state means public DNS did not
// answer at all, which is worth saying rather than leaving to be guessed.
func explainDialFailure(host, ip, via string, err error) error {
	simple := simplifyTLSError(err)
	if ip == "" {
		return simple
	}

	parsed := net.ParseIP(ip)
	if parsed != nil && (parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast()) {
		return fmt.Errorf("%w (reached the internal address %s, from %s — public DNS did not answer "+
			"for %s, so the check fell back to local DNS and landed on an internal host)",
			simple, ip, via, host)
	}
	return fmt.Errorf("%w (reached %s, from %s)", simple, ip, via)
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

		// The rest of what the handshake told us. Written through a helper
		// because each field has to land in both the update map and the
		// in-memory row, and the caller is handed that row back.
		setStr := func(col string, v string, dst **string) {
			if v == "" {
				updates[col] = nil
				*dst = nil
				return
			}
			val := v
			updates[col] = val
			*dst = &val
		}
		setStr("subject_common_name", info.SubjectCommonName, &cert.SubjectCommonName)
		setStr("issuer_common_name", info.IssuerCommonName, &cert.IssuerCommonName)
		setStr("serial_number", info.SerialNumber, &cert.SerialNumber)
		setStr("signature_algorithm", info.SignatureAlgorithm, &cert.SignatureAlgorithm)
		setStr("public_key_algorithm", info.PublicKeyAlgorithm, &cert.PublicKeyAlgorithm)
		// Only overwritten when known: an expired certificate is read from the
		// verification error, which carries no connection to take an address
		// from, and forgetting the last known address would be a loss.
		if info.ResolvedIP != "" {
			setStr("resolved_ip", info.ResolvedIP, &cert.ResolvedIP)
		}

		sans := models.StringSlice(info.SubjectAltNames)
		updates["subject_alternative_names"] = sans
		cert.SubjectAltNames = sans

		if !info.ValidFrom.IsZero() {
			updates["valid_from"] = info.ValidFrom
			vf := info.ValidFrom
			cert.ValidFrom = &vf
		}
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
	// Detached from the caller's cancellation. On the create and check-now
	// paths this is a request context, and a browser that navigates away would
	// otherwise cancel the lookup and store the cancellation as the domain's
	// error. The lookup bounds itself, and CheckAll re-checks ctx between
	// domains, so shutdown still stops promptly.
	info, err := LookupRegistration(context.WithoutCancel(ctx), cert.Domain)

	updates := map[string]interface{}{"registration_checked_at": now, "updated_at": now}
	if err != nil {
		msg := err.Error()
		// A failed lookup today does not change when the domain expires, so
		// everything already known is kept — including the status, which is
		// re-derived from the stored date. Resetting it to "unknown" would
		// turn a healthy domain into a warning on one timeout and throw away
		// the expiry date that is still perfectly good.
		if cert.DomainExpiryDate != nil {
			status, days := models.DeriveStatus(*cert.DomainExpiryDate, cert.ExpiryNotificationDays, now)
			updates["domain_status"] = status
			updates["domain_days_until_expiry"] = days
			cert.DomainStatus = status
			cert.DomainDaysUntilExpiry = &days
		} else {
			// Nothing was ever read for this domain, so there is genuinely
			// nothing to report but the failure.
			updates["domain_status"] = models.SSLStatusUnknown
			cert.DomainStatus = models.SSLStatusUnknown
		}
		updates["registration_error"] = msg
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

// Refresh re-reads both clocks for one domain and reports each result
// separately.
//
// The two are independent on purpose: a certificate that cannot be read must
// not stop the registration lookup, and vice versa. Every path that refreshes
// a domain goes through here so none of them can quietly cover only one half.
func (s *SSLCheckerService) Refresh(ctx context.Context, cert *models.SSLCertificate) (certErr, regErr error) {
	certErr = s.CheckCertificate(ctx, cert)
	regErr = s.CheckRegistration(ctx, cert)
	return certErr, regErr
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
	certErr, regErr := s.Refresh(ctx, cert)
	if certErr != nil {
		s.logger.Printf("[ssl] first certificate check of %s failed: %v", cert.Domain, certErr)
	}
	if regErr != nil {
		s.logger.Printf("[ssl] first registration check of %s failed: %v", cert.Domain, regErr)
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
