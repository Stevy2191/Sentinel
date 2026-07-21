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
	// No gorm `default` tag: with a bool default, GORM omits the false (zero)
	// value on insert and the DB default would apply — which would wrongly make
	// non-admin users admins. IsAdmin is always set explicitly by the caller.
	IsAdmin bool `json:"is_admin" gorm:"column:is_admin"`
	// Role mirrors IsAdmin ('admin' | 'user'). IsAdmin remains the authority for
	// JWT/authorization; Role is the human-facing value for user management.
	Role           string      `json:"role" gorm:"column:role;default:user"`
	LastLogin      *time.Time  `json:"last_login" gorm:"column:last_login"`
	// Per-user theme, synced across devices. Serialized as a nested object by the
	// /auth/me handler rather than these flat fields.
	ThemePrimaryColor string     `json:"-" gorm:"column:theme_primary_color;default:#10b981"`
	ThemeAccentColor  string     `json:"-" gorm:"column:theme_accent_color;default:#f59e0b"`
	ThemeMode         string     `json:"-" gorm:"column:theme_mode;default:auto"`
	CreatedAt      time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the User model.
func (User) TableName() string {
	return "users"
}

// User roles.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// RoleForAdmin maps the is_admin flag to a role string.
func RoleForAdmin(isAdmin bool) string {
	if isAdmin {
		return RoleAdmin
	}
	return RoleUser
}

// ValidRole reports whether r is an accepted role.
func ValidRole(r string) bool {
	return r == RoleAdmin || r == RoleUser
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
