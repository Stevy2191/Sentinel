package dashboards

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Service stores dashboards and decides who may do what with them.
type Service struct {
	db       *gorm.DB
	sites    SiteLeveler
	registry *Registry
	checker  Checker
}

// NewService builds the dashboard service.
func NewService(db *gorm.DB, sites SiteLeveler, registry *Registry, checker Checker) *Service {
	return &Service{db: db, sites: sites, registry: registry, checker: checker}
}

// DashboardView is a dashboard as one caller sees it, with what the caller
// may do, so the page knows which controls to show without a second request.
type DashboardView struct {
	models.Dashboard
	SiteName    string `json:"site_name"`
	Access      string `json:"access"`
	Published   bool   `json:"published"`
	CanEdit     bool   `json:"can_edit"`
	CanShare    bool   `json:"can_share"`
	WidgetCount int    `json:"widget_count"`
	level       Level
}

// WidgetView is a widget as one caller sees it. Config is sent only to
// callers with edit access: it holds subject ids.
type WidgetView struct {
	ID     uuid.UUID      `json:"id"`
	Type   string         `json:"type"`
	Title  string         `json:"title"`
	X      int            `json:"x"`
	Y      int            `json:"y"`
	W      int            `json:"w"`
	H      int            `json:"h"`
	Config models.RawJSON `json:"config,omitempty"`
}

// DashboardDetail is a dashboard and its widgets.
type DashboardDetail struct {
	DashboardView
	Widgets []WidgetView `json:"widgets"`
}

// CreateInput is what a create request supplies. SiteID set: a site
// dashboard; nil: a personal one owned by the caller.
type CreateInput struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	SiteID      *uuid.UUID `json:"site_id"`
	// Starter fills a new site dashboard with the standard widgets.
	Starter bool `json:"starter"`
}

// dashboardRow is one dashboard joined to what decides the caller's access.
type dashboardRow struct {
	models.Dashboard
	SiteName        string `gorm:"column:site_name"`
	Published       bool   `gorm:"column:published"`
	SharePermission string `gorm:"column:share_permission"`
	SitePermission  string `gorm:"column:site_permission"`
	WidgetCount     int    `gorm:"column:widget_count"`
}

const dashboardSelect = `d.*, COALESCE(st.name, '') AS site_name,
	(pl.dashboard_id IS NOT NULL) AS published,
	COALESCE(sh.permission, '') AS share_permission,
	COALESCE(ss.permission, '') AS site_permission,
	(SELECT count(*) FROM dashboard_widgets w WHERE w.dashboard_id = d.id) AS widget_count`

// query selects dashboards with the caller's share and site share joined in.
func (s *Service) query(ctx context.Context, v Viewer) *gorm.DB {
	return s.db.WithContext(ctx).Table("dashboards AS d").Select(dashboardSelect).
		Joins("LEFT JOIN sites st ON st.id = d.site_id").
		Joins("LEFT JOIN dashboard_public_links pl ON pl.dashboard_id = d.id").
		Joins("LEFT JOIN dashboard_sharing sh ON sh.dashboard_id = d.id AND sh.user_id = ?", v.UserID).
		Joins("LEFT JOIN site_sharing ss ON ss.site_id = d.site_id AND ss.shared_with_user_id = ?", v.UserID)
}

func (r dashboardRow) view(v Viewer) DashboardView {
	level := resolveAccess(v, &r.Dashboard, sitePermissionLevel(r.SitePermission), r.SharePermission)
	return DashboardView{
		Dashboard:   r.Dashboard,
		SiteName:    r.SiteName,
		Access:      level.String(),
		Published:   r.Published,
		CanEdit:     canChangeContent(level, r.Published),
		CanShare:    canShare(v, &r.Dashboard),
		WidgetCount: r.WidgetCount,
		level:       level,
	}
}

// List returns the dashboards v can see, optionally only one site's.
func (s *Service) List(ctx context.Context, v Viewer, siteID *uuid.UUID) ([]DashboardView, error) {
	if v.Public {
		return nil, ErrNotFound
	}
	q := s.query(ctx, v)
	if !v.IsAdmin {
		q = q.Where("(d.owner_id = ? OR sh.user_id IS NOT NULL OR ss.id IS NOT NULL)", v.UserID)
	}
	if siteID != nil {
		q = q.Where("d.site_id = ?", *siteID)
	}
	var rows []dashboardRow
	if err := q.Order("lower(d.name), d.id").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing dashboards: %w", err)
	}
	out := make([]DashboardView, 0, len(rows))
	for _, r := range rows {
		if vw := r.view(v); vw.level >= LevelView {
			out = append(out, vw)
		}
	}
	return out, nil
}

