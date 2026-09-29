package notifications

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWebhookPayloadForDevice(t *testing.T) {
	id := uuid.New()
	p := (&WebhookPlugin{}).buildPayload(&NotificationMessage{
		DeviceID: &id, MonitorName: "core-sw-1", MonitorURL: "10.20.0.2", SiteName: "Warehouse",
		Status: "down", Timestamp: time.Now(),
	})
	if p.Type != "sentinel_device_alert" || p.Device == nil || p.Device.ID != id.String() ||
		p.Device.Site != "Warehouse" || p.Monitor.ID != "" {
		t.Fatalf("payload: %+v", p)
	}
	if !strings.HasSuffix(p.Links.ViewInSentinel, "/network/devices/"+id.String()) {
		t.Errorf("link = %q", p.Links.ViewInSentinel)
	}
}
