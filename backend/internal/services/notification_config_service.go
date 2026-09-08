package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
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
	markDuplicates(configs)
	for i := range configs {
		configs[i].HideSecrets()
	}
	return configs, nil
}

// markDuplicates pairs up channels that deliver to the same destination.
//
// Run before HideSecrets, because a webhook URL is part of the identity and is
// cleared by it.
func markDuplicates(configs []models.NotificationConfig) {
	// The first channel with a given destination is treated as the original;
	// every later one points back at it, so the list reads as "this is a copy
	// of that" rather than flagging both and saying neither.
	firstByKey := make(map[string]string, len(configs))
	for i := range configs {
		key := configs[i].Channel + "\x00" + configs[i].DestinationKey()
		if strings.Trim(configs[i].DestinationKey(), "|/") == "" {
			continue
		}
		if name, seen := firstByKey[key]; seen {
			configs[i].DuplicateOf = name
			continue
		}
		firstByKey[key] = configs[i].Name
	}
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
	if err := s.checkDuplicate(ctx, config, uuid.Nil); err != nil {
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

// ErrDuplicateDestination means another channel already delivers to the same
// place, so saving this one would send every alert twice.
var ErrDuplicateDestination = errors.New("duplicate notification destination")

// checkDuplicate refuses a channel that would deliver to somewhere an existing
// channel already covers, ignoring the row being edited.
//
// Worth refusing rather than merely warning: two channels on one destination
// deliver every alert twice, and nothing about the list makes that visible —
// the names differ, so the pair looks deliberate. The usual way to end up here
// is configuring a channel in the environment, which is imported on first run,
// and then adding the same one again through the UI without realising the
// first came from env.
func (s *NotificationConfigService) checkDuplicate(ctx context.Context, config *models.NotificationConfig, excludeID uuid.UUID) error {
	key := config.DestinationKey()
	// An incomplete configuration has no destination to collide with, and
	// Validate already rejects the cases that matter.
	if key == "" || strings.Trim(key, "|/") == "" {
		return nil
	}

	var siblings []models.NotificationConfig
	q := s.db.WithContext(ctx).Where("channel = ?", config.Channel)
	if excludeID != uuid.Nil {
		q = q.Where("id <> ?", excludeID)
	}
	if err := q.Find(&siblings).Error; err != nil {
		return fmt.Errorf("checking for a duplicate %s channel: %w", config.Channel, err)
	}

	for i := range siblings {
		if siblings[i].DestinationKey() == key {
			return fmt.Errorf("%w: %q already sends to %s. Edit or delete it instead of adding a second channel to the same destination, or every alert will be delivered twice",
				ErrDuplicateDestination, siblings[i].Name, siblings[i].Summary())
		}
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

	if err := s.checkDuplicate(ctx, config, id); err != nil {
		return err
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
