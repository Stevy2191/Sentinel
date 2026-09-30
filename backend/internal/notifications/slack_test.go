package notifications

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestSlackBuildPayloadWarning(t *testing.T) {
	p := &SlackPlugin{}
	payload := p.buildPayload(&NotificationMessage{MonitorName: "web-01", Status: "warning"})

	if len(payload.Attachments) == 0 {
		t.Fatal("expected at least one attachment")
	}
	if payload.Attachments[0].Color != colorWarning {
		t.Errorf("got color %q, want colorWarning (%q)", payload.Attachments[0].Color, colorWarning)
	}
	if payload.Attachments[0].Color == colorSuccess {
		t.Error("warning must not render with the success color")
	}
}

// actionURLs collects every button URL from a Slack payload's "actions" block.
func actionURLs(payload slackPayload) []string {
	var urls []string
	for _, a := range payload.Attachments {
		for _, b := range a.Blocks {
			if b.Type != "actions" {
				continue
			}
			for _, el := range b.Elements {
				urls = append(urls, el.URL)
			}
		}
	}
	return urls
}

// A device alert's action button must link to its device page, not
// /monitors/00000000-... (MonitorID's zero value), and must not offer a
// second "View Report" button - devices have no report page.
func TestSlackBuildPayloadDeviceLinksToDevicePage(t *testing.T) {
	deviceID := uuid.New()
	p := &SlackPlugin{}
	payload := p.buildPayload(&NotificationMessage{
		MonitorName: "core-sw-1", Status: "down", DeviceID: &deviceID,
	})

	urls := actionURLs(payload)
	if len(urls) != 1 {
		t.Fatalf("got %d action buttons, want exactly 1 (no report link): %v", len(urls), urls)
	}
	wantPath := "/network/devices/" + deviceID.String()
	if !strings.Contains(urls[0], wantPath) {
		t.Errorf("button URL = %q, want it to contain %q", urls[0], wantPath)
	}
	if strings.Contains(urls[0], "/monitors/") {
		t.Errorf("device alert must not link to /monitors/: %q", urls[0])
	}
}
