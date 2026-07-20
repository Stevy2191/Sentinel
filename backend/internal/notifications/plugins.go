// Package notifications defines Sentinel's pluggable notification system: a
// common plugin interface, a manager that fans a message out to all registered
// channels, and persistence of delivery records.
package notifications

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// Notification delivery status values (mirrors the notifications.status CHECK
// constraint in the schema).
const (
	statusPending = "pending"
	statusSent    = "sent"
	statusFailed  = "failed"
)

// maxHistoryLimit caps how many notification records a single history query may
// return.
const maxHistoryLimit = 1000

// NotificationMessage is the channel-agnostic payload describing a status change
// that plugins render and deliver.
type NotificationMessage struct {
	MonitorID        uuid.UUID     `json:"monitor_id"`
	MonitorName      string        `json:"monitor_name"`
	MonitorURL       string        `json:"monitor_url"`
	Status           string        `json:"status"`
	Message          string        `json:"message"`
	PreviousStatus   string        `json:"previous_status"`
	Timestamp        time.Time     `json:"timestamp"`
	IncidentID       *uuid.UUID    `json:"incident_id,omitempty"`
	DowntimeDuration time.Duration `json:"downtime_duration,omitempty"`
	ResponseTimeMs   int           `json:"response_time_ms"`
}

// NotificationPlugin is implemented by each delivery channel (email, Slack,
// Discord, ...). Implementations must respect the provided context's deadline.
type NotificationPlugin interface {
	// Send delivers the message through this channel.
	Send(ctx context.Context, message *NotificationMessage) error
	// ValidateConfig verifies the plugin's configuration is complete and usable.
	ValidateConfig(config map[string]interface{}) error
	// Name returns the plugin's unique channel name (e.g. "email", "slack").
	Name() string
	// IsEnabled reports whether the plugin is configured and ready to send.
	IsEnabled() bool
}

// NotificationManager holds the registered plugins and records deliveries.
// Plugins may be reloaded at runtime (when an admin changes a channel config),
// so access to the plugins map is guarded by mu.
type NotificationManager struct {
	mu      sync.RWMutex
	plugins map[string]NotificationPlugin
	db      *gorm.DB
	logger  *log.Logger
}

// NewNotificationManager returns a manager with an empty plugin registry backed
// by the given database.
func NewNotificationManager(db *gorm.DB) *NotificationManager {
	return &NotificationManager{
		plugins: make(map[string]NotificationPlugin),
		db:      db,
		logger:  log.Default(),
	}
}

// RegisterPlugin validates and registers a plugin under its Name(). It returns
// an error if a plugin with the same name is already registered or if the
// plugin's configuration is invalid.
func (m *NotificationManager) RegisterPlugin(plugin NotificationPlugin) error {
	if plugin == nil {
		return errors.New("plugin is nil")
	}

	name := plugin.Name()
	if name == "" {
		return errors.New("plugin name is empty")
	}

	if err := plugin.ValidateConfig(nil); err != nil {
		return fmt.Errorf("invalid configuration for %q plugin: %w", name, err)
	}

	m.mu.Lock()
	if _, exists := m.plugins[name]; exists {
		m.mu.Unlock()
		return fmt.Errorf("notification plugin %q already registered", name)
	}
	m.plugins[name] = plugin
	m.mu.Unlock()
	m.logger.Printf("[notify] %s notification plugin registered", name)
	return nil
}

// SendNotification fans the message out to every enabled plugin, recording each
// delivery attempt. It returns an error only if every enabled plugin fails;
// success from at least one plugin (or having no enabled plugins) yields nil.
func (m *NotificationManager) SendNotification(ctx context.Context, message *NotificationMessage) error {
	if message == nil {
		return errors.New("message is nil")
	}

	m.logger.Printf("[notify] sending %s notification for %q", message.Status, message.MonitorName)

	var (
		attempted int
		succeeded int
		lastErr   error
	)

	for name, plugin := range m.snapshot() {
		if err := ctx.Err(); err != nil {
			m.logger.Printf("[notify] context cancelled before %s: %v", name, err)
			return fmt.Errorf("notification cancelled: %w", err)
		}

		if !plugin.IsEnabled() {
			continue
		}
		attempted++

		sendErr := plugin.Send(ctx, message)
		status := statusSent
		if sendErr != nil {
			status = statusFailed
			lastErr = sendErr
			m.logger.Printf("[notify] ❌ %s failed: %v", name, sendErr)
		} else {
			succeeded++
			m.logger.Printf("[notify] ✅ %s sent", name)
		}

		if err := m.StoreNotificationRecord(ctx, message, name, status, sendErr); err != nil {
			m.logger.Printf("[notify] warning: could not store %s notification record: %v", name, err)
		}
	}

	if attempted > 0 && succeeded == 0 {
		return fmt.Errorf("all %d notification plugin(s) failed; last error: %w", attempted, lastErr)
	}
	return nil
}

