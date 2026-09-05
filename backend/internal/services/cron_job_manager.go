// Package services - cron_job_manager.go owns the mapping between a schedule and
// the cron entry currently running it.
//
// It exists so schedule lifecycle (add, replace, remove) is in one place with
// one lock, rather than spread across the scheduler. The registry is written
// from HTTP handlers on concurrent requests, so it is mutex-guarded: the
// original design used a bare map, which is a data race and can panic the
// process under concurrent schedule edits.
package services

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// CronJobManager tracks which cron entry belongs to which schedule.
type CronJobManager struct {
	cron   *cron.Cron
	parser cron.Parser
	logger *log.Logger

	mu       sync.Mutex
	registry map[string]cron.EntryID
}

// NewCronJobManager returns a manager driving c. parser must be the same parser
// c was built with, so an expression that registers can also be used to compute
// the next run time - otherwise "@daily" would register and then fail to
// schedule a next run.
func NewCronJobManager(c *cron.Cron, parser cron.Parser) *CronJobManager {
	return &CronJobManager{
		cron:     c,
		parser:   parser,
		logger:   log.Default(),
		registry: make(map[string]cron.EntryID),
	}
}

// AddJob registers job under scheduleID, replacing any existing entry for that
// schedule, and returns the new cron entry id.
func (cm *CronJobManager) AddJob(scheduleID, cronExpr string, job func()) (int, error) {
	spec, err := cm.parser.Parse(cronExpr)
	if err != nil {
		return 0, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Replace rather than accumulate: without this an edited schedule would run
	// on both its old and its new cadence.
	if existing, ok := cm.registry[scheduleID]; ok {
		cm.cron.Remove(existing)
		delete(cm.registry, scheduleID)
	}

	entryID := cm.cron.Schedule(spec, cron.FuncJob(job))
	cm.registry[scheduleID] = entryID
	cm.logger.Printf("[cron] schedule %s registered as entry %d (%s)", scheduleID, entryID, cronExpr)
	return int(entryID), nil
}

// RemoveJob unregisters a schedule's cron entry. Removing a schedule that was
// never registered is not an error: an inactive schedule legitimately has no
// job, and treating that as a failure would block deleting it.
func (cm *CronJobManager) RemoveJob(scheduleID string) bool {
	cm.mu.Lock()
	entryID, ok := cm.registry[scheduleID]
	if ok {
		delete(cm.registry, scheduleID)
	}
	cm.mu.Unlock()

	if !ok {
		return false
	}
	cm.cron.Remove(entryID)
	cm.logger.Printf("[cron] schedule %s unregistered (entry %d)", scheduleID, entryID)
	return true
}

// EntryID returns the cron entry a schedule is registered under, if any.
func (cm *CronJobManager) EntryID(scheduleID string) (int, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	id, ok := cm.registry[scheduleID]
	return int(id), ok
}

// Count returns how many schedules are currently registered.
func (cm *CronJobManager) Count() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return len(cm.registry)
}

// GetNextRunTime reports when cronExpr would next fire.
func (cm *CronJobManager) GetNextRunTime(cronExpr string) (*time.Time, error) {
	spec, err := cm.parser.Parse(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}
	next := spec.Next(time.Now())
	return &next, nil
}
