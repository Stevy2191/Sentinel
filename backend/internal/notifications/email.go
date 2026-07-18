package notifications

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultSMTPPort    = 587
	defaultSMTPTimeout = 30 * time.Second
	defaultBaseURL     = "http://localhost:3000"

	colorSuccess = "#10b981" // emerald
	colorError   = "#ef4444" // red
	colorText    = "#334155" // slate
	colorMuted   = "#64748b"
)

// EmailPlugin delivers notifications over SMTP with an HTML + plain-text body.
type EmailPlugin struct {
	host       string
	port       int
	user       string
	password   string
	from       string
	to         []string
	tlsEnabled bool
	logger     *log.Logger
}

// nonRetriable wraps errors (auth/config) that must not be retried.
type nonRetriable struct{ err error }

func (e nonRetriable) Error() string { return e.err.Error() }
func (e nonRetriable) Unwrap() error { return e.err }

// NewEmailPlugin builds an EmailPlugin from SMTP_* environment variables,
// returning an error naming the first missing/invalid field.
func NewEmailPlugin() (*EmailPlugin, error) {
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	if host == "" {
		return nil, errors.New("SMTP_HOST is required")
	}

	port := defaultSMTPPort
	if v := strings.TrimSpace(os.Getenv("SMTP_PORT")); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return nil, fmt.Errorf("invalid SMTP_PORT %q: must be an integer 1-65535", v)
		}
		port = p
	}

	user := strings.TrimSpace(os.Getenv("SMTP_USER"))
	if user == "" {
		return nil, errors.New("SMTP_USER is required")
	}

	password := os.Getenv("SMTP_PASSWORD")
	if password == "" {
		return nil, errors.New("SMTP_PASSWORD is required")
	}

	from := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if from == "" {
		from = user
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("invalid SMTP_FROM address %q: %w", from, err)
	}

	tlsEnabled := true
	if v := strings.TrimSpace(os.Getenv("SMTP_TLS")); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			tlsEnabled = parsed
		}
	}

	to := parseRecipients(os.Getenv("SMTP_TO"))
	if len(to) == 0 {
		to = []string{user}
	}

	p := &EmailPlugin{
		host:       host,
		port:       port,
		user:       user,
		password:   password,
		from:       from,
		to:         to,
		tlsEnabled: tlsEnabled,
		logger:     log.Default(),
	}
	p.logger.Printf("[email] Email plugin initialized for %s", from)
	return p, nil
}

// parseRecipients splits a comma-separated recipient list, trimming blanks.
func parseRecipients(raw string) []string {
	var out []string
	for _, r := range strings.Split(raw, ",") {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// Name returns the plugin's channel name.
func (p *EmailPlugin) Name() string { return "email" }

// IsEnabled reports whether all required fields are configured.
func (p *EmailPlugin) IsEnabled() bool {
	return p.host != "" && p.user != "" && p.password != "" && p.from != ""
}

// ValidateConfig validates the given config map. When config is nil, the
// plugin's own current configuration is validated instead (this is how the
// NotificationManager validates a self-configured plugin at registration).
func (p *EmailPlugin) ValidateConfig(config map[string]interface{}) error {
	if config == nil {
		config = map[string]interface{}{
			"host":     p.host,
			"port":     p.port,
			"user":     p.user,
			"password": p.password,
			"from":     p.from,
		}
	}

	if host, _ := config["host"].(string); strings.TrimSpace(host) == "" {
		return errors.New("host is required")
	}

	port, err := toInt(config["port"])
	if err != nil {
		return fmt.Errorf("port is invalid: %w", err)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", port)
	}

	if user, _ := config["user"].(string); strings.TrimSpace(user) == "" {
		return errors.New("user is required")
	}
	if password, _ := config["password"].(string); password == "" {
		return errors.New("password is required")
	}

	from, _ := config["from"].(string)
	if _, err := mail.ParseAddress(from); err != nil {
		return fmt.Errorf("from is not a valid email address: %w", err)
	}

	return nil
}

// toInt coerces an int/float64/string config value to an int.
func toInt(v interface{}) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(n))
	default:
		return 0, fmt.Errorf("expected integer, got %T", v)
	}
}

