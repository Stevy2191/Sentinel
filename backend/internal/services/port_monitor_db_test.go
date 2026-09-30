package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// The real store, metrics and incidents behind a scripted switch: samples,
// port state, events, an incident on the important port, and the stats run
// timing all land in the database.
func TestDBPortMonitorEndToEnd(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	ctx := context.Background()
	settings := NewSettingsService(db)
	incidents := NewIncidentService(db)
	devices := NewDeviceService(db, NewSNMPCredentialService(db), incidents)
	testdb.Must(t, devices.SaveInventory(ctx, s.DeviceID, switchInventory(true), time.Now()))
	testdb.Exec(t, db, `UPDATE device_interfaces SET important = true WHERE device_id = ? AND if_index = 1`, s.DeviceID)

	metrics := NewMetricsStore(db)
	ports := NewPortService(db, metrics, incidents, settings)
	fake := newPMSNMP()
	notif := &fakeNotifier{}
	m := NewPortMonitor(ports, metrics, incidents, notif, fake, settings)
	clock := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Minute)
	m.now = func() time.Time { return clock }
	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)
	d.PollInterval = 60
	target := snmp.Target{Credential: snmp.Credential{Version: "2c"}}

	// Inventory stored last_change_seconds = 0; keep it there while up so the
	// first poll is not read as a bounce between polls.
	fake.port(1, 0, 0, true, 0)
	m.PollStats(ctx, d, target, 100)
	clock = clock.Add(time.Minute)
	fake.port(1, 750_000_000, 1_000, true, 0)
	m.PollStats(ctx, d, target, 160)
	fake.port(1, 750_000_000, 1_000, false, 9000)
	for i := 0; i < 4; i++ {
		clock = clock.Add(time.Minute)
		m.PollStats(ctx, d, target, int64(220+60*i))
	}

	var series int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.series WHERE device_id = ? AND metric = 'if_in_bps' AND instance = '1'`, s.DeviceID).Scan(&series).Error)
	if series != 1 {
		t.Errorf("if_in_bps series: %d", series)
	}
	var port models.DeviceInterface
	testdb.Must(t, db.First(&port, "device_id = ? AND if_index = 1", s.DeviceID).Error)
	if port.OperStatus != "down" || port.OperChangedAt == nil {
		t.Errorf("port state %q changed %v", port.OperStatus, port.OperChangedAt)
	}
	var events int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM port_events WHERE interface_id = ? AND kind = 'link_down'`, port.ID).Scan(&events).Error)
	if events != 1 {
		t.Errorf("link_down events: %d", events)
	}
	open, err := incidents.OpenPortIncidents(ctx, port.ID)
	if err != nil || len(open) != 1 || open[0].Condition == nil || *open[0].Condition != "link_down" {
		t.Errorf("port incidents: %+v %v", open, err)
	}
	var ms *int
	testdb.Must(t, db.Raw(`SELECT last_stats_duration_ms FROM devices WHERE id = ?`, s.DeviceID).Scan(&ms).Error)
	if ms == nil {
		t.Error("stats duration not recorded")
	}
}
