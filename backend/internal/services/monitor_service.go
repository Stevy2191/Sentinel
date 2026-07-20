package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// MonitorService provides CRUD and lifecycle operations for monitors.
type MonitorService struct {
	db     *gorm.DB
	logger *log.Logger
}

// NewMonitorService returns a MonitorService backed by the given database.
func NewMonitorService(db *gorm.DB) *MonitorService {
	return &MonitorService{
		db:     db,
		logger: log.Default(),
	}
}

// CreateMonitor validates and persists a new monitor, assigning an ID and
// timestamps. It returns the stored monitor or a validation/database error.
func (s *MonitorService) CreateMonitor(ctx context.Context, monitor *models.Monitor) (*models.Monitor, error) {
	if monitor == nil {
		return nil, errors.New("monitor is required")
	}

	// Default enabled to true before validation so a zero-value request is
	// treated as an active monitor.
	if !monitor.Enabled {
		monitor.Enabled = true
	}

	if err := monitor.Validate(); err != nil {
		return nil, fmt.Errorf("invalid monitor: %w", err)
	}

	if monitor.ID == uuid.Nil {
		monitor.ID = uuid.New()
	}
	now := time.Now()
	monitor.CreatedAt = now
	monitor.UpdatedAt = now
	if monitor.CurrentStatus == "" {
		monitor.CurrentStatus = models.StatusUnknown
	}

	if err := s.db.WithContext(ctx).Create(monitor).Error; err != nil {
		return nil, fmt.Errorf("creating monitor: %w", err)
	}

	s.logger.Printf("[monitor] created id=%s name=%q type=%s", monitor.ID, monitor.Name, monitor.Type)
	return monitor, nil
}

// GetMonitor fetches a monitor by ID, returning a wrapped
// gorm.ErrRecordNotFound if it does not exist.
func (s *MonitorService) GetMonitor(ctx context.Context, id uuid.UUID) (*models.Monitor, error) {
	s.logger.Printf("[monitor] lookup id=%s", id)

	var monitor models.Monitor
	err := s.db.WithContext(ctx).First(&monitor, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("monitor %s not found: %w", id, err)
		}
		return nil, fmt.Errorf("fetching monitor %s: %w", id, err)
	}
	return &monitor, nil
}

// ListMonitors returns monitors newest-first, optionally filtered by the
// "enabled" (bool), "type" (string), and "status" (string) keys.
func (s *MonitorService) ListMonitors(ctx context.Context, filters map[string]interface{}) ([]models.Monitor, error) {
	query := s.db.WithContext(ctx).Model(&models.Monitor{})

	if v, ok := filters["enabled"]; ok {
		query = query.Where("enabled = ?", v)
	}
	if v, ok := filters["type"]; ok {
		query = query.Where("type = ?", v)
	}
	if v, ok := filters["status"]; ok {
		query = query.Where("current_status = ?", v)
	}

	s.logger.Printf("[monitor] list filters=%v", filters)

	var monitors []models.Monitor
	if err := query.Order("created_at DESC").Find(&monitors).Error; err != nil {
		return nil, fmt.Errorf("listing monitors: %w", err)
	}
	return monitors, nil
}

// UpdateMonitor applies the non-zero, user-editable fields of updates onto the
// existing monitor, re-validates it, and saves. System-managed fields (ID,
// CreatedAt, CurrentStatus, LastCheckAt, LastResponseTimeMs) are preserved.
func (s *MonitorService) UpdateMonitor(ctx context.Context, id uuid.UUID, updates *models.Monitor) (*models.Monitor, error) {
	if updates == nil {
		return nil, errors.New("updates are required")
	}

	existing, err := s.GetMonitor(ctx, id)
	if err != nil {
		return nil, err
	}

	applyMonitorUpdates(existing, updates)

	if err := existing.Validate(); err != nil {
		return nil, fmt.Errorf("invalid monitor update: %w", err)
	}

	existing.UpdatedAt = time.Now()
	if err := s.db.WithContext(ctx).Save(existing).Error; err != nil {
		return nil, fmt.Errorf("updating monitor %s: %w", id, err)
	}

	s.logger.Printf("[monitor] updated id=%s name=%q", existing.ID, existing.Name)
	return existing, nil
}

