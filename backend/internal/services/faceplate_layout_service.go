package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
)

var (
	ErrFaceplateLayoutNotFound = errors.New("faceplate layout not found")
	ErrFaceplateLayoutNoModel  = errors.New("Set this device's model in Edit details first")
	// ErrFaceplateGroupsInvalid wraps a portmon.ValidateGroups message.
	ErrFaceplateGroupsInvalid = errors.New("invalid faceplate layout")
)

// invalidGroups wraps a validation error so callers can tell it apart and
// still show its message.
type invalidGroups struct{ err error }

func (e invalidGroups) Error() string { return e.err.Error() }
func (e invalidGroups) Is(target error) bool {
	return target == ErrFaceplateGroupsInvalid
}

func validateGroups(gs []portmon.LayoutGroup) error {
	if err := portmon.ValidateGroups(gs); err != nil {
		return invalidGroups{err}
	}
	return nil
}

// FaceplateLayoutService keeps the admins' saved faceplate layouts, one per
// device model, with an in-memory copy for the faceplate's many reads.
type FaceplateLayoutService struct {
	db      *gorm.DB
	mu      sync.RWMutex
	byModel map[string]models.FaceplateLayout
}

func NewFaceplateLayoutService(db *gorm.DB) *FaceplateLayoutService {
	return &FaceplateLayoutService{db: db, byModel: map[string]models.FaceplateLayout{}}
}

// Load (re)reads every saved layout: at startup, after a change and after a
// backup restore.
func (s *FaceplateLayoutService) Load(ctx context.Context) error {
	var rows []models.FaceplateLayout
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return fmt.Errorf("loading faceplate layouts: %w", err)
	}
	m := make(map[string]models.FaceplateLayout, len(rows))
	for _, r := range rows {
		m[r.ModelKey] = r
	}
	s.mu.Lock()
	s.byModel = m
	s.mu.Unlock()
	return nil
}

// For is the saved layout for model (compared normalized), or nil.
func (s *FaceplateLayoutService) For(model string) *models.FaceplateLayout {
	key := portmon.NormalizeModel(model)
	if key == "" {
		return nil
	}
	s.mu.RLock()
	l, ok := s.byModel[key]
	s.mu.RUnlock()
	if !ok {
		return nil
	}
	l.Groups = append(models.FaceplateGroups(nil), l.Groups...)
	return &l
}

type modelDevice struct {
	ID           uuid.UUID
	Model        string
	LastPolledAt *time.Time
}

func (s *FaceplateLayoutService) modelDevices(ctx context.Context) ([]modelDevice, error) {
	var ds []modelDevice
	err := s.db.WithContext(ctx).Raw(`SELECT id, COALESCE(NULLIF(model_override, ''), model, '') AS model, last_polled_at
		FROM devices ORDER BY last_polled_at DESC NULLS LAST, id`).Scan(&ds).Error
	if err != nil {
		return nil, fmt.Errorf("listing device models: %w", err)
	}
	return ds, nil
}

// DeviceCount is how many devices have model as their effective model.
func (s *FaceplateLayoutService) DeviceCount(ctx context.Context, model string) (int, error) {
	ds, err := s.modelDevices(ctx)
	if err != nil {
		return 0, err
	}
	key, n := portmon.NormalizeModel(model), 0
	for _, d := range ds {
		if key != "" && portmon.NormalizeModel(d.Model) == key {
			n++
		}
	}
	return n, nil
}

// FaceplateLayoutView is one row of Settings → Faceplate layouts.
type FaceplateLayoutView struct {
	ID     *uuid.UUID            `json:"id"`
	Model  string                `json:"model"`
	Groups []portmon.LayoutGroup `json:"groups"`
	// Source is "saved" or "builtin".
	Source string `json:"source"`
	// Devices is how many devices are drawn by this layout's model.
	Devices int `json:"devices"`
	// PreviewDeviceID is the most recently polled of them, for the editor.
	PreviewDeviceID *uuid.UUID `json:"preview_device_id"`
	UpdatedAt       *time.Time `json:"updated_at"`
	UpdatedBy       string     `json:"updated_by"`
}

