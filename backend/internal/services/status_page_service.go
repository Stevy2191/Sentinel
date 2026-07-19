package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ErrMonitorAlreadyOnPage is returned when a monitor is added to a status page
// it is already part of.
var ErrMonitorAlreadyOnPage = errors.New("monitor already on this page")

// StatusPageService manages status pages and their monitor associations.
type StatusPageService struct {
	db     *gorm.DB
	logger *log.Logger
}

// NewStatusPageService returns a StatusPageService backed by the given database.
func NewStatusPageService(db *gorm.DB) *StatusPageService {
	return &StatusPageService{
		db:     db,
		logger: log.Default(),
	}
}

// CreateStatusPage validates and persists a new status page.
func (s *StatusPageService) CreateStatusPage(ctx context.Context, page *models.StatusPage) (*models.StatusPage, error) {
	if page == nil {
		return nil, errors.New("status page is required")
	}

	// Default published to true before validation.
	if !page.Published {
		page.Published = true
	}

	if err := page.Validate(); err != nil {
		return nil, fmt.Errorf("invalid status page: %w", err)
	}

	if page.ID == uuid.Nil {
		page.ID = uuid.New()
	}
	now := time.Now()
	page.CreatedAt = now
	page.UpdatedAt = now

	if err := s.db.WithContext(ctx).Create(page).Error; err != nil {
		return nil, fmt.Errorf("creating status page: %w", err)
	}

	s.logger.Printf("[statuspage] created slug=%q id=%s", page.Slug, page.ID)
	return page, nil
}

// GetStatusPageBySlug fetches a status page by its slug, returning a wrapped
// gorm.ErrRecordNotFound if it does not exist.
func (s *StatusPageService) GetStatusPageBySlug(ctx context.Context, slug string) (*models.StatusPage, error) {
	var page models.StatusPage
	err := s.db.WithContext(ctx).First(&page, "slug = ?", slug).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("status page %q not found: %w", slug, err)
		}
		return nil, fmt.Errorf("fetching status page %q: %w", slug, err)
	}
	return &page, nil
}

// ListStatusPages returns all status pages, newest first.
func (s *StatusPageService) ListStatusPages(ctx context.Context) ([]models.StatusPage, error) {
	s.logger.Printf("[statuspage] list")

	var pages []models.StatusPage
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&pages).Error; err != nil {
		return nil, fmt.Errorf("listing status pages: %w", err)
	}
	return pages, nil
}

// UpdateStatusPage applies the editable fields of updates onto the existing page
// identified by slug. The slug itself is immutable.
func (s *StatusPageService) UpdateStatusPage(ctx context.Context, slug string, updates *models.StatusPage) (*models.StatusPage, error) {
	if updates == nil {
		return nil, errors.New("updates are required")
	}

	existing, err := s.GetStatusPageBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}

	// Slug is immutable; only these fields may be updated.
	if updates.Name != "" {
		existing.Name = updates.Name
	}
	if updates.Description != "" {
		existing.Description = updates.Description
	}
	if updates.LogoURL != "" {
		existing.LogoURL = updates.LogoURL
	}
	if updates.ThemeColor != "" {
		existing.ThemeColor = updates.ThemeColor
	}
	existing.Published = updates.Published

	if err := existing.Validate(); err != nil {
		return nil, fmt.Errorf("invalid status page update: %w", err)
	}

	existing.UpdatedAt = time.Now()
	if err := s.db.WithContext(ctx).Save(existing).Error; err != nil {
		return nil, fmt.Errorf("updating status page %q: %w", slug, err)
	}

	s.logger.Printf("[statuspage] updated slug=%q", slug)
	return existing, nil
}