// Send builds and delivers the notification email, retrying on temporary
// network errors. It respects the context deadline (defaulting to 30s).
func (p *EmailPlugin) Send(ctx context.Context, message *NotificationMessage) error {
	if message == nil {
		return errors.New("message is nil")
	}
	if message.MonitorName == "" || message.Status == "" || message.MonitorURL == "" {
		return errors.New("message MonitorName, Status, and MonitorURL are required")
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultSMTPTimeout)
		defer cancel()
	}

	subject := p.buildSubject(message)
	htmlBody := p.buildHTMLBody(message)
	textBody := p.buildTextBody(message)
	mime := p.buildMIME(subject, htmlBody, textBody)

	start := time.Now()
	if err := p.sendWithRetry(ctx, mime); err != nil {
		p.logger.Printf("[email] ❌ Email failed: %v", err)
		return err
	}
	p.logger.Printf("[email] ✅ Email sent to %s (%dms)", strings.Join(p.to, ", "), time.Since(start).Milliseconds())
	return nil
}

// sendWithRetry attempts delivery, retrying up to twice (1s then 3s backoff) on
// temporary network errors only. Auth/config errors are returned immediately.
func (p *EmailPlugin) sendWithRetry(ctx context.Context, mime string) error {
	backoffs := []time.Duration{1 * time.Second, 3 * time.Second}

	var err error
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		if attempt > 0 {
			wait := backoffs[attempt-1]
			p.logger.Printf("[email] retry %d/%d after %s", attempt, len(backoffs), wait)
			select {
			case <-ctx.Done():
				return fmt.Errorf("email send cancelled: %w", ctx.Err())
			case <-time.After(wait):
			}
		}

		err = p.deliver(ctx, mime)
		if err == nil {
			return nil
		}

		// Never retry auth/config problems.
		var nr nonRetriable
		if errors.As(err, &nr) {
			return err
		}
		// Only retry temporary network errors.
		var netErr net.Error
		if !(errors.As(err, &netErr) && netErr.Temporary()) {
			return err
		}
	}
	return err
}

// deliver performs a single SMTP conversation to send the message.
func (p *EmailPlugin) deliver(ctx context.Context, mime string) error {
	addr := net.JoinHostPort(p.host, strconv.Itoa(p.port))

	deadline := time.Now().Add(defaultSMTPTimeout)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SMTP server unreachable: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)

	client, err := smtp.NewClient(conn, p.host)
	if err != nil {
		return fmt.Errorf("SMTP handshake failed: %w", err)
	}
	defer client.Close()

	if p.tlsEnabled {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: p.host}); err != nil {
				return fmt.Errorf("STARTTLS failed: %w", err)
			}
		}
	}

	if p.user != "" && p.password != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", p.user, p.password, p.host)
			if err := client.Auth(auth); err != nil {
				return nonRetriable{fmt.Errorf("invalid SMTP credentials: %w", err)}
			}
		}
	}

	if err := client.Mail(p.from); err != nil {
		return fmt.Errorf("MAIL FROM failed: %w", err)
	}
	for _, rcpt := range p.to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("RCPT TO %q failed: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA command failed: %w", err)
	}
	if _, err := w.Write([]byte(mime)); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return client.Quit()
}

// buildSubject constructs the subject line for the message's status.
func (p *EmailPlugin) buildSubject(m *NotificationMessage) string {
	switch m.Status {
	case "down":
		return fmt.Sprintf("[ALERT] %s is DOWN", m.MonitorName)
	case "up":
		return fmt.Sprintf("[RECOVERED] %s is UP", m.MonitorName)
	case "recovered":
		return fmt.Sprintf("[RECOVERED] %s has recovered", m.MonitorName)
	default:
		return fmt.Sprintf("[Sentinel] %s status: %s", m.MonitorName, m.Status)
	}
}

