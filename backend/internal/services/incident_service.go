package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// defaultIncidentSeverity is applied to newly opened incidents.
const defaultIncidentSeverity = "high"

// IncidentService manages the lifecycle of downtime incidents and derives
// downtime metrics from them.
type IncidentService struct {
	db     *gorm.DB
	logger *log.Logger
}

// NewIncidentService returns an IncidentService backed by the given database.
func NewIncidentService(db *gorm.DB) *IncidentService {
	return &IncidentService{
		db:     db,
		logger: log.Default(),
	}
}

// CreateIncident opens a new, ongoing incident for a monitor.
func (s *IncidentService) CreateIncident(ctx context.Context, monitorID uuid.UUID, startTime time.Time) (*models.Incident, error) {
	if monitorID == uuid.Nil {
		return nil, errors.New("monitor id is required")
	}
	if startTime.IsZero() {
		return nil, errors.New("start time is required")
	}

	now := time.Now()
	incident := &models.Incident{
		ID:              uuid.New(),
		MonitorID:       monitorID,
		StartTime:       startTime,
		EndTime:         nil,
		DurationSeconds: 0,
		Severity:        defaultIncidentSeverity,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.db.WithContext(ctx).Create(incident).Error; err != nil {
		return nil, fmt.Errorf("creating incident for monitor %s: %w", monitorID, err)
	}

	s.logger.Printf("[incident] opened id=%s monitor=%s start=%s", incident.ID, monitorID, startTime.Format(time.RFC3339))
	return incident, nil
}

// CloseIncident marks an ongoing incident as resolved, recording its end time
// and computed duration. It returns an error if the incident does not exist or
// is already closed.
func (s *IncidentService) CloseIncident(ctx context.Context, incidentID uuid.UUID, endTime time.Time) (*models.Incident, error) {
	if incidentID == uuid.Nil {
		return nil, errors.New("incident id is required")
	}
	if endTime.IsZero() {
		return nil, errors.New("end time is required")
	}

	var incident models.Incident
	err := s.db.WithContext(ctx).First(&incident, "id = ?", incidentID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("incident %s not found: %w", incidentID, err)
		}
		return nil, fmt.Errorf("fetching incident %s: %w", incidentID, err)
	}

	if incident.EndTime != nil {
		return nil, errors.New("incident already closed")
	}
	if endTime.Before(incident.StartTime) {
		return nil, fmt.Errorf("end time %s is before start time %s", endTime.Format(time.RFC3339), incident.StartTime.Format(time.RFC3339))
	}

	incident.EndTime = &endTime
	incident.DurationSeconds = int(endTime.Sub(incident.StartTime).Seconds())
	incident.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Save(&incident).Error; err != nil {
		return nil, fmt.Errorf("closing incident %s: %w", incidentID, err)
	}

	s.logger.Printf("[incident] closed id=%s monitor=%s duration=%ds", incident.ID, incident.MonitorID, incident.DurationSeconds)
	return &incident, nil
}

// GetIncidents returns incidents for a monitor whose start_time falls within
// [start, end], newest first.
func (s *IncidentService) GetIncidents(ctx context.Context, monitorID uuid.UUID, start time.Time, end time.Time) ([]models.Incident, error) {
	if monitorID == uuid.Nil {
		return nil, errors.New("monitor id is required")
	}
	if end.Before(start) {
		return nil, fmt.Errorf("end %s is before start %s", end.Format(time.RFC3339), start.Format(time.RFC3339))
	}

	s.logger.Printf("[incident] list monitor=%s range=[%s,%s]", monitorID, start.Format(time.RFC3339), end.Format(time.RFC3339))

	var incidents []models.Incident
	err := s.db.WithContext(ctx).
		Where("monitor_id = ? AND start_time >= ? AND start_time <= ?", monitorID, start, end).
		Order("start_time DESC").
		Find(&incidents).Error
	if err != nil {
		return nil, fmt.Errorf("querying incidents for monitor %s: %w", monitorID, err)
	}
	return incidents, nil
}

// GetActiveIncident returns the currently open incident for a monitor, or
// (nil, nil) if there is none.
func (s *IncidentService) GetActiveIncident(ctx context.Context, monitorID uuid.UUID) (*models.Incident, error) {
	if monitorID == uuid.Nil {
		return nil, errors.New("monitor id is required")
	}

	s.logger.Printf("[incident] active lookup monitor=%s", monitorID)

	var incident models.Incident
	err := s.db.WithContext(ctx).First(&incident, "monitor_id = ? AND end_time IS NULL", monitorID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("querying active incident for monitor %s: %w", monitorID, err)
	}
	return &incident, nil
}

