package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// IncidentRetentionService deletes incident history older than the configured
// window.
type IncidentRetentionService struct {
	db       *gorm.DB
	settings *SettingsService
	logger   *log.Logger
}

// NewIncidentRetentionService returns a purge service backed by the given
// database, reading its window from settings on each run so a change takes
// effect on the next nightly pass rather than at the next restart.
func NewIncidentRetentionService(db *gorm.DB, settings *SettingsService) *IncidentRetentionService {
	return &IncidentRetentionService{db: db, settings: settings, logger: log.Default()}
}

// Purge deletes incidents that both started and finished before the cutoff,
// returning how many rows went.
//
// Two conditions, not one. Filtering on start_time alone would delete an
// incident that began before the cutoff and is still running — exactly the
// outage someone is most likely to be looking at. An ongoing incident is never
// removed, however old, because it is still happening.
func (s *IncidentRetentionService) Purge(ctx context.Context) (int64, error) {
	days := s.settings.IncidentRetentionDays(ctx)
	cutoff := time.Now().AddDate(0, 0, -days)

	res := s.db.WithContext(ctx).
		Where("end_time IS NOT NULL AND end_time < ?", cutoff).
		Delete(&models.Incident{})
	if res.Error != nil {
		return 0, fmt.Errorf("purging incidents older than %d days: %w", days, res.Error)
	}
	if res.RowsAffected > 0 {
		s.logger.Printf("[incidents] purged %d incident(s) resolved before %s (retention %d days)",
			res.RowsAffected, cutoff.Format("2006-01-02"), days)
	}
	return res.RowsAffected, nil
}

// nextRunAt returns the next occurrence of the given hour, local time.
func nextRunAt(now time.Time, hour int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// PurgeHour is when the nightly purge runs, local time.
const PurgeHour = 2

// StartPurgeLoop runs Purge every night at PurgeHour.
//
// Scheduled against the wall clock rather than a fixed ticker so it stays at 2
// AM across restarts and daylight-saving changes; a 24-hour ticker started at
// boot would drift to whenever the process last happened to start.
func (s *IncidentRetentionService) StartPurgeLoop(ctx context.Context) {
	s.logger.Printf("[incidents] retention purge scheduled for %02d:00 daily", PurgeHour)
	for {
		wait := time.Until(nextRunAt(time.Now(), PurgeHour))
		select {
		case <-ctx.Done():
			s.logger.Println("[incidents] retention purge stopped")
			return
		case <-time.After(wait):
		}
		// Bounded so a slow delete cannot hold the loop past tomorrow's run.
		runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		if _, err := s.Purge(runCtx); err != nil {
			// Logged, never fatal: losing a night's purge is a housekeeping
			// miss, not a reason to take the process down.
			s.logger.Printf("[incidents] purge failed: %v", err)
		}
		cancel()
	}
}
