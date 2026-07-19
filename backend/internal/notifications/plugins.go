// Package notifications defines Sentinel's pluggable notification system: a
// common plugin interface, a manager that fans a message out to all registered
// channels, and persistence of delivery records.
package notifications

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
type NotificationManager struct {
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
	if _, exists := m.plugins[name]; exists {
		return fmt.Errorf("notification plugin %q already registered", name)
	}

	if err := plugin.ValidateConfig(nil); err != nil {
		return fmt.Errorf("invalid configuration for %q plugin: %w", name, err)
	}

	m.plugins[name] = plugin
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

	for name, plugin := range m.plugins {
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

// Channels returns the names of all registered notification plugins.
func (m *NotificationManager) Channels() []string {
	names := make([]string, 0, len(m.plugins))
	for name := range m.plugins {
		names = append(names, name)
	}
	return names
}

// IsRegistered reports whether a plugin with the given name is registered.
func (m *NotificationManager) IsRegistered(name string) bool {
	_, ok := m.plugins[name]
	return ok
}

// SendToChannel delivers a message through a single named channel (used by the
// per-channel test endpoint). Unlike SendNotification it does not persist a
// record, so it is safe to call with a synthetic (non-persisted) monitor. It
// returns an error if the channel is not registered or the send fails.
func (m *NotificationManager) SendToChannel(ctx context.Context, name string, message *NotificationMessage) error {
	plugin, ok := m.plugins[name]
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
