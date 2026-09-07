// Package models defines the core domain entities for Sentinel and their
// mapping to the PostgreSQL schema via GORM.
package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/netguard"
)

// Monitor type values.
const (
	MonitorTypeHTTP    = "http"
	MonitorTypeTCP     = "tcp"
	MonitorTypePing    = "ping"
	MonitorTypeDNS     = "dns"
	MonitorTypeWebhook = "webhook"
)

// Monitor status values (current_status).
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
	StatusUnknown = "unknown"
)

// Validation bounds for a monitor's schedule.
const (
	minIntervalSeconds = 10
	maxIntervalSeconds = 3600
	minTimeoutSeconds  = 1
	maxTimeoutSeconds  = 300
)

// StringMap is a string-keyed map persisted as a JSONB column. It implements
// driver.Valuer and sql.Scanner so GORM can read and write it transparently.
type StringMap map[string]string

// Value serializes the map to JSON for storage. A nil map is stored as SQL NULL.
func (m StringMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	return json.Marshal(m)
}

// Scan deserializes a JSONB value from the database into the map.
func (m *StringMap) Scan(value any) error {
	if value == nil {
		*m = nil
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning StringMap: %w", err)
	}
	return json.Unmarshal(data, m)
}

// StringSlice is a slice of strings persisted as a JSONB column. It implements
// driver.Valuer and sql.Scanner for transparent GORM persistence.
type StringSlice []string

// Value serializes the slice to JSON for storage. A nil slice is stored as SQL NULL.
func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return json.Marshal(s)
}

// Scan deserializes a JSONB value from the database into the slice.
func (s *StringSlice) Scan(value any) error {
	if value == nil {
		*s = nil
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning StringSlice: %w", err)
	}
	return json.Unmarshal(data, s)
}

// asBytes normalizes the raw value the database driver hands to Scan (either a
// []byte or a string) into a byte slice.
func asBytes(value any) ([]byte, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("unsupported source type %T", value)
	}
}

