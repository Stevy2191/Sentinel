package netreport

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func newSite(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, ?)`, id, name)
	return id
}

// newDevice inserts a device named name in siteID, with its own v2c credential.
func newDevice(t *testing.T, db *gorm.DB, siteID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	credID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, ?, '2c', 'x')`,
		credID, "cred-"+credID.String()[:8])
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, ?, ?)`,
		id, siteID, credID, name, name+".example.test")
	return id
}

func shareSite(t *testing.T, db *gorm.DB, siteID, userID uuid.UUID, permission string) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO site_sharing (site_id, shared_with_user_id, permission) VALUES (?, ?, ?)`,
		siteID, userID, permission)
}

// newPort inserts a present, collected, physical (ethernetCsmacd) port with
// the default role, access. speedBps 0 is an unknown speed.
func newPort(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int, name string, speedBps int64) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, if_type, speed_bps, present, collect_default)
		VALUES (?, ?, ?, ?, 6, ?, true, true)`, id, deviceID, ifIndex, name, speedBps)
	return id
}

// newVirtualPort inserts a present interface that is not physical (ifType 53
// is a VLAN interface, 161 a port-channel) and that the user chose to collect.
func newVirtualPort(t *testing.T, db *gorm.DB, deviceID uuid.UUID, ifIndex int, name string, ifType int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name, if_type, present, collect_default, collect)
		VALUES (?, ?, ?, ?, ?, true, false, true)`, id, deviceID, ifIndex, name, ifType)
	return id
}

func setRole(t *testing.T, db *gorm.DB, portID uuid.UUID, role string) {
	t.Helper()
	testdb.Exec(t, db, `UPDATE device_interfaces SET role = ? WHERE id = ?`, role, portID)
}

// newSeries inserts a series; portID links a port metric's series to its port.
func newSeries(t *testing.T, db *gorm.DB, deviceID uuid.UUID, metric, instance string, portID *uuid.UUID, label string) int64 {
	t.Helper()
	var id int64
	testdb.Must(t, db.Raw(`INSERT INTO metrics.series (device_id, metric, instance, interface_id, label)
		VALUES (?, ?, ?, ?, ?) RETURNING id`, deviceID, metric, instance, portID, label).Scan(&id).Error)
	return id
}

// portSeries inserts a port metric's series, as the port poll writes it.
func portSeries(t *testing.T, db *gorm.DB, deviceID, portID uuid.UUID, ifIndex int, metric string) int64 {
	t.Helper()
	return newSeries(t, db, deviceID, metric, strconv.Itoa(ifIndex), &portID, "")
}

// steady writes v one minute into each of n 5-minute buckets from start.
func steady(t *testing.T, db *gorm.DB, series int64, start time.Time, n int, v float64) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT ?::timestamptz + make_interval(mins => 5 * g + 1), ?, ? FROM generate_series(0, ? - 1) g`,
		start, series, v, n)
}

// sampleAt writes one raw sample.
func sampleAt(t *testing.T, db *gorm.DB, series int64, at time.Time, v float64) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value) VALUES (?, ?, ?)`, at, series, v)
}

// refresh materializes the rollups. They are real-time anyway, but reports
// on past periods read the materialized part, so the tests do too.
func refresh(t *testing.T, db *gorm.DB) {
	t.Helper()
	testdb.Exec(t, db, `CALL refresh_continuous_aggregate('metrics.samples_5m', NULL, NULL)`)
	testdb.Exec(t, db, `CALL refresh_continuous_aggregate('metrics.samples_1h', NULL, NULL)`)
}

func newBuilder(db *gorm.DB) *Builder { return NewBuilder(db, services.NewMetricsStore(db)) }

// newProfileMetric adds a metric to the profile named profile, creating the
// profile on first use.
func newProfileMetric(t *testing.T, db *gorm.DB, profile string, builtin bool, key, name, units, kind string, position int) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metric_profiles (name, builtin) VALUES (?, ?) ON CONFLICT (name) DO NOTHING`, profile, builtin)
	testdb.Exec(t, db, `INSERT INTO profile_metrics (profile_id, name, key, source, kind, units, oid, position)
		SELECT id, ?, ?, 'scalar', ?, ?, '1.3.6.1.4.1.99999.1', ? FROM metric_profiles WHERE name = ?`,
		name, key, kind, units, position, profile)
}
