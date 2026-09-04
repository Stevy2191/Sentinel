// Package services - report_mailer.go delivers a generated report by email.
//
// It deliberately does NOT open its own SMTP connection. The spec proposed a
// standalone EmailService using smtp.SendMail with PlainAuth, which would have
// bypassed the connection-security modes (none/STARTTLS/SSL-TLS), the
// certificate policy, and the admin-configured SMTP settings that the
// notifications package already implements - reintroducing the cleartext
// credential path that work removed. This resolves the same configuration the
// email notification channel uses and sends through it.
package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
)

// maxAttachmentBytes bounds what will be inlined into a message. Most providers
// reject messages well below this; failing here names the reason instead of
// letting the SMTP server return an opaque error after the upload.
const maxAttachmentBytes = 20 << 20 // 20 MiB

// ReportMailer sends generated reports to a schedule's recipients.
type ReportMailer struct {
	db      *gorm.DB
	baseURL string
}

// NewReportMailer returns a mailer bound to db. baseURL is used to build the
// "view online" link; an empty value falls back to SENTINEL_BASE_URL.
func NewReportMailer(db *gorm.DB, baseURL string) *ReportMailer {
	if baseURL == "" {
		baseURL = strings.TrimRight(os.Getenv("SENTINEL_BASE_URL"), "/")
	}
	return &ReportMailer{db: db, baseURL: strings.TrimRight(baseURL, "/")}
}

// ReportEmail is one outgoing scheduled-report message.
type ReportEmail struct {
	To         []string
	ReportName string
	// AttachmentPath, when set, is attached as a PDF.
	AttachmentPath string
	// ShareLink, when set, is offered as a "view online" button.
	ShareLink string
	// Summary lines are rendered in the body when the schedule asks for them.
	Summary []string
}

// resolveSender builds an email sender from the stored notification config for
// the email channel, falling back to the SMTP_* environment. The DB config
// wins, matching how the notification manager resolves the same channel.
func (m *ReportMailer) resolveSender(ctx context.Context) (*notifications.EmailPlugin, error) {
	var cfg models.NotificationConfig
	err := m.db.WithContext(ctx).First(&cfg, "channel = ?", "email").Error
	if err == nil && cfg.SMTPHost != nil && *cfg.SMTPHost != "" {
		port := 0
		if cfg.SMTPPort != nil {
			port = *cfg.SMTPPort
		}
		return notifications.NewEmailPluginFromConfig(
			deref(cfg.SMTPHost), port, deref(cfg.SMTPUser), deref(cfg.SMTPPassword),
			deref(cfg.SMTPFrom), models.ResolveSMTPSecurity(cfg.SMTPSecurity),
			cfg.SMTPSkipTLSVerify,
		), nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("loading email configuration: %w", err)
	}

	plugin, envErr := notifications.NewEmailPlugin()
	if envErr != nil {
		return nil, fmt.Errorf("email is not configured: %w", envErr)
	}
	return plugin, nil
}

// deref returns the pointed-to string, or "" if nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Send delivers the report email, attaching the PDF when one is given.
func (m *ReportMailer) Send(ctx context.Context, email ReportEmail) error {
	if len(email.To) == 0 {
		return errors.New("no recipients")
	}

	sender, err := m.resolveSender(ctx)
	if err != nil {
		return err
	}

	msg, err := m.buildMIME(sender.From(), email)
	if err != nil {
		return err
	}
	return sender.SendRaw(ctx, email.To, msg)
}

// buildMIME assembles the message. With an attachment it is multipart/mixed
// wrapping a multipart/alternative body; without one it is just the body.
//
// The spec's version declared an Attachments field but wrote a plain text/html
// message, so "send as attachment" silently sent nothing. The attachment is
// actually encoded here.
func (m *ReportMailer) buildMIME(from string, email ReportEmail) (string, error) {
	subject := fmt.Sprintf("Your report: %s", email.ReportName)
	textBody := m.textBody(email)
	htmlBody := m.htmlBody(email)

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(email.To, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")

	var attachment []byte
	attachName := ""
	if email.AttachmentPath != "" {
		data, err := os.ReadFile(email.AttachmentPath)
		if err != nil {
			return "", fmt.Errorf("reading report attachment: %w", err)
		}
		if len(data) > maxAttachmentBytes {
			return "", fmt.Errorf("report attachment is %d bytes, over the %d byte limit", len(data), maxAttachmentBytes)
		}
		attachment = data
		attachName = attachmentName(email.ReportName)
	}

	const (
		mixedBoundary = "sentinel-mixed-b1c2d3"
		altBoundary   = "sentinel-alt-e4f5a6"
	)

	writeAlternative := func() {
		fmt.Fprintf(&b, "--%s\r\n", altBoundary)
		b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n\r\n")
		b.WriteString(normalizeCRLF(textBody))
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s\r\n", altBoundary)
		b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n\r\n")
		b.WriteString(normalizeCRLF(htmlBody))
		b.WriteString("\r\n")
		fmt.Fprintf(&b, "--%s--\r\n", altBoundary)
	}

	if attachment == nil {
		fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", altBoundary)
		writeAlternative()
		return b.String(), nil
	}

	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mixedBoundary)
	fmt.Fprintf(&b, "--%s\r\n", mixedBoundary)
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", altBoundary)
	writeAlternative()

	fmt.Fprintf(&b, "--%s\r\n", mixedBoundary)
	b.WriteString("Content-Type: application/pdf\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n")
	fmt.Fprintf(&b, "Content-Disposition: attachment; filename=%q\r\n\r\n", attachName)
	b.WriteString(wrapBase64(attachment))
	fmt.Fprintf(&b, "\r\n--%s--\r\n", mixedBoundary)

	return b.String(), nil
}

