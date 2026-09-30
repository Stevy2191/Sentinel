package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func seedPort(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int, name, alias string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, alias) VALUES (?, ?, ?, ?, ?)`,
		id, deviceID, ifIndex, name, alias)
	return id
}

// Review Focus 1: a port incident is never the device's own incident.
func TestDBPortIncidentsDoNotAffectDeviceIncident(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET created_at = now() - interval '2 days' WHERE id = ?`, s.DeviceID)
	port := seedPort(t, db, s.DeviceID, 51, "0/51", "Uplink")
	svc := NewIncidentService(db)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, opened, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "errors", now.Add(-time.Hour), "errors rising"); err != nil || !opened {
		t.Fatalf("open port incident: opened %v err %v", opened, err)
	}
	dev, opened, err := svc.OpenDeviceIncident(ctx, s.DeviceID, now, "unreachable")
	if err != nil || !opened || dev.InterfaceID != nil {
		t.Fatalf("device incident should open beside a port incident: opened %v err %v inc %+v", opened, err, dev)
	}
	closed, err := svc.CloseDeviceIncident(ctx, s.DeviceID, now.Add(time.Minute), "")
	if err != nil || closed == nil || closed.ID != dev.ID {
		t.Fatalf("device close closed %+v, want the device incident", closed)
	}
	open, err := svc.OpenPortIncidents(ctx, port)
	if err != nil || len(open) != 1 {
		t.Fatalf("port incident should still be open: %v %v", open, err)
	}

	// Availability ignores port incidents: only the 1-minute device outage
	// counts against it.
	devices := NewDeviceService(db, NewSNMPCredentialService(db), svc)
	v, err := devices.Get(ctx, s.DeviceID)
	if err != nil || v.Availability30d == nil || *v.Availability30d < 99.9 {
		t.Fatalf("availability counted the port incident: %v %v", v.Availability30d, err)
	}
}

func TestDBPortIncidentOpenCloseIdempotent(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 3, "0/3", "")
	svc := NewIncidentService(db)
	ctx := context.Background()
	now := time.Now().UTC()

	a, opened, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "link_down", now, "down")
	if err != nil || !opened {
		t.Fatal(err)
	}
	b, opened, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "link_down", now.Add(time.Minute), "down")
	if err != nil || opened || b.ID != a.ID {
		t.Fatalf("second open: opened %v id %v want %v", opened, b.ID, a.ID)
	}
	if _, _, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "saturated", now, "full"); err != nil {
		t.Fatal(err)
	}
	c, err := svc.ClosePortIncident(ctx, port, "link_down", now.Add(2*time.Minute), "")
	if err != nil || c == nil || c.ID != a.ID || c.EndTime == nil || c.DurationSeconds != 120 {
		t.Fatalf("close: %+v %v", c, err)
	}
	if c, err := svc.ClosePortIncident(ctx, port, "link_down", now.Add(3*time.Minute), ""); err != nil || c != nil {
		t.Fatalf("closing nothing: %+v %v", c, err)
	}
	n, err := svc.ClosePortIncidents(ctx, port, now.Add(4*time.Minute), "The port is no longer marked important.")
	if err != nil || n != 1 {
		t.Fatalf("close all: %d %v", n, err)
	}
}

func TestDBIncidentListNamesThePort(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 51, "0/51", "Uplink")
	svc := NewIncidentService(db)
	ctx := context.Background()
	if _, _, err := svc.OpenPortIncident(ctx, s.DeviceID, port, "saturated", time.Now().UTC(), "full"); err != nil {
		t.Fatal(err)
	}
	rows, _, err := svc.ListIncidents(ctx, IncidentListOptions{Viewer: &IncidentViewer{IsAdmin: true}})
	if err != nil || len(rows) != 1 {
		t.Fatalf("%v %v", rows, err)
	}
	r := rows[0]
	if r.SubjectType != "device" || r.SubjectName != "dev-10.0.0.2 · 0/51 (Uplink)" || r.PortIfIndex == nil || *r.PortIfIndex != 51 {
		t.Errorf("row: type %q name %q port %v", r.SubjectType, r.SubjectName, r.PortIfIndex)
	}
}
