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

	// Channels restricts delivery to the selected channels. A nil slice means
	// every enabled channel — the behaviour before monitors could choose —
	// while an empty non-nil slice means deliver nowhere. Set from
	// Monitor.NotifyChannels.
	//
	// Entries are channel instance ids. A channel *type* is also accepted and
	// matches every instance of that type; see deliversTo.
	Channels []string `json:"channels,omitempty"`
}

// deliversTo reports whether this message should go out over the given channel.
//
// An entry matches either the instance's id or its type. The type fallback is
// deliberate rather than transitional: selections were stored as type names
// before an install could hold several channels of one type, and a monitor
// whose stored selection silently stopped matching would stop alerting without
// anything appearing to be wrong. Matching both is cheap and fails safe.
func (m *NotificationMessage) deliversTo(inst *ChannelInstance) bool {
	if m.Channels == nil {
		return true
	}
	id := inst.ID.String()
	for _, c := range m.Channels {
		if c == id || c == inst.Type {
			return true
		}
	}
	return false
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

// ChannelInstance is one configured channel: a plugin plus the identity of the
// row it was built from. Several instances may share a Type — two ntfy topics,
// three Discord servers — which is why the registry is keyed by ID and not, as
// it once was, by the channel type.
type ChannelInstance struct {
	ID     uuid.UUID
	Name   string
	Type   string
	Plugin NotificationPlugin
}

// Label names an instance for logs and delivery records: "Ops Slack (slack)".
func (c *ChannelInstance) Label() string {
	if c.Name == "" {
		return c.Type
	}
	return fmt.Sprintf("%s (%s)", c.Name, c.Type)
}

// NotificationManager holds the configured channels and records deliveries.
// Channels may be reloaded at runtime (when an admin changes one), so access to
// the registry is guarded by mu.
type NotificationManager struct {
	mu        sync.RWMutex
	instances map[uuid.UUID]*ChannelInstance
	db        *gorm.DB
	logger    *log.Logger
}

// NewNotificationManager returns a manager with an empty registry backed by the
// given database.
func NewNotificationManager(db *gorm.DB) *NotificationManager {
	return &NotificationManager{
		instances: make(map[uuid.UUID]*ChannelInstance),
		db:        db,
		logger:    log.Default(),
	}
}

// SendNotification fans the message out to every enabled channel the message is
// addressed to, recording each delivery attempt. It returns an error only if
// every attempted channel fails; success from at least one (or having none to
// attempt) yields nil.
func (m *NotificationManager) SendNotification(ctx context.Context, message *NotificationMessage) error {
	if message == nil {
		return errors.New("message is nil")
	}

	// An empty (but non-nil) channel list means the monitor is opted out. Return
	// before logging a send that is not going to happen.
	if message.Channels != nil && len(message.Channels) == 0 {
		return nil
	}

	m.logger.Printf("[notify] sending %s notification for %q", message.Status, message.MonitorName)

	var (
		attempted int
		succeeded int
		lastErr   error
	)

	for _, inst := range m.snapshot() {
		if err := ctx.Err(); err != nil {
			m.logger.Printf("[notify] context cancelled before %s: %v", inst.Label(), err)
			return fmt.Errorf("notification cancelled: %w", err)
		}

		if !inst.Plugin.IsEnabled() {
			continue
		}
		if !message.deliversTo(inst) {
			continue
		}
		attempted++

		sendErr := inst.Plugin.Send(ctx, message)
		status := statusSent
		if sendErr != nil {
			status = statusFailed
			lastErr = sendErr
			m.logger.Printf("[notify] ❌ %s failed: %v", inst.Label(), sendErr)
		} else {
			succeeded++
			m.logger.Printf("[notify] ✅ %s sent", inst.Label())
		}

		if err := m.StoreNotificationRecord(ctx, message, inst, status, sendErr); err != nil {
			m.logger.Printf("[notify] warning: could not store %s notification record: %v", inst.Label(), err)
		}
	}

	if attempted > 0 && succeeded == 0 {
		return fmt.Errorf("all %d notification channel(s) failed; last error: %w", attempted, lastErr)
	}
	return nil
}

// StoreNotificationRecord persists the outcome of a single delivery attempt.
// Both the channel's type and its id are recorded: the type keeps old history
// readable, the id says which of several same-type channels actually sent.
func (m *NotificationManager) StoreNotificationRecord(ctx context.Context, message *NotificationMessage, inst *ChannelInstance, status string, sendErr error) error {
	channelID := inst.ID
	record := &models.Notification{
		ID:         uuid.New(),
		MonitorID:  message.MonitorID,
		IncidentID: message.IncidentID,
		Channel:    inst.Type,
		ChannelID:  &channelID,
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
func (m *NotificationManager) snapshot() map[uuid.UUID]*ChannelInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[uuid.UUID]*ChannelInstance, len(m.instances))
	for id, inst := range m.instances {
		out[id] = inst
	}
	return out
}

// setInstance registers or replaces a channel by id.
func (m *NotificationManager) setInstance(inst *ChannelInstance) {
	m.mu.Lock()
	m.instances[inst.ID] = inst
	m.mu.Unlock()
}

// removeInstance unregisters a channel if present.
func (m *NotificationManager) removeInstance(id uuid.UUID) {
	m.mu.Lock()
	delete(m.instances, id)
	m.mu.Unlock()
}

// Instance returns the loaded channel with the given id.
func (m *NotificationManager) Instance(id uuid.UUID) (*ChannelInstance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	inst, ok := m.instances[id]
	return inst, ok
}

// Instances returns every loaded channel.
func (m *NotificationManager) Instances() []*ChannelInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ChannelInstance, 0, len(m.instances))
	for _, inst := range m.instances {
		out = append(out, inst)
	}
	return out
}

