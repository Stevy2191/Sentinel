package services

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBCustomMetricsSchema(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	// A metric incident needs both key and instance.
	if err := db.Exec(`INSERT INTO incidents (device_id, condition, start_time, incident_type) VALUES (?, 'metric', now(), 'error')`, s.DeviceID).Error; err == nil {
		t.Error("metric incident without key accepted")
	}
	testdb.Exec(t, db, `INSERT INTO incidents (device_id, condition, metric_key, metric_instance, start_time, incident_type) VALUES (?, 'metric', 'cisco_fan_state', '1004', now(), 'error')`, s.DeviceID)
	// One open per (device, key, instance).
	if err := db.Exec(`INSERT INTO incidents (device_id, condition, metric_key, metric_instance, start_time, incident_type) VALUES (?, 'metric', 'cisco_fan_state', '1004', now(), 'error')`, s.DeviceID).Error; err == nil {
		t.Error("second open metric incident accepted")
	}
	// Other conditions must leave the metric columns NULL.
	if err := db.Exec(`INSERT INTO incidents (device_id, condition, metric_key, start_time, incident_type) VALUES (?, 'ups_on_battery', 'x', now(), 'error')`, s.DeviceID).Error; err == nil {
		t.Error("UPS incident with a metric key accepted")
	}
	// Profiles, metrics, overrides, runs, modules, objects exist and cascade.
	p := uuid.New()
	testdb.Exec(t, db, `INSERT INTO metric_profiles (id, name, match_prefixes) VALUES (?, 'P', '{1.3.6.1.4.1.9.1}')`, p)
	testdb.Exec(t, db, `INSERT INTO profile_metrics (profile_id, name, key, source, kind, oid, label_mode) VALUES (?, 'CPU', 'cisco_cpu_5min', 'column', 'gauge', '1.3.6.1.4.1.9.9.109.1.1.1.1.8', 'index')`, p)
	if err := db.Exec(`INSERT INTO profile_metrics (profile_id, name, key, source, kind, oid, label_mode) VALUES (?, 'Bad', 'ups_x', 'column', 'gauge', '1.3', 'index')`, p).Error; err == nil {
		t.Error("reserved key prefix accepted")
	}
	testdb.Exec(t, db, `INSERT INTO device_profile_overrides (device_id, profile_id, mode) VALUES (?, ?, 'detach')`, s.DeviceID, p)
	testdb.Exec(t, db, `INSERT INTO device_profile_runs (device_id, profile_id, ran_at, ok) VALUES (?, ?, now(), true)`, s.DeviceID, p)
	testdb.Exec(t, db, `DELETE FROM metric_profiles WHERE id = ?`, p)
	var n int64
	testdb.Must(t, db.Raw(`SELECT (SELECT count(*) FROM profile_metrics) + (SELECT count(*) FROM device_profile_overrides) + (SELECT count(*) FROM device_profile_runs)`).Scan(&n).Error)
	if n != 0 {
		t.Errorf("%d rows survived the profile delete", n)
	}
	testdb.Exec(t, db, `UPDATE metrics.series SET label = 'x' WHERE false`)
}
