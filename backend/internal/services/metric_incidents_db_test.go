package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBMetricIncidents(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET created_at = now() - interval '2 days' WHERE id = ?`, s.DeviceID)
	inc := NewIncidentService(db)
	start := time.Now().UTC().Add(-time.Hour)
	a, opened, err := inc.OpenMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1004", start, "Fan 2 is down")
	if err != nil || !opened || a.MetricKey == nil || *a.Condition != models.IncidentConditionMetric {
		t.Fatalf("open %+v %v %v", a, opened, err)
	}
	if b, opened, _ := inc.OpenMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1004", start, "again"); opened || b.ID != a.ID {
		t.Fatal("second open was not idempotent")
	}
	if _, opened, _ := inc.OpenMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1005", start, "other row"); !opened {
		t.Fatal("another row did not open its own incident")
	}
	// UPS reconciliation lists only UPS-style conditions.
	if ups, _ := inc.OpenDeviceConditionIncidents(ctx, s.DeviceID); len(ups) != 0 {
		t.Fatalf("metric incidents listed as UPS ones: %d", len(ups))
	}
	if open, _ := inc.OpenMetricIncidents(ctx, s.DeviceID); len(open) != 2 {
		t.Fatalf("open metric incidents %d", len(open))
	}
	// Not downtime.
	d, err := NewDeviceService(db, NewSNMPCredentialService(db), inc).Get(ctx, s.DeviceID)
	testdb.Must(t, err)
	if d.Availability30d == nil || *d.Availability30d != 100 {
		t.Errorf("availability %v", d.Availability30d)
	}
	closed, err := inc.CloseMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1004", time.Now().UTC(), "")
	if err != nil || closed == nil || closed.EndTime == nil {
		t.Fatalf("close %+v %v", closed, err)
	}
	if again, err := inc.CloseMetricIncident(ctx, s.DeviceID, "cisco_fan_fru", "1004", time.Now().UTC(), ""); err != nil || again != nil {
		t.Fatalf("second close %+v %v", again, err)
	}
	// Subject names the metric and the row's label.
	testdb.Exec(t, db, `INSERT INTO metrics.series (device_id, metric, instance, label) VALUES (?, 'cisco_fan_fru', '1005', 'Switch 1 - Fan 2')`, s.DeviceID)
	list, _, err := inc.ListIncidents(ctx, IncidentListOptions{Viewer: &IncidentViewer{IsAdmin: true}, DeviceID: &s.DeviceID, Limit: 10})
	testdb.Must(t, err)
	found := false
	for _, r := range list {
		if r.MetricInstance != nil && *r.MetricInstance == "1005" {
			found = true
			if r.SubjectName != "dev-10.0.0.9 · cisco_fan_fru: Switch 1 - Fan 2" {
				t.Errorf("subject %q", r.SubjectName)
			}
		}
	}
	if !found {
		t.Error("metric incident not listed")
	}
}
