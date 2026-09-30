package notifications

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTelegramBuildTextWarning(t *testing.T) {
	p := &TelegramPlugin{}
	got := p.buildText(&NotificationMessage{MonitorName: "web-01", Status: "warning"})

	if !strings.Contains(got, "🟡") {
		t.Errorf("expected the warning emoji in the message, got: %s", got)
	}
	if strings.Contains(got, "🟢") {
		t.Error("warning must not render with the green/good emoji")
	}
}

// A device alert must link to its device page and never offer a report link.
func TestTelegramBuildTextDeviceLinksToDevicePage(t *testing.T) {
	deviceID := uuid.New()
	p := &TelegramPlugin{}
	got := p.buildText(&NotificationMessage{MonitorName: "core-sw-1", Status: "down", DeviceID: &deviceID})

	if !strings.Contains(got, "network/devices/"+deviceID.String()) {
		t.Errorf("expected a link to the device page, got: %s", got)
	}
	if strings.Contains(got, "/monitors/") {
		t.Errorf("device alert must not link to /monitors/, got: %s", got)
	}
	if strings.Contains(got, "View Report") {
		t.Errorf("device alert must not offer a report link, got: %s", got)
	}
}
