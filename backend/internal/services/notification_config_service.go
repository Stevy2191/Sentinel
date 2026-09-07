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
	// Grouped by type, then by creation, so several channels of one type sit
	// together in a stable order rather than shuffling between requests.
	if err := s.db.WithContext(ctx).Order("channel ASC, created_at ASC").Find(&configs).Error; err != nil {
		return nil, fmt.Errorf("listing notification configs: %w", err)
	}
	for i := range configs {
		configs[i].HideSecrets()
	}
	return configs, nil
}

// GetConfig returns a single channel's config including secrets (for editing).
// It returns ErrConfigNotFound when no row has that id.
func (s *NotificationConfigService) GetConfig(ctx context.Context, id uuid.UUID) (*models.NotificationConfig, error) {
	var config models.NotificationConfig
	err := s.db.WithContext(ctx).First(&config, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrConfigNotFound
		}
		return nil, fmt.Errorf("fetching notification config %s: %w", id, err)
	}
	return &config, nil
}

// CreateConfig validates and inserts a new channel. Several channels may share
// a type, so this always inserts rather than upserting by type as it once did.
func (s *NotificationConfigService) CreateConfig(ctx context.Context, config *models.NotificationConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if config.ID == uuid.Nil {
		config.ID = uuid.New()
	}
	now := time.Now()
	config.CreatedAt = now
	config.UpdatedAt = now
	if err := s.db.WithContext(ctx).Create(config).Error; err != nil {
		return fmt.Errorf("creating notification channel %q: %w", config.Name, err)
	}

	s.logger.Printf("[notify-config] channel created: %s (%s)", config.Name, config.Channel)
	if err := s.manager.ReloadChannel(ctx, config.ID); err != nil {
		s.logger.Printf("[notify-config] warning: reload of %s failed: %v", config.ID, err)
	}
	return nil
}

// UpdateConfig validates and saves an existing channel by id, preserving the
// fields the caller does not own (created_at and the last-test status), then
// reloads it so the change takes effect without a restart.
func (s *NotificationConfigService) UpdateConfig(ctx context.Context, id uuid.UUID, config *models.NotificationConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}

	var existing models.NotificationConfig
	if err := s.db.WithContext(ctx).First(&existing, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrConfigNotFound
		}
		return fmt.Errorf("fetching notification channel %s: %w", id, err)
	}

	config.ID = existing.ID
	config.CreatedAt = existing.CreatedAt
	config.LastTestAt = existing.LastTestAt
	config.LastTestSuccess = existing.LastTestSuccess
	config.LastTestError = existing.LastTestError
	config.UpdatedAt = time.Now()
	if err := s.db.WithContext(ctx).Save(config).Error; err != nil {
		return fmt.Errorf("updating notification channel %s: %w", id, err)
	}

	s.logger.Printf("[notify-config] channel updated: %s (%s)", config.Name, config.Channel)
	if err := s.manager.ReloadChannel(ctx, id); err != nil {
		s.logger.Printf("[notify-config] warning: reload of %s failed: %v", id, err)
	}
	return nil
}

// SetEnabled switches a channel on or off without touching anything else.
//
// A dedicated operation rather than a full update, because the list a UI toggle
// is driven from has its secrets stripped: sending that row back as an update
// would fail validation (a webhook with no URL) or, worse, blank the stored
// credential. Only the one column moves here.
func (s *NotificationConfigService) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	res := s.db.WithContext(ctx).Model(&models.NotificationConfig{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{"enabled": enabled, "updated_at": time.Now()})
	if res.Error != nil {
		return fmt.Errorf("updating notification channel %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrConfigNotFound
	}

	s.logger.Printf("[notify-config] channel %s %s", id, map[bool]string{true: "enabled", false: "disabled"}[enabled])
	if err := s.manager.ReloadChannel(ctx, id); err != nil {
		s.logger.Printf("[notify-config] warning: reload of %s failed: %v", id, err)
	}
	return nil
}

// DeleteConfig removes a channel and unregisters it so it stops sending. It
// returns ErrConfigNotFound when absent.
//
// The row is deleted outright. It used to be blanked and kept, because the type
// was the identity and the UI needed something to show as "not configured";
// now that a type can have any number of channels, an emptied row is just a
// broken entry in the table. Delivery history survives: notifications.channel_id
// is ON DELETE SET NULL and keeps the channel's type either way.
func (s *NotificationConfigService) DeleteConfig(ctx context.Context, id uuid.UUID) error {
	res := s.db.WithContext(ctx).Delete(&models.NotificationConfig{}, "id = ?", id)
	if res.Error != nil {
		return fmt.Errorf("deleting notification channel %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrConfigNotFound
	}

	s.logger.Printf("[notify-config] channel deleted: %s", id)
	if err := s.manager.ReloadChannel(ctx, id); err != nil {
		s.logger.Printf("[notify-config] warning: reload of %s failed: %v", id, err)
	}
	return nil
}

// TestConnection sends a test message using the channel's stored config and
// records the outcome (last_test_at / _success / _error). It returns whether the
// test succeeded and a human-readable error message (empty on success).
func (s *NotificationConfigService) TestConnection(ctx context.Context, id uuid.UUID) (bool, string, error) {
	config, err := s.GetConfig(ctx, id)
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
		Where("id = ?", id).Updates(updates).Error; err != nil {
		return success, errMsg, fmt.Errorf("recording test result for %s: %w", id, err)
	}

	s.logger.Printf("[notify-config] test connection for %s (%s): %t", config.Name, config.Channel, success)
	return success, errMsg, nil
}
