package models

import (
	"time"

	"github.com/google/uuid"
)

// Invitation is a pending, tokenized invite for someone to create an account.
type Invitation struct {
	ID              uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Email           string     `json:"email" gorm:"column:email;not null"`
	Token           string     `json:"-" gorm:"column:token;not null"` // never serialized to clients
	Role            string     `json:"role" gorm:"column:role"`
	InvitedByUserID uuid.UUID  `json:"invited_by_user_id" gorm:"column:invited_by_user_id;type:uuid;not null"`
	Accepted        bool       `json:"accepted" gorm:"column:accepted"`
	AcceptedAt      *time.Time `json:"accepted_at" gorm:"column:accepted_at"`
	ExpiresAt       time.Time  `json:"expires_at" gorm:"column:expires_at"`
	CreatedAt       time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName tells GORM which table backs the Invitation model.
func (Invitation) TableName() string {
	return "invitations"
}

// IsExpired reports whether the invitation's expiry has passed.
func (i *Invitation) IsExpired() bool {
	return time.Now().After(i.ExpiresAt)
}
