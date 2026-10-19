package models

import (
	"time"

	"github.com/google/uuid"
)

// SiteMapPosition is one device's place on its site's map (migration 064).
// HasPosition false means the map lays it out itself; the row may exist only
// to say the device is hidden, or was added by an editor.
type SiteMapPosition struct {
	SiteID      uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid;primaryKey"`
	DeviceID    uuid.UUID `json:"device_id" gorm:"column:device_id;type:uuid;primaryKey"`
	X           float64   `json:"x" gorm:"column:x"`
	Y           float64   `json:"y" gorm:"column:y"`
	HasPosition bool      `json:"has_position" gorm:"column:has_position"`
	Hidden      bool      `json:"hidden" gorm:"column:hidden"`
	Added       bool      `json:"added" gorm:"column:added"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at"`
}

func (SiteMapPosition) TableName() string { return "site_map_positions" }

// Site map link kinds: a link someone drew, or a found link someone hid.
const (
	SiteMapLinkManual = "manual"
	SiteMapLinkHidden = "hidden"
)

// SiteMapLink is a hand-drawn link, or a "hidden" override on a found one
// (matched by its ends). A nil if_index is the device itself, port unknown.
type SiteMapLink struct {
	ID        uuid.UUID  `json:"id" gorm:"column:id;type:uuid;primaryKey;default:gen_random_uuid()"`
	SiteID    uuid.UUID  `json:"site_id" gorm:"column:site_id;type:uuid"`
	Kind      string     `json:"kind" gorm:"column:kind"`
	ADeviceID uuid.UUID  `json:"a_device_id" gorm:"column:a_device_id;type:uuid"`
	AIfIndex  *int       `json:"a_if_index" gorm:"column:a_if_index"`
	BDeviceID uuid.UUID  `json:"b_device_id" gorm:"column:b_device_id;type:uuid"`
	BIfIndex  *int       `json:"b_if_index" gorm:"column:b_if_index"`
	CreatedBy *uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid"`
	CreatedAt time.Time  `json:"created_at" gorm:"column:created_at"`
}

func (SiteMapLink) TableName() string { return "site_map_links" }

// SiteMapPublicLink is a site map's read-only public link.
type SiteMapPublicLink struct {
	SiteID    uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid;primaryKey"`
	Token     string    `json:"token" gorm:"column:token"`
	CreatedBy uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at"`
}

func (SiteMapPublicLink) TableName() string { return "site_map_public_links" }
