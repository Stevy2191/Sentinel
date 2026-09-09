package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// BaseURLFunc resolves the instance's externally reachable base URL. It is
// called at the moment a link is built rather than captured at construction, so
// an admin's edit in Settings -> System takes effect on the next email instead
// of the next restart.
type BaseURLFunc func() string

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

// GetString returns a string setting, falling back to the given default when
// the key is absent or stored empty.
func (s *SettingsService) GetString(ctx context.Context, key, fallback string) string {
	raw, ok, err := s.getString(ctx, key)
	if err != nil {
		s.logger.Printf("[settings] %v; using default %q for %q", err, fallback, key)
		return fallback
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback
	}
	return raw
}

// SetString stores a string setting.
func (s *SettingsService) SetString(ctx context.Context, key, value string) error {
	return s.setString(ctx, key, value)
}

// SeedString inserts a string setting only if the key does not already exist,
// so an admin's runtime change survives a restart. An empty seed is skipped
// rather than stored: writing "" would shadow the fallback for good.
func (s *SettingsService) SeedString(ctx context.Context, key, value string) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return false, nil
	}
	if _, ok, err := s.getString(ctx, key); err != nil {
		return false, err
	} else if ok {
		return false, nil
	}
	if err := s.setString(ctx, key, value); err != nil {
		return false, err
	}
	s.logger.Printf("[settings] seeded %q = %q", key, value)
	return true, nil
}

// GetInt returns an integer setting, falling back when absent or unparseable.
func (s *SettingsService) GetInt(ctx context.Context, key string, fallback int) int {
	raw, ok, err := s.getString(ctx, key)
	if err != nil {
		s.logger.Printf("[settings] %v; using default %d for %q", err, fallback, key)
		return fallback
	}
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		s.logger.Printf("[settings] value %q for %q is not an int; using default %d", raw, key, fallback)
		return fallback
	}
	return parsed
}

// SetInt stores an integer setting.
func (s *SettingsService) SetInt(ctx context.Context, key string, value int) error {
	return s.setString(ctx, key, strconv.Itoa(value))
}

// SeedInt inserts an integer setting only if the key does not already exist.
func (s *SettingsService) SeedInt(ctx context.Context, key string, value int) (bool, error) {
	if _, ok, err := s.getString(ctx, key); err != nil {
		return false, err
	} else if ok {
		return false, nil
	}
	if err := s.SetInt(ctx, key, value); err != nil {
		return false, err
	}
	s.logger.Printf("[settings] seeded %q = %d", key, value)
	return true, nil
}

// AppName returns the instance's display name, defaulting to "Sentinel".
func (s *SettingsService) AppName(ctx context.Context) string {
	return s.GetString(ctx, models.SettingAppName, models.DefaultAppName)
}

// BaseURL returns the instance's externally reachable base URL with any
// trailing slash removed, so callers can join a path onto it directly. Empty
// when unset — callers must treat that as "cannot build a link".
func (s *SettingsService) BaseURL(ctx context.Context) string {
	return strings.TrimRight(strings.TrimSpace(s.GetString(ctx, models.SettingBaseURL, "")), "/")
}

// DefaultCheckInterval returns the interval, in seconds, new monitors are
// created with.
func (s *SettingsService) DefaultCheckInterval(ctx context.Context, fallback int) int {
	return s.GetInt(ctx, models.SettingDefaultCheckInterval, fallback)
}

// CheckRetentionDays returns how long individual check results are kept,
// clamped to a sane range.
func (s *SettingsService) CheckRetentionDays(ctx context.Context) int {
	days := s.GetInt(ctx, models.SettingCheckRetentionDays, models.DefaultCheckRetentionDays)
	if days < models.MinCheckRetentionDays || days > models.MaxCheckRetentionDays {
		return models.DefaultCheckRetentionDays
	}
	return days
}

// IncidentRetentionDays returns how many days of incident history to keep.
func (s *SettingsService) IncidentRetentionDays(ctx context.Context) int {
	days := s.GetInt(ctx, models.SettingIncidentRetentionDays, models.DefaultIncidentRetentionDays)
	// Clamped on read as well as on write: a value edited directly in the
	// database must not be able to make the purge delete everything.
	if days < models.MinIncidentRetentionDays || days > models.MaxIncidentRetentionDays {
		s.logger.Printf("[settings] stored %s=%d is out of range; using %d",
			models.SettingIncidentRetentionDays, days, models.DefaultIncidentRetentionDays)
		return models.DefaultIncidentRetentionDays
	}
	return days
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
