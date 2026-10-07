package notifications

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The report link must open a page the web app actually serves: the
// analytics view, opened on this monitor. /monitors/<id>/report is only an
// API route (the browser got a blank page), and /reports?monitor_id= is the
// saved-reports list, which ignores the id.
func TestReportPath(t *testing.T) {
	id := uuid.New()
	msg := NotificationMessage{MonitorID: id}
	if got, want := msg.ReportPath(), "/reports/analytics?monitor_id="+id.String(); got != want {
		t.Errorf("ReportPath() = %q, want %q", got, want)
	}
}

// Every channel must link the report through ReportPath, so they all land on
// the same working page.
func TestEveryChannelLinksTheReportPage(t *testing.T) {
	SetBaseURLResolver(func() string { return "https://sentinel.test" })
	t.Cleanup(func() { SetBaseURLResolver(nil) })

	id := uuid.New()
	msg := &NotificationMessage{MonitorID: id, MonitorName: "web-01", Status: "down", Timestamp: time.Now()}
	want := "https://sentinel.test" + msg.ReportPath()

	email := &EmailPlugin{}
	bodies := map[string]string{
		"email html": email.buildHTMLBody(msg),
		"email text": email.buildTextBody(msg),
		"slack":      strings.Join(actionURLs((&SlackPlugin{}).buildPayload(msg)), " "),
		"discord":    (&DiscordPlugin{}).buildPayload(msg).Embeds[0].Description,
		"telegram":   (&TelegramPlugin{}).buildText(msg),
		"webhook":    (&WebhookPlugin{}).buildPayload(msg).Links.ViewReport,
	}
	for name, body := range bodies {
		if !strings.Contains(body, want) {
			t.Errorf("%s: missing report link %q in: %s", name, want, body)
		}
		if strings.Contains(body, "/report\"") || strings.Contains(body, "/report\n") ||
			strings.Contains(body, "/reports?monitor_id=") {
			t.Errorf("%s: still has an old report link: %s", name, body)
		}
	}
}
