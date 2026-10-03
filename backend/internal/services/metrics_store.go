package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// SamplePoint is one value to write for a device.
type SamplePoint struct {
	Metric   string
	Instance string
	// InterfaceID links a port series to its interface row (no FK: see 048).
	InterfaceID *uuid.UUID
	// Label names the instance for people (a custom metric's row label). It
	// is stored on the series when non-empty; it is not part of its identity.
	Label string
	Value float64
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

// cachedSeries is a resolved series id and the label last stored on it.
type cachedSeries struct {
	id    int64
	label string
}

// MetricsStore reads and writes the generic metrics model (migration 048).
type MetricsStore struct {
	db  *gorm.DB
	mu  sync.Mutex
	ids map[seriesKey]cachedSeries
	// skipped is the unknown metric keys Write has already logged, so each
	// is logged once rather than on every poll.
	skipped map[string]bool

	// retMu guards retentionDays, a cache of the raw-samples retention policy
	// (in days) so Query does not read timescaledb_information.jobs on every
	// call. 0 means "not yet known". ApplyRetention keeps it current; Query
	// lazily loads it via RetentionDays otherwise.
	retMu         sync.Mutex
	retentionDays int
}

func NewMetricsStore(db *gorm.DB) *MetricsStore {
	return &MetricsStore{db: db, ids: map[seriesKey]cachedSeries{}, skipped: map[string]bool{}}
}

func (m *MetricsStore) seriesID(ctx context.Context, deviceID uuid.UUID, p SamplePoint) (int64, error) {
	k := seriesKey{deviceID, p.Metric, p.Instance}
	m.mu.Lock()
	cached, ok := m.ids[k]
	m.mu.Unlock()
	if ok {
		if p.Label != "" && p.Label != cached.label {
			if err := m.db.WithContext(ctx).Exec(`UPDATE metrics.series SET label = ? WHERE id = ?`, p.Label, cached.id).Error; err != nil {
				return 0, fmt.Errorf("relabelling series %s/%s: %w", p.Metric, p.Instance, err)
			}
			m.mu.Lock()
			m.ids[k] = cachedSeries{id: cached.id, label: p.Label}
			m.mu.Unlock()
		}
		return cached.id, nil
	}
	var row struct {
		ID    int64
		Label string
	}
	err := m.db.WithContext(ctx).Raw(`INSERT INTO metrics.series (device_id, metric, instance, interface_id, label)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (device_id, metric, instance)
		DO UPDATE SET interface_id = COALESCE(EXCLUDED.interface_id, metrics.series.interface_id),
			label = CASE WHEN EXCLUDED.label <> '' THEN EXCLUDED.label ELSE metrics.series.label END
		RETURNING id, label`, deviceID, p.Metric, p.Instance, p.InterfaceID, p.Label).Scan(&row).Error
	if err != nil {
		return 0, fmt.Errorf("resolving series %s/%s: %w", p.Metric, p.Instance, err)
	}
	m.mu.Lock()
	m.ids[k] = cachedSeries{id: row.ID, label: row.Label}
	m.mu.Unlock()
	return row.ID, nil
}

// Write stores one poll's points for a device, in one batch. Points of an
// unknown metric are skipped (logged once per key): a custom key the
// registry does not know yet, or no longer knows, must not lose the rest of
// the batch. NaN and infinite values are dropped.
func (m *MetricsStore) Write(ctx context.Context, deviceID uuid.UUID, at time.Time, points []SamplePoint) error {
	rows := make([]metricsSample, 0, len(points))
	for _, p := range points {
		if !KnownMetric(p.Metric) {
			m.mu.Lock()
			first := !m.skipped[p.Metric]
			m.skipped[p.Metric] = true
			m.mu.Unlock()
			if first {
				log.Printf("[metrics] skipping samples of unknown metric %q", p.Metric)
			}
			continue
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

// Forget drops a device's cached series ids (M7: DeviceService.Delete calls
// this once the device and its series rows are gone). Without this, a
// device id reused by a restored backup would resolve writes to this
// process's still-cached (but now deleted, and queued for the nightly
// cleanup to remove) series ids instead of fresh ones, silently losing the
// restored device's new samples.
func (m *MetricsStore) Forget(deviceID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.ids {
		if k.device == deviceID {
			delete(m.ids, k)
		}
	}
}

// ForgetMetrics drops every device's cached series ids for these metric
// keys. ProfileService calls it once a custom metric's (or a whole
// profile's) series rows are deleted, so a key that comes back resolves a
// fresh series instead of the deleted id the nightly cleanup will purge.
func (m *MetricsStore) ForgetMetrics(keys []string) {
	if len(keys) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.ids {
		if slices.Contains(keys, k.metric) {
			delete(m.ids, k)
		}
	}
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
	// Sum combines every selected series into one per metric and step (device
	// and site totals): added up, or averaged for the metrics in Average. Min
	// and Max then equal Avg: a peak of a total cannot be derived from
	// per-series rollups.
	Sum bool
	// Average, with Sum, lists the metrics whose series are averaged instead
	// of added up: units such as %, °C or V, whose sum means nothing.
	Average []string
	// PerDevice, with Sum, keeps devices apart: one total per device, metric
	// and step instead of one per metric and step.
	PerDevice bool
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
//
// rawSince is the oldest instant the raw table can still answer for (now
// minus the current raw retention). A window of 6 h or less that starts
// before rawSince reads the 5-minute rollup instead of raw: the rollups are
// kept forever, but old raw chunks are not, and retention can be as low as
// 7 days, far shorter than a 6 h window's default range would assume.
func PickResolution(from, to, rawSince time.Time) (string, time.Duration) {
	span := to.Sub(from)
	source, base := "1h", time.Hour
	switch {
	case span <= 6*time.Hour:
		if from.Before(rawSince) {
			source, base = "5m", 5*time.Minute
		} else {
			source, base = "raw", time.Minute
		}
	case span <= 7*24*time.Hour:
		source, base = "5m", 5*time.Minute
	}
	step := base
	if n := span / maxQueryPoints; n > step {
		step = ((n + base - 1) / base) * base
	}
	return source, step
}

// rawRetentionDays returns the cached raw-sample retention (days), reading it
// from the database once if not yet known; ApplyRetention keeps the cache
// current afterwards. Falls back to the default when the policy cannot be
// read, so a query never mistakes "we don't know" for "keep nothing" (which
// would make every query, however recent, skip the raw table).
func (m *MetricsStore) rawRetentionDays(ctx context.Context) int {
	m.retMu.Lock()
	days := m.retentionDays
	m.retMu.Unlock()
	if days > 0 {
		return days
	}
	read, err := m.RetentionDays(ctx)
	if err != nil || read <= 0 {
		return models.DefaultMetricsRawRetentionDays
	}
	m.retMu.Lock()
	m.retentionDays = read
	m.retMu.Unlock()
	return read
}

// Query returns the selected series at the resolution PickResolution picks.
func (m *MetricsStore) Query(ctx context.Context, q MetricsQuery) (*MetricsResult, error) {
	rawSince := time.Now().UTC().Add(-time.Duration(m.rawRetentionDays(ctx)) * 24 * time.Hour)
	source, step := PickResolution(q.From, q.To, rawSince)
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
		device, group := "NULL::uuid", "metric, t"
		if q.PerDevice {
			device, group = "device_id", "device_id, metric, t"
		}
		total := "sum(avg)"
		if len(q.Average) > 0 {
			// This placeholder comes before the inner query's in the text.
			total = "CASE WHEN metric IN ? THEN avg(avg) ELSE sum(avg) END"
			args = append([]any{q.Average}, args...)
		}
		sql = `SELECT device_id, metric, '' AS instance, t, v AS avg, v AS min, v AS max
			FROM (SELECT ` + device + ` AS device_id, metric, t, ` + total + ` AS v
				FROM (` + inner + `) b GROUP BY ` + group + `) c
			ORDER BY device_id, metric, t`
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

// SeriesLabels returns the stored labels of a device's series of the given
// metrics, as metric -> instance -> label (unlabelled series are left out).
func (m *MetricsStore) SeriesLabels(ctx context.Context, deviceID uuid.UUID, metrics []string) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	if len(metrics) == 0 {
		return out, nil
	}
	var rows []struct{ Metric, Instance, Label string }
	err := m.db.WithContext(ctx).Raw(`SELECT metric, instance, label FROM metrics.series
		WHERE device_id = ? AND metric IN ? AND label <> ''`, deviceID, metrics).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("reading series labels: %w", err)
	}
	for _, r := range rows {
		if out[r.Metric] == nil {
			out[r.Metric] = map[string]string{}
		}
		out[r.Metric][r.Instance] = r.Label
	}
	return out, nil
}

// ---- Retention and maintenance ---------------------------------------------

// ApplyRetention replaces the raw-samples retention policy. days must be
// within [models.MinMetricsRawRetentionDays, models.MaxMetricsRawRetentionDays]:
// unlike the rest of this file's settings-backed knobs, a bad value here does
// not just get clamped for one caller — it becomes the policy every future
// read and query relies on, so it is rejected outright instead.
func (m *MetricsStore) ApplyRetention(ctx context.Context, days int) error {
	if days < models.MinMetricsRawRetentionDays || days > models.MaxMetricsRawRetentionDays {
		return fmt.Errorf("retention days %d out of range [%d, %d]",
			days, models.MinMetricsRawRetentionDays, models.MaxMetricsRawRetentionDays)
	}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT remove_retention_policy('metrics.samples', if_exists => true)`).Error; err != nil {
			return fmt.Errorf("removing retention policy: %w", err)
		}
		if err := tx.Exec(`SELECT add_retention_policy('metrics.samples', make_interval(days => ?))`, days).Error; err != nil {
			return fmt.Errorf("adding retention policy: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	m.retMu.Lock()
	m.retentionDays = days
	m.retMu.Unlock()
	return nil
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

// cleanupBatchSize bounds how many series ids go in one "IN (...)" list.
// Postgres rejects a statement with more than 65535 bind parameters; a
// single large deletion (about 150 48-port devices, each with several
// metrics per port) can queue far more series than that, so the queue is
// drained in batches well under the limit instead of in one shot.
const cleanupBatchSize = 1000

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

	removedAny := false
	for {
		var batch []int64
		if err := db.Raw(`SELECT series_id FROM metrics.deleted_series ORDER BY series_id LIMIT ?`,
			cleanupBatchSize).Scan(&batch).Error; err != nil {
			return res, fmt.Errorf("reading the deleted-series queue: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		r := db.Exec(`DELETE FROM metrics.series WHERE id IN ?`, batch)
		if r.Error != nil {
			return res, fmt.Errorf("deleting orphan series: %w", r.Error)
		}
		res.SeriesRemoved += r.RowsAffected
		r = db.Exec(`DELETE FROM metrics.samples WHERE series_id IN ?`, batch)
		if r.Error != nil {
			return res, fmt.Errorf("deleting samples of removed series: %w", r.Error)
		}
		res.SamplesRemoved += r.RowsAffected
		if err := db.Exec(`DELETE FROM metrics.deleted_series WHERE series_id IN ?`, batch).Error; err != nil {
			return res, fmt.Errorf("clearing the deleted-series queue: %w", err)
		}
		removedAny = true
		if len(batch) < cleanupBatchSize {
			break
		}
	}
	if removedAny {
		m.mu.Lock()
		m.ids = map[seriesKey]cachedSeries{}
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
