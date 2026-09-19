package models

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MaxIncidentCommentLength bounds one comment. Generous enough for a paragraph
// of reasoning, short of letting a paste of an entire log become a row.
const MaxIncidentCommentLength = 5000

// IncidentComment is one entry in an incident's thread.
//
// AuthorName is stored alongside UserID rather than being joined on demand, so
// the thread still reads correctly after the account is deleted. An incident
// record that develops holes because somebody left the company is worse than a
// denormalised name.
type IncidentComment struct {
	ID         uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	IncidentID uuid.UUID  `json:"incident_id" gorm:"column:incident_id;type:uuid;not null"`
	UserID     *uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid"`
	AuthorName string     `json:"author_name" gorm:"column:author_name;not null"`
	Body       string     `json:"body" gorm:"column:body;not null"`
	CreatedAt  time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt  time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the model.
func (IncidentComment) TableName() string {
	return "incident_comments"
}

// Validate checks a comment before it is stored.
func (c *IncidentComment) Validate() error {
	body := strings.TrimSpace(c.Body)
	if body == "" {
		return errors.New("a comment cannot be empty")
	}
	if len([]rune(body)) > MaxIncidentCommentLength {
		return errors.New("a comment must be 5000 characters or fewer")
	}
	if c.IncidentID == uuid.Nil {
		return errors.New("incident_id is required")
	}
	return nil
}
