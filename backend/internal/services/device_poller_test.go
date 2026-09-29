package services

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type fakeSNMP struct {
	getErr  error
	walkErr error
	gets    int
	walks   int
}

func (f *fakeSNMP) Get(_ context.Context, _ snmp.Target, oids []string) ([]snmp.PDU, error) {
	f.gets++
	if f.getErr != nil {
		return nil, f.getErr
	}
	out := []snmp.PDU{}
	for _, o := range oids {
		switch o {
		case snmp.OIDSysUpTime:
			out = append(out, snmp.PDU{OID: o, Value: uint64(360000)})
		case snmp.OIDSysName:
			out = append(out, snmp.PDU{OID: o, Value: []byte("core-sw-1")})
		case snmp.OIDSysObjectID:
			out = append(out, snmp.PDU{OID: o, Value: "1.3.6.1.4.1.4413"})
		}
	}
	return out, nil
}

func (f *fakeSNMP) Walk(_ context.Context, _ snmp.Target, root string) ([]snmp.PDU, error) {
	f.walks++
	if f.walkErr != nil {
		return nil, f.walkErr
	}
	if root == "1.3.6.1.2.1.2.2.1.1" {
		return []snmp.PDU{{OID: root + ".1", Value: int64(1)}}, nil
	}
	return nil, nil
}

type fakeStore struct {
	mu        sync.Mutex
	due       []models.Device
	saved     []ReachabilityUpdate
	inventory []snmp.Inventory
	invErrors []string
}

func (s *fakeStore) DueDevices(context.Context, time.Time, int) ([]models.Device, error) {
	return s.due, nil
}
func (s *fakeStore) DeviceCredential(context.Context, uuid.UUID) (snmp.Credential, error) {
	return snmp.Credential{Version: "2c", Community: "public"}, nil
}
func (s *fakeStore) SiteName(context.Context, uuid.UUID) (string, error) { return "Warehouse", nil }
func (s *fakeStore) SaveReachability(_ context.Context, _ uuid.UUID, u ReachabilityUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, u)
	return nil
}
func (s *fakeStore) SaveInventory(_ context.Context, _ uuid.UUID, inv snmp.Inventory, _ time.Time) error {
	s.inventory = append(s.inventory, inv)
	return nil
}
func (s *fakeStore) SaveInventoryError(_ context.Context, _ uuid.UUID, detail string, _ time.Time) error {
	s.invErrors = append(s.invErrors, detail)
	return nil
}

type fakeDeviceIncidents struct{ opened, closed int }

func (f *fakeDeviceIncidents) OpenDeviceIncident(_ context.Context, id uuid.UUID, start time.Time, _ string) (*models.Incident, bool, error) {
	f.opened++
	return &models.Incident{ID: uuid.New(), DeviceID: &id, StartTime: start}, true, nil
}
func (f *fakeDeviceIncidents) CloseDeviceIncident(_ context.Context, id uuid.UUID, end time.Time, _ string) (*models.Incident, error) {
	f.closed++
	return &models.Incident{ID: uuid.New(), DeviceID: &id, StartTime: end.Add(-12 * time.Minute), EndTime: &end, DurationSeconds: 720}, nil
}

type fakeNotifier struct {
	sent []*notifications.NotificationMessage
}

func (f *fakeNotifier) SendNotification(_ context.Context, m *notifications.NotificationMessage) error {
	f.sent = append(f.sent, m)
	return nil
}

