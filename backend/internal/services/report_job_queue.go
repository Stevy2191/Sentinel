// Package services - report_job_queue.go runs report rendering off the request
// path.
//
// The API enqueues a job and returns immediately; a small pool of workers
// claims jobs and renders them. Claiming uses SELECT ... FOR UPDATE SKIP LOCKED,
// so two workers - in this process or in another replica - can never take the
// same job, and a slow render never blocks a peer from picking up the next one.
package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

const (
	// jobPollInterval is how often an idle worker looks for work. Enqueue also
	// nudges the pool, so this is a backstop rather than the main path.
	jobPollInterval = 2 * time.Second
	// jobRunTimeout bounds a single render.
	jobRunTimeout = 5 * time.Minute
)

// ReportJobQueue accepts render requests and runs them on a worker pool.
type ReportJobQueue struct {
	db        *gorm.DB
	generator *ReportGenerator
	logger    *log.Logger

	workers int
	// wake carries a non-blocking nudge so an enqueue starts work immediately
	// instead of waiting for the next poll.
	wake chan struct{}
	stop chan struct{}
	wg   sync.WaitGroup
	once sync.Once
}

// NewReportJobQueue returns a queue backed by workers goroutines.
func NewReportJobQueue(db *gorm.DB, generator *ReportGenerator, workers int) *ReportJobQueue {
	if workers < 1 {
		workers = 2
	}
	return &ReportJobQueue{
		db:        db,
		generator: generator,
		logger:    log.Default(),
		workers:   workers,
		wake:      make(chan struct{}, 1),
		stop:      make(chan struct{}),
	}
}

// Enqueue records a render request and returns the job.
func (q *ReportJobQueue) Enqueue(ctx context.Context, reportID, requestedBy uuid.UUID) (*models.ReportJob, error) {
	job := &models.ReportJob{
		ID:          uuid.New(),
		ReportID:    reportID,
		RequestedBy: requestedBy,
		Status:      models.JobQueued,
	}
	if err := q.db.WithContext(ctx).Create(job).Error; err != nil {
		return nil, fmt.Errorf("enqueueing report job: %w", err)
	}
	q.nudge()
	return job, nil
}

// nudge wakes one idle worker without blocking if none is waiting.
func (q *ReportJobQueue) nudge() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// Start recovers interrupted jobs and launches the worker pool.
func (q *ReportJobQueue) Start(ctx context.Context) {
	// A job left "running" belongs to a process that is gone; nothing will ever
	// finish it. Put it back in the queue so a restart does not strand work.
	res := q.db.WithContext(ctx).Model(&models.ReportJob{}).
		Where("status = ?", models.JobRunning).
		Updates(map[string]interface{}{"status": models.JobQueued, "started_at": nil})
	if res.Error != nil {
		q.logger.Printf("[report-jobs] could not requeue interrupted jobs: %v", res.Error)
	} else if res.RowsAffected > 0 {
		q.logger.Printf("[report-jobs] requeued %d job(s) interrupted by a restart", res.RowsAffected)
	}

	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
	q.logger.Printf("[report-jobs] %d worker(s) started", q.workers)
}

// Stop signals the workers and waits for the in-flight render to finish.
func (q *ReportJobQueue) Stop() {
	q.once.Do(func() { close(q.stop) })
	q.wg.Wait()
	q.logger.Println("[report-jobs] workers stopped")
}

func (q *ReportJobQueue) worker(n int) {
	defer q.wg.Done()
	ticker := time.NewTicker(jobPollInterval)
	defer ticker.Stop()

	for {
		// Drain the queue before going back to sleep, so a burst is not paced
		// at one job per tick.
		for {
			claimed, err := q.runNext()
			if err != nil {
				q.logger.Printf("[report-jobs] worker %d: %v", n, err)
				break
			}
			if !claimed {
				break
			}
			select {
			case <-q.stop:
				return
			default:
			}
		}

		select {
		case <-q.stop:
			return
		case <-q.wake:
		case <-ticker.C:
		}
	}
}

// runNext claims one queued job and runs it. It reports whether a job was
// claimed, so the caller knows if more work may be waiting.
func (q *ReportJobQueue) runNext() (bool, error) {
	job, err := q.claim()
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), jobRunTimeout)
	defer cancel()

	generated, runErr := q.render(ctx, job)
	if runErr != nil {
		q.finishFailed(job, runErr)
		return true, nil
	}

	now := time.Now()
	if err := q.db.Model(&models.ReportJob{}).Where("id = ?", job.ID).
		Updates(map[string]interface{}{
			"status":        models.JobSucceeded,
			"generation_id": generated.Generation.ID,
			"finished_at":   now,
			"error":         nil,
		}).Error; err != nil {
		q.logger.Printf("[report-jobs] job %s rendered but could not be marked done: %v", job.ID, err)
	}
	q.logger.Printf("[report-jobs] job %s succeeded (generation %s)", job.ID, generated.Generation.ID)
	return true, nil
}

// claim atomically takes the oldest queued job. SKIP LOCKED means a worker that
// finds a row already being claimed moves on rather than waiting for it.
func (q *ReportJobQueue) claim() (*models.ReportJob, error) {
	var job models.ReportJob
	err := q.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Raw(`
			SELECT * FROM report_jobs
			 WHERE status = ?
			 ORDER BY created_at
			 LIMIT 1
			   FOR UPDATE SKIP LOCKED`, models.JobQueued).Scan(&job)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return tx.Model(&models.ReportJob{}).Where("id = ?", job.ID).
			Updates(map[string]interface{}{
				"status":     models.JobRunning,
				"started_at": time.Now(),
				"attempts":   gorm.Expr("attempts + 1"),
			}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claiming a report job: %w", err)
	}
	job.Attempts++
	return &job, nil
}

// render loads the job's report and generates it.
func (q *ReportJobQueue) render(ctx context.Context, job *models.ReportJob) (*GeneratedReport, error) {
	var report models.Report
	if err := q.db.WithContext(ctx).First(&report, "id = ?", job.ReportID).Error; err != nil {
		return nil, fmt.Errorf("loading report: %w", err)
	}
	return q.generator.GenerateAndSaveReport(ctx, &report, job.RequestedBy)
}

// finishFailed records a failure, requeueing while attempts remain.
func (q *ReportJobQueue) finishFailed(job *models.ReportJob, runErr error) {
	msg := runErr.Error()
	now := time.Now()

	updates := map[string]interface{}{"error": msg}
	if job.Attempts < models.MaxJobAttempts {
		// Transient causes (a locked file, a brief database blip) deserve
		// another pass; the attempt counter stops it becoming a loop.
		updates["status"] = models.JobQueued
		updates["started_at"] = nil
		q.logger.Printf("[report-jobs] job %s failed on attempt %d/%d, requeueing: %v",
			job.ID, job.Attempts, models.MaxJobAttempts, runErr)
	} else {
		updates["status"] = models.JobFailed
		updates["finished_at"] = now
		q.logger.Printf("[report-jobs] job %s failed permanently after %d attempts: %v",
			job.ID, job.Attempts, runErr)
	}

	if err := q.db.Model(&models.ReportJob{}).Where("id = ?", job.ID).
		Updates(updates).Error; err != nil {
		q.logger.Printf("[report-jobs] could not record failure for job %s: %v", job.ID, err)
	}
	if updates["status"] == models.JobQueued {
		q.nudge()
	}
}
