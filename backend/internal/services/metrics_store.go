package services

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// SamplePoint is one value to write for a device.
type SamplePoint struct {
	Metric   string
	Instance string
	// InterfaceID links a port series to its interface row (no FK: see 048).
	InterfaceID *uuid.UUID
	Value       float64
}

type metricsSample struct {
	Time     time.Time `gorm:"column:time"`
	SeriesID int64     `gorm:"column:series_id"`
	Value    float64   `gorm:"column:value"`
}

type seriesKey struct {
	device           uuid.UUID
	metric, instance string
}

// MetricsStore reads and writes the generic metrics model (migration 048).
type MetricsStore struct {
	db  *gorm.DB
	mu  sync.Mutex
	ids map[seriesKey]int64
}

func NewMetricsStore(db *gorm.DB) *MetricsStore {
	return &MetricsStore{db: db, ids: map[seriesKey]int64{}}
}

func (m *MetricsStore) seriesID(ctx context.Context, deviceID uuid.UUID, p SamplePoint) (int64, error) {
	k := seriesKey{deviceID, p.Metric, p.Instance}
	m.mu.Lock()
	id, ok := m.ids[k]
	m.mu.Unlock()
	if ok {
		return id, nil
	}
	err := m.db.WithContext(ctx).Raw(`INSERT INTO metrics.series (device_id, metric, instance, interface_id)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (device_id, metric, instance)
		DO UPDATE SET interface_id = COALESCE(EXCLUDED.interface_id, metrics.series.interface_id)
		RETURNING id`, deviceID, p.Metric, p.Instance, p.InterfaceID).Scan(&id).Error
	if err != nil {
		return 0, fmt.Errorf("resolving series %s/%s: %w", p.Metric, p.Instance, err)
	}
	m.mu.Lock()
	m.ids[k] = id
	m.mu.Unlock()
	return id, nil
}

// Write stores one poll's points for a device, in one batch. Unknown metrics
// are an error; NaN and infinite values are dropped.
func (m *MetricsStore) Write(ctx context.Context, deviceID uuid.UUID, at time.Time, points []SamplePoint) error {
	rows := make([]metricsSample, 0, len(points))
	for _, p := range points {
		if !KnownMetric(p.Metric) {
			return fmt.Errorf("unknown metric %q", p.Metric)
		}
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			continue
		}
		id, err := m.seriesID(ctx, deviceID, p)
		if err != nil {
			return err
		}
		rows = append(rows, metricsSample{Time: at, SeriesID: id, Value: p.Value})
	}
	if len(rows) == 0 {
		return nil
	}
	if err := m.db.WithContext(ctx).Table("metrics.samples").CreateInBatches(rows, 500).Error; err != nil {
		return fmt.Errorf("writing %d samples: %w", len(rows), err)
	}
	return nil
}

// deleteDeviceSeries removes a device's series and queues their ids for the
// nightly sample cleanup. Run inside the device deletion's transaction.
func deleteDeviceSeries(tx *gorm.DB, deviceID uuid.UUID) error {
	if err := tx.Exec(`INSERT INTO metrics.deleted_series (series_id)
		SELECT id FROM metrics.series WHERE device_id = ? ON CONFLICT DO NOTHING`, deviceID).Error; err != nil {
		return fmt.Errorf("queueing series for cleanup: %w", err)
	}
	if err := tx.Exec(`DELETE FROM metrics.series WHERE device_id = ?`, deviceID).Error; err != nil {
		return fmt.Errorf("deleting series: %w", err)
	}
	return nil
}

// ---- Query ------------------------------------------------------------------

// MetricsQuery selects series and a time range.
type MetricsQuery struct {
	DeviceIDs []uuid.UUID
	Metrics   []string
	// Instances restricts to these instances (ports' ifIndex); empty = all.
	Instances []string
	// InterfaceIDs restricts to series of these interfaces; empty = all.
	InterfaceIDs []uuid.UUID
	From, To     time.Time
	// Sum adds up every selected series per metric and step (device and site
	// totals). Min and Max then equal Avg: a peak of a sum cannot be derived
	// from per-series rollups.
	Sum bool
}

