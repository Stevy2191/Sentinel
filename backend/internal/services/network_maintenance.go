package services

import (
	"context"
	"errors"
	"log"
	"time"
)

// NetworkMaintenanceHour is when the nightly network maintenance runs, local
// time: an hour after the incident purge (PurgeHour).
const NetworkMaintenanceHour = 3

// NetworkMaintenance is the nightly housekeeping of the metrics store:
// removing deleted devices' samples and old port events, and recomputing each
// port's usual speed.
type NetworkMaintenance struct {
	metrics  *MetricsStore
	settings *SettingsService
	now      func() time.Time
	logger   *log.Logger
}

func NewNetworkMaintenance(metrics *MetricsStore, settings *SettingsService) *NetworkMaintenance {
	return &NetworkMaintenance{metrics: metrics, settings: settings, now: time.Now, logger: log.Default()}
}

// RunOnce does one pass. Both halves run even if the first fails.
func (n *NetworkMaintenance) RunOnce(ctx context.Context) error {
	res, cleanErr := n.metrics.Cleanup(ctx, n.settings.MetricsRawRetentionDays(ctx))
	if cleanErr != nil {
		n.logger.Printf("[metrics] cleanup: %v", cleanErr)
	} else if res.SeriesRemoved+res.SamplesRemoved+res.EventsRemoved > 0 {
		n.logger.Printf("[metrics] cleanup removed %d series, %d samples, %d port events",
			res.SeriesRemoved, res.SamplesRemoved, res.EventsRemoved)
	}
	updated, speedErr := n.metrics.UpdateUsualSpeeds(ctx, n.now().UTC())
	if speedErr != nil {
		n.logger.Printf("[metrics] usual speeds: %v", speedErr)
	} else if updated > 0 {
		n.logger.Printf("[metrics] usual speed set for %d ports", updated)
	}
	return errors.Join(cleanErr, speedErr)
}

// Start runs a pass five minutes after startup, then nightly, until ctx ends.
func (n *NetworkMaintenance) Start(ctx context.Context) {
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = n.RunOnce(ctx)
			timer.Reset(time.Until(nextRunAt(time.Now(), NetworkMaintenanceHour)))
		}
	}
}
