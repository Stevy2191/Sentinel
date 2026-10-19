package services

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
