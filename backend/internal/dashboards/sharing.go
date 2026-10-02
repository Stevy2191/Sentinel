package dashboards

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// ShareView is one share with the recipient's name, for the sharing panel.
type ShareView struct {
	UserID     uuid.UUID `json:"user_id" gorm:"column:user_id"`
	Username   string    `json:"username" gorm:"column:username"`
	Email      string    `json:"email" gorm:"column:email"`
	Permission string    `json:"permission" gorm:"column:permission"`
	CreatedAt  time.Time `json:"created_at" gorm:"column:created_at"`
}

// shareable loads a personal dashboard v may share.
func (s *Service) shareable(ctx context.Context, v Viewer, id uuid.UUID) (*DashboardView, error) {
	vw, err := s.load(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if vw.SiteID != nil {
		return nil, ErrShareSiteDashboard
	}
	if !canShare(v, &vw.Dashboard) {
		return nil, ErrForbidden
	}
	return vw, nil
}

// ListShares returns who a personal dashboard is shared with, oldest first.
func (s *Service) ListShares(ctx context.Context, v Viewer, id uuid.UUID) ([]ShareView, error) {
	if _, err := s.shareable(ctx, v, id); err != nil {
		return nil, err
	}
	var out []ShareView
	err := s.db.WithContext(ctx).Table("dashboard_sharing AS sh").
		Select("sh.user_id, u.username, u.email, sh.permission, sh.created_at").
		Joins("JOIN users u ON u.id = sh.user_id").
		Where("sh.dashboard_id = ?", id).Order("sh.created_at").Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing dashboard shares: %w", err)
	}
	return out, nil
}

// UpsertShare shares a personal dashboard with userID, or changes the
// permission of an existing share.
func (s *Service) UpsertShare(ctx context.Context, v Viewer, id, userID uuid.UUID, permission string) error {
	vw, err := s.shareable(ctx, v, id)
	if err != nil {
		return err
	}
	if permission != models.PermissionReadonly && permission != models.PermissionEditable {
		return invalid("permission must be readonly or editable")
	}
	if vw.OwnerID != nil && *vw.OwnerID == userID {
		return ErrShareOwner
	}
	var n int64
	if err := s.db.WithContext(ctx).Table("users").Where("id = ?", userID).Count(&n).Error; err != nil {
		return fmt.Errorf("checking user: %w", err)
	}
	if n == 0 {
		return ErrUnknownUser
	}
	share := models.DashboardShare{DashboardID: id, UserID: userID, Permission: permission}
	err = s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dashboard_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"permission"}),
	}).Create(&share).Error
	if err != nil {
		return fmt.Errorf("sharing dashboard: %w", err)
	}
	return nil
}

// RemoveShare stops sharing a personal dashboard with userID. Removing a
// share that does not exist is not an error.
func (s *Service) RemoveShare(ctx context.Context, v Viewer, id, userID uuid.UUID) error {
	if _, err := s.shareable(ctx, v, id); err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&models.DashboardShare{}, "dashboard_id = ? AND user_id = ?", id, userID).Error; err != nil {
		return fmt.Errorf("unsharing dashboard: %w", err)
	}
	return nil
}
