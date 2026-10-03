package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func refreshRollups(t *testing.T, db *gorm.DB) {
	t.Helper()
	testdb.Exec(t, db, `CALL refresh_continuous_aggregate('metrics.samples_5m', NULL, NULL)`)
	testdb.Exec(t, db, `CALL refresh_continuous_aggregate('metrics.samples_1h', NULL, NULL)`)
}

func TestDBMetricsWriteAndQueryRaw(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Minute)
	for i := 0; i < 3; i++ {
		at := now.Add(time.Duration(i-3) * time.Minute)
		testdb.Must(t, m.Write(ctx, s.DeviceID, at, []SamplePoint{
			{Metric: MetricIfInBps, Instance: "1", Value: float64(100 * (i + 1))},
			{Metric: MetricIfInBps, Instance: "2", Value: 10},
		}))
	}
	res, err := m.Query(ctx, MetricsQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Metrics: []string{MetricIfInBps},
		From: now.Add(-time.Hour), To: now})
	if err != nil || res.Resolution != "raw" || len(res.Series) != 2 {
		t.Fatalf("query: %+v %v", res, err)
	}
	p := res.Series[0].Points
	if res.Series[0].Instance != "1" || len(p) != 3 || p[2].Avg != 300 {
		t.Errorf("instance 1 points: %+v", res.Series[0])
	}

	sum, err := m.Query(ctx, MetricsQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Metrics: []string{MetricIfInBps},
		From: now.Add(-time.Hour), To: now, Sum: true})
	if err != nil || len(sum.Series) != 1 || sum.Series[0].Points[2].Avg != 310 {
		t.Errorf("sum: %+v %v", sum, err)
	}
	avg, err := m.Query(ctx, MetricsQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Metrics: []string{MetricIfInBps},
		From: now.Add(-time.Hour), To: now, Sum: true, Average: []string{MetricIfInBps}})
	if err != nil || len(avg.Series) != 1 || avg.Series[0].Points[2].Avg != 155 || avg.Series[0].Points[2].Max != 155 {
		t.Errorf("sum with Average: %+v %v, want the last point averaged to 155", avg, err)
	}
	per, err := m.Query(ctx, MetricsQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Metrics: []string{MetricIfInBps},
		From: now.Add(-time.Hour), To: now, Sum: true, PerDevice: true})
	if err != nil || len(per.Series) != 1 || per.Series[0].DeviceID == nil || *per.Series[0].DeviceID != s.DeviceID ||
		per.Series[0].Instance != "" || per.Series[0].Points[2].Avg != 310 {
		t.Errorf("sum per device: %+v %v, want one series for the device with 310", per, err)
	}

	// An unknown metric (a custom key the registry does not know, e.g.
	// right after a restore) is skipped, not allowed to sink the batch.
	if err := m.Write(ctx, s.DeviceID, now, []SamplePoint{{Metric: "bogus", Instance: "1", Value: 1},
		{Metric: MetricIfInBps, Instance: "7", Value: 42}}); err != nil {
		t.Errorf("a batch with an unknown metric failed: %v", err)
	}
	var bogus, kept int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.series WHERE metric = 'bogus'`).Scan(&bogus).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples x JOIN metrics.series s ON s.id = x.series_id
		WHERE s.metric = ? AND s.instance = '7'`, MetricIfInBps).Scan(&kept).Error)
	if bogus != 0 || kept != 1 {
		t.Errorf("unknown metric: %d bogus series, %d known samples kept", bogus, kept)
	}
}

// Rollups keep min/max/sum/count, and the hourly average is weighted, not an
// average of averages.
func TestDBMetricsRollups(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	ctx := context.Background()
	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	// Bucket A (5 samples of 10) and bucket B (1 sample of 70).
	for i := 0; i < 5; i++ {
		testdb.Must(t, m.Write(ctx, s.DeviceID, base.Add(time.Duration(i)*time.Minute),
			[]SamplePoint{{Metric: MetricIfInUtilPct, Instance: "1", Value: 10}}))
	}
	testdb.Must(t, m.Write(ctx, s.DeviceID, base.Add(5*time.Minute), []SamplePoint{{Metric: MetricIfInUtilPct, Instance: "1", Value: 70}}))
	refreshRollups(t, db)

	var hour struct {
		Vmin, Vmax, Vsum float64
		N                int
	}
	testdb.Must(t, db.Raw(`SELECT vmin, vmax, vsum, n FROM metrics.samples_1h WHERE bucket = ?`, base).Scan(&hour).Error)
	if hour.Vmin != 10 || hour.Vmax != 70 || hour.Vsum != 120 || hour.N != 6 {
		t.Fatalf("hourly rollup %+v", hour)
	}

	res, err := m.Query(ctx, MetricsQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Metrics: []string{MetricIfInUtilPct},
		From: base.Add(-24 * time.Hour), To: base.Add(24 * time.Hour)})
	if err != nil || res.Resolution != "5m" {
		t.Fatalf("%+v %v", res, err)
	}
	if len(res.Series) != 1 || len(res.Series[0].Points) == 0 {
		t.Fatalf("no points from the 5-minute rollup: %+v", res)
	}
	// A 48 h range steps by 10 minutes: one step holds all six samples, so
	// avg = 120/6 = 20.
	if res.Series[0].Points[0].Avg != 20 {
		t.Errorf("weighted avg %v, want 20 (not (10+70)/2 = 40)", res.Series[0].Points[0].Avg)
	}
}

