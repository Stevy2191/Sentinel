package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/sitemap"
)

// mapGrid is the grid positions snap to, in map units.
const mapGrid = 8

func snap(v float64) float64 { return math.Round(v/mapGrid) * mapGrid }

// editable loads the site's map for an edit, or says why it can't be edited.
func (s *SiteMapService) editable(ctx context.Context, v Viewer, siteID uuid.UUID) (*mapWorld, error) {
	level, err := s.access(ctx, v, siteID)
	if err != nil {
		return nil, err
	}
	if level < SiteAccessEditable {
		return nil, ErrSiteMapForbidden
	}
	return s.load(ctx, siteID)
}

// validEnd: the device is on the map, and its port (when given) exists.
func (w *mapWorld) validEnd(e sitemap.End) bool {
	if !w.onMap[e.DeviceID] {
		return false
	}
	if e.IfIndex == 0 {
		return true
	}
	_, ok := w.ports[e]
	return ok
}

func nullableIf(ix int) *int {
	if ix == 0 {
		return nil
	}
	return &ix
}

// SetPositions saves where devices sit, snapped to the grid.
func (s *SiteMapService) SetPositions(ctx context.Context, v Viewer, siteID uuid.UUID, ps []PositionInput) error {
	w, err := s.editable(ctx, v, siteID)
	if err != nil {
		return err
	}
	for _, p := range ps {
		if !w.onMap[p.DeviceID] || math.IsNaN(p.X) || math.IsNaN(p.Y) || math.IsInf(p.X, 0) || math.IsInf(p.Y, 0) {
			return ErrSiteMapInvalid
		}
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, p := range ps {
			row := models.SiteMapPosition{SiteID: siteID, DeviceID: p.DeviceID, X: snap(p.X), Y: snap(p.Y), HasPosition: true, UpdatedAt: time.Now().UTC()}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "site_id"}, {Name: "device_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"x", "y", "has_position", "updated_at"}),
			}).Create(&row).Error; err != nil {
				return fmt.Errorf("saving a map position: %w", err)
			}
		}
		return nil
	})
	if err == nil {
		s.changed(siteID)
	}
	return err
}

// SetDevice hides or shows a device, or adds or removes a device of another
// type (a firewall filed as Other). Routers and switches can't be added:
// they're on the map already.
func (s *SiteMapService) SetDevice(ctx context.Context, v Viewer, siteID, deviceID uuid.UUID, hidden, added *bool) error {
	w, err := s.editable(ctx, v, siteID)
	if err != nil {
		return err
	}
	d, ok := w.devices[deviceID]
	if !ok || (added != nil && onMapType(d.Type)) || (hidden != nil && added == nil && !w.onMap[deviceID]) {
		return ErrSiteMapInvalid
	}
	row := w.positions[deviceID]
	row.SiteID, row.DeviceID, row.UpdatedAt = siteID, deviceID, time.Now().UTC()
	cols := []string{"updated_at"}
	if hidden != nil {
		row.Hidden = *hidden
		cols = append(cols, "hidden")
	}
	if added != nil {
		row.Added = *added
		cols = append(cols, "added")
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "site_id"}, {Name: "device_id"}},
		DoUpdates: clause.AssignmentColumns(cols),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("saving a map device: %w", err)
	}
	s.changed(siteID)
	return nil
}

// AddLink draws a link by hand between two devices on the map.
func (s *SiteMapService) AddLink(ctx context.Context, v Viewer, siteID uuid.UUID, a, b sitemap.End) (*models.SiteMapLink, error) {
	w, err := s.editable(ctx, v, siteID)
	if err != nil {
		return nil, err
	}
	if a.DeviceID == b.DeviceID || !w.validEnd(a) || !w.validEnd(b) {
		return nil, ErrSiteMapInvalid
	}
	return s.insertLink(ctx, v, siteID, models.SiteMapLinkManual, a, b)
}

func (s *SiteMapService) insertLink(ctx context.Context, v Viewer, siteID uuid.UUID, kind string, a, b sitemap.End) (*models.SiteMapLink, error) {
	by := v.UserID
	row := &models.SiteMapLink{ID: uuid.New(), SiteID: siteID, Kind: kind, ADeviceID: a.DeviceID, AIfIndex: nullableIf(a.IfIndex),
		BDeviceID: b.DeviceID, BIfIndex: nullableIf(b.IfIndex), CreatedAt: time.Now().UTC()}
	if by != uuid.Nil {
		row.CreatedBy = &by
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return nil, fmt.Errorf("saving a map link: %w", err)
	}
	s.changed(siteID)
	return row, nil
}

func (s *SiteMapService) manualRow(ctx context.Context, siteID, linkID uuid.UUID) (*models.SiteMapLink, error) {
	var row models.SiteMapLink
	err := s.db.WithContext(ctx).Where("id = ? AND site_id = ? AND kind = ?", linkID, siteID, models.SiteMapLinkManual).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSiteMapInvalid
	}
	return &row, err
}

// UpdateLink moves a hand-drawn link's ends.
func (s *SiteMapService) UpdateLink(ctx context.Context, v Viewer, siteID, linkID uuid.UUID, a, b sitemap.End) error {
	w, err := s.editable(ctx, v, siteID)
	if err != nil {
		return err
	}
	if _, err := s.manualRow(ctx, siteID, linkID); err != nil {
		return err
	}
	if a.DeviceID == b.DeviceID || !w.validEnd(a) || !w.validEnd(b) {
		return ErrSiteMapInvalid
	}
	if err := s.db.WithContext(ctx).Model(&models.SiteMapLink{}).Where("id = ?", linkID).Updates(map[string]any{
		"a_device_id": a.DeviceID, "a_if_index": nullableIf(a.IfIndex), "b_device_id": b.DeviceID, "b_if_index": nullableIf(b.IfIndex),
	}).Error; err != nil {
		return fmt.Errorf("moving a map link: %w", err)
	}
	s.changed(siteID)
	return nil
}