// DeleteStatusPage removes a status page by slug. Associated
// status_page_monitors rows are removed by the database via ON DELETE CASCADE.
func (s *StatusPageService) DeleteStatusPage(ctx context.Context, slug string) error {
	result := s.db.WithContext(ctx).Where("slug = ?", slug).Delete(&models.StatusPage{})
	if result.Error != nil {
		return fmt.Errorf("deleting status page %q: %w", slug, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("status page %q not found: %w", slug, gorm.ErrRecordNotFound)
	}

	s.logger.Printf("[statuspage] deleted slug=%q", slug)
	return nil
}

// AddMonitorToPage associates a monitor with a status page. It returns a wrapped
// gorm.ErrRecordNotFound if the page or monitor does not exist, or
// ErrMonitorAlreadyOnPage if the monitor is already associated.
func (s *StatusPageService) AddMonitorToPage(ctx context.Context, slug string, monitorID uuid.UUID, groupName string, position int) error {
	page, err := s.GetStatusPageBySlug(ctx, slug)
	if err != nil {
		return err
	}

	// Verify the monitor exists.
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.Monitor{}).Where("id = ?", monitorID).Count(&count).Error; err != nil {
		return fmt.Errorf("verifying monitor %s: %w", monitorID, err)
	}
	if count == 0 {
		return fmt.Errorf("monitor %s not found: %w", monitorID, gorm.ErrRecordNotFound)
	}

	// Reject duplicates up front (also enforced by a unique constraint).
	var existing int64
	if err := s.db.WithContext(ctx).Model(&models.StatusPageMonitor{}).
		Where("status_page_id = ? AND monitor_id = ?", page.ID, monitorID).
		Count(&existing).Error; err != nil {
		return fmt.Errorf("checking existing association: %w", err)
	}
	if existing > 0 {
		return ErrMonitorAlreadyOnPage
	}

	entry := &models.StatusPageMonitor{
		ID:           uuid.New(),
		StatusPageID: page.ID,
		MonitorID:    monitorID,
		GroupName:    groupName,
		Position:     position,
		CreatedAt:    time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(entry).Error; err != nil {
		return fmt.Errorf("adding monitor %s to status page %q: %w", monitorID, slug, err)
	}

	s.logger.Printf("[statuspage] monitor %s added to slug=%q", monitorID, slug)
	return nil
}

// RemoveMonitorFromPage removes a monitor association from a status page. It
// returns a wrapped gorm.ErrRecordNotFound if the page does not exist or the
// monitor is not associated with it.
func (s *StatusPageService) RemoveMonitorFromPage(ctx context.Context, slug string, monitorID uuid.UUID) error {
	page, err := s.GetStatusPageBySlug(ctx, slug)
	if err != nil {
		return err
	}

	result := s.db.WithContext(ctx).
		Where("status_page_id = ? AND monitor_id = ?", page.ID, monitorID).
		Delete(&models.StatusPageMonitor{})
	if result.Error != nil {
		return fmt.Errorf("removing monitor %s from status page %q: %w", monitorID, slug, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("monitor %s not on status page %q: %w", monitorID, slug, gorm.ErrRecordNotFound)
	}

	s.logger.Printf("[statuspage] monitor %s removed from slug=%q", monitorID, slug)
	return nil
}

// GetPageMonitors returns a status page's monitor associations ordered by
// position, alongside the corresponding Monitor records aligned by index.
func (s *StatusPageService) GetPageMonitors(ctx context.Context, slug string) ([]models.StatusPageMonitor, []models.Monitor, error) {
	page, err := s.GetStatusPageBySlug(ctx, slug)
	if err != nil {
		return nil, nil, err
	}

	var entries []models.StatusPageMonitor
	if err := s.db.WithContext(ctx).
		Where("status_page_id = ?", page.ID).
		Order("position ASC").
		Find(&entries).Error; err != nil {
		return nil, nil, fmt.Errorf("listing monitors for status page %q: %w", slug, err)
	}
	if len(entries) == 0 {
		return entries, []models.Monitor{}, nil
	}

	ids := make([]uuid.UUID, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.MonitorID)
	}

	var monitors []models.Monitor
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&monitors).Error; err != nil {
		return nil, nil, fmt.Errorf("fetching monitors for status page %q: %w", slug, err)
	}

	// Align the monitor slice to the association order (by position).
	byID := make(map[uuid.UUID]models.Monitor, len(monitors))
	for _, m := range monitors {
		byID[m.ID] = m
	}
	ordered := make([]models.Monitor, 0, len(entries))
	for _, e := range entries {
		if m, ok := byID[e.MonitorID]; ok {
			ordered = append(ordered, m)
		}
	}

	s.logger.Printf("[statuspage] slug=%q has %d monitor(s)", slug, len(ordered))
	return entries, ordered, nil
}