func TestDBMetricsRetentionSetting(t *testing.T) {
	db := testdb.Open(t)
	m := NewMetricsStore(db)
	ctx := context.Background()
	testdb.Must(t, m.ApplyRetention(ctx, 30))
	days, err := m.RetentionDays(ctx)
	if err != nil || days != 30 {
		t.Fatalf("retention %d %v", days, err)
	}

	// A retention of 0 (or anything outside the settings bounds) would drop
	// every raw chunk; it must be rejected, leaving the policy unchanged.
	if err := m.ApplyRetention(ctx, 0); err == nil {
		t.Error("ApplyRetention(0) should be rejected")
	}
	days, err = m.RetentionDays(ctx)
	if err != nil || days != 30 {
		t.Fatalf("retention after a rejected update: %d %v, want unchanged 30", days, err)
	}
}

// A short window entirely older than the raw retention must still read the
// 5-minute rollup (kept forever), not the raw table (whose old chunks the
// retention policy has already dropped, or soon will).
func TestDBMetricsQueryOldShortRangeUsesRollups(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	ctx := context.Background()
	testdb.Must(t, m.ApplyRetention(ctx, 7))

	old := time.Now().UTC().Add(-10 * 24 * time.Hour).Truncate(time.Minute)
	for i := 0; i < 3; i++ {
		testdb.Must(t, m.Write(ctx, s.DeviceID, old.Add(time.Duration(i)*time.Minute),
			[]SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: float64(100 * (i + 1))}}))
	}
	refreshRollups(t, db)

	res, err := m.Query(ctx, MetricsQuery{DeviceIDs: []uuid.UUID{s.DeviceID}, Metrics: []string{MetricIfInBps},
		From: old.Add(-30 * time.Minute), To: old.Add(30 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Resolution != "5m" {
		t.Fatalf("resolution %q, want 5m", res.Resolution)
	}
	if len(res.Series) == 0 || len(res.Series[0].Points) == 0 {
		t.Fatalf("no points from the rollup for an old short range: %+v", res)
	}
}

// Deleting a device removes its series at once and its samples at the next
// cleanup; the cleanup also catches series whose device vanished another way.
func TestDBDeviceDeleteAndCleanup(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	ctx := context.Background()
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 1}}))
	orphan := uuid.New()
	testdb.Must(t, m.Write(ctx, orphan, time.Now().UTC(), []SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 1}}))

	devices := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	if _, err := devices.Delete(ctx, s.DeviceID); err != nil {
		t.Fatal(err)
	}
	var series, queued, samples int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.series WHERE device_id = ?`, s.DeviceID).Scan(&series).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.deleted_series`).Scan(&queued).Error)
	if series != 0 || queued != 1 {
		t.Fatalf("after delete: %d series, %d queued", series, queued)
	}

	res, err := m.Cleanup(ctx, 365)
	if err != nil || res.SeriesRemoved != 1 || res.SamplesRemoved != 2 {
		t.Fatalf("cleanup %+v %v", res, err)
	}
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples`).Scan(&samples).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.deleted_series`).Scan(&queued).Error)
	if samples != 0 || queued != 0 {
		t.Errorf("after cleanup: %d samples, %d queued", samples, queued)
	}
}

