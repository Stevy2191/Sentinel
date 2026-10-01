package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBUPSStatus(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET device_type = 'ups', ups_high_load_pct = 70 WHERE id = ?`, s.DeviceID)
	incidents := NewIncidentService(db)
	metrics := NewMetricsStore(db)
	devices := NewDeviceService(db, NewSNMPCredentialService(db), incidents)
	ports := NewPortService(db, metrics, incidents, NewSettingsService(db))
	testdb.Must(t, metrics.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{
		{Metric: MetricUPSChargePct, Value: 96}, {Metric: MetricUPSLoadPct, Value: 34}, {Metric: MetricUPSOnBattery, Value: 1},
	}))
	if _, _, err := incidents.OpenDeviceConditionIncident(ctx, s.DeviceID, models.UPSConditionOnBattery, time.Now().UTC(), "x"); err != nil {
		t.Fatal(err)
	}
	d, err := devices.Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	v, err := ports.UPSStatus(ctx, d)
	testdb.Must(t, err)
	if v.Readings[MetricUPSChargePct] != 96 || v.Readings[MetricUPSOnBattery] != 1 || len(v.Readings) != 3 {
		t.Errorf("readings %v", v.Readings)
	}
	if len(v.Conditions) != 1 || v.Conditions[0] != models.UPSConditionOnBattery {
		t.Errorf("conditions %v", v.Conditions)
	}
	if v.LowBatteryPct != 25 || v.HighLoadPct != 70 || v.DefaultLowBatteryPct != 25 || v.DefaultHighLoadPct != 80 {
		t.Errorf("thresholds %+v", v)
	}
}
