package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBDeviceHealth(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET sys_object_id = '1.3.6.1.4.1.9.1.2066' WHERE id = ?`, s.DeviceID)
	profiles := NewProfileService(db)
	testdb.Must(t, profiles.SeedStarter(ctx))
	testdb.Must(t, profiles.Load(ctx))
	defer SetCustomMetricKeys(nil)
	incidents := NewIncidentService(db)
	devices := NewDeviceService(db, NewSNMPCredentialService(db), incidents)
	d, err := devices.Get(ctx, s.DeviceID)
	testdb.Must(t, err)

	now := time.Now().UTC()
	metrics := NewMetricsStore(db)
	testdb.Must(t, metrics.Write(ctx, s.DeviceID, now.Add(-time.Minute), []SamplePoint{
		{Metric: "cisco_fan_envmon", Instance: "1", Label: "Fan 1", Value: 1},
		{Metric: "cisco_fan_envmon", Instance: "2", Label: "Fan 2", Value: 3},
	}))
	// Too old to be live: not shown.
	testdb.Must(t, metrics.Write(ctx, s.DeviceID, now.Add(-time.Hour), []SamplePoint{{Metric: "cisco_cpu_5min", Instance: "1", Value: 50}}))
	_, _, err = incidents.OpenMetricIncident(ctx, s.DeviceID, "cisco_fan_envmon", "2", now.Add(-time.Minute), "Fan 2 is critical")
	testdb.Must(t, err)
	views, err := profiles.DeviceProfiles(ctx, d.Device)
	testdb.Must(t, err)
	testdb.Must(t, profiles.SaveRun(ctx, s.DeviceID, views[0].Profile.ID, now, true, ""))

	h, err := profiles.DeviceHealth(ctx, d, metrics, incidents)
	testdb.Must(t, err)
	if len(h.Profiles) != 1 || h.Profiles[0].LastRun == nil || !h.Profiles[0].LastRun.OK || !h.Profiles[0].Applies {
		t.Fatalf("profiles %+v", h.Profiles)
	}
	if len(h.Metrics) != 1 || h.Metrics[0].Key != "cisco_fan_envmon" || h.Metrics[0].Name != "Fan" || h.Metrics[0].Rule != "not OK" {
		t.Fatalf("metrics %+v", h.Metrics)
	}
	rows := h.Metrics[0].Rows
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows)
	}
	if r := rows[0]; r.Instance != "1" || r.Label != "Fan 1" || r.State != "normal" || !r.OK || r.Problem {
		t.Errorf("fan 1 %+v", r)
	}
	if r := rows[1]; r.Instance != "2" || r.Label != "Fan 2" || r.State != "critical" || r.OK || !r.Problem || r.Value != 3 {
		t.Errorf("fan 2 %+v", r)
	}
}
