package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBProfilesForDevice(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewProfileService(db)
	testdb.Must(t, svc.SeedStarter(ctx))
	testdb.Must(t, svc.SeedStarter(ctx)) // once only
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	testdb.Exec(t, db, `UPDATE devices SET sys_object_id = '1.3.6.1.4.1.9.1.1208' WHERE id = ?`, s.DeviceID)
	var d models.Device
	testdb.Must(t, db.First(&d, "id = ?", s.DeviceID).Error)

	got, err := svc.ProfilesForDevice(ctx, d)
	testdb.Must(t, err)
	if len(got) != 1 || got[0].Profile.Name != "Cisco switch health" || len(got[0].Metrics) != 9 {
		t.Fatalf("profiles %+v", got)
	}
	testdb.Must(t, svc.SetDeviceProfile(ctx, d.ID, got[0].Profile.ID, "detach"))
	if got, _ := svc.ProfilesForDevice(ctx, d); len(got) != 0 {
		t.Fatalf("detached profile still applies")
	}
	views, err := svc.DeviceProfiles(ctx, d)
	testdb.Must(t, err)
	if len(views) != 1 || views[0].Applies || !views[0].Matched || views[0].Mode != "detach" {
		t.Errorf("views %+v", views)
	}
	testdb.Must(t, svc.SetDeviceProfile(ctx, d.ID, got0(t, svc, ctx).ID, "auto"))
	if got, _ := svc.ProfilesForDevice(ctx, d); len(got) != 1 {
		t.Errorf("auto did not restore the match")
	}
	testdb.Must(t, svc.SaveRun(ctx, d.ID, got0(t, svc, ctx).ID, time.Now().UTC(), false, "timeout"))
	views, _ = svc.DeviceProfiles(ctx, d)
	if views[0].LastRun == nil || views[0].LastRun.OK || views[0].LastRun.Error != "timeout" {
		t.Errorf("last run %+v", views[0].LastRun)
	}
}

func got0(t *testing.T, svc *ProfileService, ctx context.Context) models.MetricProfile {
	t.Helper()
	list, err := svc.List(ctx)
	testdb.Must(t, err)
	return list[0].MetricProfile
}

func TestDBProfileCopyDeleteAndRegistry(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewProfileService(db)
	testdb.Must(t, svc.SeedStarter(ctx))
	testdb.Must(t, svc.Load(ctx))
	if !KnownMetric("cisco_cpu_5min") {
		t.Fatal("starter key not registered")
	}
	starter := got0(t, svc, ctx)
	if _, err := svc.Delete(ctx, starter.ID); !errors.Is(err, ErrProfileBuiltin) {
		t.Errorf("deleting built-in: %v", err)
	}
	cp, err := svc.Copy(ctx, starter.ID)
	testdb.Must(t, err)
	if cp.Builtin || cp.Name != "Cisco switch health (copy)" || len(cp.Metrics) != 9 || cp.Metrics[0].Key == "cisco_cpu_5min" {
		t.Fatalf("copy %+v", cp.MetricProfile)
	}
	if _, err := svc.Copy(ctx, starter.ID); err != nil { // second copy: unique name and keys
		t.Fatalf("second copy: %v", err)
	}
	// Deleting a metric queues its history.
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	m := NewMetricsStore(db)
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: cp.Metrics[0].Key, Instance: "1", Value: 5}}))
	if n, _ := svc.MetricDataDevices(ctx, cp.Metrics[0].Key); n != 1 {
		t.Errorf("data devices %d", n)
	}
	_, err = svc.DeleteMetric(ctx, cp.Metrics[0].ID)
	testdb.Must(t, err)
	var queued int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.deleted_series`).Scan(&queued).Error)
	if queued != 1 || KnownMetric(cp.Metrics[0].Key) {
		t.Errorf("queued %d, still known %v", queued, KnownMetric(cp.Metrics[0].Key))
	}
	if _, _, err := svc.UpdateMetric(ctx, cp.Metrics[1].ID, withKey(cp.Metrics[1], "renamed_key")); !errors.Is(err, ErrMetricKeyImmutable) {
		t.Errorf("key change: %v", err)
	}
	_ = uuid.Nil
}

func withKey(m models.ProfileMetric, k string) models.ProfileMetric { m.Key = k; return m }

// A name or key Copy would pick can already be taken by a row that never
// went through Copy itself (not just a previous copy): Copy must still find
// the next free one.
func TestDBProfileCopySkipsExistingNameAndKey(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewProfileService(db)
	testdb.Must(t, svc.SeedStarter(ctx))
	starter := got0(t, svc, ctx)

	conflictID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO metric_profiles (id, name) VALUES (?, ?)`, conflictID, "Cisco switch health (copy)")
	testdb.Exec(t, db, `INSERT INTO profile_metrics (id, profile_id, name, key, source, kind, oid) VALUES (?, ?, ?, ?, 'column', 'gauge', ?)`,
		uuid.New(), conflictID, "CPU busy (5 min)", "cisco_cpu_5min_copy", "1.3.6.1.4.1.9.9.109.1.1.1.1.8")

	cp, err := svc.Copy(ctx, starter.ID)
	testdb.Must(t, err)
	if cp.Name != "Cisco switch health (copy 2)" || cp.Metrics[0].Key != "cisco_cpu_5min_copy2" {
		t.Errorf("copy name %q, metric key %q", cp.Name, cp.Metrics[0].Key)
	}
}