// applyMonitorUpdates copies the editable fields from updates onto target,
// skipping zero values so callers can send partial updates. Enabled is always
// applied because false is a meaningful value (see PauseMonitor/ResumeMonitor
// for dedicated toggling).
func applyMonitorUpdates(target, updates *models.Monitor) {
	if updates.Name != "" {
		target.Name = updates.Name
	}
	if updates.Description != "" {
		target.Description = updates.Description
	}
	if updates.Type != "" {
		target.Type = updates.Type
	}
	if updates.URL != "" {
		target.URL = updates.URL
	}
	if updates.Method != "" {
		target.Method = updates.Method
	}
	if updates.Headers != nil {
		target.Headers = updates.Headers
	}
	if updates.Body != "" {
		target.Body = updates.Body
	}
	if updates.IntervalSeconds != 0 {
		target.IntervalSeconds = updates.IntervalSeconds
	}
	if updates.TimeoutSeconds != 0 {
		target.TimeoutSeconds = updates.TimeoutSeconds
	}
	if updates.Retries != 0 {
		target.Retries = updates.Retries
	}
	if updates.Tags != nil {
		target.Tags = updates.Tags
	}
	target.Enabled = updates.Enabled
}

// DeleteMonitor hard-deletes a monitor by ID. Related checks, incidents,
// notifications, and status_page_monitors rows are removed by the database via
// ON DELETE CASCADE foreign keys.
func (s *MonitorService) DeleteMonitor(ctx context.Context, id uuid.UUID) error {
	result := s.db.WithContext(ctx).Delete(&models.Monitor{}, "id = ?", id)
	if result.Error != nil {
		return fmt.Errorf("deleting monitor %s: %w", id, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("monitor %s not found: %w", id, gorm.ErrRecordNotFound)
	}

	s.logger.Printf("[monitor] deleted id=%s", id)
	return nil
}

// PauseMonitor disables a monitor so the scheduler stops checking it.
func (s *MonitorService) PauseMonitor(ctx context.Context, id uuid.UUID) error {
	return s.setEnabled(ctx, id, false, "paused")
}

// ResumeMonitor re-enables a previously paused monitor.
func (s *MonitorService) ResumeMonitor(ctx context.Context, id uuid.UUID) error {
	return s.setEnabled(ctx, id, true, "resumed")
}

// setEnabled toggles a monitor's enabled flag and persists it.
func (s *MonitorService) setEnabled(ctx context.Context, id uuid.UUID, enabled bool, action string) error {
	monitor, err := s.GetMonitor(ctx, id)
	if err != nil {
		return err
	}

	monitor.Enabled = enabled
	monitor.UpdatedAt = time.Now()
	if err := s.db.WithContext(ctx).Save(monitor).Error; err != nil {
		return fmt.Errorf("%s monitor %s: %w", action, id, err)
	}

	s.logger.Printf("[monitor] %s id=%s", action, id)
	return nil
}

// GetMonitorStatus returns just the current_status of a monitor.
func (s *MonitorService) GetMonitorStatus(ctx context.Context, id uuid.UUID) (string, error) {
	s.logger.Printf("[monitor] status lookup id=%s", id)

	var monitor models.Monitor
	err := s.db.WithContext(ctx).
		Select("current_status").
		First(&monitor, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("monitor %s not found: %w", id, err)
		}
		return "", fmt.Errorf("fetching status for monitor %s: %w", id, err)
	}
	return monitor.CurrentStatus, nil
}

// MaintenanceStatus summarizes a monitor's maintenance configuration.
type MaintenanceStatus struct {
	Enabled                  bool       `json:"enabled"`
	StartTime                *time.Time `json:"start_time"`
	EndTime                  *time.Time `json:"end_time"`
	IsCurrentlyInMaintenance bool       `json:"is_currently_in_maintenance"`
	TimeRemainingMinutes     int        `json:"time_remaining_minutes"`
	Status                   string     `json:"status"` // disabled | scheduled | active | expired
}

// validateMaintenanceWindow ensures the window is coherent and not already over.
func validateMaintenanceWindow(start, end time.Time) error {
	if !start.Before(end) {
		return errors.New("invalid maintenance window: start time must be before end time")
	}
	if !end.After(time.Now()) {
		return errors.New("invalid maintenance window: end time must be in the future")
	}
	return nil
}

