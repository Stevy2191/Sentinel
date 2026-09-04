// Package services - report_scheduler.go runs report schedules on a cron and
// delivers the results by email.
package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// scheduledRunTimeout bounds one scheduled run so a hung SMTP server or a slow
// aggregation cannot pin a cron worker indefinitely.
const scheduledRunTimeout = 5 * time.Minute

// cronParser accepts standard five-field expressions plus the @every/@daily
// descriptors, matching what the API documents.
var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// ReportSchedulerService owns the cron runner and the set of registered jobs.
type ReportSchedulerService struct {
	db        *gorm.DB
	cron      *cron.Cron
	jobs      *CronJobManager
	generator *ReportGenerator
	mailer    *ReportMailer
	logger    *log.Logger
}

// NewReportSchedulerService returns a scheduler bound to its dependencies.
func NewReportSchedulerService(db *gorm.DB, generator *ReportGenerator, mailer *ReportMailer) *ReportSchedulerService {
	c := cron.New(cron.WithParser(cronParser))
	return &ReportSchedulerService{
		db:        db,
		cron:      c,
		jobs:      NewCronJobManager(c, cronParser),
		generator: generator,
		mailer:    mailer,
		logger:    log.Default(),
	}
}

// Start loads every active schedule and begins running them.
func (s *ReportSchedulerService) Start(ctx context.Context) error {
	// cron entry ids are local to a cron runner and restart at 1, so any value
	// left by a previous process now points at a different schedule's job.
	// Clear them all before registering, so a non-null cron_entry_id always
	// belongs to this process.
	if err := s.db.WithContext(ctx).Model(&models.ReportSchedule{}).
		Where("cron_entry_id IS NOT NULL").
		Update("cron_entry_id", nil).Error; err != nil {
		s.logger.Printf("[report-scheduler] could not clear stale cron entry ids: %v", err)
	}

	var schedules []models.ReportSchedule
	if err := s.db.WithContext(ctx).Where("is_active = ?", true).Find(&schedules).Error; err != nil {
		return fmt.Errorf("loading report schedules: %w", err)
	}

	registered := 0
	for i := range schedules {
		if err := s.Register(&schedules[i]); err != nil {
			// One bad schedule must not stop the others from running.
			s.logger.Printf("[report-scheduler] schedule %s not registered: %v", schedules[i].ID, err)
			continue
		}
		registered++
	}

	s.cron.Start()
	s.logger.Printf("[report-scheduler] started with %d of %d active schedules", registered, len(schedules))
	return nil
}

// Stop halts the cron runner and waits for any in-flight run to finish.
func (s *ReportSchedulerService) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.logger.Println("[report-scheduler] stopped")
}

// Register adds or replaces a schedule's cron job and records its next run.
// Calling it again for the same schedule replaces the previous entry, which is
// what makes editing a schedule take effect without a restart.
func (s *ReportSchedulerService) Register(schedule *models.ReportSchedule) error {
	if err := schedule.Validate(); err != nil {
		return err
	}
	expr, err := CronExpressionFor(schedule.ScheduleType, schedule.CronExpression)
	if err != nil {
		return err
	}

	// Copy the identifier rather than capturing the caller's pointer: the row is
	// reloaded at run time so an edit made since registration is honoured.
	id := schedule.ID
	entryID, err := s.jobs.AddJob(id.String(), expr, func() {
		if runErr := s.RunSchedule(context.Background(), id); runErr != nil {
			s.logger.Printf("[report-scheduler] scheduled run of %s failed: %v", id, runErr)
		}
	})
	if err != nil {
		return err
	}

	next, err := s.jobs.GetNextRunTime(expr)
	if err != nil {
		// Unreachable in practice - AddJob parsed the same expression - but the
		// job is rolled back rather than left running with no recorded next run.
		s.jobs.RemoveJob(id.String())
		return err
	}

	if err := s.db.Model(&models.ReportSchedule{}).Where("id = ?", id).
		Updates(map[string]interface{}{
			"cron_entry_id": entryID,
			"next_run_at":   next,
			"updated_at":    time.Now(),
		}).Error; err != nil {
		// Leave no orphan job running for a schedule whose state could not be
		// recorded.
		s.jobs.RemoveJob(id.String())
		return fmt.Errorf("recording cron registration for %s: %w", id, err)
	}

	s.logger.Printf("[report-scheduler] schedule %s registered as entry %d (%s), next run %s",
		id, entryID, expr, next.Format(time.RFC3339))
	return nil
}

// Unregister removes a schedule's cron job if it has one. A schedule with no
// job is not an error: an inactive schedule legitimately has none, and treating
// that as a failure would block deleting it.
func (s *ReportSchedulerService) Unregister(scheduleID uuid.UUID) {
	if !s.jobs.RemoveJob(scheduleID.String()) {
		return
	}
	// Clear the recorded entry so the column reflects reality. The row may
	// already be gone if the schedule was deleted; that is not an error.
	if err := s.db.Model(&models.ReportSchedule{}).Where("id = ?", scheduleID).
		Update("cron_entry_id", nil).Error; err != nil {
		s.logger.Printf("[report-scheduler] could not clear cron entry id for %s: %v", scheduleID, err)
	}
}

