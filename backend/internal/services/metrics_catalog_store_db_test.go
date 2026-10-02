package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBDeviceMetricsAndLabels(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	port := seedPort(t, db, s.DeviceID, 5, "Gi1/0/5", "uplink")
	testdb.Exec(t, db, `INSERT INTO metric_profiles (id, name) VALUES (gen_random_uuid(), 'Test')`)
	testdb.Exec(t, db, `INSERT INTO profile_metrics (profile_id, name, key, source, kind, units, oid)
		SELECT id, 'Fan speed', 'fan_rpm', 'column', 'gauge', 'rpm', '1.3.6.1.4.1.9.9.13.1.4.1.3' FROM metric_profiles WHERE name = 'Test'`)
	SetCustomMetricKeys([]string{"fan_rpm"})
	t.Cleanup(func() { SetCustomMetricKeys(nil) })

	m := NewMetricsStore(db)
	at := time.Now().UTC().Add(-time.Minute)
	testdb.Must(t, m.Write(ctx, s.DeviceID, at, []SamplePoint{
		{Metric: MetricIfInBps, Instance: "5", InterfaceID: &port, Value: 1000},
		{Metric: MetricUPSChargePct, Instance: "", Value: 97},
		{Metric: "fan_rpm", Instance: "1", Label: "Fan 1", Value: 5200},
	}))

	got, err := m.DeviceMetrics(ctx, s.DeviceID)
	testdb.Must(t, err)
	byKey := map[string]DeviceMetric{}
	for _, d := range got {
		byKey[d.Metric] = d
	}
	if d := byKey[MetricIfInBps]; d.Label != "Traffic in" || d.Unit != "bps" || len(d.Instances) != 1 || d.Instances[0].Label != "Gi1/0/5" {
		t.Errorf("if_in_bps = %+v, want the catalogue label and the port's name", d)
	}
	if d := byKey["fan_rpm"]; d.Label != "Fan speed" || d.Unit != "rpm" || d.Instances[0].Label != "Fan 1" {
		t.Errorf("fan_rpm = %+v, want the profile metric's name, units and the row label", d)
	}
	if d := byKey[MetricUPSChargePct]; d.Unit != "%" || d.Instances[0].Instance != "" {
		t.Errorf("ups_charge_pct = %+v", d)
	}

	defs, err := m.Describe(ctx, []string{MetricIfInBps, "fan_rpm", "nope"})
	testdb.Must(t, err)
	if len(defs) != 2 || defs["fan_rpm"].Label != "Fan speed" {
		t.Errorf("Describe = %+v, want two known keys", defs)
	}
	labels, err := m.InstanceLabels(ctx, []uuid.UUID{s.DeviceID}, []string{MetricIfInBps, "fan_rpm"})
	testdb.Must(t, err)
	if labels[InstanceKey{DeviceID: s.DeviceID, Metric: MetricIfInBps, Instance: "5"}] != "Gi1/0/5" {
		t.Errorf("InstanceLabels = %+v", labels)
	}
}
