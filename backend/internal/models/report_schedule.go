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
	// ScheduleTypeQuarterly delivers on the first day of each quarter, which is
	// when a report covering the quarter that just ended becomes worth sending.
	ScheduleTypeQuarterly = "quarterly"
	ScheduleTypeCustom    = "custom"
)

// ValidScheduleTypes lists the accepted schedule_type values.
var ValidScheduleTypes = map[string]bool{
	ScheduleTypeDaily:     true,
	ScheduleTypeWeekly:    true,
	ScheduleTypeMonthly:   true,
	ScheduleTypeQuarterly: true,
	ScheduleTypeCustom:    true,
}

// MaxScheduleDayOfMonth is capped below 29 on purpose: a schedule set to the
// 31st would not fire in February at all, and a report that silently skips a
// month is worse than one that arrives on the 28th.
const MaxScheduleDayOfMonth = 28

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
	// SendHour and SendMinute are the local time of day the report is sent,
	// read in the instance's report timezone rather than the server process's.
	SendHour   int `json:"send_hour" gorm:"column:send_hour;default:8"`
	SendMinute int `json:"send_minute" gorm:"column:send_minute;default:0"`
	// DayOfWeek (0 = Sunday) applies to the weekly cadence, DayOfMonth to the
	// monthly and quarterly ones. Nil keeps the original behaviour: Monday, and
	// the 1st.
	DayOfWeek  *int `json:"day_of_week" gorm:"column:day_of_week"`
	DayOfMonth *int `json:"day_of_month" gorm:"column:day_of_month"`
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
		return errors.New("schedule_type must be one of: daily, weekly, monthly, quarterly, custom")
	}
	if rs.SendHour < 0 || rs.SendHour > 23 {
		return fmt.Errorf("send_hour must be between 0 and 23, got %d", rs.SendHour)
	}
	if rs.SendMinute < 0 || rs.SendMinute > 59 {
		return fmt.Errorf("send_minute must be between 0 and 59, got %d", rs.SendMinute)
	}
	if rs.DayOfWeek != nil && (*rs.DayOfWeek < 0 || *rs.DayOfWeek > 6) {
		return fmt.Errorf("day_of_week must be between 0 and 6, got %d", *rs.DayOfWeek)
	}
	if rs.DayOfMonth != nil && (*rs.DayOfMonth < 1 || *rs.DayOfMonth > MaxScheduleDayOfMonth) {
		return fmt.Errorf("day_of_month must be between 1 and %d, got %d",
			MaxScheduleDayOfMonth, *rs.DayOfMonth)
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
