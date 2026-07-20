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
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
)

// ErrConfigNotFound is returned when no config row exists for a channel.
var ErrConfigNotFound = errors.New("notification config not found")

// NotificationConfigService manages persisted per-channel notification settings
// and keeps the live NotificationManager in sync after changes.
type NotificationConfigService struct {
	db      *gorm.DB
	manager *notifications.NotificationManager
	logger  *log.Logger
}

// NewNotificationConfigService returns a service backed by the given database
// and notification manager (used to hot-reload channels after edits).
func NewNotificationConfigService(db *gorm.DB, manager *notifications.NotificationManager) *NotificationConfigService {
	return &NotificationConfigService{db: db, manager: manager, logger: log.Default()}
}

// GetAllConfigs returns every channel config with secrets stripped, suitable for
// a list view.
func (s *NotificationConfigService) GetAllConfigs(ctx context.Context) ([]models.NotificationConfig, error) {
	var configs []models.NotificationConfig
	if err := s.db.WithContext(ctx).Order("channel ASC").Find(&configs).Error; err != nil {
		return nil, fmt.Errorf("listing notification configs: %w", err)
	}
	for i := range configs {
		configs[i].HideSecrets()
	}
	return configs, nil
}

// GetConfig returns a single channel's config including secrets (for editing).
// It returns ErrConfigNotFound when no row exists for the channel.
func (s *NotificationConfigService) GetConfig(ctx context.Context, channel string) (*models.NotificationConfig, error) {
	var config models.NotificationConfig
	err := s.db.WithContext(ctx).First(&config, "channel = ?", channel).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConfigNotFound
		}
		return nil, fmt.Errorf("fetching notification config %q: %w", channel, err)
	}
	return &config, nil
}

// CreateOrUpdateConfig validates and upserts a channel config (one row per
// channel), then reloads that channel in the live manager so the change takes
// effect immediately.
func (s *NotificationConfigService) CreateOrUpdateConfig(ctx context.Context, config *models.NotificationConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}

	var existing models.NotificationConfig
	err := s.db.WithContext(ctx).First(&existing, "channel = ?", config.Channel).Error
	switch {
	case err == nil:
		// Update in place, preserving id/created_at and test-status fields.
		config.ID = existing.ID
		config.CreatedAt = existing.CreatedAt
		config.LastTestAt = existing.LastTestAt
		config.LastTestSuccess = existing.LastTestSuccess
		config.LastTestError = existing.LastTestError
		config.UpdatedAt = time.Now()
		if err := s.db.WithContext(ctx).Save(config).Error; err != nil {
			return fmt.Errorf("updating notification config %q: %w", config.Channel, err)
		}
	case errors.Is(err, gorm.ErrRecordNotFound):
		if config.ID == uuid.Nil {
			config.ID = uuid.New()
		}
		now := time.Now()
		config.CreatedAt = now
		config.UpdatedAt = now
		if err := s.db.WithContext(ctx).Create(config).Error; err != nil {
			return fmt.Errorf("creating notification config %q: %w", config.Channel, err)
		}
	default:
		return fmt.Errorf("checking notification config %q: %w", config.Channel, err)
	}

	s.logger.Printf("[notify-config] Notification config saved: %s", config.Channel)
	if err := s.manager.ReloadChannel(ctx, config.Channel); err != nil {
		s.logger.Printf("[notify-config] warning: reload of %q failed: %v", config.Channel, err)
	}
	return nil
}

// DeleteConfig disables a channel and clears its stored settings, then reloads
// the channel so it stops sending. It returns ErrConfigNotFound when absent.
func (s *NotificationConfigService) DeleteConfig(ctx context.Context, channel string) error {
	var existing models.NotificationConfig
	err := s.db.WithContext(ctx).First(&existing, "channel = ?", channel).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrConfigNotFound
		}
		return fmt.Errorf("fetching notification config %q: %w", channel, err)
	}

	// Disable and clear every configurable field (keep the row so the channel's
	// id is stable and the UI can show it as "not configured"). Updates with an
	// explicit map writes the nils/false regardless of Go zero-value omission.
	if err := s.db.WithContext(ctx).Model(&models.NotificationConfig{}).
		Where("channel = ?", channel).Updates(map[string]interface{}{
		"enabled": false, "smtp_host": nil, "smtp_port": nil, "smtp_user": nil,
		"smtp_password": nil, "smtp_from": nil, "webhook_url": nil,
		"telegram_bot_token": nil, "telegram_chat_id": nil, "ntfy_url": nil,
		"ntfy_topic": nil, "custom_headers": nil, "last_test_at": nil,
		"last_test_success": nil, "last_test_error": nil, "updated_at": time.Now(),
	}).Error; err != nil {
		return fmt.Errorf("clearing notification config %q: %w", channel, err)
	}

	s.logger.Printf("[notify-config] Notification config deleted: %s", channel)
	if err := s.manager.ReloadChannel(ctx, channel); err != nil {
		s.logger.Printf("[notify-config] warning: reload of %q failed: %v", channel, err)
	}
	return nil
}

// TestConnection sends a test message using the channel's stored config and
// records the outcome (last_test_at / _success / _error). It returns whether the
// test succeeded and a human-readable error message (empty on success).
func (s *NotificationConfigService) TestConnection(ctx context.Context, channel string) (bool, string, error) {
	config, err := s.GetConfig(ctx, channel)
	if err != nil {
		return false, "", err
	}

	sendErr := s.manager.TestConfig(ctx, *config)
	success := sendErr == nil
	var errMsg string
	if sendErr != nil {
		errMsg = sendErr.Error()
	}

	now := time.Now()
	updates := map[string]interface{}{
		"last_test_at":      now,
		"last_test_success": success,
		"last_test_error":   nil,
		"updated_at":        now,
	}
	if !success {
		updates["last_test_error"] = errMsg
	}
	if err := s.db.WithContext(ctx).Model(&models.NotificationConfig{}).
		Where("channel = ?", channel).Updates(updates).Error; err != nil {
		return success, errMsg, fmt.Errorf("recording test result for %q: %w", channel, err)
	}

	s.logger.Printf("[notify-config] Test connection for %s: %t", channel, success)
	return success, errMsg, nil
}
