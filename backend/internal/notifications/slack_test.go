package notifications

import "testing"

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
