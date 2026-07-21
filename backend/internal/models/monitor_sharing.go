package models

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Monitor sharing permission levels.
const (
	PermissionReadonly = "readonly"
	PermissionEditable = "editable"
)

// ValidSharePermissions is the set of accepted permission values.
var ValidSharePermissions = map[string]bool{
	PermissionReadonly: true,
	PermissionEditable: true,
}

// MonitorSharing grants another user access to a monitor at a permission level.
type MonitorSharing struct {
	ID               uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	MonitorID        uuid.UUID `json:"monitor_id" gorm:"column:monitor_id;type:uuid;not null"`
	SharedWithUserID uuid.UUID `json:"shared_with_user_id" gorm:"column:shared_with_user_id;type:uuid;not null"`
	Permission       string    `json:"permission" gorm:"column:permission"` // readonly | editable
	SharedByUserID   uuid.UUID `json:"shared_by_user_id" gorm:"column:shared_by_user_id;type:uuid;not null"`
	CreatedAt        time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt        time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the MonitorSharing model.
func (MonitorSharing) TableName() string {
	return "monitor_sharing"
}

// ValidateSharePermission returns an error for an unknown permission value.
func ValidateSharePermission(p string) error {
	if !ValidSharePermissions[p] {
		return errors.New("permission must be 'readonly' or 'editable'")
	}
	return nil
}
