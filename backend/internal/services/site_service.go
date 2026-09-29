package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

var (
	ErrSiteNotFound         = errors.New("site not found")
	ErrSiteNameTaken        = errors.New("a site with this name already exists")
	ErrSiteNotEmpty         = errors.New("this site still contains items; remove them before deleting it")
	ErrSiteShareNotFound    = errors.New("this site is not shared with that user")
	ErrSiteShareUnknownUser = errors.New("no such user")
)

// siteContentTables lists the tables whose rows belong to a site, each with a
// site_id column. Delete refuses while any of them has a row for the site, so
// deleting a site is never a way to silently delete its devices.
//
// Empty in phase 0: nothing can belong to a site yet. Each later phase adds
// its own tables (devices in phase 1, dashboards in 4, maps in 6).
var siteContentTables = []string{}

// SiteShareView is a share with the recipient's name, for the sharing panel.
type SiteShareView struct {
	models.SiteSharing
	Username string `json:"username"`
	Email    string `json:"email"`
}

// SiteService stores sites and their sharing. Permission rules live in
// resolveSiteAccess, not here.
type SiteService struct {
	db *gorm.DB
}

func NewSiteService(db *gorm.DB) *SiteService {
	return &SiteService{db: db}
}

// List returns every site for an admin, and the shared ones for anyone else.
func (s *SiteService) List(ctx context.Context, userID uuid.UUID, isAdmin bool) ([]models.Site, error) {
	q := s.db.WithContext(ctx).Model(&models.Site{})
	if !isAdmin {
		q = q.Where("id IN (SELECT site_id FROM site_sharing WHERE shared_with_user_id = ?)", userID)
	}
	var sites []models.Site
	if err := q.Order("lower(name)").Find(&sites).Error; err != nil {
		return nil, fmt.Errorf("listing sites: %w", err)
	}
	return sites, nil
}

// Get returns one site, or ErrSiteNotFound.
func (s *SiteService) Get(ctx context.Context, id uuid.UUID) (*models.Site, error) {
	var site models.Site
	err := s.db.WithContext(ctx).First(&site, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSiteNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading site: %w", err)
	}
	return &site, nil
}

// SiteAccess is what userID may do in siteID. It returns ErrSiteNotFound for a
// site that does not exist, so callers can answer 404 for both "missing" and
// "not yours" without telling the two apart to the client.
func (s *SiteService) SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (SiteAccessLevel, error) {
	if _, err := s.Get(ctx, siteID); err != nil {
		return SiteAccessNone, err
	}
	if isAdmin {
		return resolveSiteAccess(true, nil), nil
	}
	var share models.SiteSharing
	err := s.db.WithContext(ctx).
		Where("site_id = ? AND shared_with_user_id = ?", siteID, userID).
		First(&share).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return resolveSiteAccess(false, nil), nil
	}
	if err != nil {
		return SiteAccessNone, fmt.Errorf("loading site share: %w", err)
	}
	return resolveSiteAccess(false, &share), nil
}

// Create stores a new site. in must already be normalized.
func (s *SiteService) Create(ctx context.Context, in models.SiteInput, createdBy uuid.UUID) (*models.Site, error) {
	site := models.Site{Name: in.Name, Description: in.Description, Address: in.Address, CreatedBy: &createdBy}
	if err := s.db.WithContext(ctx).Create(&site).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrSiteNameTaken
		}
		return nil, fmt.Errorf("creating site: %w", err)
	}
	return &site, nil
}

// Update replaces a site's editable fields. in must already be normalized.
// Returns the site before and after, for the audit log.
func (s *SiteService) Update(ctx context.Context, id uuid.UUID, in models.SiteInput) (*models.Site, *models.Site, error) {
	before, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	err = s.db.WithContext(ctx).Model(&models.Site{}).Where("id = ?", id).
		Updates(map[string]any{"name": in.Name, "description": in.Description, "address": in.Address, "updated_at": gorm.Expr("now()")}).Error
	if err != nil {
		if isDuplicateKey(err) {
			return nil, nil, ErrSiteNameTaken
		}
		return nil, nil, fmt.Errorf("updating site: %w", err)
	}
	after, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

// Delete removes an empty site and its shares. Returns the deleted site, for
// the audit log.
func (s *SiteService) Delete(ctx context.Context, id uuid.UUID) (*models.Site, error) {
	site, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, table := range siteContentTables {
		var n int64
		// table comes from the constant list above, never from input.
		if err := s.db.WithContext(ctx).Table(table).Where("site_id = ?", id).Count(&n).Error; err != nil {
			return nil, fmt.Errorf("checking %s for site contents: %w", table, err)
		}
		if n > 0 {
			return nil, ErrSiteNotEmpty
		}
	}
	// site_sharing rows go with it (ON DELETE CASCADE).
	if err := s.db.WithContext(ctx).Delete(&models.Site{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting site: %w", err)
	}
	return site, nil
}

// ListShares returns everyone the site is shared with, oldest first.
func (s *SiteService) ListShares(ctx context.Context, siteID uuid.UUID) ([]SiteShareView, error) {
	if _, err := s.Get(ctx, siteID); err != nil {
		return nil, err
	}
	var out []SiteShareView
	err := s.db.WithContext(ctx).
		Table("site_sharing").
		Select("site_sharing.*, users.username, users.email").
		Joins("JOIN users ON users.id = site_sharing.shared_with_user_id").
		Where("site_sharing.site_id = ?", siteID).
		Order("site_sharing.created_at ASC").
		Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing site shares: %w", err)
	}
	return out, nil
}

// UpsertShare grants userID access to siteID, or changes the permission of an
// existing share. permission must already be validated.
func (s *SiteService) UpsertShare(ctx context.Context, siteID, userID, sharedBy uuid.UUID, permission string) (*models.SiteSharing, error) {
	if _, err := s.Get(ctx, siteID); err != nil {
		return nil, err
	}
	share := models.SiteSharing{SiteID: siteID, SharedWithUserID: userID, Permission: permission, SharedByUserID: &sharedBy}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "site_id"}, {Name: "shared_with_user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"permission":        permission,
			"shared_by_user_id": sharedBy,
			"updated_at":        gorm.Expr("now()"),
		}),
	}).Create(&share).Error
	if err != nil {
		if isForeignKeyViolation(err) {
			return nil, ErrSiteShareUnknownUser
		}
		return nil, fmt.Errorf("sharing site: %w", err)
	}
	// Reload: on conflict, share.ID is not the stored row's id.
	var stored models.SiteSharing
	if err := s.db.WithContext(ctx).
		Where("site_id = ? AND shared_with_user_id = ?", siteID, userID).
		First(&stored).Error; err != nil {
		return nil, fmt.Errorf("loading site share: %w", err)
	}
	return &stored, nil
}

// RemoveShare revokes userID's access to siteID.
func (s *SiteService) RemoveShare(ctx context.Context, siteID, userID uuid.UUID) error {
	res := s.db.WithContext(ctx).
		Where("site_id = ? AND shared_with_user_id = ?", siteID, userID).
		Delete(&models.SiteSharing{})
	if res.Error != nil {
		return fmt.Errorf("removing site share: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrSiteShareNotFound
	}
	return nil
}

// isForeignKeyViolation reports whether err is a foreign-key violation, which
// for a share means the user id does not exist (the site was checked first).
func isForeignKeyViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "foreign key") || strings.Contains(msg, "23503")
}
