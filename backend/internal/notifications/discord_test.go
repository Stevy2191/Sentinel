package notifications

import "testing"

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
