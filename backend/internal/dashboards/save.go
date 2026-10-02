package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// WidgetInput is one widget in a save. ID is nil for a widget added since the
// editor loaded the dashboard.
type WidgetInput struct {
	ID     *uuid.UUID      `json:"id"`
	Type   string          `json:"type"`
	Title  string          `json:"title"`
	Config json.RawMessage `json:"config"`
	X      int             `json:"x"`
	Y      int             `json:"y"`
	W      int             `json:"w"`
	H      int             `json:"h"`
}

// SaveInput is a whole dashboard as the editor has it. Version is the
// version the editor started from.
type SaveInput struct {
	Version     int           `json:"version"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Widgets     []WidgetInput `json:"widgets"`
}

// Save replaces a dashboard's name, description and widgets in one
// transaction. A stale Version is a *VersionConflictError and writes nothing.
func (s *Service) Save(ctx context.Context, v Viewer, id uuid.UUID, in SaveInput) (*DashboardDetail, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if !vw.CanEdit {
		return nil, contentError(vw)
	}
	name, desc, err := normalizeNames(in.Name, in.Description)
	if err != nil {
		return nil, err
	}
	if len(in.Widgets) > models.MaxDashboardWidgets {
		return nil, invalid(fmt.Sprintf("a dashboard holds at most %d widgets", models.MaxDashboardWidgets))
	}
	clean := make([]models.DashboardWidget, len(in.Widgets))
	for i, w := range in.Widgets {
		if clean[i], err = s.validateWidget(ctx, v, i, w); err != nil {
			return nil, err
		}
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current models.Dashboard
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("locking dashboard: %w", err)
		}
		if current.Version != in.Version {
			return &VersionConflictError{Current: current.Version}
		}
		var existing []uuid.UUID
		if err := tx.Model(&models.DashboardWidget{}).Where("dashboard_id = ?", id).Pluck("id", &existing).Error; err != nil {
			return fmt.Errorf("loading widget ids: %w", err)
		}
		known := make(map[uuid.UUID]bool, len(existing))
		for _, e := range existing {
			known[e] = true
		}
		keep := make([]uuid.UUID, 0, len(clean))
		kept := map[uuid.UUID]bool{}
		for i := range clean {
			wid := clean[i].ID
			if wid == uuid.Nil {
				continue
			}
			if !known[wid] {
				return &WidgetError{Index: i, Field: "id", Msg: "is not a widget of this dashboard"}
			}
			if kept[wid] {
				return &WidgetError{Index: i, Field: "id", Msg: "appears twice"}
			}
			kept[wid] = true
			keep = append(keep, wid)
		}
		del := tx.Where("dashboard_id = ?", id)
		if len(keep) > 0 {
			del = del.Where("id NOT IN ?", keep)
		}
		if err := del.Delete(&models.DashboardWidget{}).Error; err != nil {
			return fmt.Errorf("removing widgets: %w", err)
		}
		for i := range clean {
			w := &clean[i]
			w.DashboardID = id
			if w.ID == uuid.Nil {
				w.ID = uuid.New()
				if err := tx.Create(w).Error; err != nil {
					return fmt.Errorf("adding widget: %w", err)
				}
				continue
			}
			err := tx.Model(&models.DashboardWidget{}).Where("id = ?", w.ID).Updates(map[string]any{
				"type": w.Type, "title": w.Title, "config": w.Config, "x": w.X, "y": w.Y, "w": w.W, "h": w.H,
			}).Error
			if err != nil {
				return fmt.Errorf("updating widget: %w", err)
			}
		}
		return tx.Model(&models.Dashboard{}).Where("id = ?", id).Updates(map[string]any{
			"name": name, "description": desc, "version": current.Version + 1, "updated_at": time.Now().UTC(),
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, v, id)
}

// validateWidget checks one widget and returns it ready to store: its type
// exists, its title and position fit, its config is valid for its type, and
// every subject it uses is one the editor may see.
func (s *Service) validateWidget(ctx context.Context, v Viewer, i int, in WidgetInput) (models.DashboardWidget, error) {
	fail := func(field, msg string) (models.DashboardWidget, error) {
		return models.DashboardWidget{}, &WidgetError{Index: i, Field: field, Msg: msg}
	}
	wd, ok := s.registry.Get(in.Type)
	if !ok {
		return fail("type", fmt.Sprintf("%q is not a widget type", in.Type))
	}
	title := strings.TrimSpace(in.Title)
	if utf8.RuneCountInString(title) > models.MaxWidgetTitleLen {
		return fail("title", "must be 100 characters or fewer")
	}
	cols, maxH := models.DashboardGridColumns, models.MaxWidgetHeight
	if in.X < 0 || in.Y < 0 || in.W < 1 || in.W > cols || in.X+in.W > cols || in.H < 1 || in.H > maxH {
		return fail("position", "must fit the 12-column grid (width 1 to 12, height 1 to 24)")
	}
	raw := in.Config
	if len(raw) == 0 || string(raw) == "null" {
		raw = json.RawMessage("{}")
	}
	if len(raw) > models.MaxWidgetConfigBytes {
		return fail("config", "is larger than 16 KB")
	}
	cfg, err := wd.Validate(ctx, raw)
	if err != nil {
		var fe *FieldError
		if errors.As(err, &fe) {
			return fail(fe.Field, fe.Msg)
		}
		return models.DashboardWidget{}, fmt.Errorf("validating %s widget: %w", in.Type, err)
	}
	f, err := Filter(ctx, s.checker, v, wd.Subjects(cfg))
	if err != nil {
		return models.DashboardWidget{}, fmt.Errorf("checking widget subjects: %w", err)
	}
	// One message for both, so a save cannot be used to probe which ids exist.
	if f.Hidden+f.Removed > 0 {
		return fail("config", msgSubjectUnavailable)
	}
	out := models.DashboardWidget{Type: in.Type, Title: title, Config: models.RawJSON(cfg), X: in.X, Y: in.Y, W: in.W, H: in.H}
	if in.ID != nil {
		out.ID = *in.ID
	}
	return out, nil
}
