package services

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func countRows(t *testing.T, db *gorm.DB, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	testdb.Must(t, db.Raw(sql, args...).Scan(&n).Error)
	return n
}

// The site map's tables: their checks, and that deleting a device or a site
// takes its rows with it.
func TestSiteMapSchema(t *testing.T) {
	db := testdb.Open(t)
	sw := seedDevice(t, db, "HQ", "10.0.0.2")
	router := seedDeviceInSite(t, db, sw.SiteID, "10.0.0.1")
	user := testdb.NewUser(t, db, true)

	testdb.Exec(t, db, `INSERT INTO site_map_positions (site_id, device_id, x, y) VALUES (?, ?, 16, 24)`, sw.SiteID, sw.DeviceID)
	testdb.Exec(t, db, `INSERT INTO site_map_links (site_id, kind, a_device_id, a_if_index, b_device_id, b_if_index, created_by)
		VALUES (?, 'manual', ?, 51, ?, 4, ?)`, sw.SiteID, sw.DeviceID, router, user)

	if err := db.Exec(`INSERT INTO site_map_links (site_id, kind, a_device_id, b_device_id) VALUES (?, 'manual', ?, ?)`,
		sw.SiteID, sw.DeviceID, sw.DeviceID).Error; err == nil {
		t.Error("a link from a device to itself was accepted")
	}
	if err := db.Exec(`INSERT INTO site_map_links (site_id, kind, a_device_id, b_device_id) VALUES (?, 'other', ?, ?)`,
		sw.SiteID, sw.DeviceID, router).Error; err == nil {
		t.Error("kind 'other' was accepted")
	}

	testdb.Exec(t, db, `DELETE FROM devices WHERE id = ?`, sw.DeviceID)
	if n := countRows(t, db, `SELECT count(*) FROM site_map_positions WHERE device_id = ?`, sw.DeviceID); n != 0 {
		t.Errorf("positions after deleting the device = %d, want 0", n)
	}
	if n := countRows(t, db, `SELECT count(*) FROM site_map_links WHERE a_device_id = ? OR b_device_id = ?`, sw.DeviceID, sw.DeviceID); n != 0 {
		t.Errorf("links after deleting the device = %d, want 0", n)
	}

	empty := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Branch')`, empty)
	testdb.Exec(t, db, `INSERT INTO site_map_public_links (site_id, token, created_by) VALUES (?, 'tok', ?)`, empty, user)
	testdb.Exec(t, db, `DELETE FROM sites WHERE id = ?`, empty)
	if n := countRows(t, db, `SELECT count(*) FROM site_map_public_links WHERE site_id = ?`, empty); n != 0 {
		t.Errorf("public links after deleting the site = %d, want 0", n)
	}
}
