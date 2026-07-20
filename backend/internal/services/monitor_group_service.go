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

// This file adds monitor-group management to MonitorService: named, colored,
// orderable sections that monitors can be assigned to for the dashboard.

// CreateMonitorGroup creates a new group, appended after existing groups.
func (s *MonitorService) CreateMonitorGroup(ctx context.Context, name string, description, color *string) (*models.MonitorGroup, error) {
	group := &models.MonitorGroup{
		ID:          uuid.New(),
		Name:        name,
		Description: description,
		Color:       color,
	}
	if err := group.Validate(); err != nil {
		return nil, err
	}

	// Append to the end by default.
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.MonitorGroup{}).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("counting monitor groups: %w", err)
	}
	group.Position = int(count)

	now := time.Now()
	group.CreatedAt = now
	group.UpdatedAt = now
	if err := s.db.WithContext(ctx).Omit("Monitors").Create(group).Error; err != nil {
		return nil, fmt.Errorf("creating monitor group: %w", err)
	}

	s.logger.Printf("[monitor] Monitor group created: %s", name)
	return group, nil
}

// GetMonitorGroup fetches a single group (without preloading monitors).
func (s *MonitorService) GetMonitorGroup(ctx context.Context, groupID uuid.UUID) (*models.MonitorGroup, error) {
	var group models.MonitorGroup
	err := s.db.WithContext(ctx).First(&group, "id = ?", groupID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("monitor group %s not found: %w", groupID, err)
		}
		return nil, fmt.Errorf("fetching monitor group %s: %w", groupID, err)
	}
	return &group, nil
}

// GetMonitorGroups returns all groups ordered by position, each with its
// monitors preloaded.
func (s *MonitorService) GetMonitorGroups(ctx context.Context) ([]models.MonitorGroup, error) {
	var groups []models.MonitorGroup
	err := s.db.WithContext(ctx).
		Preload("Monitors", func(db *gorm.DB) *gorm.DB { return db.Order("created_at DESC") }).
		Order("position ASC, created_at ASC").
		Find(&groups).Error
	if err != nil {
		return nil, fmt.Errorf("listing monitor groups: %w", err)
	}
	return groups, nil
}

// UpdateMonitorGroup replaces a group's editable fields (name/description/color).
func (s *MonitorService) UpdateMonitorGroup(ctx context.Context, groupID uuid.UUID, name string, description, color *string) (*models.MonitorGroup, error) {
	group, err := s.GetMonitorGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}

	group.Name = name
	group.Description = description
	group.Color = color
	if err := group.Validate(); err != nil {
		return nil, err
	}
	group.UpdatedAt = time.Now()

	if err := s.db.WithContext(ctx).Omit("Monitors").Save(group).Error; err != nil {
		return nil, fmt.Errorf("updating monitor group %s: %w", groupID, err)
	}
	s.logger.Printf("[monitor] Monitor group updated: %s", group.Name)
	return group, nil
}

// DeleteMonitorGroup removes a group. The monitors.group_id foreign key uses
// ON DELETE SET NULL, so member monitors are automatically ungrouped.
func (s *MonitorService) DeleteMonitorGroup(ctx context.Context, groupID uuid.UUID) error {
	result := s.db.WithContext(ctx).Delete(&models.MonitorGroup{}, "id = ?", groupID)
	if result.Error != nil {
		return fmt.Errorf("deleting monitor group %s: %w", groupID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("monitor group %s not found: %w", groupID, gorm.ErrRecordNotFound)
	}
	s.logger.Printf("[monitor] Monitor group deleted, monitors ungrouped: %s", groupID)
	return nil
}

// MoveMonitorToGroup assigns a monitor to a group, or ungroups it when groupID
// is nil. A non-nil group must exist.
func (s *MonitorService) MoveMonitorToGroup(ctx context.Context, monitorID uuid.UUID, groupID *uuid.UUID) error {
	if _, err := s.GetMonitor(ctx, monitorID); err != nil {
		return err
	}
	if groupID != nil {
		if _, err := s.GetMonitorGroup(ctx, *groupID); err != nil {
			return err
		}
	}

	// Use a map so a nil group_id is written as SQL NULL (ungroup).
	result := s.db.WithContext(ctx).Model(&models.Monitor{}).
		Where("id = ?", monitorID).
		Updates(map[string]interface{}{"group_id": groupID, "updated_at": time.Now()})
	if result.Error != nil {
		return fmt.Errorf("moving monitor %s to group: %w", monitorID, result.Error)
	}
	s.logger.Printf("[monitor] Monitor moved to group: monitor=%s group=%v", monitorID, groupID)
	return nil
}

// ReorderMonitorGroup sets a group's ordering position.
func (s *MonitorService) ReorderMonitorGroup(ctx context.Context, groupID uuid.UUID, position int) error {
	if position < 0 {
		return errors.New("position must be non-negative")
	}
	result := s.db.WithContext(ctx).Model(&models.MonitorGroup{}).
		Where("id = ?", groupID).
		Updates(map[string]interface{}{"position": position, "updated_at": time.Now()})
	if result.Error != nil {
		return fmt.Errorf("reordering monitor group %s: %w", groupID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("monitor group %s not found: %w", groupID, gorm.ErrRecordNotFound)
	}
	s.logger.Printf("[monitor] Monitor group reordered: group=%s position=%d", groupID, position)
	return nil
}
