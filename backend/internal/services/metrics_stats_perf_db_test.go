package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// TestDBPerfReportStats measures the Metrics report's statistics queries on
// generated 5-minute data: 100 ports' traffic for 90 days, 2.6 million rollup
// rows. It logs each timing as a PERF line and fails only past a generous
// budget; the numbers decide whether the 500-row cap
// (models.MaxReportSubjects) stands. Generating and rolling up the data takes
// minutes, so it runs only when asked:
//
//	SENTINEL_PERF=1 ./scripts/test-db.sh -run TestDBPerfReportStats -v -timeout 30m
func TestDBPerfReportStats(t *testing.T) {
	if os.Getenv("SENTINEL_PERF") != "1" {
		t.Skip("set SENTINEL_PERF=1 to measure the report statistics queries (slow)")
	}
	db := testdb.Open(t)
	ctx := context.Background()
	// Background jobs (compression, retention, the rollup refresh) would
	// compete with the timed queries.
	testdb.Exec(t, db, `SELECT alter_job(job_id, scheduled => false) FROM timescaledb_information.jobs WHERE job_id >= 1000`)

	const ports, days = 100, 90
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -days)
	month := end.AddDate(0, 0, -30)
	ids := make([]int64, ports)
	for i := range ids {
		ids[i] = statsSeries(t, db, MetricIfInBps)
	}
	began := time.Now()
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT g, s.id, 1e6 * (1.5 + sin(extract(epoch FROM g)::double precision / 3600 + s.id))
		FROM generate_series(?::timestamptz, ?::timestamptz - interval '5 minutes', interval '5 minutes') g,
		     unnest(?::bigint[]) AS s(id)`, start, end, models.Int64Array(ids))
	t.Logf("PERF generated %d samples in %s", ports*days*288, time.Since(began).Round(time.Second))
	began = time.Now()
	refreshRollups(t, db)
	testdb.Exec(t, db, `ANALYZE`)
	t.Logf("PERF refreshed the rollups in %s", time.Since(began).Round(time.Second))

	m := NewMetricsStore(db)
	timed := func(name string, budget time.Duration, run func() error) time.Duration {
		t.Helper()
		began := time.Now()
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		took := time.Since(began)
		t.Logf("PERF %-48s %9s (budget %s)", name, took.Round(time.Millisecond), budget)
		if took > budget {
			t.Errorf("%s took %s, over its %s budget", name, took, budget)
		}
		return took
	}
	// whole checks every series came back with every bucket.
	whole := func(st []SeriesStat, buckets int) error {
		if len(st) != ports {
			return fmt.Errorf("%d series came back, want %d", len(st), ports)
		}
		for _, s := range st {
			if s.Buckets != buckets || s.Expected != buckets {
				return fmt.Errorf("series %d: %d of %d buckets, want %d", s.SeriesID, s.Buckets, s.Expected, buckets)
			}
		}
		return nil
	}

	month30 := timed("SeriesStats, 100 ports x 30 days", 15*time.Second, func() error {
		st, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: ids, From: month, To: end})
		if err != nil {
			return err
		}
		return whole(st, 30*288)
	})
	timed("SeriesStats, 100 ports x 90 days", 45*time.Second, func() error {
		st, err := m.SeriesStats(ctx, StatsQuery{SeriesIDs: ids, From: start, To: end})
		if err != nil {
			return err
		}
		return whole(st, 90*288)
	})
	for _, w := range []struct {
		name   string
		from   time.Time
		want   int
		budget time.Duration
	}{
		{"CombinedStats, 100 ports summed x 30 days", month, 30 * 288, 15 * time.Second},
		{"CombinedStats, 100 ports summed x 90 days", start, 90 * 288, 45 * time.Second},
	} {
		timed(w.name, w.budget, func() error {
			st, err := m.CombinedStats(ctx, CombinedQuery{SeriesIDs: ids, From: w.from, To: end})
			if err == nil && st.Buckets != w.want {
				err = fmt.Errorf("%d of %d buckets, want %d", st.Buckets, st.Expected, w.want)
			}
			return err
		})
	}
	timed("GroupedStats, 10 totals of 10 ports x 30 days", 15*time.Second, func() error {
		groups := make([][]int64, 10)
		for i := range groups {
			groups[i] = ids[i*10 : i*10+10]
		}
		st, err := m.GroupedStats(ctx, groups, month, end, false)
		if err != nil {
			return err
		}
		for _, s := range st {
			if s.Buckets != 30*288 {
				return fmt.Errorf("group %d: %d buckets, want %d", s.SeriesID, s.Buckets, 30*288)
			}
		}
		return nil
	})
	timed("CombinedSeries, 100 ports x 90 days (hourly)", 15*time.Second, func() error {
		pts, err := m.CombinedSeries(ctx, CombinedQuery{SeriesIDs: ids, From: start, To: end}, 200)
		if err == nil && len(pts) == 0 {
			err = errors.New("no chart points")
		}
		return err
	})
	// A monthly report reads each metric's statistics for this month and the
	// last. At 500 ports that is about 2 x 5 = 10 times the 30-day figure per
	// metric, and 40 times it for four metrics.
	t.Logf("PERF estimate for a monthly 500-port report: 1 metric %s, 4 metrics %s (the job limit is 5 minutes)",
		(10 * month30).Round(time.Second), (40 * month30).Round(time.Second))
}
