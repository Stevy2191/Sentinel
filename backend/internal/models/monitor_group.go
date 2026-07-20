package models

import (
	"errors"
	"regexp"
	"time"

	"github.com/google/uuid"
)

var hexColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// MonitorGroup organizes monitors into a named, colored, orderable section on
// the dashboard.
type MonitorGroup struct {
	ID          uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name        string    `json:"name" gorm:"column:name;not null"`
	Description *string   `json:"description" gorm:"column:description"`
	Color       *string   `json:"color" gorm:"column:color"` // hex, e.g. #10b981
	Position    int       `json:"position" gorm:"column:position"`
	// Monitors is populated when a group is fetched with its members preloaded.
	Monitors  []Monitor `json:"monitors" gorm:"foreignKey:GroupID;references:ID"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the MonitorGroup model.
func (MonitorGroup) TableName() string {
	return "monitor_groups"
}

// Validate checks a group's user-editable fields.
func (g *MonitorGroup) Validate() error {
	if g.Name == "" {
		return errors.New("group name is required")
	}
	if len(g.Name) > 255 {
		return errors.New("group name must be at most 255 characters")
	}
	if g.Color != nil && *g.Color != "" && !hexColorPattern.MatchString(*g.Color) {
		return errors.New("color must be a hex value like #10b981")
	}
	return nil
}
