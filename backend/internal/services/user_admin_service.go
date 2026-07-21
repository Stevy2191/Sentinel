package services

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// This file adds admin-facing user management to AuthService. Authorization
// (admin-only, no self-delete/self-demote) is enforced by the handlers; these
// methods perform the data operations. IsAdmin remains the authorization
// authority and is kept in sync with Role.

// generatePassword returns a random 16-char password containing at least one
// upper, lower, digit, and symbol.
func generatePassword() string {
	const (
		lower  = "abcdefghijkmnopqrstuvwxyz"
		upper  = "ABCDEFGHJKLMNPQRSTUVWXYZ"
		digits = "23456789"
		syms   = "!@#$%^&*-_"
		all    = lower + upper + digits + syms
	)
	pick := func(set string) byte {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
		return set[n.Int64()]
	}
	b := []byte{pick(lower), pick(upper), pick(digits), pick(syms)}
	for len(b) < 16 {
		b = append(b, pick(all))
	}
	// Shuffle so the guaranteed characters aren't always first.
	for i := len(b) - 1; i > 0; i-- {
		nj, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		j := int(nj.Int64())
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

// GetUserCount returns the number of user accounts.
func (s *AuthService) GetUserCount(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("counting users: %w", err)
	}
	return count, nil
}

// GetUserByEmail fetches a user by (case-insensitive, non-empty) email.
func (s *AuthService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, errors.New("email is required")
	}
	var user models.User
	err := s.db.WithContext(ctx).First(&user, "email <> '' AND LOWER(email) = LOWER(?)", email).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user with email %q not found: %w", email, err)
		}
		return nil, fmt.Errorf("fetching user by email: %w", err)
	}
	return &user, nil
}

// emailInUse reports whether a non-empty email already belongs to an account.
func (s *AuthService) emailInUse(ctx context.Context, email string) (bool, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return false, nil
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&models.User{}).
		Where("email <> '' AND LOWER(email) = LOWER(?)", email).Count(&count).Error
	if err != nil {
		return false, fmt.Errorf("checking email: %w", err)
	}
	return count > 0, nil
}

// CreateManagedUser creates a user with an explicit email + role (admin action).
func (s *AuthService) CreateManagedUser(ctx context.Context, username, email, password, role string) (*models.User, error) {
	if !models.ValidRole(role) {
		return nil, errors.New("role must be 'admin' or 'user'")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, errors.New("email is required")
	}
	if inUse, err := s.emailInUse(ctx, email); err != nil {
		return nil, err
	} else if inUse {
		return nil, fmt.Errorf("email %q is already in use", email)
	}

	isAdmin := role == models.RoleAdmin
	user := &models.User{
		ID:       uuid.New(),
		Username: username,
		Email:    email,
		IsAdmin:  isAdmin,
		Role:     role,
	}
	if err := user.Validate(); err != nil {
		return nil, err
	}
	if err := models.ValidatePassword(password); err != nil {
		return nil, err
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).
		Where("LOWER(username) = LOWER(?)", username).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("checking username: %w", err)
	}
	if count > 0 {
		return nil, fmt.Errorf("username %q is already taken", username)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}
	now := time.Now()
	user.PasswordHash = string(hash)
	user.CreatedAt = now
	user.UpdatedAt = now
	if err := s.db.WithContext(ctx).Create(user).Error; err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}
	s.logger.Printf("[users] created user %s (role=%s)", username, role)
	return user, nil
}

// CreateManagedUserAutoPassword creates a user with a generated password,
// returning the plaintext password once.
func (s *AuthService) CreateManagedUserAutoPassword(ctx context.Context, username, email, role string) (*models.User, string, error) {
	pw := generatePassword()
	user, err := s.CreateManagedUser(ctx, username, email, pw, role)
	if err != nil {
		return nil, "", err
	}
	return user, pw, nil
}

// ListUsersDetailed returns all users ordered by creation (newest first).
func (s *AuthService) ListUsersDetailed(ctx context.Context) ([]models.User, error) {
	var users []models.User
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	return users, nil
}

// DeleteUser removes a user and the sharing rows they created (their owned
// monitors and shares-as-recipient cascade via FKs; shares they granted on
// others' monitors do not, so remove those first).
func (s *AuthService) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("shared_by_user_id = ?", userID).Delete(&models.MonitorSharing{}).Error; err != nil {
			return fmt.Errorf("removing shares granted by user: %w", err)
		}
		result := tx.Delete(&models.User{}, "id = ?", userID)
		if result.Error != nil {
			return fmt.Errorf("deleting user %s: %w", userID, result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("user %s not found: %w", userID, gorm.ErrRecordNotFound)
		}
		return nil
	})
}

// ResetUserPassword sets a new password for a user (admin action).
func (s *AuthService) ResetUserPassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	if err := models.ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	result := s.db.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{"password_hash": string(hash), "updated_at": time.Now()})
	if result.Error != nil {
		return fmt.Errorf("resetting password for %s: %w", userID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user %s not found: %w", userID, gorm.ErrRecordNotFound)
	}
	s.logger.Printf("[users] password reset for %s", userID)
	return nil
}

// ResetUserPasswordAuto resets a user's password to a generated one, returning
// the plaintext once.
func (s *AuthService) ResetUserPasswordAuto(ctx context.Context, userID uuid.UUID) (string, error) {
	pw := generatePassword()
	if err := s.ResetUserPassword(ctx, userID, pw); err != nil {
		return "", err
	}
	return pw, nil
}

// ChangeUserRole updates a user's role and keeps is_admin in sync.
func (s *AuthService) ChangeUserRole(ctx context.Context, userID uuid.UUID, newRole string) error {
	if !models.ValidRole(newRole) {
		return errors.New("role must be 'admin' or 'user'")
	}
	result := s.db.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"role":       newRole,
			"is_admin":   newRole == models.RoleAdmin,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		return fmt.Errorf("changing role for %s: %w", userID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user %s not found: %w", userID, gorm.ErrRecordNotFound)
	}
	s.logger.Printf("[users] role changed for %s -> %s", userID, newRole)
	return nil
}
