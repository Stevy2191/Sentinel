package toolruns

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ErrRunNotFound is returned for an unknown run id.
var ErrRunNotFound = errors.New("tool run not found")

// Get returns one run.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*models.ToolRun, error) {
	var run models.ToolRun
	err := s.db.WithContext(ctx).First(&run, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading tool run %s: %w", id, err)
	}
	return &run, nil
}

// Events returns a run's events after afterSeq, in seq order (never nil).
func (s *Service) Events(ctx context.Context, id uuid.UUID, afterSeq int) ([]models.ToolRunEvent, error) {
	events := []models.ToolRunEvent{}
	if err := s.db.WithContext(ctx).Where("run_id = ? AND seq > ?", id, afterSeq).
		Order("seq").Find(&events).Error; err != nil {
		return nil, fmt.Errorf("loading events of tool run %s: %w", id, err)
	}
	return events, nil
}

// ListFilter narrows the run history. Empty fields do not filter.
type ListFilter struct {
	Tool    string
	UserID  *uuid.UUID
	AgentID string // readable agent id (agent_ref)
	Target  string // exact match on target or target_ip
	Status  string
	Limit   int // default 50, max 200
	Offset  int
}

// List returns runs newest first and the number matching the filter.
func (s *Service) List(ctx context.Context, f ListFilter) ([]models.ToolRun, int64, error) {
	q := s.db.WithContext(ctx).Model(&models.ToolRun{})
	if f.Tool != "" {
		q = q.Where("tool = ?", f.Tool)
	}
	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}
	if f.AgentID != "" {
		q = q.Where("agent_ref = ?", f.AgentID)
	}
	if f.Target != "" {
		q = q.Where("(target = ? OR target_ip = ?)", f.Target, f.Target)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting tool runs: %w", err)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := max(f.Offset, 0)
	runs := []models.ToolRun{}
	// A page past the end is empty: no need to make Postgres walk to it.
	if int64(offset) >= total {
		return runs, total, nil
	}
	if err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&runs).Error; err != nil {
		return nil, 0, fmt.Errorf("listing tool runs: %w", err)
	}
	return runs, total, nil
}

// Cancel stops a run at once: its owner or an admin may. A local run's tool
// is stopped; an agent sees cancel: true on its next events post. A later
// finish never undoes it (ruling 8).
func (s *Service) Cancel(ctx context.Context, who Requester, id uuid.UUID) (*models.ToolRun, error) {
	run, err := s.Get(ctx, id)
	if errors.Is(err, ErrRunNotFound) {
		return nil, refuse(http.StatusNotFound, CodeNotFound, "no such tool run")
	}
	if err != nil {
		return nil, err
	}
	if !who.IsAdmin && (run.UserID == nil || *run.UserID != who.UserID) {
		return nil, refuse(http.StatusForbidden, CodeForbidden, "only the user who started a run, or an admin, can cancel it")
	}
	if !models.ToolRunActive(run.Status) {
		return nil, refuse(http.StatusConflict, CodeConflict, "the run has already finished")
	}
	moved, err := s.finish(ctx, id, models.ToolRunCancelled, nil, "", who.actor())
	if err != nil {
		return nil, err
	}
	if !moved {
		return nil, refuse(http.StatusConflict, CodeConflict, "the run has already finished")
	}
	return s.Get(ctx, id)
}
