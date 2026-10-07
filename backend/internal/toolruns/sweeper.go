package toolruns

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// pruneEvery is how often finished runs past the retention are deleted;
// the first prune runs a minute after start.
const pruneEvery = 24 * time.Hour

// finishAll finishes each run in ids as the system and counts the ones this
// call moved.
func (s *Service) finishAll(ctx context.Context, ids []uuid.UUID, status, errText string, actor services.Actor) (int64, error) {
	var n int64
	for _, id := range ids {
		moved, err := s.finish(ctx, id, status, nil, errText, actor)
		if err != nil {
			return n, err
		}
		if moved {
			n++
		}
	}
	return n, nil
}

// activeIDs returns the ids of runs matching where (plus args).
func (s *Service) activeIDs(ctx context.Context, where string, args ...any) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	if err := s.db.WithContext(ctx).Model(&models.ToolRun{}).Where(where, args...).
		Order("created_at").Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("finding tool runs to sweep: %w", err)
	}
	return ids, nil
}

// InterruptStale marks every queued or running run interrupted. Called once
// at startup: no run survives a restart, and an agent still working on one
// gets 409 on its next post and stops.
func (s *Service) InterruptStale(ctx context.Context) (int64, error) {
	ids, err := s.activeIDs(ctx, "status IN (?, ?)", models.ToolRunQueued, models.ToolRunRunning)
	if err != nil {
		return 0, err
	}
	return s.finishAll(ctx, ids, models.ToolRunInterrupted, msgInterrupted, systemActor)
}

// Sweep fails queued runs no agent picked up within PickupTimeout and times
// out running runs past their deadline plus OverdueGrace (an agent that
// vanished mid-run). Each finish ends open streams and is audited as the
// system.
func (s *Service) Sweep(ctx context.Context) error {
	now := s.now()
	stale, err := s.activeIDs(ctx, "status = ? AND created_at < ?", models.ToolRunQueued, now.Add(-PickupTimeout))
	if err != nil {
		return err
	}
	if _, err := s.finishAll(ctx, stale, models.ToolRunFailed, msgNotPickedUp, systemActor); err != nil {
		return err
	}
	overdue, err := s.activeIDs(ctx, "status = ? AND deadline < ?", models.ToolRunRunning, now.Add(-OverdueGrace))
	if err != nil {
		return err
	}
	_, err = s.finishAll(ctx, overdue, models.ToolRunTimedOut, msgTimedOut, systemActor)
	return err
}

// Prune deletes finished runs older than the retention setting; their
// events go with them. Audit entries are kept.
func (s *Service) Prune(ctx context.Context) (int64, error) {
	days := LoadSettings(ctx, s.settings).RetentionDays
	cutoff := s.now().AddDate(0, 0, -days)
	res := s.db.WithContext(ctx).
		Where("finished_at IS NOT NULL AND finished_at < ? AND status NOT IN (?, ?)",
			cutoff, models.ToolRunQueued, models.ToolRunRunning).
		Delete(&models.ToolRun{})
	if res.Error != nil {
		return 0, fmt.Errorf("pruning tool runs: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// Start interrupts the runs a previous process left behind, then sweeps
// every SweepEvery and prunes a minute after start and every 24 h after
// that, until ctx ends.
func (s *Service) Start(ctx context.Context) {
	if n, err := s.InterruptStale(ctx); err != nil {
		log.Printf("[tools] interrupting runs left from before the restart: %v", err)
	} else if n > 0 {
		log.Printf("[tools] marked %d run(s) left from before the restart as interrupted", n)
	}
	sweep := time.NewTicker(SweepEvery)
	defer sweep.Stop()
	prune := time.NewTimer(time.Minute)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sweep.C:
			if err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
				log.Printf("[tools] sweep: %v", err)
			}
		case <-prune.C:
			if n, err := s.Prune(ctx); err != nil {
				log.Printf("[tools] prune: %v", err)
			} else if n > 0 {
				log.Printf("[tools] pruned %d finished run(s) past the retention period", n)
			}
			prune.Reset(pruneEvery)
		}
	}
}