// List is the saved layouts by model, then the built-in ones that no saved
// layout replaces.
func (s *FaceplateLayoutService) List(ctx context.Context) ([]FaceplateLayoutView, error) {
	var rows []struct {
		models.FaceplateLayout
		Username *string
	}
	err := s.db.WithContext(ctx).Table("faceplate_layouts f").
		Select("f.*, u.username").Joins("LEFT JOIN users u ON u.id = f.updated_by").
		Order("f.model_key").Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("listing faceplate layouts: %w", err)
	}
	ds, err := s.modelDevices(ctx)
	if err != nil {
		return nil, err
	}
	saved := map[string]bool{}
	out := make([]FaceplateLayoutView, 0, len(rows)+1)
	for _, r := range rows {
		saved[r.ModelKey] = true
		v := FaceplateLayoutView{ID: &r.ID, Model: r.Model, Groups: r.Groups, Source: "saved", UpdatedAt: &r.UpdatedAt}
		if r.Username != nil {
			v.UpdatedBy = *r.Username
		}
		for _, d := range ds {
			if portmon.NormalizeModel(d.Model) == r.ModelKey {
				v.Devices++
				if v.PreviewDeviceID == nil {
					id := d.ID
					v.PreviewDeviceID = &id
				}
			}
		}
		out = append(out, v)
	}
	for _, b := range portmon.BuiltinLayouts() {
		if saved[portmon.NormalizeModel(b.Name)] {
			continue
		}
		v := FaceplateLayoutView{Model: b.Name, Groups: b.Groups, Source: "builtin"}
		for _, d := range ds {
			// A device with its own saved layout is not drawn by this one.
			if b.Matches(d.Model) && !saved[portmon.NormalizeModel(d.Model)] {
				v.Devices++
				if v.PreviewDeviceID == nil {
					id := d.ID
					v.PreviewDeviceID = &id
				}
			}
		}
		out = append(out, v)
	}
	sort.SliceStable(out[len(rows):], func(i, j int) bool { return out[len(rows)+i].Model < out[len(rows)+j].Model })
	return out, nil
}

// Save stores groups as the layout for model, replacing any there is, and
// with clearDevice also clears that device's own faceplate settings (which
// would override it), in one transaction. before is nil for a new layout.
func (s *FaceplateLayoutService) Save(ctx context.Context, model string, groups []portmon.LayoutGroup, actor uuid.UUID, clearDevice *uuid.UUID) (before, after *models.FaceplateLayout, err error) {
	model = strings.Join(strings.Fields(model), " ")
	key := portmon.NormalizeModel(model)
	if key == "" {
		return nil, nil, ErrFaceplateLayoutNoModel
	}
	if err := validateGroups(groups); err != nil {
		return nil, nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var cur models.FaceplateLayout
		switch err := tx.Where("model_key = ?", key).First(&cur).Error; {
		case err == nil:
			b := cur
			before = &b
			cur.Model, cur.Groups, cur.UpdatedBy, cur.UpdatedAt = model, groups, &actor, time.Now().UTC()
			if err := tx.Save(&cur).Error; err != nil {
				return fmt.Errorf("updating faceplate layout: %w", err)
			}
		case errors.Is(err, gorm.ErrRecordNotFound):
			now := time.Now().UTC()
			cur = models.FaceplateLayout{Model: model, ModelKey: key, Groups: groups, CreatedBy: &actor, UpdatedBy: &actor, CreatedAt: now, UpdatedAt: now}
			if err := tx.Create(&cur).Error; err != nil {
				return fmt.Errorf("saving faceplate layout: %w", err)
			}
		default:
			return fmt.Errorf("finding faceplate layout: %w", err)
		}
		if clearDevice != nil {
			if err := tx.Exec(`UPDATE devices SET faceplate_rows = NULL, faceplate_sfp_ports = NULL,
				faceplate_port_style = NULL WHERE id = ?`, *clearDevice).Error; err != nil {
				return fmt.Errorf("clearing device faceplate settings: %w", err)
			}
		}
		after = &cur
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return before, after, s.Load(ctx)
}

// Delete removes a saved layout; that model's devices go back to the
// built-in table or the automatic rules.
func (s *FaceplateLayoutService) Delete(ctx context.Context, id uuid.UUID) (*models.FaceplateLayout, error) {
	var l models.FaceplateLayout
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&l).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFaceplateLayoutNotFound
		}
		return nil, fmt.Errorf("finding faceplate layout: %w", err)
	}
	if err := s.db.WithContext(ctx).Delete(&l).Error; err != nil {
		return nil, fmt.Errorf("deleting faceplate layout: %w", err)
	}
	return &l, s.Load(ctx)
}