// load is one dashboard as v sees it; ErrNotFound when missing or not v's.
func (s *Service) load(ctx context.Context, v Viewer, id uuid.UUID) (*DashboardView, error) {
	var rows []dashboardRow
	if err := s.query(ctx, v).Where("d.id = ?", id).Limit(1).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading dashboard: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	vw := rows[0].view(v)
	if vw.level < LevelView {
		return nil, ErrNotFound
	}
	return &vw, nil
}

// widgets returns a dashboard's widgets top to bottom, left to right.
func (s *Service) widgets(ctx context.Context, id uuid.UUID) ([]models.DashboardWidget, error) {
	var ws []models.DashboardWidget
	if err := s.db.WithContext(ctx).Where("dashboard_id = ?", id).Order("y, x, id").Find(&ws).Error; err != nil {
		return nil, fmt.Errorf("loading widgets: %w", err)
	}
	return ws, nil
}

// Get returns a dashboard and its widgets as v sees them.
func (s *Service) Get(ctx context.Context, v Viewer, id uuid.UUID) (*DashboardDetail, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	ws, err := s.widgets(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &DashboardDetail{DashboardView: *vw, Widgets: make([]WidgetView, 0, len(ws))}
	for _, w := range ws {
		wv := WidgetView{ID: w.ID, Type: w.Type, Title: w.Title, X: w.X, Y: w.Y, W: w.W, H: w.H}
		if vw.level >= LevelEdit {
			wv.Config = w.Config
		}
		out.Widgets = append(out.Widgets, wv)
	}
	return out, nil
}

// normalizeNames trims and checks a dashboard's name and description.
func normalizeNames(name, desc string) (string, string, error) {
	name, desc = strings.TrimSpace(name), strings.TrimSpace(desc)
	if name == "" {
		return "", "", invalid("name is required")
	}
	if utf8.RuneCountInString(name) > models.MaxDashboardNameLen {
		return "", "", invalid("name must be 100 characters or fewer")
	}
	if utf8.RuneCountInString(desc) > models.MaxDashboardDescriptionLen {
		return "", "", invalid("description must be 500 characters or fewer")
	}
	return name, desc, nil
}

// Create stores a new, empty dashboard: personal unless in.SiteID is set, in
// which case the caller needs editable access to the site.
func (s *Service) Create(ctx context.Context, v Viewer, in CreateInput) (*DashboardDetail, error) {
	if v.Public {
		return nil, ErrNotFound
	}
	name, desc, err := normalizeNames(in.Name, in.Description)
	if err != nil {
		return nil, err
	}
	creator := v.UserID
	d := models.Dashboard{Name: name, Description: desc, Version: 1, CreatedBy: &creator}
	if in.SiteID != nil {
		level, err := s.sites.SiteAccess(ctx, v.UserID, v.IsAdmin, *in.SiteID)
		if errors.Is(err, services.ErrSiteNotFound) || (err == nil && level == services.SiteAccessNone) {
			return nil, ErrSiteNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("checking site access: %w", err)
		}
		if level < services.SiteAccessEditable {
			return nil, ErrForbidden
		}
		siteID := *in.SiteID
		d.SiteID = &siteID
	} else {
		d.OwnerID = &creator
	}
	var starter []models.DashboardWidget
	if in.Starter {
		if d.SiteID == nil {
			return nil, invalid("the standard widgets are for site dashboards")
		}
		for i, w := range starterWidgets(*d.SiteID) {
			clean, err := s.validateWidget(ctx, v, i, w)
			if err != nil {
				return nil, fmt.Errorf("standard widget %d: %w", i, err)
			}
			starter = append(starter, clean)
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&d).Error; err != nil {
			return fmt.Errorf("creating dashboard: %w", err)
		}
		for i := range starter {
			starter[i].ID = uuid.New()
			starter[i].DashboardID = d.ID
			if err := tx.Create(&starter[i]).Error; err != nil {
				return fmt.Errorf("adding standard widget: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, v, d.ID)
}

// contentError is why vw's content may not be changed: ErrPublished for an
// editor of a published dashboard, ErrForbidden for a viewer.
func contentError(vw *DashboardView) error {
	if vw.Published && vw.level >= LevelEdit {
		return ErrPublished
	}
	return ErrForbidden
}

// Delete removes a dashboard, its widgets, shares and public link.
func (s *Service) Delete(ctx context.Context, v Viewer, id uuid.UUID) (*models.Dashboard, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if !vw.CanEdit {
		return nil, contentError(vw)
	}
	if err := s.db.WithContext(ctx).Delete(&models.Dashboard{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting dashboard: %w", err)
	}
	return &vw.Dashboard, nil
}