// GetIncidentDuration returns the total downtime for a monitor over [start, end].
//
// It sums the portion of each incident that overlaps the window, computed from
// timestamps rather than the stored duration_seconds. This makes it correct for:
//   - ongoing incidents (end_time IS NULL), which are counted up to now — so a
//     monitor that is offline right now contributes live downtime instead of the
//     0 that its unset duration_seconds would imply;
//   - incidents that begin before start or extend past end, which are clamped to
//     the window rather than counted in full or dropped.
func (s *IncidentService) GetIncidentDuration(ctx context.Context, monitorID uuid.UUID, start time.Time, end time.Time) (time.Duration, error) {
	if monitorID == uuid.Nil {
		return 0, errors.New("monitor id is required")
	}
	if end.Before(start) {
		return 0, fmt.Errorf("end %s is before start %s", end.Format(time.RFC3339), start.Format(time.RFC3339))
	}

	// Any incident overlapping the window: it started at/before end, and either is
	// still open or ended at/after start.
	var incidents []models.Incident
	err := s.db.WithContext(ctx).
		Where("monitor_id = ? AND start_time <= ? AND (end_time IS NULL OR end_time >= ?)", monitorID, end, start).
		Find(&incidents).Error
	if err != nil {
		return 0, fmt.Errorf("querying incidents for downtime of monitor %s: %w", monitorID, err)
	}

	now := time.Now()
	var total time.Duration
	for i := range incidents {
		inc := incidents[i]

		segStart := inc.StartTime
		if segStart.Before(start) {
			segStart = start
		}

		// Ongoing incidents run to "now"; closed incidents to their end time.
		segEnd := now
		if inc.EndTime != nil {
			segEnd = *inc.EndTime
		}
		if segEnd.After(end) {
			segEnd = end
		}

		if segEnd.After(segStart) {
			total += segEnd.Sub(segStart)
		}
	}

	s.logger.Printf("[incident] downtime monitor=%s range=[%s,%s] total=%s", monitorID, start.Format(time.RFC3339), end.Format(time.RFC3339), total)
	return total, nil
}

// GetCurrentDowntime reports whether the monitor is offline right now (has an
// open incident) and, if so, how long it has been down (now - incident start).
func (s *IncidentService) GetCurrentDowntime(ctx context.Context, monitorID uuid.UUID) (ongoing bool, downtime time.Duration, err error) {
	active, err := s.GetActiveIncident(ctx, monitorID)
	if err != nil {
		return false, 0, err
	}
	if active == nil {
		return false, 0, nil
	}
	d := time.Since(active.StartTime)
	if d < 0 {
		d = 0
	}
	return true, d, nil
}

// GetDowntimePercentage returns the percentage (0-100, two decimals) of the
// [start, end] window during which the monitor was down.
func (s *IncidentService) GetDowntimePercentage(ctx context.Context, monitorID uuid.UUID, start time.Time, end time.Time) (float64, error) {
	totalWindow := end.Sub(start)
	if totalWindow <= 0 {
		return 0, fmt.Errorf("invalid time range: end %s must be after start %s", end.Format(time.RFC3339), start.Format(time.RFC3339))
	}

	downtime, err := s.GetIncidentDuration(ctx, monitorID, start, end)
	if err != nil {
		return 0, err
	}

	percentage := (downtime.Seconds() / totalWindow.Seconds()) * 100
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}
	percentage = math.Round(percentage*100) / 100

	s.logger.Printf("[incident] downtime%% monitor=%s range=[%s,%s] value=%.2f", monitorID, start.Format(time.RFC3339), end.Format(time.RFC3339), percentage)
	return percentage, nil
}

// GetIncidentCount returns the number of incidents for a monitor whose
// start_time falls within [start, end].
func (s *IncidentService) GetIncidentCount(ctx context.Context, monitorID uuid.UUID, start time.Time, end time.Time) (int64, error) {
	if monitorID == uuid.Nil {
		return 0, errors.New("monitor id is required")
	}
	if end.Before(start) {
		return 0, fmt.Errorf("end %s is before start %s", end.Format(time.RFC3339), start.Format(time.RFC3339))
	}

	var count int64
	err := s.db.WithContext(ctx).
		Model(&models.Incident{}).
		Where("monitor_id = ? AND start_time >= ? AND start_time <= ?", monitorID, start, end).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("counting incidents for monitor %s: %w", monitorID, err)
	}

	s.logger.Printf("[incident] count monitor=%s range=[%s,%s] count=%d", monitorID, start.Format(time.RFC3339), end.Format(time.RFC3339), count)
	return count, nil
}
