// Package services - metrics_stats.go computes the Metrics report's figures
// from the 5-minute rollup (metrics.samples_5m, kept forever): per series,
// per combined group of series, and chart lines. Only whole 5-minute buckets
// that have ended count (see statsWindow).
package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

const (
	// statsBucket is the rollup every report statistic reads.
	statsBucket = 5 * time.Minute
	// statsStatementTimeout bounds one statistics or chart query. A report
	// makes a few dozen of them inside the job's 5-minute limit; one that
	// runs this long means the scope or the period is too big to report on.
	statsStatementTimeout = 2 * time.Minute
)

// StatsQuery selects series and a window for SeriesStats.
type StatsQuery struct {
	SeriesIDs []int64
	From, To  time.Time // [From, To); only complete 5-minute buckets inside count
}

// SeriesStat is one series' figures over a window, from metrics.samples_5m.
type SeriesStat struct {
	SeriesID  int64
	Avg       float64 // sum(vsum)/sum(n)
	Min       float64 // min(vmin)
	Peak      float64 // max(vmax)
	P95       float64 // percentile_cont(0.95) over the bucket averages
	BucketSum float64 // sum of the 5-minute bucket averages (totals: bps x300/8 bytes, per_min x5)
	Buckets   int     // complete buckets with data
	Expected  int     // complete buckets in the window
}

// Coverage is the share of the window's complete buckets that have data,
// 0-100; 0 for an empty window.
func (s SeriesStat) Coverage() float64 {
	if s.Expected == 0 {
		return 0
	}
	return float64(s.Buckets) / float64(s.Expected) * 100
}

// statsWindow narrows [from, to) to the whole buckets of size base inside it
// that have ended by now, and counts them. The rollups are real-time
// (materialized_only = false), so the bucket now falls in already has a row
// built from the samples so far: counting it would lower coverage and put a
// partial average into the 95th. The count comes from the real duration, so
// a month with a DST change has its 23- or 25-hour day counted as it was.
func statsWindow(from, to, now time.Time, base time.Duration) (lo, hi time.Time, expected int) {
	lo = from.Truncate(base)
	if lo.Before(from) {
		lo = lo.Add(base)
	}
	if now.Before(to) {
		to = now
	}
	hi = to.Truncate(base)
	if !hi.After(lo) {
		return lo, lo, 0
	}
	return lo, hi, int(hi.Sub(lo) / base)
}

// clock is the store's idea of now: time.Now, or a fixed instant in tests.
func (m *MetricsStore) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

// withStatementTimeout runs fn in a transaction whose statements may each
// run for at most the statistics timeout (set_config is_local: it ends with
// the transaction and never reaches the pool). A statement cancelled by it
// is ErrReportTooLarge; the underlying error is logged first.
func (m *MetricsStore) withStatementTimeout(ctx context.Context, fn func(tx *gorm.DB) error) error {
	timeout := m.statsTimeout
	if timeout <= 0 {
		timeout = statsStatementTimeout
	}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('statement_timeout', ?, true)`,
			strconv.FormatInt(timeout.Milliseconds(), 10)).Error; err != nil {
			return fmt.Errorf("setting the statistics timeout: %w", err)
		}
		return fn(tx)
	})
	if isStatementTimeout(err) {
		log.Printf("[metrics] statistics query hit its %s statement timeout: %v", timeout, err)
		return ErrReportTooLarge
	}
	return err
}

// isStatementTimeout reports Postgres' query_canceled (57014), which a
// statement timeout raises.
func isStatementTimeout(err error) bool {
	var coded interface{ SQLState() string }
	return errors.As(err, &coded) && coded.SQLState() == "57014"
}

// statRow is one row of a statistics query.
type statRow struct {
	ID        int64   `gorm:"column:id"`
	Avg       float64 `gorm:"column:avg"`
	Min       float64 `gorm:"column:min"`
	Peak      float64 `gorm:"column:peak"`
	P95       float64 `gorm:"column:p95"`
	BucketSum float64 `gorm:"column:bucket_sum"`
	Buckets   int     `gorm:"column:buckets"`
}

func (r statRow) stat(expected int) SeriesStat {
	return SeriesStat{SeriesID: r.ID, Avg: r.Avg, Min: r.Min, Peak: r.Peak, P95: r.P95,
		BucketSum: r.BucketSum, Buckets: r.Buckets, Expected: expected}
}

// SeriesStats returns each series' figures over the window's complete
// 5-minute buckets, in one query. A series with no data there is absent.
// The 95th is percentile_cont over the 5-minute averages, as carriers bill.
func (m *MetricsStore) SeriesStats(ctx context.Context, q StatsQuery) ([]SeriesStat, error) {
	lo, hi, expected := statsWindow(q.From, q.To, m.clock(), statsBucket)
	out := []SeriesStat{}
	if len(q.SeriesIDs) == 0 || expected == 0 {
		return out, nil
	}
	var rows []statRow
	err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT r.series_id AS id,
				sum(r.vsum) / sum(r.n) AS avg,
				min(r.vmin) AS min,
				max(r.vmax) AS peak,
				percentile_cont(0.95) WITHIN GROUP (ORDER BY r.vsum / r.n) AS p95,
				sum(r.vsum / r.n) AS bucket_sum,
				count(*) AS buckets
			FROM metrics.samples_5m r
			WHERE r.series_id = ANY(?::bigint[]) AND r.bucket >= ? AND r.bucket < ?
			GROUP BY r.series_id
			ORDER BY r.series_id`, models.Int64Array(q.SeriesIDs), lo, hi).Scan(&rows).Error
	})
	if err != nil {
		if errors.Is(err, ErrReportTooLarge) {
			return nil, err
		}
		return nil, fmt.Errorf("reading series statistics: %w", err)
	}
	for _, r := range rows {
		out = append(out, r.stat(expected))
	}
	return out, nil
}

