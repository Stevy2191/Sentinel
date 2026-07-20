package models

import "time"

// Setting is a single persisted key/value application setting. Values are stored
// as text; typed accessors on the settings service handle parsing.
type Setting struct {
	Key       string    `json:"key" gorm:"column:key;primaryKey"`
	Value     string    `json:"value" gorm:"column:value;not null"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the Setting model.
func (Setting) TableName() string {
	return "settings"
}

// Setting keys. Kept as constants so callers don't hardcode strings.
const (
	// SettingRegistrationEnabled controls whether new users may self-register.
	SettingRegistrationEnabled = "registration_enabled"
)
