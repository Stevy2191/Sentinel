// Package models defines the core domain entities for Sentinel and their
// mapping to the PostgreSQL schema via GORM.
package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
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
	CreatedAt          time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt          time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the Monitor model.
func (Monitor) TableName() string {
	return "monitors"
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
	case MonitorTypeHTTP, MonitorTypeTCP, MonitorTypePing, MonitorTypeDNS, MonitorTypeWebhook:
		// valid
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
	CreatedAt       time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt       time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the Incident model.
func (Incident) TableName() string {
	return "incidents"
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
