package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// This file keeps the maintenance *history* concerns together: the monitors row
// tracks the window in force (what the checker consults), while these methods
// maintain the durable record of windows a monitor has been under (what
// reporting reads). Every mutation of a monitor's window is mirrored here
// inside the same transaction, so the two never disagree.

// recordMaintenanceWindow stores a newly enabled window. Re-enabling the exact
// same window is treated as a no-op rather than a second identical row.
func recordMaintenanceWindow(tx *gorm.DB, monitorID uuid.UUID, start, end time.Time) error {
	var existing models.MaintenanceHistory
	err := tx.Where("monitor_id = ? AND start_time = ? AND end_time = ?", monitorID, start, end).
		First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("looking up maintenance history for monitor %s: %w", monitorID, err)
	}

	entry := &models.MaintenanceHistory{
		MonitorID: monitorID,
		StartTime: start.UTC(),
		EndTime:   end.UTC(),
	}
	if err := tx.Create(entry).Error; err != nil {
		return fmt.Errorf("recording maintenance history for monitor %s: %w", monitorID, err)
	}
	return nil
}

// reviseMaintenanceWindow moves the recorded window to new bounds. If the old
// window was never recorded (it pre-dates this table), the new one is inserted
// so history picks up from here rather than staying empty.
func reviseMaintenanceWindow(tx *gorm.DB, monitorID uuid.UUID, oldStart, oldEnd *time.Time, newStart, newEnd time.Time) error {
	if oldStart != nil && oldEnd != nil {
		result := tx.Model(&models.MaintenanceHistory{}).
			Where("monitor_id = ? AND start_time = ? AND end_time = ?", monitorID, *oldStart, *oldEnd).
			Updates(map[string]interface{}{
				"start_time": newStart.UTC(),
				"end_time":   newEnd.UTC(),
				"updated_at": time.Now().UTC(),
			})
		if result.Error != nil {
			return fmt.Errorf("revising maintenance history for monitor %s: %w", monitorID, result.Error)
		}
		if result.RowsAffected > 0 {
			return nil
		}
	}
	return recordMaintenanceWindow(tx, monitorID, newStart, newEnd)
}

// closeMaintenanceWindow settles the recorded window when maintenance is turned
// off, so history reflects what actually happened rather than what was planned:
//
//   - not started yet — the window was cancelled, so drop the record;
//   - in progress — maintenance ended early, so truncate it to `at`;
//   - already elapsed — it ran as recorded, so leave it alone.
func closeMaintenanceWindow(tx *gorm.DB, monitorID uuid.UUID, start, end *time.Time, at time.Time) error {
	if start == nil || end == nil {
		return nil
	}
	at = at.UTC()

	switch {
	case !at.After(*start):
		if err := tx.Where("monitor_id = ? AND start_time = ? AND end_time = ?", monitorID, *start, *end).
			Delete(&models.MaintenanceHistory{}).Error; err != nil {
			return fmt.Errorf("discarding cancelled maintenance for monitor %s: %w", monitorID, err)
		}
	case at.Before(*end):
		if err := tx.Model(&models.MaintenanceHistory{}).
			Where("monitor_id = ? AND start_time = ? AND end_time = ?", monitorID, *start, *end).
			Updates(map[string]interface{}{"end_time": at, "updated_at": time.Now().UTC()}).Error; err != nil {
			return fmt.Errorf("truncating maintenance history for monitor %s: %w", monitorID, err)
		}
	}
	return nil
}

// GetMaintenanceHistory returns the maintenance windows for a monitor that
// overlap [start, end] — a window counts if it began at or before end and
// finished at or after start — ordered oldest first.
func (s *MonitorService) GetMaintenanceHistory(ctx context.Context, monitorID uuid.UUID, start, end time.Time) ([]models.MaintenanceHistory, error) {
	if monitorID == uuid.Nil {
		return nil, errors.New("monitor id is required")
	}
	if end.Before(start) {
		return nil, fmt.Errorf("end %s is before start %s", end.Format(time.RFC3339), start.Format(time.RFC3339))
	}

	var windows []models.MaintenanceHistory
	err := s.db.WithContext(ctx).
		Where("monitor_id = ? AND start_time <= ? AND end_time >= ?", monitorID, end, start).
		Order("start_time ASC").
		Find(&windows).Error
	if err != nil {
		return nil, fmt.Errorf("querying maintenance history for monitor %s: %w", monitorID, err)
	}
	return windows, nil
}
