// Package services - audit_service.go records who changed what.
package services

import (
	"context"
	"log"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// AuditService writes audit entries.
type AuditService struct {
	db     *gorm.DB
	logger *log.Logger
}

// NewAuditService returns a service bound to db.
func NewAuditService(db *gorm.DB) *AuditService {
	return &AuditService{db: db, logger: log.Default()}
}

// Actor identifies who performed an action.
type Actor struct {
	UserID   uuid.UUID
	Username string
	IP       string
}

// Record writes one audit entry.
//
// A failure here is logged and swallowed rather than returned. The alternative
// is worse in both directions: propagating the error would let a full disk or a
// locked table block report deletion, and retrying inline would stall the
// request. The log line preserves the fact that an entry was lost, which is the
// part an operator needs.
func (s *AuditService) Record(
	ctx context.Context,
	actor Actor,
	action, resourceType string,
	resourceID *uuid.UUID,
	changes models.AuditChanges,
) {
	entry := models.AuditEntry{
		ID:           uuid.New(),
		Username:     actor.Username,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		Changes:      changes,
		IPAddress:    actor.IP,
	}
	if actor.UserID != uuid.Nil {
		id := actor.UserID
		entry.UserID = &id
	}

	if err := s.db.WithContext(ctx).Create(&entry).Error; err != nil {
		s.logger.Printf("[audit] LOST ENTRY %s on %s %v by %s: %v",
			action, resourceType, resourceID, actor.Username, err)
	}
}

// AuditFilter narrows a listing.
type AuditFilter struct {
	Action       string
	ResourceType string
	ResourceID   *uuid.UUID
	UserID       *uuid.UUID
	Limit        int
	Offset       int
}

// List returns audit entries newest first, along with the unfiltered total.
func (s *AuditService) List(ctx context.Context, f AuditFilter) ([]models.AuditEntry, int64, error) {
	q := s.db.WithContext(ctx).Model(&models.AuditEntry{})
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.ResourceType != "" {
		q = q.Where("resource_type = ?", f.ResourceType)
	}
	if f.ResourceID != nil {
		q = q.Where("resource_id = ?", *f.ResourceID)
	}
	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	// Offset is already >0-checked by the handler, but has no upper bound -
	// an admin (or a leaked/misused admin session) requesting a very large
	// offset makes Postgres walk and discard that many rows before returning
	// anything. total is already known from the Count above, so any offset
	// past it can only return an empty page; clamp there instead of letting
	// an arbitrarily large value reach the query.
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	if int64(offset) > total {
		offset = int(total)
	}

	var entries []models.AuditEntry
	if err := q.Order("created_at DESC").Limit(limit).Offset(f.Offset).
		Find(&entries).Error; err != nil {
		return nil, 0, err
	}
	return entries, total, nil
}