type MetricPoint struct {
	Time time.Time `json:"t"`
	Avg  float64   `json:"avg"`
	Min  float64   `json:"min"`
	Max  float64   `json:"max"`
}

type MetricSeries struct {
	DeviceID *uuid.UUID    `json:"device_id"`
	Metric   string        `json:"metric"`
	Instance string        `json:"instance"`
	Points   []MetricPoint `json:"points"`
}

type MetricsResult struct {
	Resolution  string         `json:"resolution"`
	StepSeconds int            `json:"step_seconds"`
	Series      []MetricSeries `json:"series"`
}

const maxQueryPoints = 500

// PickResolution chooses the source (raw up to 6 h, 5-minute rollup up to 7
// days, hourly beyond) and a step that is a multiple of the source's bucket
// and keeps a series to about 500 points.
func PickResolution(from, to time.Time) (string, time.Duration) {
	span := to.Sub(from)
	source, base := "1h", time.Hour
	switch {
	case span <= 6*time.Hour:
		source, base = "raw", time.Minute
	case span <= 7*24*time.Hour:
		source, base = "5m", 5*time.Minute
	}
	step := base
	if n := span / maxQueryPoints; n > step {
		step = ((n + base - 1) / base) * base
	}
	return source, step
}

// Query returns the selected series at the resolution PickResolution picks.
func (m *MetricsStore) Query(ctx context.Context, q MetricsQuery) (*MetricsResult, error) {
	source, step := PickResolution(q.From, q.To)
	res := &MetricsResult{Resolution: source, StepSeconds: int(step.Seconds()), Series: []MetricSeries{}}
	if len(q.DeviceIDs) == 0 || len(q.Metrics) == 0 {
		return res, nil
	}

	var inner string
	switch source {
	case "raw":
		inner = `SELECT s.device_id, s.metric, s.instance, time_bucket(make_interval(secs => ?), x.time) AS t,
			avg(x.value) AS avg, min(x.value) AS min, max(x.value) AS max
			FROM metrics.samples x JOIN metrics.series s ON s.id = x.series_id
			WHERE s.device_id IN ? AND s.metric IN ? AND x.time >= ? AND x.time < ?`
	default:
		table := "metrics.samples_5m"
		if source == "1h" {
			table = "metrics.samples_1h"
		}
		inner = `SELECT s.device_id, s.metric, s.instance, time_bucket(make_interval(secs => ?), r.bucket) AS t,
			COALESCE(sum(r.vsum) / NULLIF(sum(r.n), 0), 0) AS avg, min(r.vmin) AS min, max(r.vmax) AS max
			FROM ` + table + ` r JOIN metrics.series s ON s.id = r.series_id
			WHERE s.device_id IN ? AND s.metric IN ? AND r.bucket >= ? AND r.bucket < ?`
	}
	args := []any{step.Seconds(), q.DeviceIDs, q.Metrics, q.From, q.To}
	if len(q.Instances) > 0 {
		inner += " AND s.instance IN ?"
		args = append(args, q.Instances)
	}
	if len(q.InterfaceIDs) > 0 {
		inner += " AND s.interface_id IN ?"
		args = append(args, q.InterfaceIDs)
	}
	inner += " GROUP BY 1, 2, 3, 4"

	sql := inner + " ORDER BY 1, 2, 3, 4"
	if q.Sum {
		sql = `SELECT NULL::uuid AS device_id, metric, '' AS instance, t,
			sum(avg) AS avg, sum(avg) AS min, sum(avg) AS max
			FROM (` + inner + `) b GROUP BY metric, t ORDER BY metric, t`
	}

	var rows []struct {
		DeviceID *uuid.UUID
		Metric   string
		Instance string
		T        time.Time
		Avg      float64
		Min      float64
		Max      float64
	}
	if err := m.db.WithContext(ctx).Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("querying metrics: %w", err)
	}
	for _, r := range rows {
		n := len(res.Series)
		if n == 0 || res.Series[n-1].Metric != r.Metric || res.Series[n-1].Instance != r.Instance ||
			!sameDevice(res.Series[n-1].DeviceID, r.DeviceID) {
			res.Series = append(res.Series, MetricSeries{DeviceID: r.DeviceID, Metric: r.Metric, Instance: r.Instance})
			n++
		}
		res.Series[n-1].Points = append(res.Series[n-1].Points, MetricPoint{Time: r.T, Avg: r.Avg, Min: r.Min, Max: r.Max})
	}
	return res, nil
}

