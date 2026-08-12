package models

import (
	"time"

	"github.com/google/uuid"
)

// MaintenanceHistory is a maintenance window a monitor has been placed under,
// kept after the window is cleared or replaced.
//
// The monitors table holds only the window currently configured, which is what
// the checker consults to decide whether to suppress an incident. This table is
// the record of what has happened, so reporting can attribute a past hour to
// maintenance long after that window is gone.
type MaintenanceHistory struct {
	ID        uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	MonitorID uuid.UUID `json:"monitor_id" gorm:"column:monitor_id;type:uuid;not null"`
	StartTime time.Time `json:"start_time" gorm:"column:start_time;type:timestamptz;not null"`
	EndTime   time.Time `json:"end_time" gorm:"column:end_time;type:timestamptz;not null"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the MaintenanceHistory model.
func (MaintenanceHistory) TableName() string {
	return "maintenance_histories"
}
