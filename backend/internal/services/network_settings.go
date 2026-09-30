package services

import (
	"context"
	"fmt"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// NetworkSettings are the network section's instance-wide settings.
type NetworkSettings struct {
	MetricsRawRetentionDays  int `json:"metrics_raw_retention_days"`
	PortErrorThresholdPerMin int `json:"port_error_threshold_per_min"`
	PortUtilThresholdPct     int `json:"port_util_threshold_pct"`
	PortDownGraceSeconds     int `json:"port_down_grace_seconds"`
}

// NetworkSettingsPatch changes only the fields present.
type NetworkSettingsPatch struct {
	MetricsRawRetentionDays  *int `json:"metrics_raw_retention_days"`
	PortErrorThresholdPerMin *int `json:"port_error_threshold_per_min"`
	PortUtilThresholdPct     *int `json:"port_util_threshold_pct"`
	PortDownGraceSeconds     *int `json:"port_down_grace_seconds"`
}

type NetworkSettingsService struct {
	settings *SettingsService
	metrics  *MetricsStore
}

func NewNetworkSettingsService(settings *SettingsService, metrics *MetricsStore) *NetworkSettingsService {
	return &NetworkSettingsService{settings: settings, metrics: metrics}
}

func (s *NetworkSettingsService) Get(ctx context.Context) NetworkSettings {
	th := s.settings.PortThresholds(ctx)
	return NetworkSettings{
		MetricsRawRetentionDays:  s.settings.MetricsRawRetentionDays(ctx),
		PortErrorThresholdPerMin: int(th.ErrorsPerMin),
		PortUtilThresholdPct:     int(th.UtilPct),
		PortDownGraceSeconds:     int(th.DownGrace.Seconds()),
	}
}

// Update validates and saves the patch. A new retention replaces the
// TimescaleDB policy at once.
func (s *NetworkSettingsService) Update(ctx context.Context, p NetworkSettingsPatch) (NetworkSettings, error) {
	fields := []struct {
		v          *int
		key, label string
		min, max   int
	}{
		{p.MetricsRawRetentionDays, models.SettingMetricsRawRetentionDays, "Detailed history",
			models.MinMetricsRawRetentionDays, models.MaxMetricsRawRetentionDays},
		{p.PortErrorThresholdPerMin, models.SettingPortErrorThresholdPerMin, "The error threshold",
			models.MinPortErrorThresholdPerMin, models.MaxPortErrorThresholdPerMin},
		{p.PortUtilThresholdPct, models.SettingPortUtilThresholdPct, "The busy threshold",
			models.MinPortUtilThresholdPct, models.MaxPortUtilThresholdPct},
		{p.PortDownGraceSeconds, models.SettingPortDownGraceSeconds, "The down grace period",
			models.MinPortDownGraceSeconds, models.MaxPortDownGraceSeconds},
	}
	for _, f := range fields {
		if f.v != nil && (*f.v < f.min || *f.v > f.max) {
			return NetworkSettings{}, fmt.Errorf("%s must be between %d and %d", f.label, f.min, f.max)
		}
	}
	before := s.settings.MetricsRawRetentionDays(ctx)
	for _, f := range fields {
		if f.v != nil {
			if err := s.settings.SetInt(ctx, f.key, *f.v); err != nil {
				return NetworkSettings{}, fmt.Errorf("saving %s: %w", f.key, err)
			}
		}
	}
	if p.MetricsRawRetentionDays != nil && *p.MetricsRawRetentionDays != before {
		if err := s.metrics.ApplyRetention(ctx, *p.MetricsRawRetentionDays); err != nil {
			return NetworkSettings{}, err
		}
	}
	return s.Get(ctx), nil
}

// EnsureRetention makes the TimescaleDB policy match the setting (run at
// startup: a restored config may carry a different retention).
func (s *NetworkSettingsService) EnsureRetention(ctx context.Context) error {
	want := s.settings.MetricsRawRetentionDays(ctx)
	have, err := s.metrics.RetentionDays(ctx)
	if err != nil {
		return err
	}
	if have == want {
		return nil
	}
	return s.metrics.ApplyRetention(ctx, want)
}
