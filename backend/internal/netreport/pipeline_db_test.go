package netreport

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A metrics report built end to end through the report pipeline's
// aggregator, as the job queue and the scheduler call it.
func TestDBMetricsReportThroughTheAggregator(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)
	hq := newSite(t, db, "HQ")
	sw := newDevice(t, db, hq, "core-sw1")
	gi1 := newPort(t, db, sw, 1, "Gi1/0/1", gigabit)
	from := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	steady(t, db, portSeries(t, db, sw, gi1, 1, services.MetricIfInBps), from, 12, 4000)
	steady(t, db, portSeries(t, db, sw, gi1, 1, services.MetricIfOutBps), from, 12, 1000)
	refresh(t, db)
	report := models.Report{ID: uuid.New(), UserID: admin, CreatedBy: admin, Name: "Uplinks", ReportType: models.ReportTypeMetrics,
		ScopeType:  models.ScopeTypePorts,
		ScopeData:  models.ReportScope{PortIDs: []uuid.UUID{gi1}, Metrics: []string{services.MetricIfInBps, services.MetricIfOutBps}},
		PeriodKind: models.PeriodRolling, TimeRangeDays: 1}
	testdb.Must(t, report.Validate())
	testdb.Must(t, db.Create(&report).Error)
	agg := services.NewReportAggregatorService(db, nil)
	agg.SetNetworkBuilder(newBuilder(db))

	data, err := agg.AggregateReportData(ctx, &report, admin)
	testdb.Must(t, err)
	if data.Network == nil || len(data.Metrics) != 0 || len(data.Warnings) != 0 || data.ReportName != "Uplinks" {
		t.Fatalf("data = %+v, want the network part only", data)
	}
	if d := data.TimeRangeEnd.Sub(data.TimeRangeStart); d != 24*time.Hour {
		t.Errorf("window = %v, want the rolling day", d)
	}
	n := data.Network
	if n.ScopeLabel != "core-sw1 · Gi1/0/1" || n.Ports != 1 || n.Devices != 1 || len(n.Tables) != 1 || len(n.Tables[0].Rows) != 1 {
		t.Fatalf("network = %+v", n)
	}
	// 4000 bps for 12 buckets: 4000 x 12 x 300 / 8 = 1,800,000 bytes, with data
	// in 12 of the day's 287 or 288 complete buckets (about 4.2%).
	row := n.Tables[0].Rows[0]
	if row.Billable == nil || *row.Billable != 4000 || row.In == nil || row.In.Total == nil || *row.In.Total != 1_800_000 ||
		!row.LowCoverage || row.Coverage < 4.1 || row.Coverage > 4.2 {
		t.Errorf("row = %+v, want billable 4000, 1800000 bytes in, flagged near 4.2%% coverage", row)
	}
}