// RegisteredCount reports how many schedules currently have a cron job. Used by
// tests and for logging.
func (s *ReportSchedulerService) RegisteredCount() int { return s.jobs.Count() }

// EntryIDFor returns the cron entry a schedule is registered under, if any.
func (s *ReportSchedulerService) EntryIDFor(scheduleID uuid.UUID) (int, bool) {
	return s.jobs.EntryID(scheduleID.String())
}

// RunSchedule generates a schedule's report and emails it. It reloads the
// schedule first so a run always uses current settings, and it is safe to call
// directly for a manual trigger.
func (s *ReportSchedulerService) RunSchedule(ctx context.Context, scheduleID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, scheduledRunTimeout)
	defer cancel()

	var schedule models.ReportSchedule
	if err := s.db.WithContext(ctx).First(&schedule, "id = ?", scheduleID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// The schedule was deleted between firing and running; drop the job.
			s.Unregister(scheduleID)
			return nil
		}
		return fmt.Errorf("loading schedule: %w", err)
	}

	var report models.Report
	if err := s.db.WithContext(ctx).First(&report, "id = ?", schedule.ReportID).Error; err != nil {
		return s.recordRun(ctx, &schedule, fmt.Errorf("loading report: %w", err))
	}

	generated, err := s.generator.GenerateAndSaveReport(ctx, &report, schedule.UserID)
	if err != nil {
		return s.recordRun(ctx, &schedule, err)
	}

	email := ReportEmail{
		To:         schedule.EmailRecipients,
		ReportName: report.Name,
	}
	if schedule.SendAsAttachment {
		email.AttachmentPath = generated.Path
	}
	if schedule.IncludeInEmail.IncludeSummary {
		email.Summary = SummaryLines(generated.Data)
	}
	if schedule.IncludeInEmail.IncludeLink {
		if token, tokErr := s.shareTokenFor(ctx, report.ID); tokErr == nil && token != "" {
			email.ShareLink = s.mailer.ShareURL(token)
		}
	}

	if err := s.mailer.Send(ctx, email); err != nil {
		return s.recordRun(ctx, &schedule, fmt.Errorf("sending report email: %w", err))
	}

	s.logger.Printf("[report-scheduler] schedule %s delivered %q to %d recipients",
		schedule.ID, report.Name, len(schedule.EmailRecipients))
	return s.recordRun(ctx, &schedule, nil)
}

// shareTokenFor returns an existing share token for the report, if one exists.
// A schedule deliberately does not mint a new public token on its own: creating
// unauthenticated access is an explicit action, not a side effect of delivery.
func (s *ReportSchedulerService) shareTokenFor(ctx context.Context, reportID uuid.UUID) (string, error) {
	var access models.ReportAccess
	err := s.db.WithContext(ctx).
		Where("report_id = ? AND share_token IS NOT NULL", reportID).
		Order("created_at DESC").First(&access).Error
	if err != nil {
		return "", err
	}
	if access.ShareToken == nil {
		return "", nil
	}
	return *access.ShareToken, nil
}

// recordRun stamps last_run_at and next_run_at, and returns runErr unchanged so
// the timestamps are written whether or not the run succeeded.
func (s *ReportSchedulerService) recordRun(ctx context.Context, schedule *models.ReportSchedule, runErr error) error {
	now := time.Now()
	updates := map[string]interface{}{"last_run_at": now, "updated_at": now}

	if expr, err := CronExpressionFor(schedule.ScheduleType, schedule.CronExpression); err == nil {
		if next, nerr := s.jobs.GetNextRunTime(expr); nerr == nil {
			updates["next_run_at"] = next
		}
	}
	if err := s.db.WithContext(ctx).Model(&models.ReportSchedule{}).
		Where("id = ?", schedule.ID).Updates(updates).Error; err != nil {
		s.logger.Printf("[report-scheduler] could not record run for %s: %v", schedule.ID, err)
	}

	if runErr != nil {
		s.logger.Printf("[report-scheduler] schedule %s failed: %v", schedule.ID, runErr)
	}
	return runErr
}

// CronExpressionFor maps a cadence to a cron expression. A custom cadence
// requires its own expression: falling back to a daily default, as the original
// design did, would deliver mail on a cadence nobody chose.
func CronExpressionFor(scheduleType string, custom *string) (string, error) {
	switch scheduleType {
	case models.ScheduleTypeDaily:
		return "0 8 * * *", nil // 08:00 daily
	case models.ScheduleTypeWeekly:
		return "0 8 * * MON", nil // 08:00 Mondays
	case models.ScheduleTypeMonthly:
		return "0 8 1 * *", nil // 08:00 on the 1st
	case models.ScheduleTypeCustom:
		if custom == nil || *custom == "" {
			return "", errors.New("a custom schedule requires a cron expression")
		}
		return *custom, nil
	default:
		return "", fmt.Errorf("unknown schedule type %q", scheduleType)
	}
}

// ValidateCronExpression reports whether expr is one the scheduler can run.
func ValidateCronExpression(expr string) error {
	_, err := cronParser.Parse(expr)
	return err
}