func sameDevice(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// LatestMany returns each series' newest value since `since`, as
// device -> metric -> instance -> value.
func (m *MetricsStore) LatestMany(ctx context.Context, deviceIDs []uuid.UUID, metrics []string, since time.Time) (map[uuid.UUID]map[string]map[string]float64, error) {
	out := map[uuid.UUID]map[string]map[string]float64{}
	if len(deviceIDs) == 0 || len(metrics) == 0 {
		return out, nil
	}
	var rows []struct {
		DeviceID uuid.UUID
		Metric   string
		Instance string
		Value    float64
	}
	err := m.db.WithContext(ctx).Raw(`SELECT DISTINCT ON (x.series_id) s.device_id, s.metric, s.instance, x.value
		FROM metrics.samples x JOIN metrics.series s ON s.id = x.series_id
		WHERE s.device_id IN ? AND s.metric IN ? AND x.time >= ?
		ORDER BY x.series_id, x.time DESC`, deviceIDs, metrics, since).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("reading latest values: %w", err)
	}
	for _, r := range rows {
		if out[r.DeviceID] == nil {
			out[r.DeviceID] = map[string]map[string]float64{}
		}
		if out[r.DeviceID][r.Metric] == nil {
			out[r.DeviceID][r.Metric] = map[string]float64{}
		}
		out[r.DeviceID][r.Metric][r.Instance] = r.Value
	}
	return out, nil
}

// ---- Retention and maintenance ---------------------------------------------

