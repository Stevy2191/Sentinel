package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Schedule cadences.
const (
	ScheduleTypeDaily   = "daily"
	ScheduleTypeWeekly  = "weekly"
	ScheduleTypeMonthly = "monthly"
	ScheduleTypeCustom  = "custom"
)

// ValidScheduleTypes lists the accepted schedule_type values.
var ValidScheduleTypes = map[string]bool{
	ScheduleTypeDaily:   true,
	ScheduleTypeWeekly:  true,
	ScheduleTypeMonthly: true,
	ScheduleTypeCustom:  true,
}

// maxScheduleRecipients caps the recipient list. A schedule sends mail on a
// timer without further review, so an unbounded list is a standing amplifier.
const maxScheduleRecipients = 50

// EmailInclusions is the JSONB payload on report_schedules.include_in_email:
// what the delivery email carries besides the attachment.
type EmailInclusions struct {
	IncludeLink    bool `json:"include_link"`
	IncludeSummary bool `json:"include_summary"`
}

// Value serializes the inclusions to JSON for storage.
func (e EmailInclusions) Value() (driver.Value, error) {
	return json.Marshal(e)
}

// Scan deserializes a JSONB value into the inclusions.
func (e *EmailInclusions) Scan(value any) error {
	if value == nil {
		*e = EmailInclusions{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning EmailInclusions: %w", err)
	}
	return json.Unmarshal(data, e)
}

// ReportSchedule delivers a report to a recipient list on a cadence.
type ReportSchedule struct {
	ID           uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	ReportID     uuid.UUID `json:"report_id" gorm:"column:report_id;type:uuid;not null"`
	UserID       uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;not null"`
	ScheduleType string    `json:"schedule_type" gorm:"column:schedule_type;not null"`
	// CronExpression applies only to the "custom" cadence.
	CronExpression *string `json:"cron_expression" gorm:"column:cron_expression"`
	// EmailRecipients is a non-empty list of validated addresses.
	EmailRecipients  StringSlice     `json:"email_recipients" gorm:"column:email_recipients;type:jsonb;not null"`
	SendAsAttachment bool            `json:"send_as_attachment" gorm:"column:send_as_attachment"`
	IncludeInEmail   EmailInclusions `json:"include_in_email" gorm:"column:include_in_email;type:jsonb"`
	// CronEntryID is the cron entry this schedule is registered under in the
	// CURRENT process, or nil when it is not registered. It is not stable across
	// restarts - see migration 017 - so it is for observability only and is
	// never used to decide which job to remove. Not serialized to the API.
	CronEntryID *int       `json:"-" gorm:"column:cron_entry_id"`
	LastRunAt   *time.Time `json:"last_run_at" gorm:"column:last_run_at"`
	NextRunAt   *time.Time `json:"next_run_at" gorm:"column:next_run_at"`
	IsActive    bool       `json:"is_active" gorm:"column:is_active"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the ReportSchedule model.
func (ReportSchedule) TableName() string {
	return "report_schedules"
}

// Validate checks the schedule before it is persisted or registered.
func (rs *ReportSchedule) Validate() error {
	if rs.ReportID == uuid.Nil {
		return errors.New("report_id is required")
	}
	if !ValidScheduleTypes[rs.ScheduleType] {
		return errors.New("schedule_type must be one of: daily, weekly, monthly, custom")
	}
	// A custom cadence with no expression would silently fall back to a daily
	// run, delivering mail the operator never asked for.
	if rs.ScheduleType == ScheduleTypeCustom {
		if rs.CronExpression == nil || strings.TrimSpace(*rs.CronExpression) == "" {
			return errors.New("cron_expression is required when schedule_type is \"custom\"")
		}
	}

	if len(rs.EmailRecipients) == 0 {
		return errors.New("at least one email recipient is required")
	}
	if len(rs.EmailRecipients) > maxScheduleRecipients {
		return fmt.Errorf("at most %d recipients are allowed, got %d", maxScheduleRecipients, len(rs.EmailRecipients))
	}
	// Recipients are validated here rather than at send time: a schedule fires
	// unattended, so a bad address should be rejected while someone is looking.
	for _, r := range rs.EmailRecipients {
		if _, err := mail.ParseAddress(strings.TrimSpace(r)); err != nil {
			return fmt.Errorf("invalid email recipient %q", r)
		}
	}
	return nil
}
