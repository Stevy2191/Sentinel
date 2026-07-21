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

// This file adds monitor ownership + sharing access control to MonitorService.

// ErrShareExists is returned when a monitor is already shared with a user.
var ErrShareExists = errors.New("monitor is already shared with this user")

// MonitorShareInfo is a share row enriched with the target user's identity.
type MonitorShareInfo struct {
	models.MonitorSharing
	Username string `json:"username"`
	Email    string `json:"email"`
}

// ListAccessibleMonitors returns the monitors a user may see — everything for an
// admin, otherwise monitors they own or that are shared with them — with the
// same optional filters as ListMonitors.
func (s *MonitorService) ListAccessibleMonitors(ctx context.Context, userID uuid.UUID, isAdmin bool, filters map[string]interface{}) ([]models.Monitor, error) {
	q := s.db.WithContext(ctx).Model(&models.Monitor{})
	if v, ok := filters["enabled"]; ok {
		q = q.Where("enabled = ?", v)
	}
	if v, ok := filters["type"]; ok {
		q = q.Where("type = ?", v)
	}
	if v, ok := filters["status"]; ok {
		q = q.Where("current_status = ?", v)
	}
	if !isAdmin {
		q = q.Where(
			"owner_id = ? OR id IN (SELECT monitor_id FROM monitor_sharing WHERE shared_with_user_id = ?)",
			userID, userID,
		)
	}

	var monitors []models.Monitor
	if err := q.Order("created_at DESC").Find(&monitors).Error; err != nil {
		return nil, fmt.Errorf("listing accessible monitors: %w", err)
	}
	return monitors, nil
}

// GetMonitorsForUser returns the monitors a user owns or that are shared with
// them (no admin override, no filters).
func (s *MonitorService) GetMonitorsForUser(ctx context.Context, userID uuid.UUID) ([]models.Monitor, error) {
	return s.ListAccessibleMonitors(ctx, userID, false, nil)
}

// monitorOwner returns a monitor's owner id (nil if unowned), or an error.
func (s *MonitorService) monitorOwner(ctx context.Context, monitorID uuid.UUID) (*uuid.UUID, error) {
	var m models.Monitor
	err := s.db.WithContext(ctx).Select("owner_id").First(&m, "id = ?", monitorID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("monitor %s not found: %w", monitorID, err)
		}
		return nil, fmt.Errorf("fetching monitor owner: %w", err)
	}
	return m.OwnerID, nil
}

// IsMonitorOwner reports whether the user owns the monitor.
func (s *MonitorService) IsMonitorOwner(ctx context.Context, userID, monitorID uuid.UUID) (bool, error) {
	owner, err := s.monitorOwner(ctx, monitorID)
	if err != nil {
		return false, err
	}
	return owner != nil && *owner == userID, nil
}

// CanUserViewMonitor reports whether the user owns the monitor or has any share.
func (s *MonitorService) CanUserViewMonitor(ctx context.Context, userID, monitorID uuid.UUID) (bool, error) {
	if owns, err := s.IsMonitorOwner(ctx, userID, monitorID); err != nil {
		return false, err
	} else if owns {
		return true, nil
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&models.MonitorSharing{}).
		Where("monitor_id = ? AND shared_with_user_id = ?", monitorID, userID).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("checking view permission: %w", err)
	}
	return count > 0, nil
}

// CanUserEditMonitor reports whether the user owns the monitor or has an
// editable share.
func (s *MonitorService) CanUserEditMonitor(ctx context.Context, userID, monitorID uuid.UUID) (bool, error) {
	if owns, err := s.IsMonitorOwner(ctx, userID, monitorID); err != nil {
		return false, err
	} else if owns {
		return true, nil
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&models.MonitorSharing{}).
		Where("monitor_id = ? AND shared_with_user_id = ? AND permission = ?", monitorID, userID, models.PermissionEditable).
		Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("checking edit permission: %w", err)
	}
	return count > 0, nil
}

