package models

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Dashboard limits (spec 2026-10-02 §1, §3).
const (
	MaxDashboardWidgets        = 50
	MaxWidgetConfigBytes       = 16 * 1024
	DashboardGridColumns       = 12
	MaxWidgetHeight            = 24
	MaxDashboardNameLen        = 100
	MaxDashboardDescriptionLen = 500
	MaxWidgetTitleLen          = 100
)

// Dashboard is a grid of widgets. Exactly one of SiteID and OwnerID is set
// (dashboards_owner_xor_site): a site's dashboard follows the site's sharing,
// a personal one belongs to its owner.
type Dashboard struct {
	ID          uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name        string     `json:"name" gorm:"column:name;not null"`
	Description string     `json:"description" gorm:"column:description;not null"`
	SiteID      *uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid"`
	OwnerID     *uuid.UUID `json:"owner_id" gorm:"column:owner_id;type:uuid"`
	CreatedBy   *uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid"`
	// Version goes up by one on every save; a save carrying an older version
	// is refused, so two editors cannot overwrite each other. No gorm default:
	// GORM would omit an explicit value equal to the zero value.
	Version   int       `json:"version" gorm:"column:version;not null"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table name.
func (Dashboard) TableName() string { return "dashboards" }

// DashboardWidget is one widget: its type, its config (shaped by the type's
// own code) and its place on the 12-column grid.
type DashboardWidget struct {
	ID          uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	DashboardID uuid.UUID `json:"dashboard_id" gorm:"column:dashboard_id;type:uuid;not null"`
	Type        string    `json:"type" gorm:"column:type;not null"`
	Title       string    `json:"title" gorm:"column:title;not null"`
	Config      RawJSON   `json:"config" gorm:"column:config;type:jsonb;not null"`
	X           int       `json:"x" gorm:"column:x;not null"`
	Y           int       `json:"y" gorm:"column:y;not null"`
	W           int       `json:"w" gorm:"column:w;not null"`
	H           int       `json:"h" gorm:"column:h;not null"`
}

// TableName pins the table name.
func (DashboardWidget) TableName() string { return "dashboard_widgets" }

// DashboardShare grants one user access to one personal dashboard.
// Permission is PermissionReadonly or PermissionEditable.
type DashboardShare struct {
	DashboardID uuid.UUID `json:"dashboard_id" gorm:"column:dashboard_id;type:uuid;primaryKey"`
	UserID      uuid.UUID `json:"user_id" gorm:"column:user_id;type:uuid;primaryKey"`
	Permission  string    `json:"permission" gorm:"column:permission;not null"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName pins the table name.
func (DashboardShare) TableName() string { return "dashboard_sharing" }

// DashboardPublicLink is a dashboard's one public link. Deleting the row
// revokes it; regenerating replaces the token.
type DashboardPublicLink struct {
	DashboardID uuid.UUID `json:"dashboard_id" gorm:"column:dashboard_id;type:uuid;primaryKey"`
	Token       string    `json:"token" gorm:"column:token;not null"`
	CreatedBy   uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid;not null"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName pins the table name.
func (DashboardPublicLink) TableName() string { return "dashboard_public_links" }

// RawJSON is a JSONB column kept as raw bytes: a widget's config, whose shape
// only the widget type's own code knows. Empty marshals as {}.
type RawJSON json.RawMessage

// Value writes the bytes as JSON text.
func (r RawJSON) Value() (driver.Value, error) {
	if len(r) == 0 {
		return "{}", nil
	}
	return string(r), nil
}

// Scan copies the column's bytes (the driver may reuse its buffer).
func (r *RawJSON) Scan(value any) error {
	if value == nil {
		*r = RawJSON("{}")
		return nil
	}
	b, err := asBytes(value)
	if err != nil {
		return err
	}
	*r = append(RawJSON(nil), b...)
	return nil
}

// MarshalJSON emits the stored JSON as is.
func (r RawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("{}"), nil
	}
	return []byte(r), nil
}

// UnmarshalJSON keeps the raw bytes.
func (r *RawJSON) UnmarshalJSON(b []byte) error {
	*r = append(RawJSON(nil), b...)
	return nil
}