// Channels returns the names of all registered notification plugins.
func (m *NotificationManager) Channels() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[string]struct{}, len(m.instances))
	names := make([]string, 0, len(m.instances))
	for _, inst := range m.instances {
		if _, dup := seen[inst.Type]; dup {
			continue
		}
		seen[inst.Type] = struct{}{}
		names = append(names, inst.Type)
	}
	return names
}

// IsRegistered reports whether any loaded channel has the given type. Several
// may, so this answers "can this type deliver", not "which one".
func (m *NotificationManager) IsRegistered(channelType string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, inst := range m.instances {
		if inst.Type == channelType {
			return true
		}
	}
	return false
}

// LoadFromDatabase rebuilds the whole registry from notification_configs. The
// table is the only source of channels — anything configured through the
// environment is imported into it at startup — so this replaces the registry
// rather than merging into it, and a row that has been deleted stops sending.
func (m *NotificationManager) LoadFromDatabase(ctx context.Context) error {
	var configs []models.NotificationConfig
	if err := m.db.WithContext(ctx).Find(&configs).Error; err != nil {
		return fmt.Errorf("loading notification configs: %w", err)
	}

	next := make(map[uuid.UUID]*ChannelInstance, len(configs))
	for i := range configs {
		if inst := m.buildInstance(configs[i]); inst != nil {
			next[inst.ID] = inst
		}
	}

	m.mu.Lock()
	m.instances = next
	m.mu.Unlock()

	m.logger.Printf("[notify] loaded %d of %d channel config(s) from database", len(next), len(configs))
	return nil
}

// ReloadChannel refreshes a single channel by id (used after an admin creates,
// updates, or deletes one). A missing row unregisters it so it stops sending.
func (m *NotificationManager) ReloadChannel(ctx context.Context, id uuid.UUID) error {
	var cfg models.NotificationConfig
	err := m.db.WithContext(ctx).First(&cfg, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		m.removeInstance(id)
		return nil
	}
	if err != nil {
		return fmt.Errorf("reloading channel %s: %w", id, err)
	}
	m.applyConfig(cfg)
	return nil
}

// buildInstance turns one config row into a loaded channel, or nil when the row
// is disabled or its configuration is unusable.
func (m *NotificationManager) buildInstance(cfg models.NotificationConfig) *ChannelInstance {
	if !cfg.Enabled {
		return nil
	}
	plugin, err := BuildPluginFromConfig(cfg)
	if err != nil {
		m.logger.Printf("[notify] channel %q (%s) not loaded: %v", cfg.Name, cfg.Channel, err)
		return nil
	}
	return &ChannelInstance{ID: cfg.ID, Name: cfg.Name, Type: cfg.Channel, Plugin: plugin}
}

// applyConfig registers, replaces, or removes one channel from its config row.
func (m *NotificationManager) applyConfig(cfg models.NotificationConfig) {
	inst := m.buildInstance(cfg)
	if inst == nil {
		m.removeInstance(cfg.ID)
		return
	}
	m.setInstance(inst)
	m.logger.Printf("[notify] channel %s loaded", inst.Label())
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
func (m *NotificationManager) SendToChannel(ctx context.Context, id uuid.UUID, message *NotificationMessage) error {
	inst, ok := m.Instance(id)
	if !ok {
		return fmt.Errorf("notification channel %s is not loaded", id)
	}
	if err := inst.Plugin.Send(ctx, message); err != nil {
		return fmt.Errorf("sending via %s: %w", inst.Label(), err)
	}
	m.logger.Printf("[notify] message sent via %s", inst.Label())
	return nil
}

// SendToChannelType delivers through any one loaded channel of the given type.
// Used by the manual-send endpoint, which names a type rather than an instance.
func (m *NotificationManager) SendToChannelType(ctx context.Context, channelType string, message *NotificationMessage) error {
	for _, inst := range m.Instances() {
		if inst.Type == channelType {
			return m.SendToChannel(ctx, inst.ID, message)
		}
	}
	return fmt.Errorf("no loaded notification channel of type %q", channelType)
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
	// update this specific record with the outcome. Records written before
	// channels had instance identity carry only a type, so fall back to that.
	var sendErr error
	if record.ChannelID != nil {
		sendErr = m.SendToChannel(ctx, *record.ChannelID, message)
	} else {
		sendErr = m.SendToChannelType(ctx, record.Channel, message)
	}

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