// StoreNotificationRecord persists the outcome of a single delivery attempt.
func (m *NotificationManager) StoreNotificationRecord(ctx context.Context, message *NotificationMessage, channel, status string, sendErr error) error {
	record := &models.Notification{
		ID:         uuid.New(),
		MonitorID:  message.MonitorID,
		IncidentID: message.IncidentID,
		Channel:    channel,
		Status:     status,
		CreatedAt:  time.Now().UTC(),
	}
	if sendErr != nil {
		record.ErrorMessage = sendErr.Error()
	}
	if status == statusSent {
		now := time.Now().UTC()
		record.SentAt = &now
	}

	if err := m.db.WithContext(ctx).Create(record).Error; err != nil {
		return fmt.Errorf("creating notification record: %w", err)
	}
	return nil
}

// GetNotificationHistory returns up to limit most-recent notification records for
// a monitor, newest first. limit is clamped to (0, maxHistoryLimit].
func (m *NotificationManager) GetNotificationHistory(ctx context.Context, monitorID uuid.UUID, limit int) ([]models.Notification, error) {
	if monitorID == uuid.Nil {
		return nil, errors.New("monitor id is required")
	}
	if limit <= 0 || limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}

	m.logger.Printf("[notify] history monitor=%s limit=%d", monitorID, limit)

	var records []models.Notification
	err := m.db.WithContext(ctx).
		Where("monitor_id = ?", monitorID).
		Order("created_at DESC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("querying notification history for monitor %s: %w", monitorID, err)
	}
	return records, nil
}

// GetFailedNotifications returns all failed notification records, newest first,
// for debugging and manual retries.
func (m *NotificationManager) GetFailedNotifications(ctx context.Context) ([]models.Notification, error) {
	m.logger.Printf("[notify] listing failed notifications")

	var records []models.Notification
	err := m.db.WithContext(ctx).
		Where("status = ?", statusFailed).
		Order("created_at DESC").
		Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("querying failed notifications: %w", err)
	}
	return records, nil
}

// snapshot returns a shallow copy of the plugin map, taken under the read lock,
// so callers can iterate without holding the lock during slow network sends.
func (m *NotificationManager) snapshot() map[string]NotificationPlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]NotificationPlugin, len(m.plugins))
	for name, plugin := range m.plugins {
		out[name] = plugin
	}
	return out
}

// setPlugin registers or replaces a plugin under its name. Unlike RegisterPlugin
// it overwrites an existing entry (database configs are authoritative over the
// env-configured plugin they replace).
func (m *NotificationManager) setPlugin(plugin NotificationPlugin) {
	m.mu.Lock()
	m.plugins[plugin.Name()] = plugin
	m.mu.Unlock()
}

// removePlugin unregisters a channel if present.
func (m *NotificationManager) removePlugin(name string) {
	m.mu.Lock()
	delete(m.plugins, name)
	m.mu.Unlock()
}

// Channels returns the names of all registered notification plugins.
func (m *NotificationManager) Channels() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// IsRegistered reports whether a plugin with the given name is registered.
func (m *NotificationManager) IsRegistered(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.plugins[name]
	return ok
}

// LoadFromDatabase (re)builds plugins from the notification_configs table.
// Database rows are authoritative over env-configured plugins: an enabled row
// adds or replaces the channel's plugin; a disabled row removes it. Channels
// with no row keep whatever the environment configured (backward-compatible
// fallback).
func (m *NotificationManager) LoadFromDatabase(ctx context.Context) error {
	var configs []models.NotificationConfig
	if err := m.db.WithContext(ctx).Find(&configs).Error; err != nil {
		return fmt.Errorf("loading notification configs: %w", err)
	}
	for i := range configs {
		m.applyConfig(configs[i])
	}
	m.logger.Printf("[notify] loaded %d channel config(s) from database", len(configs))
	return nil
}

// ReloadChannel refreshes a single channel from the database (used after an
// admin creates, updates, or deletes a config). A missing row removes the
// channel's plugin so it stops sending.
func (m *NotificationManager) ReloadChannel(ctx context.Context, channel string) error {
	var cfg models.NotificationConfig
	err := m.db.WithContext(ctx).First(&cfg, "channel = ?", channel).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		m.removePlugin(channel)
		return nil
	}
	if err != nil {
		return fmt.Errorf("reloading channel %q: %w", channel, err)
	}
	m.applyConfig(cfg)
	return nil
}

// applyConfig registers/replaces or removes a plugin based on one config row.
func (m *NotificationManager) applyConfig(cfg models.NotificationConfig) {
	if !cfg.Enabled {
		m.removePlugin(cfg.Channel)
		return
	}
	plugin, err := BuildPluginFromConfig(cfg)
	if err != nil {
		m.logger.Printf("[notify] channel %q not loaded from database: %v", cfg.Channel, err)
		m.removePlugin(cfg.Channel)
		return
	}
	m.setPlugin(plugin)
	m.logger.Printf("[notify] channel %q loaded from database", cfg.Channel)
}