// GetUserShareMap returns monitorID -> permission for every monitor shared with
// a user (used to annotate the monitor list).
func (s *MonitorService) GetUserShareMap(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]string, error) {
	var shares []models.MonitorSharing
	err := s.db.WithContext(ctx).Where("shared_with_user_id = ?", userID).Find(&shares).Error
	if err != nil {
		return nil, fmt.Errorf("loading user shares: %w", err)
	}
	out := make(map[uuid.UUID]string, len(shares))
	for _, sh := range shares {
		out[sh.MonitorID] = sh.Permission
	}
	return out, nil
}

// ShareMonitor grants a user access to a monitor. Authorization (owner check) is
// performed by the caller. Returns ErrShareExists on a duplicate.
func (s *MonitorService) ShareMonitor(ctx context.Context, monitorID, sharedWith, sharedBy uuid.UUID, permission string) (*models.MonitorSharing, error) {
	if err := models.ValidateSharePermission(permission); err != nil {
		return nil, err
	}
	var existing int64
	if err := s.db.WithContext(ctx).Model(&models.MonitorSharing{}).
		Where("monitor_id = ? AND shared_with_user_id = ?", monitorID, sharedWith).
		Count(&existing).Error; err != nil {
		return nil, fmt.Errorf("checking existing share: %w", err)
	}
	if existing > 0 {
		return nil, ErrShareExists
	}

	share := &models.MonitorSharing{
		ID:               uuid.New(),
		MonitorID:        monitorID,
		SharedWithUserID: sharedWith,
		SharedByUserID:   sharedBy,
		Permission:       permission,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(share).Error; err != nil {
		return nil, fmt.Errorf("creating share: %w", err)
	}
	s.logger.Printf("[monitor] shared monitor=%s with=%s permission=%s", monitorID, sharedWith, permission)
	return share, nil
}

// UpdateMonitorShare changes an existing share's permission.
func (s *MonitorService) UpdateMonitorShare(ctx context.Context, monitorID, sharedWith uuid.UUID, permission string) error {
	if err := models.ValidateSharePermission(permission); err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&models.MonitorSharing{}).
		Where("monitor_id = ? AND shared_with_user_id = ?", monitorID, sharedWith).
		Updates(map[string]interface{}{"permission": permission, "updated_at": time.Now()})
	if result.Error != nil {
		return fmt.Errorf("updating share: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("share not found: %w", gorm.ErrRecordNotFound)
	}
	s.logger.Printf("[monitor] share updated monitor=%s with=%s permission=%s", monitorID, sharedWith, permission)
	return nil
}

// RevokeMonitorShare removes a user's access to a monitor.
func (s *MonitorService) RevokeMonitorShare(ctx context.Context, monitorID, sharedWith uuid.UUID) error {
	result := s.db.WithContext(ctx).
		Where("monitor_id = ? AND shared_with_user_id = ?", monitorID, sharedWith).
		Delete(&models.MonitorSharing{})
	if result.Error != nil {
		return fmt.Errorf("revoking share: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("share not found: %w", gorm.ErrRecordNotFound)
	}
	s.logger.Printf("[monitor] share revoked monitor=%s with=%s", monitorID, sharedWith)
	return nil
}

// GetMonitorShares returns everyone a monitor is shared with, plus their
// username/email.
func (s *MonitorService) GetMonitorShares(ctx context.Context, monitorID uuid.UUID) ([]MonitorShareInfo, error) {
	var out []MonitorShareInfo
	err := s.db.WithContext(ctx).
		Table("monitor_sharing").
		Select("monitor_sharing.*, users.username, users.email").
		Joins("JOIN users ON users.id = monitor_sharing.shared_with_user_id").
		Where("monitor_sharing.monitor_id = ?", monitorID).
		Order("monitor_sharing.created_at ASC").
		Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing monitor shares: %w", err)
	}
	return out, nil
}
