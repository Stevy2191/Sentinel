package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBDeviceConditionIncidentOpenCloseIdempotent(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	inc := NewIncidentService(db)
	start := time.Now().UTC().Add(-time.Minute)

	a, opened, err := inc.OpenDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, start, "on battery")
	if err != nil || !opened || a.Condition == nil || *a.Condition != "ups_on_battery" || a.InterfaceID != nil {
		t.Fatalf("open: %+v %v %v", a, opened, err)
	}
	b, opened, err := inc.OpenDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, start, "again")
	if err != nil || opened || b.ID != a.ID {
		t.Fatalf("second open: %+v %v %v", b, opened, err)
	}
	open, err := inc.OpenDeviceConditionIncidents(ctx, s.DeviceID)
	if err != nil || len(open) != 1 {
		t.Fatalf("open list %d %v", len(open), err)
	}
	closed, err := inc.CloseDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, time.Now().UTC(), "")
	if err != nil || closed == nil || closed.EndTime == nil {
		t.Fatalf("close: %+v %v", closed, err)
	}
	if again, err := inc.CloseDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, time.Now().UTC(), ""); err != nil || again != nil {
		t.Fatalf("second close: %+v %v", again, err)
	}
}

func TestDBDeviceConditionCheckRejectsUnknownCondition(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	err := db.Exec(`INSERT INTO incidents (device_id, condition, start_time, incident_type) VALUES (?, 'toaster', now(), 'error')`, s.DeviceID).Error
	if err == nil {
		t.Fatal("unknown device condition accepted")
	}
}

// A UPS on battery that then stops answering: the device-down incident opens
// beside the UPS one, the device coming back closes only the device-down
// one, and the UPS incident never lowers availability.
func TestDBUPSIncidentIndependentOfDeviceDown(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET created_at = now() - interval '2 days' WHERE id = ?`, s.DeviceID)
	inc := NewIncidentService(db)
	now := time.Now().UTC()

	if _, _, err := inc.OpenDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, now.Add(-time.Hour), "on battery"); err != nil {
		t.Fatal(err)
	}
	down, opened, err := inc.OpenDeviceIncident(ctx, s.DeviceID, now.Add(-30*time.Minute), "unreachable")
	if err != nil || !opened || down.Condition != nil {
		t.Fatalf("device-down open blocked by UPS incident: %+v %v %v", down, opened, err)
	}
	closed, err := inc.CloseDeviceIncident(ctx, s.DeviceID, now, "")
	if err != nil || closed == nil || closed.ID != down.ID {
		t.Fatalf("closed the wrong incident: %+v %v", closed, err)
	}
	open, _ := inc.OpenDeviceConditionIncidents(ctx, s.DeviceID)
	if len(open) != 1 || *open[0].Condition != models.UPSConditionOnBattery {
		t.Fatalf("UPS incident closed by device recovery: %+v", open)
	}

	// Over the device's 2-day life, the 30-minute outage alone is 98.96 %;
	// counting the hour-long UPS incident too would give 96.9 %.
	devices := NewDeviceService(db, NewSNMPCredentialService(db), inc)
	d, err := devices.Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	if d.Availability30d == nil || *d.Availability30d < 98.9 {
		t.Errorf("availability %v: the UPS incident was counted", d.Availability30d)
	}
}

// Pausing a UPS closes its UPS incidents: a paused device is not polled, so
// nothing else would ever close them.
func TestDBPauseClosesUPSIncidents(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET status = 'up' WHERE id = ?`, s.DeviceID)
	inc := NewIncidentService(db)
	if _, _, err := inc.OpenDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, time.Now().UTC().Add(-time.Hour), "x"); err != nil {
		t.Fatal(err)
	}
	off := false
	_, _, err := deviceSvc(db).Update(ctx, s.DeviceID, models.DeviceInput{SiteID: s.SiteID, CredentialID: s.CredentialID, Host: "10.0.0.9", Enabled: &off})
	testdb.Must(t, err)
	open, err := inc.OpenDeviceConditionIncidents(ctx, s.DeviceID)
	if err != nil || len(open) != 0 {
		t.Fatalf("UPS incidents still open after pause: %d %v", len(open), err)
	}
	var notes string
	testdb.Must(t, db.Raw(`SELECT resolution_notes FROM incidents WHERE device_id = ? AND condition IS NOT NULL`, s.DeviceID).Scan(&notes).Error)
	if notes != "Monitoring was paused." {
		t.Errorf("note %q", notes)
	}
}
