package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Audited actions.
const (
	ActionReportCreated    = "report_created"
	ActionReportDeleted    = "report_deleted"
	ActionScheduleCreated  = "schedule_created"
	ActionScheduleUpdated  = "schedule_updated"
	ActionScheduleDeleted  = "schedule_deleted"
	ActionScheduleRun      = "schedule_run"
	ActionShareLinkCreated = "share_link_created"
	ActionShareLinkRevoked = "share_link_revoked"
)

// Audited resource types.
const (
	ResourceReport    = "report"
	ResourceSchedule  = "schedule"
	ResourceShareLink = "share_link"
)

// AuditChanges is the JSONB detail on an audit entry. For an update it carries
// Before and After; for a create or delete, Summary describes the row.
type AuditChanges struct {
	Before  map[string]any `json:"before,omitempty"`
	After   map[string]any `json:"after,omitempty"`
	Summary map[string]any `json:"summary,omitempty"`
}

// Value serializes the changes to JSON for storage.
func (a AuditChanges) Value() (driver.Value, error) {
	return json.Marshal(a)
}

// Scan deserializes a JSONB value into the changes.
func (a *AuditChanges) Scan(value any) error {
	if value == nil {
		*a = AuditChanges{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning AuditChanges: %w", err)
	}
	return json.Unmarshal(data, a)
}

// AuditEntry is one recorded action.
type AuditEntry struct {
	ID uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	// UserID is nil once the account is deleted; Username preserves who it was.
	UserID       *uuid.UUID   `json:"user_id" gorm:"column:user_id;type:uuid"`
	Username     string       `json:"username" gorm:"column:username"`
	Action       string       `json:"action" gorm:"column:action;not null"`
	ResourceType string       `json:"resource_type" gorm:"column:resource_type;not null"`
	ResourceID   *uuid.UUID   `json:"resource_id" gorm:"column:resource_id;type:uuid"`
	Changes      AuditChanges `json:"changes" gorm:"column:changes;type:jsonb"`
	IPAddress    string       `json:"ip_address" gorm:"column:ip_address"`
	CreatedAt    time.Time    `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName tells GORM which table backs the AuditEntry model.
func (AuditEntry) TableName() string {
	return "audit_log"
}
