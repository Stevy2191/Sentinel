package notifications

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDiscordBuildPayloadWarning(t *testing.T) {
	p := &DiscordPlugin{}
	payload := p.buildPayload(&NotificationMessage{MonitorName: "web-01", Status: "warning"})

	if len(payload.Embeds) == 0 {
		t.Fatal("expected at least one embed")
	}
	if payload.Embeds[0].Color != colorDiscordWarning {
		t.Errorf("got color %#x, want colorDiscordWarning (%#x)", payload.Embeds[0].Color, colorDiscordWarning)
	}
	if payload.Embeds[0].Color == colorDiscordUp {
		t.Error("warning must not render with the up/success color")
	}
}

// A device alert must link to its device page, not /monitors/00000000-...
// (MonitorID's zero value, since a device message never sets it), and must
// not offer a report link - devices have no report page.
func TestDiscordBuildPayloadDeviceLinksToDevicePage(t *testing.T) {
	deviceID := uuid.New()
	p := &DiscordPlugin{}
	payload := p.buildPayload(&NotificationMessage{
		MonitorName: "core-sw-1", Status: "down", DeviceID: &deviceID,
	})

	embed := payload.Embeds[0]
	wantPath := "/network/devices/" + deviceID.String()
	if !strings.Contains(embed.URL, wantPath) {
		t.Errorf("embed URL = %q, want it to contain %q", embed.URL, wantPath)
	}
	if !strings.Contains(embed.Description, wantPath) {
		t.Errorf("embed description = %q, want it to contain %q", embed.Description, wantPath)
	}
	if strings.Contains(embed.URL, "/monitors/") || strings.Contains(embed.Description, "/monitors/") {
		t.Errorf("device alert must not link to /monitors/: url=%q description=%q", embed.URL, embed.Description)
	}
	if strings.Contains(embed.Description, "View Report") {
		t.Errorf("device alert must not offer a report link: %q", embed.Description)
	}
}
