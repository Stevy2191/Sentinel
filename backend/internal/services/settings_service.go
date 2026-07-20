package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// SettingsService reads and writes persisted application settings (a small
// key/value table). Settings survive restarts, so a value changed at runtime by
// an admin takes precedence over the environment default it was seeded from.
type SettingsService struct {
	db     *gorm.DB
	logger *log.Logger
}

// NewSettingsService returns a SettingsService backed by the given database.
func NewSettingsService(db *gorm.DB) *SettingsService {
	return &SettingsService{db: db, logger: log.Default()}
}

// getString returns the raw stored value for a key and whether it exists.
func (s *SettingsService) getString(ctx context.Context, key string) (string, bool, error) {
	var setting models.Setting
	err := s.db.WithContext(ctx).First(&setting, "key = ?", key).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("reading setting %q: %w", key, err)
	}
	return setting.Value, true, nil
}

// setString upserts a key/value pair.
func (s *SettingsService) setString(ctx context.Context, key, value string) error {
	setting := models.Setting{Key: key, Value: value, UpdatedAt: time.Now()}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&setting).Error
	if err != nil {
		return fmt.Errorf("saving setting %q: %w", key, err)
	}
	return nil
}

// GetBool returns a boolean setting, falling back to the given default when the
// key is absent or unparseable.
func (s *SettingsService) GetBool(ctx context.Context, key string, fallback bool) bool {
	raw, ok, err := s.getString(ctx, key)
	if err != nil {
		s.logger.Printf("[settings] %v; using default %t for %q", err, fallback, key)
		return fallback
	}
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		s.logger.Printf("[settings] value %q for %q is not a bool; using default %t", raw, key, fallback)
		return fallback
	}
	return parsed
}

// SetBool stores a boolean setting.
func (s *SettingsService) SetBool(ctx context.Context, key string, value bool) error {
	return s.setString(ctx, key, strconv.FormatBool(value))
}

// SeedBool inserts a boolean setting only if the key does not already exist, so
// an admin's runtime change is never overwritten by the environment default on
// a later restart. Returns true if it wrote the seed value.
func (s *SettingsService) SeedBool(ctx context.Context, key string, value bool) (bool, error) {
	if _, ok, err := s.getString(ctx, key); err != nil {
		return false, err
	} else if ok {
		return false, nil
	}
	if err := s.SetBool(ctx, key, value); err != nil {
		return false, err
	}
	s.logger.Printf("[settings] seeded %q = %t", key, value)
	return true, nil
}

// RegistrationEnabled reports whether new-user self-registration is currently
// allowed. Defaults to false (closed) when unset.
func (s *SettingsService) RegistrationEnabled(ctx context.Context) bool {
	return s.GetBool(ctx, models.SettingRegistrationEnabled, false)
}

// SetRegistrationEnabled turns self-registration on or off.
func (s *SettingsService) SetRegistrationEnabled(ctx context.Context, enabled bool) error {
	if err := s.SetBool(ctx, models.SettingRegistrationEnabled, enabled); err != nil {
		return err
	}
	s.logger.Printf("[settings] registration_enabled set to %t", enabled)
	return nil
}
