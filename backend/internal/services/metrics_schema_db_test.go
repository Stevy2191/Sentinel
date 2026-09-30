package services

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// The metrics hypertable exists, is compressed, and carries exactly the
// policies the spec names: one retention (365 days), one compression, and a
// refresh policy per continuous aggregate.
func TestDBMetricsSchema(t *testing.T) {
	db := testdb.Open(t)

	var compressed int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM timescaledb_information.hypertables
		WHERE hypertable_schema = 'metrics' AND hypertable_name = 'samples' AND compression_enabled`).Scan(&compressed).Error)
	if compressed != 1 {
		t.Fatalf("metrics.samples compressed hypertable: found %d", compressed)
	}

	var views []string
	testdb.Must(t, db.Raw(`SELECT view_name FROM timescaledb_information.continuous_aggregates
		WHERE view_schema = 'metrics' ORDER BY view_name`).Scan(&views).Error)
	if len(views) != 2 || views[0] != "samples_1h" || views[1] != "samples_5m" {
		t.Fatalf("continuous aggregates: %v", views)
	}

	var jobs []struct {
		ProcName string
		N        int
	}
	testdb.Must(t, db.Raw(`SELECT proc_name, count(*) AS n FROM timescaledb_information.jobs
		WHERE proc_name IN ('policy_retention', 'policy_compression', 'policy_refresh_continuous_aggregate')
		GROUP BY proc_name ORDER BY proc_name`).Scan(&jobs).Error)
	want := map[string]int{"policy_compression": 1, "policy_refresh_continuous_aggregate": 2, "policy_retention": 1}
	if len(jobs) != len(want) {
		t.Fatalf("policy jobs: %+v", jobs)
	}
	for _, j := range jobs {
		if want[j.ProcName] != j.N {
			t.Errorf("%s: %d jobs, want %d", j.ProcName, j.N, want[j.ProcName])
		}
	}

	var dropAfter string
	testdb.Must(t, db.Raw(`SELECT config->>'drop_after' FROM timescaledb_information.jobs
		WHERE proc_name = 'policy_retention'`).Scan(&dropAfter).Error)
	if dropAfter != "365 days" {
		t.Errorf("retention drop_after = %q, want 365 days", dropAfter)
	}
}

// A config restore drops and recreates public tables without CASCADE. A
// foreign key from the metrics schema into public would block that drop and
// fail the restore halfway, so there must be none.
func TestDBMetricsHaveNoForeignKeys(t *testing.T) {
	db := testdb.Open(t)
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM pg_constraint
		WHERE contype = 'f' AND connamespace = 'metrics'::regnamespace`).Scan(&n).Error)
	if n != 0 {
		t.Fatalf("metrics schema has %d foreign keys; it must have none", n)
	}
}

// Port incidents carry an interface and a condition together, only on a
// device incident, and at most one is open per port per condition.
func TestDBPortIncidentConstraints(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	ifID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO device_interfaces (id, device_id, if_index, name) VALUES (?, ?, 51, '0/51')`, ifID, s.DeviceID)
	start := time.Now().Add(-time.Hour)

	if err := db.Exec(`INSERT INTO incidents (device_id, interface_id, start_time) VALUES (?, ?, ?)`,
		s.DeviceID, ifID, start).Error; err == nil {
		t.Error("port incident without a condition was accepted")
	}
	if err := db.Exec(`INSERT INTO incidents (device_id, condition, start_time) VALUES (?, 'errors', ?)`,
		s.DeviceID, start).Error; err == nil {
		t.Error("condition without an interface was accepted")
	}
	if err := db.Exec(`INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'toaster', ?)`,
		s.DeviceID, ifID, start).Error; err == nil {
		t.Error("unknown condition was accepted")
	}
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'errors', ?)`,
		s.DeviceID, ifID, start)
	if err := db.Exec(`INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'errors', ?)`,
		s.DeviceID, ifID, start).Error; err == nil {
		t.Error("second open incident for the same port and condition was accepted")
	}
	// A different condition on the same port is its own incident.
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'saturated', ?)`,
		s.DeviceID, ifID, start)
	// Once closed, the same condition may open again.
	testdb.Exec(t, db, `UPDATE incidents SET end_time = now() WHERE interface_id = ? AND condition = 'errors'`, ifID)
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, interface_id, condition, start_time) VALUES (?, ?, 'errors', ?)`,
		s.DeviceID, ifID, start)
}

// New interface columns default so phase 1 rows and inserts keep working.
func TestDBInterfaceMonitoringDefaults(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name) VALUES (?, 1, '0/1')`, s.DeviceID)
	var row struct {
		Important       bool
		CollectDefault  bool
		Conditions      string
		ConditionsSince string
	}
	testdb.Must(t, db.Raw(`SELECT important, collect_default, conditions::text, conditions_since::text
		FROM device_interfaces WHERE device_id = ?`, s.DeviceID).Scan(&row).Error)
	if row.Important || row.CollectDefault || row.Conditions != "[]" || row.ConditionsSince != "{}" {
		t.Errorf("defaults: %+v", row)
	}
	var detected string
	testdb.Must(t, db.Raw(`SELECT device_type_detected FROM devices WHERE id = ?`, s.DeviceID).Scan(&detected).Error)
	if detected != "other" {
		t.Errorf("device_type_detected default = %q", detected)
	}
}
