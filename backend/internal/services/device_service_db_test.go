package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func deviceSvc(db *gorm.DB) *DeviceService {
	return NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
}

func TestDBDeviceListFollowsSiteSharing(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	alice := testdb.NewUser(t, db, false)
	shared := seedDevice(t, db, "Shared", "10.0.0.2")
	seedDevice(t, db, "Private", "10.0.0.3")
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, 'readonly')`, shared.SiteID, alice)
	svc := deviceSvc(db)

	all, err := svc.List(ctx, uuid.Nil, true, DeviceFilter{})
	testdb.Must(t, err)
	mine, err := svc.List(ctx, alice, false, DeviceFilter{})
	testdb.Must(t, err)
	if len(all) != 2 || len(mine) != 1 || mine[0].ID != shared.DeviceID || mine[0].SiteName != "Shared" {
		t.Fatalf("admin %d, alice %+v", len(all), mine)
	}
	down, err := svc.List(ctx, uuid.Nil, true, DeviceFilter{Status: models.DeviceStatusDown})
	testdb.Must(t, err)
	if len(down) != 0 {
		t.Errorf("status filter: %d down, want 0", len(down))
	}
}

func TestDBDeviceCreateChecksCredentialScope(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	hq := seedDevice(t, db, "HQ", "10.0.0.2")
	branch := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Branch')`, branch)
	branchCred := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, site_id, version, community) VALUES (?, 'branch', ?, '2c', 'x')`, branchCred, branch)
	svc := deviceSvc(db)

	if _, err := svc.Create(ctx, models.DeviceInput{SiteID: hq.SiteID, CredentialID: branchCred, Host: "10.0.0.9"}, uuid.Nil); !errors.Is(err, ErrCredentialNotUsable) {
		t.Errorf("branch credential at HQ: %v, want ErrCredentialNotUsable", err)
	}
	d, err := svc.Create(ctx, models.DeviceInput{SiteID: hq.SiteID, CredentialID: hq.CredentialID, Host: "10.0.0.9"}, uuid.Nil)
	testdb.Must(t, err)
	if d.Name != "10.0.0.9" || d.Status != models.DeviceStatusPending || d.Port != 161 {
		t.Errorf("created %+v", d)
	}
	if _, err := svc.Create(ctx, models.DeviceInput{SiteID: hq.SiteID, CredentialID: hq.CredentialID, Host: "10.0.0.9"}, uuid.Nil); !errors.Is(err, ErrDeviceHostTaken) {
		t.Errorf("duplicate host: %v, want ErrDeviceHostTaken", err)
	}
}

func TestDBSaveInventory(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET name = host WHERE id = ?`, s.DeviceID) // unnamed
	svc := deviceSvc(db)
	at := time.Now().UTC()

	inv := snmp.Inventory{
		System: snmp.System{Name: "core-sw-1", Descr: "EdgeSwitch", ObjectID: "1.3.6.1.4.1.4413", Location: "MDF"},
		Vendor: "Ubiquiti (EdgeSwitch)", Model: "ES-48-500W", Serial: "F09F",
		Interfaces: []snmp.Interface{{Index: 1, Name: "0/1", AdminStatus: "up", OperStatus: "up", SpeedBps: 1e9},
			{Index: 2, Name: "0/2", AdminStatus: "up", OperStatus: "down"}},
	}
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, inv, at))
	inv.Interfaces = inv.Interfaces[:1] // port 2 disappears (module removed)
	inv.Interfaces[0].Alias = "Uplink"
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, inv, at))

	d, err := svc.Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	if d.Name != "core-sw-1" || d.Model != "ES-48-500W" || d.Vendor != "Ubiquiti (EdgeSwitch)" || d.SysLocation != "MDF" || d.LastInventoryAt == nil {
		t.Errorf("device after inventory: %+v", d.Device)
	}
	present, err := svc.Interfaces(ctx, s.DeviceID, false)
	testdb.Must(t, err)
	all, err := svc.Interfaces(ctx, s.DeviceID, true)
	testdb.Must(t, err)
	if len(present) != 1 || present[0].Alias != "Uplink" || len(all) != 2 {
		t.Errorf("interfaces: present %+v, all %d", present, len(all))
	}

	// A name the user typed survives inventory.
	testdb.Exec(t, db, `UPDATE devices SET name = 'Core switch' WHERE id = ?`, s.DeviceID)
	testdb.Must(t, svc.SaveInventory(ctx, s.DeviceID, inv, at))
	d, _ = svc.Get(ctx, s.DeviceID)
	if d.Name != "Core switch" {
		t.Errorf("user's name overwritten: %q", d.Name)
	}
}

