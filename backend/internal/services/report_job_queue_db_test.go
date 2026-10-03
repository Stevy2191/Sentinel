package services

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// runningJob inserts a report and a job for it that a worker has claimed
// attempts times.
func runningJob(t *testing.T, db *gorm.DB, attempts int) models.ReportJob {
	t.Helper()
	user := testdb.NewUser(t, db, false)
	reportID := uuid.New()
	testdb.Exec(t, db, `INSERT INTO reports (id, user_id, name, report_type, scope_type, scope_data, time_range_days, created_by)
		VALUES (?, ?, 'r', 'uptime', 'monitors', '{"monitor_ids":[]}', 7, ?)`, reportID, user, user)
	job := models.ReportJob{ID: uuid.New(), ReportID: reportID, RequestedBy: user, Status: models.JobRunning, Attempts: attempts}
	testdb.Must(t, db.Create(&job).Error)
	return job
}

// A report too large to build fails at once with its own message; anything
// else is still put back in the queue while attempts remain.
func TestDBTooLargeReportIsNotRetried(t *testing.T) {
	db := testdb.Open(t)
	q := NewReportJobQueue(db, nil, 1)

	big := runningJob(t, db, 1)
	q.finishFailed(&big, fmt.Errorf("aggregating report data: %w", ErrReportTooLarge))
	var got models.ReportJob
	testdb.Must(t, db.First(&got, "id = ?", big.ID).Error)
	if got.Status != models.JobFailed || got.FinishedAt == nil || got.Error == nil ||
		*got.Error != "This report is too large to build: narrow the scope or shorten the period" {
		t.Errorf("too-large job = status %q, finished %v, error %v; want failed at once with the too-large message",
			got.Status, got.FinishedAt, got.Error)
	}

	blip := runningJob(t, db, 1)
	q.finishFailed(&blip, errors.New("aggregating report data: connection reset by peer"))
	got = models.ReportJob{}
	testdb.Must(t, db.First(&got, "id = ?", blip.ID).Error)
	if got.Status != models.JobQueued || got.FinishedAt != nil {
		t.Errorf("transient failure = status %q, finished %v; want it queued again", got.Status, got.FinishedAt)
	}
}