// Monitor represents a single monitored endpoint together with its check
// configuration and a denormalized snapshot of its most recent result.
type Monitor struct {
	ID                 uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name               string      `json:"name" gorm:"column:name;not null"`
	Description        string      `json:"description" gorm:"column:description"`
	Type               string      `json:"type" gorm:"column:type;not null"`
	URL                string      `json:"url" gorm:"column:url;not null"`
	Method             string      `json:"method" gorm:"column:method;default:GET"`
	Headers            StringMap   `json:"headers" gorm:"column:headers;type:jsonb"`
	Body               string      `json:"body" gorm:"column:body"`
	IntervalSeconds    int         `json:"interval_seconds" gorm:"column:interval_seconds;default:60"`
	TimeoutSeconds     int         `json:"timeout_seconds" gorm:"column:timeout_seconds;default:10"`
	Retries            int         `json:"retries" gorm:"column:retries;default:0"`
	CurrentStatus      string      `json:"current_status" gorm:"column:current_status;default:unknown"`
	LastCheckAt        *time.Time  `json:"last_check_at" gorm:"column:last_check_at"`
	LastResponseTimeMs int         `json:"last_response_time_ms" gorm:"column:last_response_time_ms"`
	Enabled            bool        `json:"enabled" gorm:"column:enabled;default:true"`
	Tags               StringSlice `json:"tags" gorm:"column:tags;type:jsonb"`
	// NotifyChannels selects which notification channels this monitor alerts on.
	// nil means every enabled channel (the default); an empty slice means none.
	NotifyChannels StringSlice `json:"notify_channels" gorm:"column:notify_channels;type:jsonb"`
	GroupID        *uuid.UUID  `json:"group_id" gorm:"column:group_id;type:uuid"`
	// SLATarget is the uptime percentage this monitor is held to (e.g. 99.9).
	// Nil means no SLA is defined and compliance is not evaluated.
	SLATarget *float64   `json:"sla_target" gorm:"column:sla_target"`
	OwnerID   *uuid.UUID `json:"owner_id" gorm:"column:owner_id;type:uuid"`
	CreatedAt time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`

	// Maintenance mode: during an active window, failed checks are recorded but
	// do not open incidents or fire notifications.
	MaintenanceModeEnabled bool       `json:"maintenance_mode_enabled" gorm:"column:maintenance_mode_enabled"`
	MaintenanceStart       *time.Time `json:"maintenance_start" gorm:"column:maintenance_start;type:timestamptz"`
	MaintenanceEnd         *time.Time `json:"maintenance_end" gorm:"column:maintenance_end;type:timestamptz"`
}

// NotifiesAnyChannel reports whether this monitor should alert at all. A nil
// NotifyChannels means "every enabled channel"; an explicitly empty one means
// the monitor has been opted out of notifications.
func (m *Monitor) NotifiesAnyChannel() bool {
	return m.NotifyChannels == nil || len(m.NotifyChannels) > 0
}

// IsInMaintenanceWindow reports whether the monitor is currently within an
// active maintenance window.
func (m *Monitor) IsInMaintenanceWindow(now time.Time) bool {
	if !m.MaintenanceModeEnabled || m.MaintenanceStart == nil || m.MaintenanceEnd == nil {
		return false
	}
	return now.After(*m.MaintenanceStart) && now.Before(*m.MaintenanceEnd)
}

// GetMaintenanceCountdown returns the time remaining in the current maintenance
// window, or nil if the monitor is not currently in maintenance.
func (m *Monitor) GetMaintenanceCountdown(now time.Time) *time.Duration {
	if !m.IsInMaintenanceWindow(now) {
		return nil
	}
	d := m.MaintenanceEnd.Sub(now)
	if d > 0 {
		return &d
	}
	return nil
}

// TableName tells GORM which table backs the Monitor model.
func (Monitor) TableName() string {
	return "monitors"
}

// validateHTTPURL rejects anything but a well-formed http(s) URL with a host.
// This matters beyond correctness: monitor URLs are rendered as clickable
// <a href> links in the frontend, so accepting arbitrary schemes (e.g.
// "javascript:...") would allow a stored-XZZ payload disguised as a monitor
// target.
func validateHTTPURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url must start with http:// or https://, got %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("url must include a host, got %q", raw)
	}
	// Fast-fail when the host is a literal IP that's already known to be
	// blocked (e.g. a cloud metadata address). This can't catch a hostname
	// that resolves to a blocked address later (DNS rebinding) - that's
	// enforced at check-execution time in CheckService via netguard.DialControl.
	if ip := net.ParseIP(u.Hostname()); ip != nil && netguard.IsBlocked(ip) {
		return fmt.Errorf("url targets a disallowed network address: %q", ip)
	}
	return nil
}

// Validate checks that the monitor's fields form a coherent, storable
// configuration. It returns a descriptive error for the first problem found, or
// nil if the monitor is valid.
func (m *Monitor) Validate() error {
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("monitor name is required")
	}
	if strings.TrimSpace(m.URL) == "" {
		return errors.New("monitor url is required")
	}

	switch m.Type {
	case MonitorTypeHTTP, MonitorTypeWebhook:
		if err := validateHTTPURL(m.URL); err != nil {
			return err
		}
	case MonitorTypeTCP, MonitorTypePing, MonitorTypeDNS:
		// Valid, and deliberately not URL-checked: these targets are
		// host[:port] strings, not URLs. Without this branch they fall through
		// to default and are rejected as invalid types - by an error message
		// that lists them as valid.
	default:
		return fmt.Errorf("invalid monitor type %q: must be one of http, tcp, ping, dns, webhook", m.Type)
	}

	if m.IntervalSeconds < minIntervalSeconds || m.IntervalSeconds > maxIntervalSeconds {
		return fmt.Errorf("interval_seconds must be between %d and %d, got %d", minIntervalSeconds, maxIntervalSeconds, m.IntervalSeconds)
	}
	if m.TimeoutSeconds < minTimeoutSeconds || m.TimeoutSeconds > maxTimeoutSeconds {
		return fmt.Errorf("timeout_seconds must be between %d and %d, got %d", minTimeoutSeconds, maxTimeoutSeconds, m.TimeoutSeconds)
	}
	if m.TimeoutSeconds >= m.IntervalSeconds {
		return fmt.Errorf("timeout_seconds (%d) must be less than interval_seconds (%d)", m.TimeoutSeconds, m.IntervalSeconds)
	}

	return nil
}

// IsValid reports whether the monitor passes Validate.
func (m *Monitor) IsValid() bool {
	return m.Validate() == nil
}

// Check is a single point-in-time result of probing a monitor. Checks are
// append-only and form the history used for uptime and SLA reporting.
type Check struct {
	ID             int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	MonitorID      uuid.UUID `json:"monitor_id" gorm:"column:monitor_id;type:uuid;not null"`
	Status         string    `json:"status" gorm:"column:status;not null"`
	ResponseTimeMs int       `json:"response_time_ms" gorm:"column:response_time_ms"`
	StatusCode     int       `json:"status_code" gorm:"column:status_code"`
	ErrorMessage   string    `json:"error_message" gorm:"column:error_message"`
	Timestamp      time.Time `json:"timestamp" gorm:"column:timestamp;not null"`
}

// TableName tells GORM which table backs the Check model.
func (Check) TableName() string {
	return "checks"
}

// Incident represents a period of downtime for a monitor, opened when it goes
// offline and closed when it recovers, enriched with human-authored context.
type Incident struct {
	ID              uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	MonitorID       uuid.UUID  `json:"monitor_id" gorm:"column:monitor_id;type:uuid;not null"`
	StartTime       time.Time  `json:"start_time" gorm:"column:start_time;not null"`
	EndTime         *time.Time `json:"end_time" gorm:"column:end_time"`
	DurationSeconds int        `json:"duration_seconds" gorm:"column:duration_seconds"`
	Severity        string     `json:"severity" gorm:"column:severity"`
	RootCause       string     `json:"root_cause" gorm:"column:root_cause"`
	Notes           string     `json:"notes" gorm:"column:notes"`
	// ResolutionNotes records how the incident was resolved, as distinct from
	// Notes (general context). Both exist: Notes predates this and is already
	// part of the API contract.
	ResolutionNotes string    `json:"resolution_notes" gorm:"column:resolution_notes"`
	CreatedAt       time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the Incident model.
func (Incident) TableName() string {
	return "incidents"
}

// Status derives the incident's state. The incidents table has no status column;
// an incident is open until it is given an end time.
func (i *Incident) Status() string {
	if i.EndTime == nil {
		return "ongoing"
	}
	return "resolved"
}

// Notification is a record of an alert dispatched over a channel, optionally
// tied to the incident that triggered it.
type Notification struct {
	ID           uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	MonitorID    uuid.UUID  `json:"monitor_id" gorm:"column:monitor_id;type:uuid;not null"`
	IncidentID   *uuid.UUID `json:"incident_id" gorm:"column:incident_id;type:uuid"`
	Channel      string     `json:"channel" gorm:"column:channel;not null"`
	Status       string     `json:"status" gorm:"column:status;not null"`
	ErrorMessage string     `json:"error_message" gorm:"column:error_message"`
	SentAt       *time.Time `json:"sent_at" gorm:"column:sent_at"`
	CreatedAt    time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName tells GORM which table backs the Notification model.
func (Notification) TableName() string {
	return "notifications"
}