// ApplyRetention replaces the raw-samples retention policy.
func (m *MetricsStore) ApplyRetention(ctx context.Context, days int) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT remove_retention_policy('metrics.samples', if_exists => true)`).Error; err != nil {
			return fmt.Errorf("removing retention policy: %w", err)
		}
		if err := tx.Exec(`SELECT add_retention_policy('metrics.samples', make_interval(days => ?))`, days).Error; err != nil {
			return fmt.Errorf("adding retention policy: %w", err)
		}
		return nil
	})
}

// RetentionDays reads the current raw-samples retention policy.
func (m *MetricsStore) RetentionDays(ctx context.Context) (int, error) {
	var secs float64
	err := m.db.WithContext(ctx).Raw(`SELECT EXTRACT(EPOCH FROM (config->>'drop_after')::interval)
		FROM timescaledb_information.jobs
		WHERE proc_name = 'policy_retention' AND hypertable_schema = 'metrics' AND hypertable_name = 'samples'`).
		Scan(&secs).Error
	if err != nil {
		return 0, fmt.Errorf("reading retention policy: %w", err)
	}
	return int(secs / 86400), nil
}

// CleanupResult says what one nightly cleanup removed.
type CleanupResult struct {
	SeriesRemoved  int64
	SamplesRemoved int64
	EventsRemoved  int64
}

// Cleanup removes series whose device is gone, the samples of every queued
// series, and point events (and ended span events) older than
// eventRetentionDays.
func (m *MetricsStore) Cleanup(ctx context.Context, eventRetentionDays int) (CleanupResult, error) {
	var res CleanupResult
	db := m.db.WithContext(ctx)
	if err := db.Exec(`INSERT INTO metrics.deleted_series (series_id)
		SELECT s.id FROM metrics.series s WHERE NOT EXISTS (SELECT 1 FROM devices d WHERE d.id = s.device_id)
		ON CONFLICT DO NOTHING`).Error; err != nil {
		return res, fmt.Errorf("queueing orphan series: %w", err)
	}
	var queued []int64
	if err := db.Raw(`SELECT series_id FROM metrics.deleted_series ORDER BY series_id`).Scan(&queued).Error; err != nil {
		return res, fmt.Errorf("reading the deleted-series queue: %w", err)
	}
	if len(queued) > 0 {
		r := db.Exec(`DELETE FROM metrics.series WHERE id IN ?`, queued)
		if r.Error != nil {
			return res, fmt.Errorf("deleting orphan series: %w", r.Error)
		}
		res.SeriesRemoved = r.RowsAffected
		r = db.Exec(`DELETE FROM metrics.samples WHERE series_id IN ?`, queued)
		if r.Error != nil {
			return res, fmt.Errorf("deleting samples of removed series: %w", r.Error)
		}
		res.SamplesRemoved = r.RowsAffected
		if err := db.Exec(`DELETE FROM metrics.deleted_series WHERE series_id IN ?`, queued).Error; err != nil {
			return res, fmt.Errorf("clearing the deleted-series queue: %w", err)
		}
		m.mu.Lock()
		m.ids = map[seriesKey]int64{}
		m.mu.Unlock()
	}
	r := db.Exec(`DELETE FROM port_events WHERE started_at < now() - make_interval(days => ?)
		AND (ended_at IS NOT NULL OR kind IN ('link_up', 'link_down', 'speed_change', 'admin_up', 'admin_down'))`,
		eventRetentionDays)
	if r.Error != nil {
		return res, fmt.Errorf("deleting old port events: %w", r.Error)
	}
	res.EventsRemoved = r.RowsAffected
	return res, nil
}

// UpdateUsualSpeeds recomputes every port's usual speed from the last 7 days
// of if_speed_bps 5-minute buckets whose speed held steady (min = max).
// Returns how many ports got a usual speed.
func (m *MetricsStore) UpdateUsualSpeeds(ctx context.Context, now time.Time) (int, error) {
	var rows []struct {
		InterfaceID uuid.UUID
		Speed       float64
		N           int
		First       time.Time
	}
	err := m.db.WithContext(ctx).Raw(`SELECT s.interface_id, r.vmax AS speed, count(*) AS n, min(r.bucket) AS first
		FROM metrics.samples_5m r JOIN metrics.series s ON s.id = r.series_id
		WHERE s.metric = ? AND s.interface_id IS NOT NULL AND r.bucket >= ? AND r.vmin = r.vmax
		GROUP BY s.interface_id, r.vmax`, MetricIfSpeedBps, now.Add(-7*24*time.Hour)).Scan(&rows).Error
	if err != nil {
		return 0, fmt.Errorf("reading speed history: %w", err)
	}
	type hist struct {
		counts map[int64]int
		first  time.Time
	}
	byPort := map[uuid.UUID]*hist{}
	for _, r := range rows {
		h := byPort[r.InterfaceID]
		if h == nil {
			h = &hist{counts: map[int64]int{}, first: r.First}
			byPort[r.InterfaceID] = h
		}
		h.counts[int64(r.Speed)] += r.N
		if r.First.Before(h.first) {
			h.first = r.First
		}
	}
	updated := 0
	for id, h := range byPort {
		usual := portmon.UsualSpeed(h.counts, now.Sub(h.first).Hours())
		if usual <= 0 {
			continue
		}
		if err := m.db.WithContext(ctx).Exec(`UPDATE device_interfaces SET usual_speed_bps = ? WHERE id = ?`, usual, id).Error; err != nil {
			return updated, fmt.Errorf("saving usual speed: %w", err)
		}
		updated++
	}
	return updated, nil
}

// instanceKey is a port series' instance: its ifIndex.
func instanceKey(ifIndex int) string { return strconv.Itoa(ifIndex) }