func TestDBDueDevices(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	never := seedDevice(t, db, "A", "10.0.0.1")
	due := seedDevice(t, db, "B", "10.0.0.2")
	fresh := seedDevice(t, db, "C", "10.0.0.3")
	off := seedDevice(t, db, "D", "10.0.0.4")
	now := time.Now().UTC()
	testdb.Exec(t, db, `UPDATE devices SET last_polled_at = ? WHERE id = ?`, now.Add(-61*time.Second), due.DeviceID)
	testdb.Exec(t, db, `UPDATE devices SET last_polled_at = ? WHERE id = ?`, now.Add(-30*time.Second), fresh.DeviceID)
	testdb.Exec(t, db, `UPDATE devices SET enabled = false WHERE id = ?`, off.DeviceID)

	got, err := deviceSvc(db).DueDevices(ctx, now, 100)
	testdb.Must(t, err)
	ids := map[uuid.UUID]bool{}
	for _, d := range got {
		ids[d.ID] = true
	}
	if !ids[never.DeviceID] || !ids[due.DeviceID] || ids[fresh.DeviceID] || ids[off.DeviceID] || len(ids) != 2 {
		t.Errorf("due = %v", ids)
	}
}

// Pausing a device that is down closes its incident, noting why.
func TestDBPauseClosesIncident(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET status = 'down', consecutive_failures = 4 WHERE id = ?`, s.DeviceID)
	svc := deviceSvc(db)
	_, _, err := NewIncidentService(db).OpenDeviceIncident(ctx, s.DeviceID, time.Now().UTC().Add(-time.Hour), "timeout")
	testdb.Must(t, err)

	off := false
	_, after, err := svc.Update(ctx, s.DeviceID, models.DeviceInput{SiteID: s.SiteID, CredentialID: s.CredentialID, Host: "10.0.0.2", Enabled: &off})
	testdb.Must(t, err)
	if after.Status != models.DeviceStatusPaused || after.ConsecutiveFailures != 0 {
		t.Errorf("after pause: %+v", after)
	}
	var notes string
	testdb.Must(t, db.Raw(`SELECT resolution_notes FROM incidents WHERE device_id = ? AND end_time IS NOT NULL`, s.DeviceID).Scan(&notes).Error)
	if notes != "Monitoring was paused." {
		t.Errorf("incident not closed with a note: %q", notes)
	}

	on := true
	_, after, err = svc.Update(ctx, s.DeviceID, models.DeviceInput{SiteID: s.SiteID, CredentialID: s.CredentialID, Host: "10.0.0.2", Enabled: &on})
	testdb.Must(t, err)
	if after.Status != models.DeviceStatusPending || after.LastPolledAt != nil {
		t.Errorf("after resume: %+v", after)
	}
}

// Availability is measured over the time the device has existed, capped at
// 30 days: a device 10 days old with one day of outage is at 90%.
func TestDBDeviceAvailability(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	now := time.Now().UTC()
	testdb.Exec(t, db, `UPDATE devices SET created_at = ? WHERE id = ?`, now.Add(-10*24*time.Hour), s.DeviceID)
	start, end := now.Add(-5*24*time.Hour), now.Add(-4*24*time.Hour)
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, start_time, end_time, severity) VALUES (?, ?, ?, 'high')`, s.DeviceID, start, end)

	d, err := deviceSvc(db).Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	if d.Availability30d == nil || *d.Availability30d < 89.9 || *d.Availability30d > 90.1 {
		t.Errorf("availability = %v, want ~90", d.Availability30d)
	}
}

// SaveReachability on a disabled device returns ErrDeviceNotEnabled: its
// WHERE clause requires enabled = true, so an UPDATE that touches 0 rows
// means the device was disabled while the poll was in flight.
func TestDBSaveReachabilityOnDisabledDevice(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `UPDATE devices SET enabled = false WHERE id = ?`, s.DeviceID)
	svc := deviceSvc(db)

	now := time.Now().UTC()
	err := svc.SaveReachability(ctx, s.DeviceID, ReachabilityUpdate{Status: models.DeviceStatusUp, PolledAt: now})
	if !errors.Is(err, ErrDeviceNotEnabled) {
		t.Errorf("SaveReachability on disabled device: %v, want ErrDeviceNotEnabled", err)
	}
}

