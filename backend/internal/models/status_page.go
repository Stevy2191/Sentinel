package models

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Slug length bounds for a status page.
const (
	minSlugLen = 3
	maxSlugLen = 50
	maxNameLen = 255
)

var (
	slugPattern = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)
	hexPattern  = regexp.MustCompile(`^#([0-9a-fA-F]{6}|[0-9a-fA-F]{3})$`)
)

// StatusPage is a public, shareable uptime page addressed by a unique slug.
type StatusPage struct {
	ID          uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Slug        string    `json:"slug" gorm:"column:slug;uniqueIndex;not null"`
	Name        string    `json:"name" gorm:"column:name;not null"`
	Description string    `json:"description" gorm:"column:description"`
	LogoURL     string    `json:"logo_url" gorm:"column:logo_url"`
	ThemeColor  string    `json:"theme_color" gorm:"column:theme_color"`
	Published   bool      `json:"published" gorm:"column:published;default:true"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the StatusPage model.
func (StatusPage) TableName() string {
	return "status_pages"
}

// Validate checks that the status page's fields are well-formed. It returns a
// descriptive error for the first problem found, or nil if valid.
func (s *StatusPage) Validate() error {
	slug := strings.TrimSpace(s.Slug)
	if slug == "" {
		return errors.New("slug is required")
	}
	if len(slug) < minSlugLen || len(slug) > maxSlugLen {
		return fmt.Errorf("slug must be between %d and %d characters, got %d", minSlugLen, maxSlugLen, len(slug))
	}
	if !slugPattern.MatchString(slug) {
		return errors.New("slug may contain only letters, numbers, and hyphens")
	}

	name := strings.TrimSpace(s.Name)
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("name must be at most %d characters", maxNameLen)
	}

	if s.ThemeColor != "" && !hexPattern.MatchString(s.ThemeColor) {
		return fmt.Errorf("theme_color must be a hex color like #10b981, got %q", s.ThemeColor)
	}

	return nil
}

// StatusPageMonitor is the join between a status page and a monitor, with
// optional grouping and display ordering. A monitor may appear on a page at
// most once.
type StatusPageMonitor struct {
	ID           uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	StatusPageID uuid.UUID `json:"status_page_id" gorm:"column:status_page_id;type:uuid;not null;uniqueIndex:idx_status_page_monitor"`
	MonitorID    uuid.UUID `json:"monitor_id" gorm:"column:monitor_id;type:uuid;not null;uniqueIndex:idx_status_page_monitor"`
	GroupName    string    `json:"group_name" gorm:"column:group_name"`
	Position     int       `json:"position" gorm:"column:position"`
	CreatedAt    time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName tells GORM which table backs the StatusPageMonitor model.
func (StatusPageMonitor) TableName() string {
	return "status_page_monitors"
}
