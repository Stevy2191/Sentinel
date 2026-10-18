package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

// FaceplateLayout is an admin's front-panel layout for one device model
// (migration 062). ModelKey is portmon.NormalizeModel(Model).
type FaceplateLayout struct {
	ID        uuid.UUID       `json:"id" gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	Model     string          `json:"model" gorm:"column:model;not null"`
	ModelKey  string          `json:"model_key" gorm:"column:model_key;not null"`
	Groups    FaceplateGroups `json:"groups" gorm:"column:groups;type:jsonb;not null"`
	CreatedBy *uuid.UUID      `json:"created_by" gorm:"column:created_by;type:uuid"`
	UpdatedBy *uuid.UUID      `json:"updated_by" gorm:"column:updated_by;type:uuid"`
	CreatedAt time.Time       `json:"created_at" gorm:"column:created_at"`
	UpdatedAt time.Time       `json:"updated_at" gorm:"column:updated_at"`
}

func (FaceplateLayout) TableName() string { return "faceplate_layouts" }

// FaceplateGroups is a layout's groups, stored as JSON.
type FaceplateGroups []portmon.LayoutGroup

func (g FaceplateGroups) Value() (driver.Value, error) {
	if g == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(g)
}

func (g *FaceplateGroups) Scan(value any) error {
	if value == nil {
		*g = nil
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning FaceplateGroups: %w", err)
	}
	return json.Unmarshal(data, g)
}