// TestConfig builds a plugin for the given config and sends a synthetic test
// message through it, without registering the plugin or persisting a record.
func (m *NotificationManager) TestConfig(ctx context.Context, cfg models.NotificationConfig) error {
	plugin, err := BuildPluginFromConfig(cfg)
	if err != nil {
		return err
	}
	return plugin.Send(ctx, testMessage())
}

// SendToChannel delivers a message through a single named channel (used by the
// per-channel test endpoint). Unlike SendNotification it does not persist a
// record, so it is safe to call with a synthetic (non-persisted) monitor. It
// returns an error if the channel is not registered or the send fails.
func (m *NotificationManager) SendToChannel(ctx context.Context, name string, message *NotificationMessage) error {
	m.mu.RLock()
	plugin, ok := m.plugins[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("notification channel %q is not registered", name)
	}
	if err := plugin.Send(ctx, message); err != nil {
		return fmt.Errorf("sending via %q: %w", name, err)
	}
	m.logger.Printf("[notify] test message sent via %s", name)
	return nil
}

// ListNotificationsOptions filters and paginates a notification history query.
type ListNotificationsOptions struct {
	Limit  int
	Offset int
	Status string // optional: pending | sent | failed
	Start  *time.Time
	End    *time.Time
}

// ListNotifications returns notification records across all monitors, filtered
// and paginated, newest first, along with the total matching count.
func (m *NotificationManager) ListNotifications(ctx context.Context, opts ListNotificationsOptions) ([]models.Notification, int64, error) {
	apply := func(q *gorm.DB) *gorm.DB {
		if opts.Status != "" {
			q = q.Where("status = ?", opts.Status)
		}
		if opts.Start != nil {
			q = q.Where("created_at >= ?", *opts.Start)
		}
		if opts.End != nil {
			q = q.Where("created_at <= ?", *opts.End)
		}
		return q
	}

	var total int64
	if err := apply(m.db.WithContext(ctx).Model(&models.Notification{})).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting notifications: %w", err)
	}

	limit := opts.Limit
	if limit <= 0 || limit > maxHistoryLimit {
		limit = maxHistoryLimit
	}
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	var records []models.Notification
	err := apply(m.db.WithContext(ctx).Model(&models.Notification{})).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&records).Error
	if err != nil {
		return nil, 0, fmt.Errorf("querying notifications: %w", err)
	}
	return records, total, nil
}

// ErrNotificationNotFailed is returned when retrying a notification that is not
// in the failed state.
var ErrNotificationNotFailed = errors.New("notification not in failed state")

// RetryNotification re-sends a previously failed notification through its
// original channel and updates the stored record with the new outcome. It
// returns a wrapped gorm.ErrRecordNotFound if the notification does not exist,
// ErrNotificationNotFailed if it is not in the failed state, or the send error
// if the retry itself fails.
func (m *NotificationManager) RetryNotification(ctx context.Context, notificationID uuid.UUID) error {
	var record models.Notification
	if err := m.db.WithContext(ctx).First(&record, "id = ?", notificationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("notification %s not found: %w", notificationID, err)
		}
		return fmt.Errorf("fetching notification %s: %w", notificationID, err)
	}
	if record.Status != statusFailed {
		return ErrNotificationNotFailed
	}

	// Reconstruct a message from the stored record and current monitor state.
	// The original alert text is not persisted, so this is a best-effort rebuild.
	var monitor models.Monitor
	if err := m.db.WithContext(ctx).First(&monitor, "id = ?", record.MonitorID).Error; err != nil {
		return fmt.Errorf("loading monitor for notification %s: %w", notificationID, err)
	}
	status := "down"
	if monitor.CurrentStatus == models.StatusOnline {
		status = "recovered"
	}
	message := &NotificationMessage{
		MonitorID:      monitor.ID,
		MonitorName:    monitor.Name,
		MonitorURL:     monitor.URL,
		Status:         status,
		Message:        "Retry of a previously failed notification",
		Timestamp:      time.Now(),
		IncidentID:     record.IncidentID,
		ResponseTimeMs: monitor.LastResponseTimeMs,
	}

	// Retry through the original channel only (not a full fan-out) so we can
	// update this specific record with the outcome.
	sendErr := m.SendToChannel(ctx, record.Channel, message)

	updates := map[string]interface{}{}
	if sendErr != nil {
		updates["status"] = statusFailed
		updates["error_message"] = sendErr.Error()
	} else {
		updates["status"] = statusSent
		updates["sent_at"] = time.Now().UTC()
		updates["error_message"] = ""
	}
	if err := m.db.WithContext(ctx).Model(&models.Notification{}).
		Where("id = ?", notificationID).
		Updates(updates).Error; err != nil {
		return fmt.Errorf("updating notification %s after retry: %w", notificationID, err)
	}

	m.logger.Printf("[notify] retried notification %s via %s (ok=%t)", notificationID, record.Channel, sendErr == nil)
	if sendErr != nil {
		return fmt.Errorf("retry failed: %w", sendErr)
	}
	return nil
}
