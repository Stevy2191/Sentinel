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