// DeleteLink removes a hand-drawn link. Found links are hidden instead.
func (s *SiteMapService) DeleteLink(ctx context.Context, v Viewer, siteID, linkID uuid.UUID) error {
	if _, err := s.editable(ctx, v, siteID); err != nil {
		return err
	}
	if _, err := s.manualRow(ctx, siteID, linkID); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&models.SiteMapLink{}, "id = ?", linkID).Error; err != nil {
		return fmt.Errorf("deleting a map link: %w", err)
	}
	s.changed(siteID)
	return nil
}

// parseLinkKey reads a sitemap.KeyOf key back into its ends.
func parseLinkKey(key string) (sitemap.End, sitemap.End, bool) {
	parts := strings.Split(key, "|")
	if len(parts) != 2 {
		return sitemap.End{}, sitemap.End{}, false
	}
	var ends [2]sitemap.End
	for i, p := range parts {
		k := strings.LastIndex(p, ":")
		if k < 0 {
			return sitemap.End{}, sitemap.End{}, false
		}
		id, err := uuid.Parse(p[:k])
		ix, err2 := strconv.Atoi(p[k+1:])
		if err != nil || err2 != nil || ix < 0 {
			return sitemap.End{}, sitemap.End{}, false
		}
		ends[i] = sitemap.End{DeviceID: id, IfIndex: ix}
	}
	return ends[0], ends[1], sitemap.KeyOf(ends[0], ends[1]) == key
}

// findLink is the map's link with key, hidden or not.
func (w *mapWorld) findLink(key string) (sitemap.Link, bool) {
	for _, l := range w.links {
		if l.Key == key {
			return l, true
		}
	}
	return sitemap.Link{}, false
}

// SetLinkHidden hides a found link (it stays hidden when found again) or
// shows it again. A bundle's key hides or shows every cable in it, all or
// none.
func (s *SiteMapService) SetLinkHidden(ctx context.Context, v Viewer, siteID uuid.UUID, key string, hidden bool) error {
	w, err := s.editable(ctx, v, siteID)
	if err != nil {
		return err
	}
	keys := []string{key}
	if isBundleKey(key) {
		keys = bundleMembers(key)
	}
	type cable struct {
		a, b sitemap.End
		hide bool
	}
	var cables []cable
	for _, k := range keys {
		a, b, ok := parseLinkKey(k)
		if !ok {
			return ErrSiteMapInvalid
		}
		c := cable{a: a, b: b}
		if hidden {
			l, ok := w.findLink(k)
			if !ok {
				return ErrSiteMapInvalid
			}
			c.hide = !l.Hidden
		}
		cables = append(cables, c)
	}
	by := v.UserID
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, c := range cables {
			a, b := c.a, c.b
			if !hidden {
				if err := tx.Exec(`DELETE FROM site_map_links WHERE site_id = ? AND kind = 'hidden'
					AND ((a_device_id = ? AND COALESCE(a_if_index, 0) = ? AND b_device_id = ? AND COALESCE(b_if_index, 0) = ?)
					  OR (a_device_id = ? AND COALESCE(a_if_index, 0) = ? AND b_device_id = ? AND COALESCE(b_if_index, 0) = ?))`,
					siteID, a.DeviceID, a.IfIndex, b.DeviceID, b.IfIndex, b.DeviceID, b.IfIndex, a.DeviceID, a.IfIndex).Error; err != nil {
					return fmt.Errorf("showing a map link: %w", err)
				}
				continue
			}
			if !c.hide {
				continue
			}
			row := &models.SiteMapLink{ID: uuid.New(), SiteID: siteID, Kind: models.SiteMapLinkHidden, ADeviceID: a.DeviceID,
				AIfIndex: nullableIf(a.IfIndex), BDeviceID: b.DeviceID, BIfIndex: nullableIf(b.IfIndex), CreatedAt: time.Now().UTC()}
			if by != uuid.Nil {
				row.CreatedBy = &by
			}
			if err := tx.Create(row).Error; err != nil {
				return fmt.Errorf("saving a map link: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.changed(siteID)
	return nil
}

// KeepGuess turns a MAC-table guess into a hand-drawn link.
func (s *SiteMapService) KeepGuess(ctx context.Context, v Viewer, siteID uuid.UUID, key string) (*models.SiteMapLink, error) {
	w, err := s.editable(ctx, v, siteID)
	if err != nil {
		return nil, err
	}
	l, ok := w.findLink(key)
	if !ok || !l.Guess {
		return nil, ErrSiteMapInvalid
	}
	return s.insertLink(ctx, v, siteID, models.SiteMapLinkManual, l.A, l.B)
}

// ResetLayout forgets every saved position, so the map lays itself out
// again. Hidden and added devices stay as they are.
func (s *SiteMapService) ResetLayout(ctx context.Context, v Viewer, siteID uuid.UUID) error {
	if _, err := s.editable(ctx, v, siteID); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Model(&models.SiteMapPosition{}).Where("site_id = ?", siteID).
		Updates(map[string]any{"has_position": false, "x": 0, "y": 0, "updated_at": time.Now().UTC()}).Error; err != nil {
		return fmt.Errorf("resetting the map layout: %w", err)
	}
	s.changed(siteID)
	return nil
}
