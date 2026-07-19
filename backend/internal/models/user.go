package models

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

const specialChars = "!@#$%^&*"

// User is an authenticated Sentinel account.
type User struct {
	ID             uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Username       string      `json:"username" gorm:"column:username;not null"`
	Email          string      `json:"email" gorm:"column:email"`
	PasswordHash   string      `json:"-" gorm:"column:password_hash;not null"`
	MFAEnabled     bool        `json:"mfa_enabled" gorm:"column:mfa_enabled;default:false"`
	MFASecret      string      `json:"-" gorm:"column:mfa_secret"`
	MFABackupCodes StringSlice `json:"-" gorm:"column:mfa_backup_codes;type:jsonb"`
	IsAdmin        bool        `json:"is_admin" gorm:"column:is_admin;default:true"`
	LastLogin      *time.Time  `json:"last_login" gorm:"column:last_login"`
	CreatedAt      time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the User model.
func (User) TableName() string {
	return "users"
}

// Validate checks the user's stored fields (username and, if set, email). The
// password is validated separately via ValidatePassword during creation, since
// it is never stored on the struct.
func (u *User) Validate() error {
	if !usernamePattern.MatchString(u.Username) {
		return errors.New("username must be 3-32 characters, letters, numbers, and underscores only")
	}
	if u.Email != "" && !strings.Contains(u.Email, "@") {
		return fmt.Errorf("invalid email address: %q", u.Email)
	}
	return nil
}

// ValidatePassword enforces the password-strength policy: at least 12 chars with
// an uppercase letter, a digit, and a special character.
func ValidatePassword(password string) error {
	if len(password) < 12 {
		return errors.New("password must be at least 12 characters")
	}
	if !strings.ContainsFunc(password, func(r rune) bool { return r >= 'A' && r <= 'Z' }) {
		return errors.New("password must contain an uppercase letter")
	}
	if !strings.ContainsFunc(password, func(r rune) bool { return r >= '0' && r <= '9' }) {
		return errors.New("password must contain a number")
	}
	if !strings.ContainsAny(password, specialChars) {
		return fmt.Errorf("password must contain a special character (%s)", specialChars)
	}
	return nil
}
