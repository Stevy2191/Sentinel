package dashboards

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// tokenLen is the length of a base64url-encoded 32-byte token.
const tokenLen = 43

// newToken returns 32 random bytes, base64url without padding (43 chars).
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// BroadWidget is a widget whose rows follow the link creator's own access, so
// a public link to it shows things added later too.
type BroadWidget struct {
	ID    uuid.UUID `json:"id"`
	Type  string    `json:"type"`
	Title string    `json:"title"`
}

// LinkView is what the Public link panel shows: the link (nil when there is
// none) and the widgets with a broad scope.
type LinkView struct {
	Link  *models.DashboardPublicLink `json:"link"`
	Broad []BroadWidget               `json:"broad_widgets"`
}

// adminDashboard loads a dashboard for an admin-only operation.
func (s *Service) adminDashboard(ctx context.Context, v Viewer, id uuid.UUID) (*DashboardView, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if vw.level < LevelManage {
		return nil, ErrForbidden
	}
	return vw, nil
}

// PublicLink returns a dashboard's link, if any, and its broad widgets.
func (s *Service) PublicLink(ctx context.Context, v Viewer, id uuid.UUID) (*LinkView, error) {
	if _, err := s.adminDashboard(ctx, v, id); err != nil {
		return nil, err
	}
	out := &LinkView{Broad: []BroadWidget{}}
	var link models.DashboardPublicLink
	err := s.db.WithContext(ctx).First(&link, "dashboard_id = ?", id).Error
	switch {
	case err == nil:
		out.Link = &link
	case !errors.Is(err, gorm.ErrRecordNotFound):
		return nil, fmt.Errorf("loading public link: %w", err)
	}
	ws, err := s.widgets(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, w := range ws {
		if wd, ok := s.registry.Get(w.Type); ok && wd.Subjects([]byte(w.Config)).Broad {
			out.Broad = append(out.Broad, BroadWidget{ID: w.ID, Type: w.Type, Title: w.Title})
		}
	}
	return out, nil
}

// CreatePublicLink gives a dashboard a public link, replacing any existing
// one (the old token stops working at once).
func (s *Service) CreatePublicLink(ctx context.Context, v Viewer, id uuid.UUID) (*models.DashboardPublicLink, error) {
	if _, err := s.adminDashboard(ctx, v, id); err != nil {
		return nil, err
	}
	token, err := newToken()
	if err != nil {
		return nil, err
	}
	link := models.DashboardPublicLink{DashboardID: id, Token: token, CreatedBy: v.UserID, CreatedAt: time.Now().UTC()}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dashboard_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"token", "created_by", "created_at"}),
	}).Create(&link).Error
	if err != nil {
		return nil, fmt.Errorf("creating public link: %w", err)
	}
	return &link, nil
}

// RevokePublicLink deletes a dashboard's public link.
func (s *Service) RevokePublicLink(ctx context.Context, v Viewer, id uuid.UUID) error {
	if _, err := s.adminDashboard(ctx, v, id); err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Delete(&models.DashboardPublicLink{}, "dashboard_id = ?", id)
	if res.Error != nil {
		return fmt.Errorf("revoking public link: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNoLink
	}
	return nil
}

// ResolveToken returns the dashboard a public token opens. It answers
// ErrNotFound unless the link exists and the admin who made it is still an
// admin; this runs on every public request, so a demotion takes effect at once.
func (s *Service) ResolveToken(ctx context.Context, token string) (*models.Dashboard, error) {
	if len(token) != tokenLen {
		return nil, ErrNotFound
	}
	var rows []models.Dashboard
	err := s.db.WithContext(ctx).Table("dashboard_public_links AS pl").Select("d.*").
		Joins("JOIN dashboards d ON d.id = pl.dashboard_id").
		Joins("JOIN users u ON u.id = pl.created_by").
		Where("pl.token = ? AND u.is_admin", token).Limit(1).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("resolving public token: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return &rows[0], nil
}

// PublicWidget is a widget as the public layout shows it: no config.
type PublicWidget struct {
	ID    uuid.UUID `json:"id"`
	Type  string    `json:"type"`
	Title string    `json:"title"`
	X     int       `json:"x"`
	Y     int       `json:"y"`
	W     int       `json:"w"`
	H     int       `json:"h"`
}

// PublicDashboard is the layout a public link serves.
type PublicDashboard struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Version     int            `json:"version"`
	Widgets     []PublicWidget `json:"widgets"`
}

// PublicLayout returns d's name, layout and titles, without configs or ids
// of anything but the widgets themselves.
func (s *Service) PublicLayout(ctx context.Context, d *models.Dashboard) (*PublicDashboard, error) {
	ws, err := s.widgets(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	out := &PublicDashboard{Name: d.Name, Description: d.Description, Version: d.Version, Widgets: make([]PublicWidget, 0, len(ws))}
	for _, w := range ws {
		out.Widgets = append(out.Widgets, PublicWidget{ID: w.ID, Type: w.Type, Title: w.Title, X: w.X, Y: w.Y, W: w.W, H: w.H})
	}
	return out, nil
}

// PublicWidget returns one widget of d.
func (s *Service) PublicWidget(ctx context.Context, d *models.Dashboard, widgetID uuid.UUID) (*models.DashboardWidget, error) {
	var w models.DashboardWidget
	err := s.db.WithContext(ctx).First(&w, "id = ? AND dashboard_id = ?", widgetID, d.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrWidgetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading widget: %w", err)
	}
	return &w, nil
}

// Widget returns one widget of a dashboard v can see, and the dashboard.
func (s *Service) Widget(ctx context.Context, v Viewer, dashboardID, widgetID uuid.UUID) (*models.DashboardWidget, *DashboardView, error) {
	vw, err := s.load(ctx, v, dashboardID)
	if err != nil {
		return nil, nil, err
	}
	w, err := s.PublicWidget(ctx, &vw.Dashboard, widgetID)
	if err != nil {
		return nil, nil, err
	}
	return w, vw, nil
}