// DeviceCredential wraps ErrCredentialUnusable when the profile is missing,
// so the poller can tell "not configured" apart from "database hiccup".
func TestDBDeviceCredentialUnusableForMissingProfile(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := deviceSvc(db)

	_, err := svc.DeviceCredential(ctx, uuid.New())
	if !errors.Is(err, ErrCredentialUnusable) {
		t.Errorf("DeviceCredential for nonexistent id: %v, want errors.Is ErrCredentialUnusable", err)
	}
}

// Changing an unnamed device's host must not leave the old address frozen
// as its name forever: a device is "unnamed" when its stored name still
// equals its (old) host, and auto-naming must keep following the device
// onto its new host so a later inventory can still rename it from sysName.
func TestDBUpdateHostKeepsAutoNamingCurrent(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := deviceSvc(db)
	hq := seedDevice(t, db, "HQ", "10.0.0.2")

	d, err := svc.Create(ctx, models.DeviceInput{SiteID: hq.SiteID, CredentialID: hq.CredentialID, Host: "10.0.0.20"}, uuid.Nil)
	testdb.Must(t, err)
	if d.Name != "10.0.0.20" {
		t.Fatalf("setup: name = %q, want the host", d.Name)
	}

	_, after, err := svc.Update(ctx, d.ID, models.DeviceInput{SiteID: hq.SiteID, CredentialID: hq.CredentialID, Host: "10.0.0.21"})
	testdb.Must(t, err)
	if after.Name != "10.0.0.21" {
		t.Fatalf("after host change: name = %q, want the new host", after.Name)
	}

	testdb.Must(t, svc.SaveInventory(ctx, d.ID, snmp.Inventory{System: snmp.System{Name: "core-sw-9"}}, time.Now().UTC()))
	v, err := svc.Get(ctx, d.ID)
	testdb.Must(t, err)
	if v.Name != "core-sw-9" {
		t.Errorf("after inventory: name = %q, want the sysName", v.Name)
	}
}

// A name the user typed is never overwritten, including by a host change.
func TestDBUpdateHostKeepsUserTypedName(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := deviceSvc(db)
	hq := seedDevice(t, db, "HQ", "10.0.0.2")

	d, err := svc.Create(ctx, models.DeviceInput{SiteID: hq.SiteID, CredentialID: hq.CredentialID, Host: "10.0.0.30", Name: "Core switch"}, uuid.Nil)
	testdb.Must(t, err)

	_, after, err := svc.Update(ctx, d.ID, models.DeviceInput{SiteID: hq.SiteID, CredentialID: hq.CredentialID, Host: "10.0.0.31"})
	testdb.Must(t, err)
	if after.Name != "Core switch" {
		t.Errorf("user-typed name lost on host change: %q", after.Name)
	}
}

// Pausing closes the device's open incident. If the device's own update
// fails (here: a host+port collision with another device in the same
// site), the whole pause must fail atomically: the incident stays open and
// the device stays exactly as it was.
func TestDBPauseFailureKeepsIncidentOpen(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	blocker := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host, port) VALUES (?, ?, ?, 'blocker', '10.0.0.5', 161)`,
		blocker, s.SiteID, s.CredentialID)
	testdb.Exec(t, db, `UPDATE devices SET status = 'down', consecutive_failures = 4 WHERE id = ?`, s.DeviceID)
	svc := deviceSvc(db)
	_, _, err := NewIncidentService(db).OpenDeviceIncident(ctx, s.DeviceID, time.Now().UTC().Add(-time.Hour), "timeout")
	testdb.Must(t, err)

	off := false
	_, _, err = svc.Update(ctx, s.DeviceID, models.DeviceInput{
		SiteID: s.SiteID, CredentialID: s.CredentialID, Host: "10.0.0.5", Port: 161, Enabled: &off,
	})
	if !errors.Is(err, ErrDeviceHostTaken) {
		t.Fatalf("colliding pause: err = %v, want ErrDeviceHostTaken", err)
	}

	v, err := svc.Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	if v.Status != models.DeviceStatusDown || !v.Enabled {
		t.Errorf("device after failed pause: status=%q enabled=%v, want down/true (unchanged)", v.Status, v.Enabled)
	}
	var openCount int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM incidents WHERE device_id = ? AND end_time IS NULL`, s.DeviceID).Scan(&openCount).Error)
	if openCount != 1 {
		t.Errorf("open incidents after failed pause: %d, want 1 (unchanged)", openCount)
	}
}
