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

// checkPurgeBatch is how many check rows one DELETE removes.
//
// Batched because this table is the largest in the schema by a wide margin: a
// single monitor on a one-minute interval adds over half a million rows a
// year, so the first purge after this was introduced can face millions at
// once. One unbounded DELETE would hold a long transaction and bloat WAL;
// batches keep each statement short and let the loop yield.
const checkPurgeBatch = 10000

// PurgeChecks deletes check results older than the configured window.
//
// The checks table had no retention at all, so it grew without limit — the
// dashboard and reports scan it by time range, and every row ever recorded
// stayed behind those scans forever.
//
// Deliberately independent of incident retention. A check is a measurement and
// an incident is a conclusion drawn from a run of them; keeping the raw
// measurements as long as the conclusions would be far more expensive and buys
// little, so the two have separate windows.
//
// This bounds growth rather than shrinking the table on disk. Postgres marks
// the rows dead and autovacuum makes that space available for reuse, so the
// file stops growing, but it is not handed back to the filesystem without a
// VACUUM FULL — which takes an exclusive lock and is not something a
// background job should do behind an operator's back.
func (s *IncidentRetentionService) PurgeChecks(ctx context.Context) (int64, error) {
	days := s.settings.CheckRetentionDays(ctx)
	cutoff := time.Now().AddDate(0, 0, -days)

	var total int64
	for {
		if err := ctx.Err(); err != nil {
			// Partial progress is kept: the rows are gone and the next run
			// picks up where this one stopped.
			return total, fmt.Errorf("purging checks: %w", err)
		}
		res := s.db.WithContext(ctx).Exec(
			`DELETE FROM checks WHERE ctid IN (
			     SELECT ctid FROM checks WHERE timestamp < ? LIMIT ?)`,
			cutoff, checkPurgeBatch)
		if res.Error != nil {
			return total, fmt.Errorf("purging checks older than %d days: %w", days, res.Error)
		}
		total += res.RowsAffected
		if res.RowsAffected < checkPurgeBatch {
			break
		}
	}

	if total > 0 {
		s.logger.Printf("[checks] purged %d check(s) recorded before %s (retention %d days)",
			total, cutoff.Format("2006-01-02"), days)
	}
	return total, nil
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
	s.logger.Printf("[retention] incident and check purge scheduled for %02d:00 daily", PurgeHour)
	for {
		wait := time.Until(nextRunAt(time.Now(), PurgeHour))
		select {
		case <-ctx.Done():
			s.logger.Println("[incidents] retention purge stopped")
			return
		case <-time.After(wait):
		}
		// Bounded so a slow delete cannot hold the loop past tomorrow's run.
		runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		if _, err := s.Purge(runCtx); err != nil {
			// Logged, never fatal: losing a night's purge is a housekeeping
			// miss, not a reason to take the process down.
			s.logger.Printf("[incidents] purge failed: %v", err)
		}
		// Runs even when the incident purge failed: they are independent, and
		// the checks table is the one that actually grows without bound.
		if _, err := s.PurgeChecks(runCtx); err != nil {
			s.logger.Printf("[checks] purge failed: %v", err)
		}
		cancel()
	}
}
