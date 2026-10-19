package services

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/l2"
)

// NetworkMaintenanceHour is when the nightly network maintenance runs, local
// time: an hour after the incident purge (PurgeHour).
const NetworkMaintenanceHour = 3

// NetworkMaintenance is the nightly housekeeping of the metrics store:
// removing deleted devices' samples and old port events, recomputing each
// port's usual speed, and dropping L2 rows not seen for 30 days.
type NetworkMaintenance struct {
	metrics  *MetricsStore
	settings *SettingsService
	l2       *L2Monitor
	now      func() time.Time
	logger   *log.Logger
}

func NewNetworkMaintenance(metrics *MetricsStore, settings *SettingsService) *NetworkMaintenance {
	return &NetworkMaintenance{metrics: metrics, settings: settings, now: time.Now, logger: log.Default()}
}

// SetL2 adds the L2 purge to the nightly pass.
func (n *NetworkMaintenance) SetL2(m *L2Monitor) { n.l2 = m }

// RunOnce does one pass. Every part runs even if an earlier one fails.
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
	var l2Err error
	if n.l2 != nil {
		var purged int64
		purged, l2Err = n.l2.Purge(ctx, n.now().UTC().AddDate(0, 0, -l2.RetentionDays))
		if l2Err != nil {
			n.logger.Printf("[l2] purge: %v", l2Err)
		} else if purged > 0 {
			n.logger.Printf("[l2] purge removed %d rows", purged)
		}
	}
	return errors.Join(cleanErr, speedErr, l2Err)
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
