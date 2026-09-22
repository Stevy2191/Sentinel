package notifications

import "testing"

func TestNtfyBuildTitleAndTagsWarning(t *testing.T) {
	p := &NtfyPlugin{}
	msg := &NotificationMessage{MonitorName: "web-01", Status: "warning"}

	title := p.buildTitle(msg)
	if title != "[WARNING] web-01" {
		t.Errorf("got title %q, want %q", title, "[WARNING] web-01")
	}

	tags := p.buildTags(msg)
	if tags != "warning" {
		t.Errorf("got tags %q, want %q", tags, "warning")
	}
}