// M7: a DeviceService wired to the MetricsStore it shares with the poller
// forgets a deleted device's cached series ids, so a device id reused by a
// restored backup resolves a fresh series instead of writing new samples
// under an id already queued for the nightly cleanup to remove (which would
// silently discard them the next time Cleanup runs).
func TestDBDeviceDeleteForgetsMetricsStoreCache(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	m := NewMetricsStore(db)
	ctx := context.Background()
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 1}}))

	devices := NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db))
	devices.SetMetricsStore(m)
	if _, err := devices.Delete(ctx, s.DeviceID); err != nil {
		t.Fatal(err)
	}

	// The device id is reused, as a restore from backup would do, and
	// written to again through the same MetricsStore instance the poller
	// would use.
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host) VALUES (?, ?, ?, 'restored', '10.0.0.2')`,
		s.DeviceID, s.SiteID, s.CredentialID)
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 2}}))

	var seriesID int64
	testdb.Must(t, db.Raw(`SELECT id FROM metrics.series WHERE device_id = ? AND metric = ? AND instance = '1'`,
		s.DeviceID, MetricIfInBps).Scan(&seriesID).Error)
	var sampleCount int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples WHERE series_id = ?`, seriesID).Scan(&sampleCount).Error)
	if sampleCount != 1 {
		t.Fatalf("restored device's write landed on a stale cached series id: %d samples on series %d", sampleCount, seriesID)
	}
}

// The deleted-series queue is processed in batches: Postgres rejects more
// than 65535 bind parameters in one "IN (...)" list, so a queue larger than
// one batch must still be fully drained in one Cleanup call.
func TestDBMetricsCleanupBatchesLargeQueue(t *testing.T) {
	db := testdb.Open(t)
	m := NewMetricsStore(db)
	ctx := context.Background()
	device := uuid.New()

	// 1,500 series (more than one 1,000-row batch), one sample apiece, all
	// queued for deletion up front via bulk SQL.
	testdb.Exec(t, db, `INSERT INTO metrics.series (id, device_id, metric, instance)
		SELECT 5000000 + g, ?, 'if_in_bps', g::text FROM generate_series(1, 1500) g`, device)
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT now(), 5000000 + g, 1 FROM generate_series(1, 1500) g`)
	testdb.Exec(t, db, `INSERT INTO metrics.deleted_series (series_id)
		SELECT 5000000 + g FROM generate_series(1, 1500) g`)

	res, err := m.Cleanup(ctx, 365)
	if err != nil {
		t.Fatal(err)
	}
	if res.SeriesRemoved != 1500 || res.SamplesRemoved != 1500 {
		t.Fatalf("cleanup %+v", res)
	}
	var queued int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.deleted_series`).Scan(&queued).Error)
	if queued != 0 {
		t.Errorf("%d series left queued after cleanup", queued)
	}
}

func TestDBUpdateUsualSpeeds(t *testing.T) {
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.2")
	long := seedPort(t, db, s.DeviceID, 1, "0/1", "")
	short := seedPort(t, db, s.DeviceID, 2, "0/2", "")
	m := NewMetricsStore(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(5 * time.Minute)

	// Port 1: 30 hours at 1 Gb/s with an hour at 100 Mb/s. Port 2: 2 hours.
	for at := now.Add(-30 * time.Hour); at.Before(now); at = at.Add(5 * time.Minute) {
		speed := 1e9
		if at.After(now.Add(-2*time.Hour)) && at.Before(now.Add(-time.Hour)) {
			speed = 1e8
		}
		points := []SamplePoint{{Metric: MetricIfSpeedBps, Instance: "1", InterfaceID: &long, Value: speed}}
		if at.After(now.Add(-2 * time.Hour)) {
			points = append(points, SamplePoint{Metric: MetricIfSpeedBps, Instance: "2", InterfaceID: &short, Value: 1e9})
		}
		testdb.Must(t, m.Write(ctx, s.DeviceID, at, points))
	}
	refreshRollups(t, db)
	if _, err := m.UpdateUsualSpeeds(ctx, now); err != nil {
		t.Fatal(err)
	}
	var usual []struct {
		IfIndex       int
		UsualSpeedBps *int64
	}
	testdb.Must(t, db.Raw(`SELECT if_index, usual_speed_bps FROM device_interfaces WHERE device_id = ? ORDER BY if_index`, s.DeviceID).Scan(&usual).Error)
	if usual[0].UsualSpeedBps == nil || *usual[0].UsualSpeedBps != 1_000_000_000 {
		t.Errorf("port 1 usual %v", usual[0].UsualSpeedBps)
	}
	if usual[1].UsualSpeedBps != nil {
		t.Errorf("port 2 has 2 h of history, must stay unset: %v", *usual[1].UsualSpeedBps)
	}
}
