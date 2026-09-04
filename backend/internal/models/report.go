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
)

// ValidScopeTypes lists the accepted scope_type values.
var ValidScopeTypes = map[string]bool{
	ScopeTypeMonitors: true,
	ScopeTypeTags:     true,
	ScopeTypeGroups:   true,
}

// Report section names a template may include.
const (
	SectionSLACompliance   = "sla_compliance"
	SectionIncidentSummary = "incident_summary"
	SectionCharts          = "charts"
	SectionCustom          = "custom"
)

// ValidReportSections lists the accepted section names.
var ValidReportSections = map[string]bool{
	SectionSLACompliance:   true,
	SectionIncidentSummary: true,
	SectionCharts:          true,
	SectionCustom:          true,
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
	default:
		return errors.New("unknown scope_type: " + scopeType)
	}
	return nil
}

// ReportTemplate defines which sections a report renders, in order.
type ReportTemplate struct {
	ID        uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name      string      `json:"name" gorm:"column:name;not null"`
	IsDefault bool        `json:"is_default" gorm:"column:is_default"`
	Sections  StringSlice `json:"sections" gorm:"column:sections_json;type:jsonb;not null"`
	CreatedAt time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the ReportTemplate model.
func (ReportTemplate) TableName() string {
	return "report_templates"
}

// Validate checks the template names at least one recognized section.
func (rt *ReportTemplate) Validate() error {
	if rt.Name == "" {
		return errors.New("template name is required")
	}
	if len(rt.Sections) == 0 {
		return errors.New("template must define at least one section")
	}
	for _, s := range rt.Sections {
		if !ValidReportSections[s] {
			return errors.New("unknown report section: " + s)
		}
	}
	return nil
}

// Report is a saved report definition. Generating it produces a ReportGeneration.
type Report struct {
	ID                uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	UserID            uuid.UUID   `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	Name              string      `json:"name" gorm:"column:name;not null"`
	TemplateID        uuid.UUID   `json:"template_id" gorm:"column:template_id;type:uuid;not null"`
	ScopeType         string      `json:"scope_type" gorm:"column:scope_type;not null"`
	ScopeData         ReportScope `json:"scope_data" gorm:"column:scope_data;type:jsonb;not null"`
	TimeRangeDays     int         `json:"time_range_days" gorm:"column:time_range_days;not null"`
	CustomTitle       *string     `json:"custom_title" gorm:"column:custom_title"`
	CustomDescription *string     `json:"custom_description" gorm:"column:custom_description"`
	CreatedAt         time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt         time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
	CreatedBy         uuid.UUID   `json:"created_by" gorm:"column:created_by;type:uuid;not null"`
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
	if r.TemplateID == uuid.Nil {
		return errors.New("template_id is required")
	}
	if !ValidScopeTypes[r.ScopeType] {
		return errors.New("scope_type must be one of: monitors, tags, groups")
	}
	if r.TimeRangeDays <= 0 {
		return errors.New("time_range_days must be greater than zero")
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
	CreatedAt  time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName tells GORM which table backs the ReportAccess model.
func (ReportAccess) TableName() string {
	return "report_access"
}