// statusStyle returns the badge color and label for a status.
func statusStyle(status string) (color, label string) {
	switch status {
	case "down":
		return colorError, "DOWN"
	case "up":
		return colorSuccess, "UP"
	case "recovered":
		return colorSuccess, "RECOVERED"
	default:
		return colorMuted, strings.ToUpper(status)
	}
}

// baseURL returns the configured Sentinel base URL for building action links.
func baseURL() string {
	if v := strings.TrimSpace(os.Getenv("SENTINEL_BASE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultBaseURL
}

// buildHTMLBody renders the styled HTML email using inline styles for broad
// email-client compatibility.
func (p *EmailPlugin) buildHTMLBody(m *NotificationMessage) string {
	color, label := statusStyle(m.Status)
	base := baseURL()
	detailURL := fmt.Sprintf("%s/monitors/%s", base, m.MonitorID)
	reportURL := fmt.Sprintf("%s/monitors/%s/report", base, m.MonitorID)
	timestamp := m.Timestamp.Format("Mon, 02 Jan 2006 15:04:05 MST")

	var extra strings.Builder
	if m.PreviousStatus != "" {
		fmt.Fprintf(&extra, `<tr><td style="padding:4px 0;color:%s;">Previous status</td><td style="padding:4px 0;color:%s;text-align:right;font-weight:600;">%s</td></tr>`, colorMuted, colorText, htmlEscape(m.PreviousStatus))
	}
	if m.ResponseTimeMs > 0 {
		fmt.Fprintf(&extra, `<tr><td style="padding:4px 0;color:%s;">Response time</td><td style="padding:4px 0;color:%s;text-align:right;font-weight:600;">%dms</td></tr>`, colorMuted, colorText, m.ResponseTimeMs)
	}
	if m.Status == "recovered" && m.DowntimeDuration > 0 {
		fmt.Fprintf(&extra, `<tr><td style="padding:4px 0;color:%s;">Downtime duration</td><td style="padding:4px 0;color:%s;text-align:right;font-weight:600;">%s</td></tr>`, colorMuted, colorText, m.DowntimeDuration.String())
	}

	message := ""
	if m.Message != "" {
		message = fmt.Sprintf(`<p style="margin:16px 0 0;color:%s;font-size:14px;line-height:1.5;">%s</p>`, colorText, htmlEscape(m.Message))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="margin:0;padding:0;background:#f1f5f9;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="background:#f1f5f9;padding:24px 0;">
    <tr><td align="center">
      <table role="presentation" width="600" cellpadding="0" cellspacing="0" style="max-width:600px;width:100%%;background:#ffffff;border-radius:12px;overflow:hidden;box-shadow:0 1px 3px rgba(0,0,0,0.1);">
        <tr><td style="background:%s;padding:20px 32px;">
          <span style="color:#ffffff;font-size:16px;font-weight:700;letter-spacing:0.3px;">Sentinel Monitoring Alert</span>
        </td></tr>
        <tr><td style="padding:32px;">
          <h1 style="margin:0 0 12px;color:%s;font-size:24px;font-weight:800;">%s</h1>
          <div style="display:inline-block;background:%s;color:#ffffff;padding:6px 14px;border-radius:999px;font-size:13px;font-weight:700;">&#9679; %s</div>
          %s
          <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" style="margin-top:20px;font-size:14px;">
            <tr><td style="padding:4px 0;color:%s;">URL</td><td style="padding:4px 0;text-align:right;"><a href="%s" style="color:%s;font-weight:600;text-decoration:none;">%s</a></td></tr>
            <tr><td style="padding:4px 0;color:%s;">Timestamp</td><td style="padding:4px 0;color:%s;text-align:right;font-weight:600;">%s</td></tr>
            %s
          </table>
          <table role="presentation" cellpadding="0" cellspacing="0" style="margin-top:28px;">
            <tr>
              <td style="padding-right:12px;"><a href="%s" style="display:inline-block;background:%s;color:#ffffff;padding:11px 22px;border-radius:8px;font-size:14px;font-weight:600;text-decoration:none;">View in Sentinel</a></td>
              <td><a href="%s" style="display:inline-block;background:#ffffff;color:%s;border:1px solid %s;padding:10px 21px;border-radius:8px;font-size:14px;font-weight:600;text-decoration:none;">View Report</a></td>
            </tr>
          </table>
        </td></tr>
        <tr><td style="padding:20px 32px;border-top:1px solid #e2e8f0;">
          <span style="color:%s;font-size:12px;">&copy; 2026 Sentinel Monitoring</span>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`,
		color,
		colorText, htmlEscape(m.MonitorName),
		color, label,
		message,
		colorMuted, htmlEscape(m.MonitorURL), colorSuccess, htmlEscape(m.MonitorURL),
		colorMuted, colorText, timestamp,
		extra.String(),
		detailURL, colorSuccess,
		reportURL, colorSuccess, colorSuccess,
		colorMuted,
	)
}

// buildTextBody renders a plain-text fallback for the email.
func (p *EmailPlugin) buildTextBody(m *NotificationMessage) string {
	base := baseURL()
	var b strings.Builder
	fmt.Fprintf(&b, "Sentinel Monitoring Alert\n\n")
	fmt.Fprintf(&b, "%s\n", m.MonitorName)
	_, label := statusStyle(m.Status)
	fmt.Fprintf(&b, "Status: %s\n", label)
	if m.PreviousStatus != "" {
		fmt.Fprintf(&b, "Previous status: %s\n", m.PreviousStatus)
	}
	fmt.Fprintf(&b, "URL: %s\n", m.MonitorURL)
	fmt.Fprintf(&b, "Timestamp: %s\n", m.Timestamp.Format("Mon, 02 Jan 2006 15:04:05 MST"))
	if m.ResponseTimeMs > 0 {
		fmt.Fprintf(&b, "Response time: %dms\n", m.ResponseTimeMs)
	}
	if m.Status == "recovered" && m.DowntimeDuration > 0 {
		fmt.Fprintf(&b, "Downtime duration: %s\n", m.DowntimeDuration.String())
	}
	if m.Message != "" {
		fmt.Fprintf(&b, "\n%s\n", m.Message)
	}
	fmt.Fprintf(&b, "\nView in Sentinel: %s/monitors/%s\n", base, m.MonitorID)
	fmt.Fprintf(&b, "View Report: %s/monitors/%s/report\n", base, m.MonitorID)
	fmt.Fprintf(&b, "\n(c) 2026 Sentinel Monitoring\n")
	return b.String()
}

// buildMIME assembles a multipart/alternative message with plain-text and HTML
// parts and the required headers.
func (p *EmailPlugin) buildMIME(subject, htmlBody, textBody string) string {
	boundary := "sentinel-boundary-a1b2c3d4"
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", p.from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(p.to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n", boundary)
	fmt.Fprintf(&b, "\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
	b.WriteString(normalizeCRLF(textBody))
	fmt.Fprintf(&b, "\r\n")

	fmt.Fprintf(&b, "--%s\r\n", boundary)
	fmt.Fprintf(&b, "Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
	b.WriteString(normalizeCRLF(htmlBody))
	fmt.Fprintf(&b, "\r\n")

	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.String()
}

// normalizeCRLF ensures the body uses CRLF line endings as required by SMTP.
func normalizeCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

// htmlEscape escapes the characters that matter for HTML text content.
func htmlEscape(s string) string {
	r := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
	)
	return r.Replace(s)
}
