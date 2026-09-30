package services

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func newMonitor(t *testing.T, db *gorm.DB, owner uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO monitors (id, name, type, url, owner_id) VALUES (?, ?, 'http', 'https://example.test', ?)`,
		id, name, owner)
	return id
}

func insertIncident(t *testing.T, db *gorm.DB, monitorID, deviceID *uuid.UUID, start time.Time, end *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO incidents (id, monitor_id, device_id, start_time, end_time, severity) VALUES (?, ?, ?, ?, ?, 'high')`,
		id, monitorID, deviceID, start, end)
	return id
}

// A switch outage must never count against a monitor. Every monitor query
// filters monitor_id = ?, so a device incident (monitor_id NULL) is invisible
// to them; this pins it against real SQL.
func TestDBDeviceIncidentsInvisibleToMonitorQueries(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	owner := testdb.NewUser(t, db, true)
	mon := newMonitor(t, db, owner, "web")
	dev := seedDevice(t, db, "HQ", "10.0.0.2")

	now := time.Now().UTC()
	start := now.Add(-2 * time.Hour)
	// The device is down for the whole window and still open; the monitor had
	// one closed 10-minute incident.
	insertIncident(t, db, nil, &dev.DeviceID, start.Add(-time.Hour), nil)
	end := start.Add(10 * time.Minute)
	insertIncident(t, db, &mon, nil, start, &end)

	svc := NewIncidentService(db)
	down, err := svc.GetIncidentDuration(ctx, mon, start.Add(-time.Minute), now)
	testdb.Must(t, err)
	if down != 10*time.Minute {
		t.Errorf("monitor downtime = %s, want 10m (device incident leaked in?)", down)
	}
	n, err := svc.GetIncidentCount(ctx, mon, start.Add(-3*time.Hour), now)
	testdb.Must(t, err)
	if n != 1 {
		t.Errorf("monitor incident count = %d, want 1", n)
	}
	active, err := svc.GetActiveIncident(ctx, mon)
	testdb.Must(t, err)
	if active != nil {
		t.Errorf("monitor has an active incident %s; only the device does", active.ID)
	}
	list, err := svc.GetOverlappingIncidents(ctx, mon, start.Add(-3*time.Hour), now)
	testdb.Must(t, err)
	if len(list) != 1 {
		t.Errorf("overlapping monitor incidents = %d, want 1", len(list))
	}
}

// Exactly one subject, enforced by the database for incidents and
// notifications alike.
func TestDBIncidentSubjectCheck(t *testing.T) {
	db := testdb.Open(t)
	owner := testdb.NewUser(t, db, true)
	mon := newMonitor(t, db, owner, "web")
	dev := seedDevice(t, db, "HQ", "10.0.0.2")

	if err := db.Exec(`INSERT INTO incidents (monitor_id, device_id, start_time) VALUES (?, ?, now())`, mon, dev.DeviceID).Error; err == nil {
		t.Error("incident with both subjects was accepted")
	}
	if err := db.Exec(`INSERT INTO incidents (start_time) VALUES (now())`).Error; err == nil {
		t.Error("incident with no subject was accepted")
	}
	if err := db.Exec(`INSERT INTO notifications (monitor_id, device_id, channel, status) VALUES (?, ?, 'webhook', 'sent')`, mon, dev.DeviceID).Error; err == nil {
		t.Error("notification with two subjects was accepted")
	}
	testdb.Exec(t, db, `INSERT INTO notifications (device_id, channel, status) VALUES (?, 'webhook', 'sent')`, dev.DeviceID)
}

// Deleting a device deletes its incidents and notifications, as deleting a
// monitor does.
func TestDBDeviceDeleteCascadesIncidents(t *testing.T) {
	db := testdb.Open(t)
	dev := seedDevice(t, db, "HQ", "10.0.0.2")
	inc := insertIncident(t, db, nil, &dev.DeviceID, time.Now().UTC(), nil)
	testdb.Exec(t, db, `INSERT INTO notifications (device_id, incident_id, channel, status) VALUES (?, ?, 'webhook', 'sent')`, dev.DeviceID, inc)
	testdb.Exec(t, db, `DELETE FROM devices WHERE id = ?`, dev.DeviceID)
	var n int64
	testdb.Must(t, db.Raw(`SELECT (SELECT count(*) FROM incidents WHERE device_id = ?) + (SELECT count(*) FROM notifications WHERE device_id = ?)`,
		dev.DeviceID, dev.DeviceID).Scan(&n).Error)
	if n != 0 {
		t.Errorf("%d incident/notification rows survived their device", n)
	}
}

// Opening is idempotent while an incident is open; closing stamps the end and
// a note; closing when nothing is open is a no-op.
func TestDBOpenCloseDeviceIncident(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	dev := seedDevice(t, db, "HQ", "10.0.0.2")
	svc := NewIncidentService(db)

	start := time.Now().UTC().Add(-5 * time.Minute)
	first, opened, err := svc.OpenDeviceIncident(ctx, dev.DeviceID, start, "no SNMP response")
	testdb.Must(t, err)
	if !opened || first.DeviceID == nil || *first.DeviceID != dev.DeviceID || first.MonitorID != nil {
		t.Fatalf("first open: opened=%v incident=%+v", opened, first)
	}
	again, opened, err := svc.OpenDeviceIncident(ctx, dev.DeviceID, time.Now().UTC(), "still down")
	testdb.Must(t, err)
	if opened || again.ID != first.ID {
		t.Errorf("second open created a new incident (opened=%v, %s vs %s)", opened, again.ID, first.ID)
	}

	closed, err := svc.CloseDeviceIncident(ctx, dev.DeviceID, time.Now().UTC(), "Monitoring was paused.")
	testdb.Must(t, err)
	if closed == nil || closed.EndTime == nil || closed.DurationSeconds < 290 || closed.ResolutionNotes != "Monitoring was paused." {
		t.Errorf("close: %+v", closed)
	}
	none, err := svc.CloseDeviceIncident(ctx, dev.DeviceID, time.Now().UTC(), "")
	testdb.Must(t, err)
	if none != nil {
		t.Errorf("closing with nothing open returned %+v", none)
	}
}