// attachmentName turns a report name into a safe .pdf file name. The spec used
// the raw report name, which can contain quotes, slashes, or newlines and would
// break - or forge - the Content-Disposition header.
func attachmentName(reportName string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == ' ':
			return r
		default:
			return '-'
		}
	}, reportName)
	safe = strings.TrimSpace(safe)
	if safe == "" {
		safe = "report"
	}
	if len(safe) > 80 {
		safe = safe[:80]
	}
	return safe + ".pdf"
}

// wrapBase64 encodes data and wraps it at 76 columns, as RFC 2045 requires.
func wrapBase64(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(encoded) > 76 {
		b.WriteString(encoded[:76])
		b.WriteString("\r\n")
		encoded = encoded[76:]
	}
	b.WriteString(encoded)
	b.WriteString("\r\n")
	return b.String()
}

// normalizeCRLF ensures SMTP line endings.
func normalizeCRLF(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "\r\n")
}

func (m *ReportMailer) textBody(email ReportEmail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", email.ReportName)
	fmt.Fprintf(&b, "Your scheduled report was generated on %s.\n",
		time.Now().Format("January 02, 2006 at 15:04 MST"))
	if email.AttachmentPath != "" {
		b.WriteString("The PDF is attached to this message.\n")
	}
	if len(email.Summary) > 0 {
		b.WriteString("\nSummary:\n")
		for _, line := range email.Summary {
			fmt.Fprintf(&b, "  - %s\n", line)
		}
	}
	if email.ShareLink != "" {
		fmt.Fprintf(&b, "\nView online: %s\n", email.ShareLink)
	}
	b.WriteString("\nGenerated automatically by Sentinel.\n")
	return b.String()
}

func (m *ReportMailer) htmlBody(email ReportEmail) string {
	var extras strings.Builder
	if len(email.Summary) > 0 {
		extras.WriteString(`<ul style="margin:12px 0;padding-left:20px;color:#333;">`)
		for _, line := range email.Summary {
			fmt.Fprintf(&extras, "<li>%s</li>", esc(line))
		}
		extras.WriteString("</ul>")
	}
	if email.ShareLink != "" {
		fmt.Fprintf(&extras,
			`<p><a href="%s" style="display:inline-block;padding:10px 20px;background:#3b82f6;color:#fff;text-decoration:none;border-radius:6px;">View report online</a></p>`,
			esc(email.ShareLink))
	}

	attachLine := ""
	if email.AttachmentPath != "" {
		attachLine = "<p>Your PDF report is attached to this email.</p>"
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html><body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Arial,sans-serif;line-height:1.6;color:#333;">
<div style="max-width:600px;margin:0 auto;padding:20px;">
  <div style="background:#f3f4f6;padding:20px;border-radius:8px;margin-bottom:20px;">
    <h1 style="margin:0;font-size:1.5em;">%s</h1>
    <p style="margin:8px 0 0;">Your scheduled report has been generated.</p>
  </div>
  <div>
    <p>Generated on: <strong>%s</strong></p>
    %s
    %s
  </div>
  <div style="color:#666;font-size:.9em;border-top:1px solid #eee;padding-top:20px;margin-top:20px;">
    <p>This report was generated automatically by Sentinel.</p>
  </div>
</div>
</body></html>`,
		esc(email.ReportName),
		esc(time.Now().Format("January 02, 2006 at 15:04 MST")),
		attachLine,
		extras.String(),
	)
}

// SummaryLines condenses report data into the bullet list an email can carry
// when the schedule asks for a summary in the body.
func SummaryLines(data *ReportData) []string {
	if data == nil || len(data.Metrics) == 0 {
		return []string{"No monitors were in scope for this report."}
	}
	lines := make([]string, 0, len(data.Metrics)+1)

	totalIncidents := 0
	avg := 0.0
	for _, m := range data.Metrics {
		totalIncidents += m.IncidentCount
		avg += m.Uptime
	}
	avg /= float64(len(data.Metrics))
	lines = append(lines, fmt.Sprintf("%d services, %.2f%% average uptime, %d incidents",
		len(data.Metrics), avg, totalIncidents))

	for _, m := range data.Metrics {
		if m.SLATarget != nil && !m.SLAMet {
			lines = append(lines, fmt.Sprintf("%s missed its %.2f%% SLA at %.2f%%",
				m.MonitorName, *m.SLATarget, m.Uptime))
		}
	}
	return lines
}

// reportBaseURL returns the configured base URL for building share links.
func (m *ReportMailer) reportBaseURL() string {
	if m.baseURL != "" {
		return m.baseURL
	}
	return "http://localhost:3000"
}

// ShareURL builds an absolute link to a shared report.
func (m *ReportMailer) ShareURL(token string) string {
	return fmt.Sprintf("%s/reports/share/%s", m.reportBaseURL(), filepath.Base(token))
}