// CombinedQuery selects series to combine into one series, per 5-minute
// bucket: added up (bps, per_min: four ports moving 10 Mbps move 40), or
// averaged (%, °C, V: four ports 50 % busy are 50 % busy).
type CombinedQuery struct {
	SeriesIDs []int64
	From, To  time.Time
	Average   bool // false = sum per bucket (bps, per_min); true = average per bucket
}

// combineFunc is the SQL aggregate that combines series in a bucket.
func combineFunc(average bool) string {
	if average {
		return "avg"
	}
	return "sum"
}

// CombinedStats combines the series per bucket, then computes one SeriesStat (SeriesID 0).
// Its figures are the combined series': Avg its mean, Min and Peak its lowest
// and highest 5-minute value (raw peaks of different series do not line up,
// so a peak of a total cannot be read from them), P95 its 95th percentile.
func (m *MetricsStore) CombinedStats(ctx context.Context, q CombinedQuery) (SeriesStat, error) {
	stats, err := m.GroupedStats(ctx, [][]int64{q.SeriesIDs}, q.From, q.To, q.Average)
	if err != nil {
		return SeriesStat{}, err
	}
	return stats[0], nil
}

// GroupedStats is CombinedStats for many groups in one query: one SeriesStat
// per group, in order, its SeriesID the group's index. A report reads every
// device or site total of a metric this way, one query per metric and
// period however many totals there are. An empty group, or one without data,
// has Buckets 0; a series listed twice in a group counts once.
func (m *MetricsStore) GroupedStats(ctx context.Context, groups [][]int64, from, to time.Time, average bool) ([]SeriesStat, error) {
	lo, hi, expected := statsWindow(from, to, m.clock(), statsBucket)
	out := make([]SeriesStat, len(groups))
	var ids, grp models.Int64Array
	for i, g := range groups {
		out[i] = SeriesStat{SeriesID: int64(i), Expected: expected}
		seen := make(map[int64]bool, len(g))
		for _, id := range g {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
				grp = append(grp, int64(i))
			}
		}
	}
	if len(ids) == 0 || expected == 0 {
		return out, nil
	}
	var rows []statRow
	err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`WITH member AS (SELECT * FROM unnest(?::bigint[], ?::bigint[]) AS u(series_id, grp)),
			b AS (SELECT member.grp, r.bucket, `+combineFunc(average)+`(r.vsum / r.n) AS v
				FROM metrics.samples_5m r JOIN member ON member.series_id = r.series_id
				WHERE r.series_id = ANY(?::bigint[]) AND r.bucket >= ? AND r.bucket < ?
				GROUP BY member.grp, r.bucket)
			SELECT grp AS id, avg(v) AS avg, min(v) AS min, max(v) AS peak,
				percentile_cont(0.95) WITHIN GROUP (ORDER BY v) AS p95,
				sum(v) AS bucket_sum, count(*) AS buckets
			FROM b GROUP BY grp ORDER BY grp`, ids, grp, ids, lo, hi).Scan(&rows).Error
	})
	if err != nil {
		return nil, fmt.Errorf("reading combined statistics: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = r.stat(expected)
	}
	return out, nil
}

// chartResolution picks a report chart's rollup and step. The rollup follows
// PickResolution's split (the 5-minute rollup up to 7 days, hourly beyond);
// rawSince = to keeps it off the raw table, as a report reads whole buckets
// only. The step is a multiple of the rollup's bucket that keeps the chart to
// about maxPoints points.
func chartResolution(from, to time.Time, maxPoints int) (table string, base, step time.Duration) {
	table, base = "metrics.samples_5m", statsBucket
	if source, _ := PickResolution(from, to, to); source == "1h" {
		table, base = "metrics.samples_1h", time.Hour
	}
	if maxPoints < 1 {
		maxPoints = 1
	}
	step = base
	if n := to.Sub(from) / time.Duration(maxPoints); n > step {
		step = ((n + base - 1) / base) * base
	}
	return table, base, step
}

// CombinedSeries returns about maxPoints chart points for the combined series,
// from samples_5m for windows up to 7 days and samples_1h beyond.
// Series combine per rollup bucket as in CombinedStats; each point is the
// mean of the combined buckets in its step. Only whole buckets that have
// ended are read.
func (m *MetricsStore) CombinedSeries(ctx context.Context, q CombinedQuery, maxPoints int) ([]ChartPoint, error) {
	out := []ChartPoint{}
	table, base, step := chartResolution(q.From, q.To, maxPoints)
	lo, hi, n := statsWindow(q.From, q.To, m.clock(), base)
	if len(q.SeriesIDs) == 0 || n == 0 {
		return out, nil
	}
	var rows []struct {
		T time.Time `gorm:"column:t"`
		V float64   `gorm:"column:v"`
	}
	err := m.withStatementTimeout(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT time_bucket(make_interval(secs => ?), b.bucket) AS t, avg(b.v) AS v
			FROM (SELECT r.bucket, `+combineFunc(q.Average)+`(r.vsum / r.n) AS v
				FROM `+table+` r
				WHERE r.series_id = ANY(?::bigint[]) AND r.bucket >= ? AND r.bucket < ?
				GROUP BY r.bucket) b
			GROUP BY 1 ORDER BY 1`, step.Seconds(), models.Int64Array(q.SeriesIDs), lo, hi).Scan(&rows).Error
	})
	if err != nil {
		return nil, fmt.Errorf("reading chart series: %w", err)
	}
	for _, r := range rows {
		out = append(out, ChartPoint{T: r.T.UTC(), V: r.V})
	}
	return out, nil
}
