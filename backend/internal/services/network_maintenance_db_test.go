package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBNetworkMaintenanceRunOnce(t *testing.T) {
	db := testdb.Open(t)
	m := NewMetricsStore(db)
	ctx := context.Background()
	testdb.Must(t, m.Write(ctx, uuid.New(), time.Now().UTC(), []SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 1}}))
	if err := NewNetworkMaintenance(m, NewSettingsService(db)).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var n int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples`).Scan(&n).Error)
	if n != 0 {
		t.Errorf("orphan samples left: %d", n)
	}
}