// Deleting a metric removes its series rows; a MetricsStore wired to the
// ProfileService must forget their cached ids, so a metric re-created with
// the same key writes to a fresh series rather than the deleted one (whose
// samples the nightly cleanup would then remove).
func TestDBDeleteMetricForgetsMetricsStoreCache(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewProfileService(db)
	m := NewMetricsStore(db)
	svc.SetMetricsStore(m)
	testdb.Must(t, svc.SeedStarter(ctx))
	cp, err := svc.Copy(ctx, got0(t, svc, ctx).ID)
	testdb.Must(t, err)
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	metric := cp.Metrics[0]
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: metric.Key, Instance: "1", Value: 5}}))

	_, err = svc.DeleteMetric(ctx, metric.ID)
	testdb.Must(t, err)
	_, err = svc.CreateMetric(ctx, cp.ID, metric)
	testdb.Must(t, err)
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: metric.Key, Instance: "1", Value: 6}}))

	var landed int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples x JOIN metrics.series s ON s.id = x.series_id
		WHERE s.metric = ? AND x.value = 6`, metric.Key).Scan(&landed).Error)
	if landed != 1 {
		t.Fatalf("the re-created metric's sample landed on a deleted series (%d on a live series)", landed)
	}

	// The same holds for deleting the whole profile and copying again
	// (which hands out the same "_copy" keys).
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: cp.Metrics[1].Key, Instance: "1", Value: 7}}))
	_, err = svc.Delete(ctx, cp.ID)
	testdb.Must(t, err)
	cp2, err := svc.Copy(ctx, got0(t, svc, ctx).ID)
	testdb.Must(t, err)
	if cp2.Metrics[1].Key != cp.Metrics[1].Key {
		t.Fatalf("expected the copy to reuse key %q, got %q", cp.Metrics[1].Key, cp2.Metrics[1].Key)
	}
	testdb.Must(t, m.Write(ctx, s.DeviceID, time.Now().UTC(), []SamplePoint{{Metric: cp2.Metrics[1].Key, Instance: "1", Value: 8}}))
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples x JOIN metrics.series s ON s.id = x.series_id
		WHERE s.metric = ? AND x.value = 8`, cp2.Metrics[1].Key).Scan(&landed).Error)
	if landed != 1 {
		t.Fatalf("the re-copied metric's sample landed on a deleted series")
	}
}

// A copy applies to no device until an admin adds prefixes or attaches it:
// keeping the original's prefixes would poll (and alert on) every matching
// device twice.
func TestDBProfileCopyHasNoMatchPrefixes(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewProfileService(db)
	testdb.Must(t, svc.SeedStarter(ctx))
	starter := got0(t, svc, ctx)
	if len(starter.MatchPrefixes) == 0 {
		t.Fatal("the starter should have match prefixes")
	}
	cp, err := svc.Copy(ctx, starter.ID)
	testdb.Must(t, err)
	stored, err := svc.Get(ctx, cp.ID)
	testdb.Must(t, err)
	if len(cp.MatchPrefixes) != 0 || len(stored.MatchPrefixes) != 0 {
		t.Errorf("copy prefixes %v, stored %v", cp.MatchPrefixes, stored.MatchPrefixes)
	}
}

// The built-in is found by builtin = true, not by name: renaming it must not
// make the next start try (and fail) to seed a second one.
func TestDBSeedStarterAfterRename(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	svc := NewProfileService(db)
	testdb.Must(t, svc.SeedStarter(ctx))
	testdb.Exec(t, db, `UPDATE metric_profiles SET name = 'Our Cisco switches' WHERE builtin`)
	testdb.Must(t, svc.SeedStarter(ctx))
	var builtins int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metric_profiles WHERE builtin`).Scan(&builtins).Error)
	if builtins != 1 {
		t.Errorf("%d built-in profiles after a rename and reseed", builtins)
	}
}
