package models

import (
	"time"

	"github.com/google/uuid"
)

// Report job states.
const (
	// JobQueued is waiting for a worker.
	JobQueued = "queued"
	// JobRunning has been claimed by a worker.
	JobRunning = "running"
	// JobSucceeded produced a generation; GenerationID is set.
	JobSucceeded = "succeeded"
	// JobFailed gave up; Error explains why.
	JobFailed = "failed"
)

// MaxJobAttempts bounds retries. A job that has failed this many times is left
// failed rather than cycling forever: the usual causes (a deleted monitor, an
// unwritable output directory) do not fix themselves.
const MaxJobAttempts = 3

// ReportJob is one queued request to render a report.
type ReportJob struct {
	ID          uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	ReportID    uuid.UUID `json:"report_id" gorm:"column:report_id;type:uuid;not null"`
	RequestedBy uuid.UUID `json:"requested_by" gorm:"column:requested_by;type:uuid;not null"`
	Status      string    `json:"status" gorm:"column:status;not null"`
	// GenerationID is the rendered artifact, set only on success.
	GenerationID *uuid.UUID `json:"generation_id" gorm:"column:generation_id;type:uuid"`
	Error        *string    `json:"error" gorm:"column:error"`
	Attempts     int        `json:"attempts" gorm:"column:attempts"`
	CreatedAt    time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	StartedAt    *time.Time `json:"started_at" gorm:"column:started_at"`
	FinishedAt   *time.Time `json:"finished_at" gorm:"column:finished_at"`
}

// TableName tells GORM which table backs the ReportJob model.
func (ReportJob) TableName() string {
	return "report_jobs"
}

// IsTerminal reports whether the job has stopped moving.
func (j *ReportJob) IsTerminal() bool {
	return j.Status == JobSucceeded || j.Status == JobFailed
}
