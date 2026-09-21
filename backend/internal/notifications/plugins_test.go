package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// spyPlugin records the message it was last sent, so a test can inspect what
// the manager actually handed it - in particular, what timezone Timestamp
// carried after passing through the manager's dispatch.
type spyPlugin struct {
	lastMessage *NotificationMessage
}

func (p *spyPlugin) Send(_ context.Context, message *NotificationMessage) error {
	p.lastMessage = message
	return nil
}
func (p *spyPlugin) ValidateConfig(map[string]interface{}) error { return nil }
func (p *spyPlugin) Name() string                                { return "spy" }
func (p *spyPlugin) IsEnabled() bool                             { return true }

// The regression this covers: a notification's Timestamp was whatever
// zone the process happened to construct it in (UTC, inside a container),
// with no regard for Settings -> System -> Timezone - the same setting
// report generation already respects. SendToChannel is the shared choke
// point SendNotification and TestConfig also dispatch through.
func TestSendToChannelConvertsTimestampToConfiguredLocation(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skipf("tzdata unavailable in this environment: %v", err)
	}
	SetLocationResolver(func() *time.Location { return loc })
	t.Cleanup(func() { SetLocationResolver(nil) })

	m := NewNotificationManager(nil)
	spy := &spyPlugin{}
	id := uuid.New()
	m.setInstance(&ChannelInstance{ID: id, Name: "Test", Type: "spy", Plugin: spy})

	sentAt := time.Date(2026, 9, 21, 20, 23, 32, 0, time.UTC)
	if err := m.SendToChannel(context.Background(), id, &NotificationMessage{
		MonitorName: "x", Timestamp: sentAt,
	}); err != nil {
		t.Fatalf("SendToChannel: %v", err)
	}

	if spy.lastMessage == nil {
		t.Fatal("plugin never received a message")
	}
	if spy.lastMessage.Timestamp.Location() != loc {
		t.Errorf("expected the message's timezone to be %v, got %v", loc, spy.lastMessage.Timestamp.Location())
	}
	// The instant itself must be unchanged - only how it is displayed differs.
	if !spy.lastMessage.Timestamp.Equal(sentAt) {
		t.Errorf("conversion changed the instant: got %v, want %v", spy.lastMessage.Timestamp, sentAt)
	}
}

func TestResolveLocationDefaultsToUTC(t *testing.T) {
	SetLocationResolver(nil)
	if got := resolveLocation(); got != time.UTC {
		t.Errorf("expected UTC with no resolver set, got %v", got)
	}

	SetLocationResolver(func() *time.Location { return nil })
	t.Cleanup(func() { SetLocationResolver(nil) })
	if got := resolveLocation(); got != time.UTC {
		t.Errorf("expected UTC when the resolver itself returns nil, got %v", got)
	}
}
