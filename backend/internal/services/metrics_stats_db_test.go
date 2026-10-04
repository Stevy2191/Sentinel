package services

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// statsSeries makes a series of metric for a device that need not exist: the
// metrics schema has no foreign keys.
func statsSeries(t *testing.T, db *gorm.DB, metric string) int64 {
	t.Helper()
	var id int64
	testdb.Must(t, db.Raw(`INSERT INTO metrics.series (device_id, metric, instance) VALUES (?, ?, '1') RETURNING id`,
		uuid.New(), metric).Scan(&id).Error)
	return id
}

// insertSample writes one raw sample.
func insertSample(t *testing.T, db *gorm.DB, series int64, at time.Time, v float64) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value) VALUES (?, ?, ?)`, at, series, v)
}

// fillSamples writes v every `every` from `from` up to, not including, `to`.
func fillSamples(t *testing.T, db *gorm.DB, series int64, from, to time.Time, every time.Duration, v float64) {
	t.Helper()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT g, ?, ? FROM generate_series(?::timestamptz, ?::timestamptz - make_interval(secs => ?), make_interval(secs => ?)) g`,
		series, v, from, to, every.Seconds(), every.Seconds())
}

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// Every figure of one series, worked out by hand. Buckets (start + avg):
//
//	b0 10, 20      -> 15     b3 0, 100, 50 -> 50
//	b1 40          -> 40     b4 30         -> 30
//	b2 (no data)             b5 60, 80     -> 70
//
// Avg = (30+40+150+30+140) / 9 samples = 390/9; Min 0; Peak 100.
// 95th of [15 30 40 50 70]: position 0.95 x 4 = 3.8, so 50 + 0.8 x 20 = 66.
// BucketSum = 15+40+50+30+70 = 205; 5 of 6 buckets have data.
func TestDBSeriesStats(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	m := NewMetricsStore(db)
	t0 := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	m.now = func() time.Time { return t0.Add(2 * time.Hour) }
	id := statsSeries(t, db, MetricIfInBps)
	empty := statsSeries(t, db, MetricIfInBps)
	minute := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Minute) }
	for _, s := range []struct {
		n int
		v float64
	}{{1, 10}, {2, 20}, {6, 40}, {16, 0}, {17, 100}, {18, 50}, {21, 30}, {26, 60}, {27, 80},
		// Inside [From, To) but in buckets cut by its edges: not counted.
		{-1, 1000}, {31, 1000}} {
		insertSample(t, db, id, minute(s.n), s.v)
	}
	refreshRollups(t, db)

	got, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: []int64{id, empty}, From: minute(-2), To: minute(32)})
	testdb.Must(t, err)
	if len(got) != 1 || got[0].SeriesID != id {
		t.Fatalf("stats = %+v, want one entry, for the series with data", got)
	}
	s := got[0]
	if !closeTo(s.Avg, 390.0/9) || s.Min != 0 || s.Peak != 100 || !closeTo(s.P95, 66) || !closeTo(s.BucketSum, 205) ||
		s.Buckets != 5 || s.Expected != 6 {
		t.Errorf("stats = %+v, want avg 43.33, min 0, peak 100, p95 66, bucket sum 205, 5 of 6 buckets", s)
	}
	if none, err := m.SeriesStats(ctx, StatsQuery{From: minute(0), To: minute(30)}); err != nil || len(none) != 0 {
		t.Errorf("no series: %v, %v", none, err)
	}
}

// Review Focus 1: a rolling period that ends now. The rollups are real-time,
// so the bucket now falls in already has a row, from the samples so far. It
// must not count: not in the expected buckets (coverage stays 100%), not in
// the 95th or the peak.
func TestDBSeriesStatsLeavesOutTheBucketStillFilling(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	m := NewMetricsStore(db)
	base := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Hour)
	now := base.Add(32 * time.Minute)
	m.now = func() time.Time { return now }
	id := statsSeries(t, db, MetricIfInUtilPct)
	for i := 0; i < 6; i++ {
		insertSample(t, db, id, base.Add(time.Duration(i)*statsBucket+time.Minute), 10)
	}
	insertSample(t, db, id, base.Add(30*time.Minute+30*time.Second), 1000) // the bucket still filling
	insertSample(t, db, id, base.Add(31*time.Minute), 1000)
	refreshRollups(t, db)

	got, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: []int64{id}, From: base, To: now})
	testdb.Must(t, err)
	if len(got) != 1 || got[0].Expected != 6 || got[0].Buckets != 6 || got[0].Coverage() != 100 ||
		got[0].P95 != 10 || got[0].Peak != 10 || got[0].Avg != 10 {
		t.Fatalf("stats = %+v, want 6 of 6 buckets, all 10: the filling bucket left out", got)
	}

	// Once that bucket has ended it counts.
	now = base.Add(35 * time.Minute)
	got, err = m.SeriesStats(ctx, StatsQuery{SeriesIDs: []int64{id}, From: base, To: now})
	testdb.Must(t, err)
	if len(got) != 1 || got[0].Expected != 7 || got[0].Buckets != 7 || got[0].Peak != 1000 {
		t.Errorf("stats after the bucket ended = %+v, want 7 of 7 buckets and its peak", got)
	}
}

// Review Focus 2: November 2026 in America/Chicago is 721 hours long (DST
// ends on 1 November). A complete month has 721 x 12 = 8652 buckets, all
// with data, so coverage is exactly 100%. Thirty days of 288 buckets (8640)
// would say 100.14%.
func TestDBSeriesStatsDSTMonthIsWhole(t *testing.T) {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	db := testdb.Open(t)
	ctx := context.Background()
	// The month is a fixed date; keep the retention policy away from it.
	testdb.Exec(t, db, `SELECT remove_retention_policy('metrics.samples', if_exists => true)`)
	r := &models.Report{PeriodKind: models.PeriodCalendar, PeriodUnit: models.UnitMonth, PeriodOffset: 1}
	now := time.Date(2026, 12, 2, 18, 0, 0, 0, time.UTC)
	start, end := r.ResolvePeriod(now, loc)
	m := NewMetricsStore(db)
	m.now = func() time.Time { return now }
	id := statsSeries(t, db, MetricIfInUtilPct)
	fillSamples(t, db, id, start, end, statsBucket, 42)
	refreshRollups(t, db)

	got, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: []int64{id}, From: start, To: end})
	testdb.Must(t, err)
	if len(got) != 1 || got[0].Expected != 8652 || got[0].Buckets != 8652 || got[0].Coverage() != 100 || got[0].Avg != 42 {
		t.Fatalf("November = %+v, want 8652 of 8652 buckets (100%%) at 42", got)
	}
}

// A statistics query that runs past its statement timeout is a report too
// large to build. The timeout is local to its transaction, so it never
// reaches later queries on the pool.
func TestDBStatsTimeoutIsTooLarge(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	m := NewMetricsStore(db)
	m.statsTimeout = 50 * time.Millisecond
	err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error { return tx.Exec(`SELECT pg_sleep(1)`).Error })
	if !errors.Is(err, ErrReportTooLarge) {
		t.Fatalf("err = %v, want ErrReportTooLarge", err)
	}
	if err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error { return tx.Exec(`SELECT 1`).Error }); err != nil {
		t.Errorf("a quick query under the timeout: %v", err)
	}
	if err := db.Exec(`SELECT pg_sleep(0.2)`).Error; err != nil {
		t.Errorf("the timeout leaked out of its transaction: %v", err)
	}
}
