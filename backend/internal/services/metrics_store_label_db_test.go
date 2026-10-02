package services

import (
	"context"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBSeriesLabel(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	SetCustomMetricKeys([]string{"acme_temp"})
	defer SetCustomMetricKeys(nil)
	m := NewMetricsStore(db)
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: "acme_temp", Instance: "1", Label: "Inlet", Value: 30}}))
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: "acme_temp", Instance: "1", Label: "Inlet (rear)", Value: 31}}))
	var label string
	var n int64
	testdb.Must(t, db.Raw(`SELECT label FROM metrics.series WHERE metric = 'acme_temp'`).Scan(&label).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.series WHERE metric = 'acme_temp'`).Scan(&n).Error)
	if label != "Inlet (rear)" || n != 1 {
		t.Errorf("label %q series %d", label, n)
	}
	// A point with no label leaves the stored one alone, and a fresh store
	// (nothing cached) keeps it too.
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: "acme_temp", Instance: "1", Value: 32}}))
	testdb.Must(t, NewMetricsStore(db).Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: "acme_temp", Instance: "1", Value: 33}}))
	testdb.Must(t, db.Raw(`SELECT label FROM metrics.series WHERE metric = 'acme_temp'`).Scan(&label).Error)
	if label != "Inlet (rear)" {
		t.Errorf("label after unlabelled writes %q", label)
	}
}
