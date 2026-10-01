package services

import (
	"context"
	"errors"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// fakeRetentionStore is a networkRetentionStore that can be made to fail on
// command, so NetworkSettingsService.Update's ordering (apply the policy
// before saving the setting) can be tested without depending on a real
// TimescaleDB retention policy.
type fakeRetentionStore struct {
	have     int
	applied  []int
	failNext bool
}

func (f *fakeRetentionStore) RetentionDays(context.Context) (int, error) { return f.have, nil }

func (f *fakeRetentionStore) ApplyRetention(_ context.Context, days int) error {
	if f.failNext {
		f.failNext = false
		return errors.New("timescaledb: policy update failed")
	}
	f.applied = append(f.applied, days)
	f.have = days
	return nil
}

func intPtr(n int) *int { return &n }

// A failed ApplyRetention must leave the stored setting untouched, and a
// retry with the same value must try again rather than being skipped as a
// no-op: before this fix, Update compared the new value against the stored
// setting, which SetInt had already updated on the first (failed) attempt.
func TestDBNetworkSettingsAppliesRetentionBeforeSaving(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	settings := NewSettingsService(db)
	fake := &fakeRetentionStore{have: models.DefaultMetricsRawRetentionDays, failNext: true}
	svc := NewNetworkSettingsService(settings, fake)

	if _, err := svc.Update(ctx, NetworkSettingsPatch{MetricsRawRetentionDays: intPtr(90)}); err == nil {
		t.Fatal("want an error when ApplyRetention fails")
	}
	if got := settings.MetricsRawRetentionDays(ctx); got != models.DefaultMetricsRawRetentionDays {
		t.Errorf("setting changed despite the failed policy update: %d", got)
	}
	if len(fake.applied) != 0 {
		t.Errorf("ApplyRetention should not have succeeded: %v", fake.applied)
	}

	got, err := svc.Update(ctx, NetworkSettingsPatch{MetricsRawRetentionDays: intPtr(90)})
	if err != nil {
		t.Fatal(err)
	}
	if got.MetricsRawRetentionDays != 90 || len(fake.applied) != 1 || fake.applied[0] != 90 {
		t.Errorf("settings %+v, applied %v", got, fake.applied)
	}
	if stored := settings.MetricsRawRetentionDays(ctx); stored != 90 {
		t.Errorf("setting not saved after a successful apply: %d", stored)
	}
}

func TestDBNetworkSettingsUPSThresholds(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	settings := NewSettingsService(db)
	svc := NewNetworkSettingsService(settings, NewMetricsStore(db))
	got := svc.Get(ctx)
	if got.UPSLowBatteryPct != 25 || got.UPSHighLoadPct != 80 {
		t.Fatalf("defaults %+v", got)
	}
	low, high := 30, 90
	got, err := svc.Update(ctx, NetworkSettingsPatch{UPSLowBatteryPct: &low, UPSHighLoadPct: &high})
	if err != nil || got.UPSLowBatteryPct != 30 || got.UPSHighLoadPct != 90 {
		t.Fatalf("update %+v %v", got, err)
	}
	if th := settings.UPSThresholds(ctx); th.LowBatteryPct != 30 || th.HighLoadPct != 90 {
		t.Errorf("thresholds %+v", th)
	}
	bad := 4
	if _, err := svc.Update(ctx, NetworkSettingsPatch{UPSLowBatteryPct: &bad}); err == nil {
		t.Error("4 % accepted")
	}
	for _, m := range UPSMetrics {
		if !KnownMetric(m) {
			t.Errorf("%s not in the catalogue", m)
		}
	}
}
