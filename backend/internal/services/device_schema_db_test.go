package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

type seeded struct {
	SiteID, CredentialID, DeviceID uuid.UUID
}

// seedDevice inserts a site, a global v2c credential and a device.
func seedDevice(t *testing.T, db *gorm.DB, siteName, host string) seeded {
	t.Helper()
	s := seeded{SiteID: uuid.New(), CredentialID: uuid.New(), DeviceID: uuid.New()}
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, s.SiteID, siteName)
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, ?, '2c', 'x')`,
		s.CredentialID, "cred-"+s.CredentialID.String()[:8])
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, ?, ?)`,
		s.DeviceID, s.SiteID, s.CredentialID, "dev-"+host, host)
	return s
}

// A site holding devices cannot be deleted; nor can one holding its own
// credential profiles. Deleting a site must never silently delete them.
func TestDBSiteWithDevicesCannotBeDeleted(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	svc := NewSiteService(db)

	if _, err := svc.Delete(context.Background(), s.SiteID); !errors.Is(err, ErrSiteNotEmpty) {
		t.Fatalf("site with a device: got %v, want ErrSiteNotEmpty", err)
	}

	other := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Branch')`, other)
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (name, site_id, version, community) VALUES ('branch-only', ?, '2c', 'x')`, other)
	if _, err := svc.Delete(context.Background(), other); !errors.Is(err, ErrSiteNotEmpty) {
		t.Fatalf("site with a site-scoped credential: got %v, want ErrSiteNotEmpty", err)
	}
}

// The same address may appear in two sites, and twice in one site on
// different ports, but not twice on the same port.
func TestDBDeviceHostUniqueness(t *testing.T) {
	db := testdb.Open(t)
	a := seedDevice(t, db, "A", "10.0.0.2")
	b := seedDevice(t, db, "B", "10.0.0.2") // same host, other site: fine

	testdb.Exec(t, db, `INSERT INTO devices (site_id, credential_id, name, host, port) VALUES (?, ?, 'nat', '10.0.0.2', 1161)`,
		a.SiteID, a.CredentialID)
	err := db.Exec(`INSERT INTO devices (site_id, credential_id, name, host, port) VALUES (?, ?, 'dup', '10.0.0.2', 161)`,
		b.SiteID, b.CredentialID).Error
	if err == nil {
		t.Fatal("duplicate site+host+port was accepted")
	}
	err = db.Exec(`INSERT INTO devices (site_id, credential_id, name, host, port) VALUES (?, ?, 'case', '10.0.0.2', 1161)`,
		a.SiteID, a.CredentialID).Error
	if err == nil {
		t.Fatal("duplicate site+host+port (second port) was accepted")
	}
}

// Deleting a device removes its interfaces.
func TestDBDeviceDeleteCascadesInterfaces(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name) VALUES (?, 1, 'ge-0/0/1')`, s.DeviceID)
	testdb.Exec(t, db, `DELETE FROM devices WHERE id = ?`, s.DeviceID)
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM device_interfaces WHERE device_id = ?`, s.DeviceID).Scan(&n).Error)
	if n != 0 {
		t.Errorf("%d interfaces left after deleting their device", n)
	}
}
