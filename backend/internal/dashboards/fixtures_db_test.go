package dashboards

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func newSite(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, id, name)
	return id
}

// newDevice inserts a device in siteID, with its own global v2c credential.
func newDevice(t *testing.T, db *gorm.DB, siteID uuid.UUID, name, host string) uuid.UUID {
	t.Helper()
	credID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, ?, '2c', 'x')`,
		credID, "cred-"+credID.String()[:8])
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, ?, ?)`,
		id, siteID, credID, name, host)
	return id
}

func newMonitor(t *testing.T, db *gorm.DB, owner uuid.UUID, name, url string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO monitors (id, name, type, url, owner_id) VALUES (?, ?, 'http', ?, ?)`,
		id, name, url, owner)
	return id
}

func shareSite(t *testing.T, db *gorm.DB, siteID, userID uuid.UUID, permission string) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, ?)`,
		siteID, userID, permission)
}

func shareMonitor(t *testing.T, db *gorm.DB, monitorID, userID, by uuid.UUID) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO monitor_sharing (monitor_id, shared_with_user_id, permission, shared_by_user_id)
		VALUES (?, ?, 'readonly', ?)`, monitorID, userID, by)
}

func newAgent(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO agents (id, name, agent_id, server_token) VALUES (?, ?, ?, ?)`,
		id, name, "a-"+id.String()[:8], "tok-"+id.String())
	return id
}

// newDashboard inserts a dashboard: personal when owner is set, a site's when
// site is set (exactly one must be).
func newDashboard(t *testing.T, db *gorm.DB, name string, site, owner *uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO dashboards (id, name, site_id, owner_id, version) VALUES (?, ?, ?, ?, 1)`,
		id, name, site, owner)
	return id
}

func newWidget(t *testing.T, db *gorm.DB, dashboardID uuid.UUID, typ, config string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO dashboard_widgets (id, dashboard_id, type, config, x, y, w, h)
		VALUES (?, ?, ?, ?::jsonb, 0, 0, 4, 2)`, id, dashboardID, typ, config)
	return id
}

// publish gives a dashboard a public link made by creator; returns the token.
func publish(t *testing.T, db *gorm.DB, dashboardID, creator uuid.UUID) string {
	t.Helper()
	token := "tok" + uuid.NewString()
	testdb.Exec(t, db, `INSERT INTO dashboard_public_links (dashboard_id, token, created_by) VALUES (?, ?, ?)`,
		dashboardID, token, creator)
	return token
}
