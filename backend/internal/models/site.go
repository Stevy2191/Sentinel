package models

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Site is a place whose network Sentinel monitors: the unit that devices,
// maps, dashboards and network reports belong to, and the unit of sharing.
type Site struct {
	ID          uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name        string     `json:"name" gorm:"column:name;not null"`
	Description *string    `json:"description" gorm:"column:description"`
	Address     *string    `json:"address" gorm:"column:address"`
	CreatedBy   *uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid"`
	CreatedAt   time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table name.
func (Site) TableName() string { return "sites" }

// SiteSharing grants one user access to one site. Mirrors MonitorSharing;
// Permission is PermissionReadonly or PermissionEditable.
type SiteSharing struct {
	ID               uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID           uuid.UUID  `json:"site_id" gorm:"column:site_id;type:uuid;not null"`
	SharedWithUserID uuid.UUID  `json:"shared_with_user_id" gorm:"column:shared_with_user_id;type:uuid;not null"`
	Permission       string     `json:"permission" gorm:"column:permission;not null"`
	SharedByUserID   *uuid.UUID `json:"shared_by_user_id" gorm:"column:shared_by_user_id;type:uuid"`
	CreatedAt        time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table name.
func (SiteSharing) TableName() string { return "site_sharing" }

// SiteInput is what a create or update request supplies.
type SiteInput struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Address     *string `json:"address"`
}

// maxSiteNameLen matches the VARCHAR(255) column, counted in characters as
// Postgres counts them, not bytes.
const maxSiteNameLen = 255

// NormalizeSiteInput trims every field, turns blank optional fields into nil,
// and validates the name. Trimming happens here rather than in the database so
// " HQ" and "HQ" collide on the unique index instead of both being stored.
func NormalizeSiteInput(in SiteInput) (SiteInput, error) {
	out := SiteInput{Name: strings.TrimSpace(in.Name)}
	if out.Name == "" {
		return SiteInput{}, errors.New("name is required")
	}
	if utf8.RuneCountInString(out.Name) > maxSiteNameLen {
		return SiteInput{}, errors.New("name must be 255 characters or fewer")
	}
	out.Description = trimOptional(in.Description)
	out.Address = trimOptional(in.Address)
	return out, nil
}

func trimOptional(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}