// The "is one already open?" lookup behind OpenDeviceIncident must not use a
// query that logs GORM's default "record not found" error when the answer
// is legitimately no — that's the normal case every time a device first
// goes down, not damage. A second *gorm.DB, sharing the same connection but
// with a real (non-discarding) logger writing to a buffer, catches it.
func TestDBOpenDeviceIncidentDoesNotLogRecordNotFound(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	dev := seedDevice(t, db, "HQ", "10.0.0.2")

	sqlDB, err := db.DB()
	testdb.Must(t, err)
	var buf bytes.Buffer
	verbose, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: logger.New(log.New(&buf, "", 0), logger.Config{LogLevel: logger.Warn}),
	})
	testdb.Must(t, err)

	svc := NewIncidentService(verbose)
	_, opened, err := svc.OpenDeviceIncident(ctx, dev.DeviceID, time.Now().UTC(), "no SNMP response")
	testdb.Must(t, err)
	if !opened {
		t.Fatal("expected the first open (nothing open yet) to succeed")
	}
	if strings.Contains(buf.String(), "record not found") {
		t.Errorf("opening the first device incident logged GORM noise:\n%s", buf.String())
	}
}

// Members see exactly their own: monitor incidents by monitor ownership or
// sharing, device incidents by site sharing. Admins see everything.
func TestDBIncidentListAccess(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	alice := testdb.NewUser(t, db, false)
	bob := testdb.NewUser(t, db, false)

	aliceMon := newMonitor(t, db, alice, "alice-web")
	adminMon := newMonitor(t, db, admin, "admin-web")
	shared := seedDevice(t, db, "Shared", "10.0.0.2")
	private := seedDevice(t, db, "Private", "10.0.0.3")
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, shared.SiteID, alice)

	now := time.Now().UTC()
	want := map[string]uuid.UUID{
		"aliceMon": insertIncident(t, db, &aliceMon, nil, now, nil),
		"adminMon": insertIncident(t, db, &adminMon, nil, now, nil),
		"shared":   insertIncident(t, db, nil, &shared.DeviceID, now, nil),
		"private":  insertIncident(t, db, nil, &private.DeviceID, now, nil),
	}
	svc := NewIncidentService(db)
	list := func(v IncidentViewer, mod func(*IncidentListOptions)) map[uuid.UUID]IncidentWithMonitor {
		opts := IncidentListOptions{Viewer: &v}
		if mod != nil {
			mod(&opts)
		}
		rows, _, err := svc.ListIncidents(ctx, opts)
		testdb.Must(t, err)
		out := map[uuid.UUID]IncidentWithMonitor{}
		for _, r := range rows {
			out[r.ID] = r
		}
		return out
	}

	if got := list(IncidentViewer{UserID: admin, IsAdmin: true}, nil); len(got) != 4 {
		t.Errorf("admin sees %d incidents, want 4", len(got))
	}
	a := list(IncidentViewer{UserID: alice}, nil)
	if len(a) != 2 || a[want["aliceMon"]].ID == uuid.Nil || a[want["shared"]].ID == uuid.Nil {
		t.Errorf("alice sees %v, want exactly her monitor's and the shared site's", keys(a))
	}
	if row := a[want["shared"]]; row.SubjectType != "device" || row.SubjectName != "dev-10.0.0.2" ||
		row.SubjectTarget != "10.0.0.2" || row.SiteName != "Shared" || row.MonitorName != "dev-10.0.0.2" {
		t.Errorf("device row fields: %+v", row)
	}
	if row := a[want["aliceMon"]]; row.SubjectType != "monitor" || row.SubjectName != "alice-web" || row.SiteID != nil {
		t.Errorf("monitor row fields: %+v", row)
	}
	if got := list(IncidentViewer{UserID: alice}, func(o *IncidentListOptions) { o.DeviceID = &private.DeviceID }); len(got) != 0 {
		t.Errorf("alice filtered to a private device sees %d incidents, want 0", len(got))
	}
	if got := list(IncidentViewer{UserID: alice}, func(o *IncidentListOptions) { o.Subject = "device" }); len(got) != 1 {
		t.Errorf("alice subject=device sees %d, want 1", len(got))
	}
	if got := list(IncidentViewer{UserID: bob}, nil); len(got) != 0 {
		t.Errorf("bob sees %d incidents, want 0", len(got))
	}

	row, err := svc.GetIncidentByID(ctx, want["private"])
	testdb.Must(t, err)
	if row.SubjectType != "device" || row.SiteID == nil || *row.SiteID != private.SiteID {
		t.Errorf("GetIncidentByID device row: %+v", row)
	}
}

func keys(m map[uuid.UUID]IncidentWithMonitor) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