func newTestPoller(client *fakeSNMP) (*DevicePoller, *fakeStore, *fakeDeviceIncidents, *fakeNotifier) {
	store, inc, notif := &fakeStore{}, &fakeDeviceIncidents{}, &fakeNotifier{}
	p := NewDevicePoller(store, client, inc, notif, 2)
	p.resolve = func(context.Context, string) (net.IP, error) { return net.ParseIP("10.20.0.2"), nil }
	p.blocked = func(net.IP) bool { return false }
	now := time.Date(2026, 9, 29, 14, 20, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	return p, store, inc, notif
}

func device(status string, failures int) models.Device {
	seen := time.Date(2026, 9, 29, 14, 2, 0, 0, time.UTC)
	inv := time.Date(2026, 9, 29, 14, 15, 0, 0, time.UTC) // fresh: no inventory due
	return models.Device{ID: uuid.New(), SiteID: uuid.New(), Name: "core-sw-1", Host: "10.20.0.2", Port: 161,
		Enabled: true, TimeoutMs: 3000, Retries: 1, Status: status, ConsecutiveFailures: failures,
		LastSeenAt: &seen, LastInventoryAt: &inv}
}

func TestPollerThirdFailureOpensIncidentAndNotifies(t *testing.T) {
	p, store, inc, notif := newTestPoller(&fakeSNMP{getErr: errors.New("request timeout")})
	p.PollOnce(context.Background(), device(models.DeviceStatusUp, 2))

	if len(store.saved) != 1 || store.saved[0].Status != models.DeviceStatusDown || store.saved[0].Failures != 3 ||
		store.saved[0].SeenAt != nil || store.saved[0].Detail != "request timeout" {
		t.Fatalf("saved %+v", store.saved)
	}
	if inc.opened != 1 || len(notif.sent) != 1 {
		t.Fatalf("opened %d incidents, sent %d notifications; want 1, 1", inc.opened, len(notif.sent))
	}
	m := notif.sent[0]
	if m.Status != "down" || m.DeviceID == nil || m.SiteName != "Warehouse" || m.MonitorURL != "10.20.0.2" ||
		m.IncidentID == nil || m.Message != "core-sw-1 at Warehouse (10.20.0.2) has stopped answering SNMP. Last answered 2026-09-29 14:02 UTC." {
		t.Errorf("message %+v", m)
	}
}

func TestPollerRecoveryClosesAndNotifies(t *testing.T) {
	p, store, inc, notif := newTestPoller(&fakeSNMP{})
	p.PollOnce(context.Background(), device(models.DeviceStatusDown, 5))
	if store.saved[0].Status != models.DeviceStatusUp || store.saved[0].SeenAt == nil ||
		store.saved[0].UptimeSeconds == nil || *store.saved[0].UptimeSeconds != 3600 {
		t.Fatalf("saved %+v", store.saved[0])
	}
	if inc.closed != 1 || len(notif.sent) != 1 || notif.sent[0].Status != "recovered" ||
		notif.sent[0].Message != "core-sw-1 at Warehouse is answering again after 12 minutes." {
		t.Fatalf("closed %d, sent %+v", inc.closed, notif.sent)
	}
}

// [] means "no channels": the incident still opens, nothing is sent. null
// means every enabled channel, passed through as nil.
func TestPollerNotificationChannels(t *testing.T) {
	for _, tc := range []struct {
		name     string
		channels models.StringSlice
		wantSent bool
	}{
		{"none chosen", models.StringSlice{}, false},
		{"all channels", nil, true},
		{"one channel", models.StringSlice{"ops-slack"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, inc, notif := newTestPoller(&fakeSNMP{getErr: errors.New("timeout")})
			d := device(models.DeviceStatusUp, 2)
			d.NotifyChannels = tc.channels
			p.PollOnce(context.Background(), d)
			if inc.opened != 1 {
				t.Errorf("incident not opened")
			}
			if (len(notif.sent) == 1) != tc.wantSent {
				t.Fatalf("sent %d, want sent=%v", len(notif.sent), tc.wantSent)
			}
			if tc.wantSent && (len(notif.sent[0].Channels) != len(tc.channels) || (tc.channels == nil) != (notif.sent[0].Channels == nil)) {
				t.Errorf("channels %#v, want %#v", notif.sent[0].Channels, tc.channels)
			}
		})
	}
}

func TestPollerBlockedIsAnErrorNotAnOutage(t *testing.T) {
	client := &fakeSNMP{}
	p, store, inc, notif := newTestPoller(client)
	p.blocked = func(net.IP) bool { return true }
	p.PollOnce(context.Background(), device(models.DeviceStatusUp, 0))
	if store.saved[0].Status != models.DeviceStatusError || client.gets != 0 || inc.opened != 0 || len(notif.sent) != 0 {
		t.Fatalf("saved %+v, gets %d, opened %d", store.saved, client.gets, inc.opened)
	}
	if store.saved[0].Detail == "" {
		t.Error("blocked device has no explanation")
	}
}

func TestPollerInventoryOnlyWhenDue(t *testing.T) {
	client := &fakeSNMP{}
	p, store, _, _ := newTestPoller(client)
	p.PollOnce(context.Background(), device(models.DeviceStatusUp, 0)) // inventory 5 minutes old
	if len(store.inventory) != 0 || client.walks != 0 {
		t.Fatalf("fresh inventory was re-read")
	}
	d := device(models.DeviceStatusUp, 0)
	d.LastInventoryAt = nil
	p.PollOnce(context.Background(), d)
	if len(store.inventory) != 1 || store.inventory[0].System.Name != "core-sw-1" || len(store.inventory[0].Interfaces) != 1 {
		t.Fatalf("inventory %+v", store.inventory)
	}
}

// A failed inventory is recorded but never changes up/down.
func TestPollerInventoryFailureKeepsDeviceUp(t *testing.T) {
	p, store, inc, _ := newTestPoller(&fakeSNMP{walkErr: errors.New("walk timeout")})
	d := device(models.DeviceStatusUp, 0)
	d.LastInventoryAt = nil
	p.PollOnce(context.Background(), d)
	if store.saved[0].Status != models.DeviceStatusUp || len(store.invErrors) != 1 || inc.opened != 0 {
		t.Fatalf("saved %+v inv errors %v", store.saved, store.invErrors)
	}
}

// A device already being polled is not queued again, so a slow link cannot
// pile up duplicate polls.
func TestDispatchSkipsDevicesInFlight(t *testing.T) {
	p, store, _, _ := newTestPoller(&fakeSNMP{})
	a, b := device(models.DeviceStatusUp, 0), device(models.DeviceStatusUp, 0)
	store.due = []models.Device{a, b}
	p.inFlight.Store(a.ID, true)
	jobs := make(chan models.Device, 10)
	if n := p.Dispatch(context.Background(), jobs); n != 1 {
		t.Fatalf("dispatched %d, want 1", n)
	}
	if got := <-jobs; got.ID != b.ID {
		t.Errorf("dispatched %s, want %s", got.ID, b.ID)
	}
	if n := p.Dispatch(context.Background(), jobs); n != 0 {
		t.Errorf("second dispatch queued %d again", n)
	}
}
