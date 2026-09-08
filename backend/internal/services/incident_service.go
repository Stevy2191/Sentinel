package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
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
	return s.CreateIncidentFromCheck(ctx, monitorID, startTime, models.IncidentTypeDown, "")
}

// CreateIncidentFromCheck opens an incident and records how the check failed
// and what it said. Without this an incident carried no explanation at all: the
// error message lived on the check row and was never copied across, so the
// incident list could say a monitor went down but never why.
func (s *IncidentService) CreateIncidentFromCheck(
	ctx context.Context,
	monitorID uuid.UUID,
	startTime time.Time,
	incidentType string,
	reason string,
) (*models.Incident, error) {
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
		IncidentType:    incidentType,
		RootCause:       reason,
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

// GetOverlappingIncidents returns every incident for a monitor that overlaps the
// [start, end] window: it began at or before end, and either is still open or
// ended at or after start.
//
// This differs from GetIncidents, which filters on start_time alone and so
// misses an incident that began before the window and is still running through
// it — the case that matters when rendering a fixed recent window.
func (s *IncidentService) GetOverlappingIncidents(ctx context.Context, monitorID uuid.UUID, start, end time.Time) ([]models.Incident, error) {
	if monitorID == uuid.Nil {
		return nil, errors.New("monitor id is required")
	}
	if end.Before(start) {
		return nil, fmt.Errorf("end %s is before start %s", end.Format(time.RFC3339), start.Format(time.RFC3339))
	}

	var incidents []models.Incident
	err := s.db.WithContext(ctx).
		Where("monitor_id = ? AND start_time <= ? AND (end_time IS NULL OR end_time >= ?)", monitorID, end, start).
		Order("start_time ASC").
		Find(&incidents).Error
	if err != nil {
		return nil, fmt.Errorf("querying overlapping incidents for monitor %s: %w", monitorID, err)
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

// IncidentListOptions filters and paginates the incidents list.
type IncidentListOptions struct {
	Page   int
	Limit  int
	Status string // "", "all", "ongoing", "resolved"
	// MonitorID restricts to one monitor. Nil means every monitor.
	MonitorID *uuid.UUID
	// Search matches the monitor's name, case-insensitively.
	Search string
	// SortBy is "started" (default) or "duration".
	SortBy string
	Desc   bool
}

// IncidentWithMonitor is a listed incident joined to the monitor it belongs to,
// so the table can show a name without a request per row.
type IncidentWithMonitor struct {
	models.Incident
	MonitorName string `json:"monitor_name" gorm:"column:monitor_name"`
	MonitorURL  string `json:"monitor_url" gorm:"column:monitor_url"`
	MonitorType string `json:"monitor_type" gorm:"column:monitor_type"`
}

// ListIncidents returns a page of incidents across all monitors, newest first
// by default, with the total matching count for pagination.
func (s *IncidentService) ListIncidents(ctx context.Context, opts IncidentListOptions) ([]IncidentWithMonitor, int64, error) {
	if opts.Page < 1 {
		opts.Page = 1
	}
	// Bounded so a crafted limit cannot ask for the whole table at once.
	if opts.Limit < 1 || opts.Limit > 200 {
		opts.Limit = 50
	}

	base := s.db.WithContext(ctx).
		Table("incidents AS i").
		Joins("JOIN monitors AS m ON m.id = i.monitor_id")

	switch opts.Status {
	case models.IncidentStatusOngoing:
		base = base.Where("i.end_time IS NULL")
	case models.IncidentStatusResolved:
		base = base.Where("i.end_time IS NOT NULL")
	}
	if opts.MonitorID != nil {
		base = base.Where("i.monitor_id = ?", *opts.MonitorID)
	}
	if q := strings.TrimSpace(opts.Search); q != "" {
		base = base.Where("m.name ILIKE ?", "%"+q+"%")
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting incidents: %w", err)
	}

	dir := "ASC"
	if opts.Desc {
		dir = "DESC"
	}
	order := "i.start_time " + dir
	if opts.SortBy == "duration" {
		// An ongoing incident has no stored duration, so it is measured from
		// its start instead; otherwise every open incident would sort as zero,
		// which is the opposite of the truth.
		order = "COALESCE(i.duration_seconds, EXTRACT(EPOCH FROM (now() - i.start_time))::int) " + dir
	}

	var rows []IncidentWithMonitor
	err := base.Session(&gorm.Session{}).
		Select("i.*, m.name AS monitor_name, m.url AS monitor_url, m.type AS monitor_type").
		Order(order).
		Limit(opts.Limit).
		Offset((opts.Page - 1) * opts.Limit).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("listing incidents: %w", err)
	}
	return rows, total, nil
}

// GetIncidentByID returns one incident joined to its monitor.
func (s *IncidentService) GetIncidentByID(ctx context.Context, id uuid.UUID) (*IncidentWithMonitor, error) {
	var row IncidentWithMonitor
	err := s.db.WithContext(ctx).
		Table("incidents AS i").
		Joins("JOIN monitors AS m ON m.id = i.monitor_id").
		Select("i.*, m.name AS monitor_name, m.url AS monitor_url, m.type AS monitor_type").
		Where("i.id = ?", id).
		Scan(&row).Error
	if err != nil {
		return nil, fmt.Errorf("fetching incident %s: %w", id, err)
	}
	if row.ID == uuid.Nil {
		return nil, gorm.ErrRecordNotFound
	}
	return &row, nil
}

// ChecksDuringIncident returns the checks recorded while the incident was open,
// oldest first, for the detail timeline. An ongoing incident runs to now.
func (s *IncidentService) ChecksDuringIncident(ctx context.Context, inc *models.Incident, limit int) ([]models.Check, error) {
	if limit < 1 || limit > 500 {
		limit = 200
	}
	end := time.Now()
	if inc.EndTime != nil {
		end = *inc.EndTime
	}
	var checks []models.Check
	err := s.db.WithContext(ctx).
		Where("monitor_id = ? AND timestamp >= ? AND timestamp <= ?", inc.MonitorID, inc.StartTime, end).
		Order("timestamp ASC").
		Limit(limit).
		Find(&checks).Error
	if err != nil {
		return nil, fmt.Errorf("loading checks for incident %s: %w", inc.ID, err)
	}
	return checks, nil
}
