package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Report scope types: how a report decides which monitors it covers.
const (
	ScopeTypeMonitors = "monitors"
	ScopeTypeTags     = "tags"
	ScopeTypeGroups   = "groups"
	// ScopeTypeTypes covers every monitor of a given check type. Distinct from
	// tags: a tag is something a person applied, a type is what the monitor
	// inherently is, so "all DNS monitors" needs no upkeep as monitors come and
	// go.
	ScopeTypeTypes = "types"
)

// ValidScopeTypes lists the accepted scope_type values.
var ValidScopeTypes = map[string]bool{
	ScopeTypeMonitors: true,
	ScopeTypeTags:     true,
	ScopeTypeGroups:   true,
	ScopeTypeTypes:    true,
}

// Report types. Every report is exactly one of these - there is no template
// system to configure sections from.
const (
	// ReportTypeUptime renders uptime vs. SLA: the summary tiles, a
	// cumulative-uptime graph, and a per-monitor SLA table.
	ReportTypeUptime = "uptime"
	// ReportTypeIncident renders the full incident list for the scope, with
	// root cause and resolution detail.
	ReportTypeIncident = "incident"
)

// ValidReportTypes lists the accepted report_type values.
var ValidReportTypes = map[string]bool{
	ReportTypeUptime:   true,
	ReportTypeIncident: true,
}

// Report access types.
const (
	AccessTypeOwner  = "owner"
	AccessTypeViewer = "viewer"
)

// ReportScope is the JSONB payload on reports.scope_data. Exactly one field is
// populated, matching the row's scope_type.
//
// This is a concrete struct rather than a generic JSON container so the scope
// can be resolved without re-parsing untyped maps at every call site.
type ReportScope struct {
	MonitorIDs []uuid.UUID `json:"monitor_ids,omitempty"`
	Tags       []string    `json:"tags,omitempty"`
	GroupIDs   []uuid.UUID `json:"group_ids,omitempty"`
	// Types holds monitor check types: http, tcp, ping, dns.
	Types []string `json:"types,omitempty"`
}

// Value serializes the scope to JSON for storage.
func (s ReportScope) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan deserializes a JSONB value from the database into the scope.
func (s *ReportScope) Scan(value any) error {
	if value == nil {
		*s = ReportScope{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning ReportScope: %w", err)
	}
	return json.Unmarshal(data, s)
}

// Validate checks that the scope carries the field its type requires. An empty
// scope is rejected: a report covering nothing is a configuration mistake, and
// silently producing an empty report hides it.
func (s ReportScope) Validate(scopeType string) error {
	switch scopeType {
	case ScopeTypeMonitors:
		if len(s.MonitorIDs) == 0 {
			return errors.New("scope_data.monitor_ids is required when scope_type is \"monitors\"")
		}
	case ScopeTypeTags:
		if len(s.Tags) == 0 {
			return errors.New("scope_data.tags is required when scope_type is \"tags\"")
		}
	case ScopeTypeGroups:
		if len(s.GroupIDs) == 0 {
			return errors.New("scope_data.group_ids is required when scope_type is \"groups\"")
		}
	case ScopeTypeTypes:
		if len(s.Types) == 0 {
			return errors.New("scope_data.types is required when scope_type is \"types\"")
		}
		// Checked here rather than left to the query: an unknown type would
		// silently resolve to no monitors and produce an empty report that
		// looks like "nothing happened".
		for _, t := range s.Types {
			if !ReportableMonitorTypes[t] {
				return errors.New("unknown monitor type in scope_data.types: " + t)
			}
		}
	default:
		return errors.New("unknown scope_type: " + scopeType)
	}
	return nil
}

// Report is a saved report definition. Generating it produces a ReportGeneration.
type Report struct {
	ID            uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	UserID        uuid.UUID   `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	Name          string      `json:"name" gorm:"column:name;not null"`
	ReportType    string      `json:"report_type" gorm:"column:report_type;not null"`
	ScopeType     string      `json:"scope_type" gorm:"column:scope_type;not null"`
	ScopeData     ReportScope `json:"scope_data" gorm:"column:scope_data;type:jsonb;not null"`
	TimeRangeDays int         `json:"time_range_days" gorm:"column:time_range_days;not null"`
	// How the window is worked out: rolling (TimeRangeDays back from now),
	// calendar (a whole month/quarter/week), or custom (explicit bounds).
	// Empty means rolling, which is what every row written before periods
	// existed is.
	PeriodKind        string     `json:"period_kind" gorm:"column:period_kind;default:rolling"`
	PeriodUnit        string     `json:"period_unit" gorm:"column:period_unit"`
	PeriodOffset      int        `json:"period_offset" gorm:"column:period_offset;default:0"`
	PeriodStart       *time.Time `json:"period_start" gorm:"column:period_start"`
	PeriodEnd         *time.Time `json:"period_end" gorm:"column:period_end"`
	CustomTitle       *string    `json:"custom_title" gorm:"column:custom_title"`
	CustomDescription *string    `json:"custom_description" gorm:"column:custom_description"`
	CreatedAt         time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
	CreatedBy         uuid.UUID  `json:"created_by" gorm:"column:created_by;type:uuid;not null"`
}

// TableName tells GORM which table backs the Report model.
func (Report) TableName() string {
	return "reports"
}

// Validate checks the report definition before it is persisted.
func (r *Report) Validate() error {
	if r.Name == "" {
		return errors.New("report name is required")
	}
	if !ValidReportTypes[r.ReportType] {
		return errors.New("report_type must be one of: uptime, incident")
	}
	if !ValidScopeTypes[r.ScopeType] {
		return errors.New("scope_type must be one of: monitors, tags, groups")
	}
	if err := r.ValidatePeriod(); err != nil {
		return err
	}
	return r.ScopeData.Validate(r.ScopeType)
}

// ReportGeneration is one rendered PDF, kept as history so a shared link can
// resolve to the exact artifact that was produced.
type ReportGeneration struct {
	ID          uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	ReportID    uuid.UUID `json:"report_id" gorm:"column:report_id;type:uuid;not null"`
	GeneratedAt time.Time `json:"generated_at" gorm:"column:generated_at;autoCreateTime"`
	PDFPath     string    `json:"pdf_path" gorm:"column:pdf_path;not null"`
	FileSize    *int      `json:"file_size" gorm:"column:file_size"`
	GeneratedBy uuid.UUID `json:"generated_by" gorm:"column:generated_by;type:uuid;not null"`
}

// TableName tells GORM which table backs the ReportGeneration model.
func (ReportGeneration) TableName() string {
	return "report_generations"
}

// ReportAccess grants a user or a share token access to a report. UserID is nil
// for a public share token that is not tied to an account.
type ReportAccess struct {
	ID         uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	ReportID   uuid.UUID  `json:"report_id" gorm:"column:report_id;type:uuid;not null"`
	UserID     *uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid"`
	AccessType string     `json:"access_type" gorm:"column:access_type;not null"`
	ShareToken *string    `json:"share_token,omitempty" gorm:"column:share_token"`
	// ExpiresAt bounds a share link's lifetime. Nil means it never expires,
	// which is what links created before expiry existed remain.
	ExpiresAt *time.Time `json:"expires_at" gorm:"column:expires_at"`
	CreatedAt time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// IsExpired reports whether this grant has lapsed. A grant with no expiry never
// lapses.
func (ra *ReportAccess) IsExpired(now time.Time) bool {
	return ra.ExpiresAt != nil && !ra.ExpiresAt.After(now)
}

// TableName tells GORM which table backs the ReportAccess model.
func (ReportAccess) TableName() string {
	return "report_access"
}