// EnableMaintenanceMode turns on maintenance mode for a monitor over [start, end].
func (s *MonitorService) EnableMaintenanceMode(ctx context.Context, monitorID uuid.UUID, startTime, endTime time.Time) error {
	monitor, err := s.GetMonitor(ctx, monitorID)
	if err != nil {
		return err
	}
	if err := validateMaintenanceWindow(startTime, endTime); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&models.Monitor{}).
		Where("id = ?", monitorID).
		Updates(map[string]interface{}{
			"maintenance_mode_enabled": true,
			"maintenance_start":        startTime,
			"maintenance_end":          endTime,
			"updated_at":               time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("enabling maintenance for monitor %s: %w", monitorID, err)
	}
	s.logger.Printf("[monitor] maintenance enabled for %q: %s to %s", monitor.Name,
		startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
	return nil
}

// UpdateMaintenanceWindow changes the maintenance window of a monitor.
func (s *MonitorService) UpdateMaintenanceWindow(ctx context.Context, monitorID uuid.UUID, startTime, endTime time.Time) error {
	if _, err := s.GetMonitor(ctx, monitorID); err != nil {
		return err
	}
	if err := validateMaintenanceWindow(startTime, endTime); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&models.Monitor{}).
		Where("id = ?", monitorID).
		Updates(map[string]interface{}{
			"maintenance_start": startTime,
			"maintenance_end":   endTime,
			"updated_at":        time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("updating maintenance window for monitor %s: %w", monitorID, err)
	}
	s.logger.Printf("[monitor] maintenance window updated for %s", monitorID)
	return nil
}

// DisableMaintenanceMode turns off maintenance mode and clears the window.
func (s *MonitorService) DisableMaintenanceMode(ctx context.Context, monitorID uuid.UUID) error {
	monitor, err := s.GetMonitor(ctx, monitorID)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&models.Monitor{}).
		Where("id = ?", monitorID).
		Updates(map[string]interface{}{
			"maintenance_mode_enabled": false,
			"maintenance_start":        nil,
			"maintenance_end":          nil,
			"updated_at":               time.Now(),
		}).Error; err != nil {
		return fmt.Errorf("disabling maintenance for monitor %s: %w", monitorID, err)
	}
	s.logger.Printf("[monitor] maintenance disabled for %q", monitor.Name)
	return nil
}

// GetMaintenanceStatus returns the maintenance state of a monitor.
func (s *MonitorService) GetMaintenanceStatus(ctx context.Context, monitorID uuid.UUID) (*MaintenanceStatus, error) {
	monitor, err := s.GetMonitor(ctx, monitorID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	status := &MaintenanceStatus{
		Enabled:                  monitor.MaintenanceModeEnabled,
		StartTime:                monitor.MaintenanceStart,
		EndTime:                  monitor.MaintenanceEnd,
		IsCurrentlyInMaintenance: monitor.IsInMaintenanceWindow(now),
	}
	switch {
	case !monitor.MaintenanceModeEnabled:
		status.Status = "disabled"
	case monitor.MaintenanceStart != nil && now.Before(*monitor.MaintenanceStart):
		status.Status = "scheduled"
	case status.IsCurrentlyInMaintenance:
		status.Status = "active"
		if cd := monitor.GetMaintenanceCountdown(now); cd != nil {
			status.TimeRemainingMinutes = int(cd.Minutes())
		}
	default:
		status.Status = "expired"
	}
	return status, nil
}

// TestMonitor runs an immediate check for a monitor and stores the result. It is
// intended for manual "test now" actions from the API.
func (s *MonitorService) TestMonitor(ctx context.Context, id uuid.UUID, checkService *CheckService) (*models.Check, error) {
	if checkService == nil {
		return nil, errors.New("check service is required")
	}

	monitor, err := s.GetMonitor(ctx, id)
	if err != nil {
		return nil, err
	}

	s.logger.Printf("[monitor] test run id=%s type=%s", monitor.ID, monitor.Type)

	check, err := checkService.ExecuteCheck(ctx, monitor)
	if err != nil {
		return nil, fmt.Errorf("executing test check for monitor %s: %w", id, err)
	}

	if err := checkService.StoreCheck(ctx, monitor.ID, check); err != nil {
		return nil, fmt.Errorf("storing test check for monitor %s: %w", id, err)
	}

	return check, nil
}
