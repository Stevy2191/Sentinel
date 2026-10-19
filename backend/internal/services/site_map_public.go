package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/sitemap"
)

// mapTokenLen is a base64url-encoded 32-byte token, as dashboards use.
const mapTokenLen = 43

func newMapToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *SiteMapService) adminOf(ctx context.Context, v Viewer, siteID uuid.UUID) error {
	level, err := s.access(ctx, v, siteID)
	if err != nil {
		return err
	}
	if level < SiteAccessAdmin || !v.IsAdmin {
		return ErrSiteMapForbidden
	}
	return nil
}

// PublicLink is the site map's public link, nil when it has none. Admins only.
func (s *SiteMapService) PublicLink(ctx context.Context, v Viewer, siteID uuid.UUID) (*models.SiteMapPublicLink, error) {
	if err := s.adminOf(ctx, v, siteID); err != nil {
		return nil, err
	}
	var row models.SiteMapPublicLink
	err := s.db.WithContext(ctx).Where("site_id = ?", siteID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading the map's public link: %w", err)
	}
	return &row, nil
}

// CreatePublicLink makes the map's public link, replacing any old one (whose
// token stops working at once); replaced reports that there was one.
func (s *SiteMapService) CreatePublicLink(ctx context.Context, v Viewer, siteID uuid.UUID) (link *models.SiteMapPublicLink, replaced bool, err error) {
	old, err := s.PublicLink(ctx, v, siteID)
	if err != nil {
		return nil, false, err
	}
	token, err := newMapToken()
	if err != nil {
		return nil, false, err
	}
	link = &models.SiteMapPublicLink{SiteID: siteID, Token: token, CreatedBy: v.UserID, CreatedAt: time.Now().UTC()}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "site_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"token", "created_by", "created_at"}),
	}).Create(link).Error; err != nil {
		return nil, false, fmt.Errorf("saving the map's public link: %w", err)
	}
	s.changed(siteID)
	return link, old != nil, nil
}

// RevokePublicLink turns the public link off.
func (s *SiteMapService) RevokePublicLink(ctx context.Context, v Viewer, siteID uuid.UUID) error {
	if err := s.adminOf(ctx, v, siteID); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&models.SiteMapPublicLink{}, "site_id = ?", siteID).Error; err != nil {
		return fmt.Errorf("removing the map's public link: %w", err)
	}
	s.changed(siteID)
	return nil
}

// ResolveToken is the site a public token opens: only a well-formed token
// whose link exists and whose creator is still an admin.
func (s *SiteMapService) ResolveToken(ctx context.Context, token string) (uuid.UUID, error) {
	if len(token) != mapTokenLen {
		return uuid.Nil, ErrSiteMapNotFound
	}
	var ids []uuid.UUID
	if err := s.db.WithContext(ctx).Raw(`SELECT pl.site_id FROM site_map_public_links pl JOIN users u ON u.id = pl.created_by
		WHERE pl.token = ? AND u.is_admin LIMIT 1`, token).Scan(&ids).Error; err != nil {
		return uuid.Nil, fmt.Errorf("resolving a public map token: %w", err)
	}
	if len(ids) == 0 {
		return uuid.Nil, ErrSiteMapNotFound
	}
	return ids[0], nil
}

// publicID is a device's stand-in id on a public map: the same every time
// for that site and device, but not the device's own id, which public views
// never show (as public dashboards don't).
func publicID(siteID, id uuid.UUID) uuid.UUID { return uuid.NewSHA1(siteID, id[:]) }

// publicKey is a link key with its devices' stand-in ids.
func publicKey(siteID uuid.UUID, key string) string {
	a, b, ok := parseLinkKey(key)
	if !ok {
		return ""
	}
	a.DeviceID, b.DeviceID = publicID(siteID, a.DeviceID), publicID(siteID, b.DeviceID)
	return sitemap.KeyOf(a, b)
}

// aliasSnapshot swaps every id in a public map for its stand-in.
func aliasSnapshot(m *MapSnapshot) {
	site := m.SiteID
	for i := range m.Devices {
		m.Devices[i].ID = publicID(site, m.Devices[i].ID)
	}
	for i := range m.Links {
		aliasLink(site, &m.Links[i])
	}
	m.SiteID = uuid.Nil
}

// aliasLink swaps a link's ids for stand-ins: a bundle's members each as a
// link, and its key rebuilt from theirs.
func aliasLink(site uuid.UUID, l *MapLink) {
	l.A.DeviceID, l.B.DeviceID = publicID(site, l.A.DeviceID), publicID(site, l.B.DeviceID)
	l.ManualID = nil
	if len(l.Members) == 0 {
		l.Key = publicKey(site, l.Key)
		return
	}
	keys := make([]string, len(l.Members))
	for i := range l.Members {
		aliasLink(site, &l.Members[i])
		keys[i] = l.Members[i].Key
	}
	l.Key = bundlePrefix + strings.Join(keys, ",")
}

// PublicTraffic is a site's traffic as its public map shows it: keyed by the
// public link keys, statuses by stand-in ids, and no site id.
func PublicTraffic(siteID uuid.UUID, t MapTraffic) MapTraffic {
	out := MapTraffic{At: t.At, Links: make(map[string]LinkRate, len(t.Links)), Devices: make(map[uuid.UUID]string, len(t.Devices))}
	for k, r := range t.Links {
		if pk := publicKey(siteID, k); pk != "" {
			out.Links[pk] = r
		}
	}
	for id, st := range t.Devices {
		out.Devices[publicID(siteID, id)] = st
	}
	return out
}

// PublicDevice is the map device a public map's stand-in id stands for.
func (s *SiteMapService) PublicDevice(ctx context.Context, siteID, alias uuid.UUID) (uuid.UUID, bool, error) {
	w, err := s.loadWith(ctx, siteID, false)
	if err != nil {
		return uuid.Nil, false, err
	}
	for id := range w.onMap {
		if !w.positions[id].Hidden && publicID(siteID, id) == alias {
			return id, true, nil
		}
	}
	return uuid.Nil, false, nil
}
