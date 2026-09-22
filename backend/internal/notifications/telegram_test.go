package notifications

import (
	"strings"
	"testing"
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
