package services

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

const (
	defaultJWTExpiry = 24 * time.Hour
	mfaTokenExpiry   = 5 * time.Minute
	minJWTSecretLen  = 32
	backupCodeCount  = 10
	backupCodeLen    = 8
)

// backupCodeCharset excludes visually ambiguous characters (0/O, 1/I).
const backupCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// AuthService handles user creation, password verification, JWT issuance, and
// TOTP-based multi-factor authentication.
type AuthService struct {
	db        *gorm.DB
	jwtSecret string
	jwtExpiry time.Duration
	logger    *log.Logger
}

// NewAuthService returns an AuthService. The JWT secret should be at least 32
// characters for HS256; a shorter secret is accepted but logged as a warning.
func NewAuthService(db *gorm.DB, jwtSecret string) *AuthService {
	if len(jwtSecret) < minJWTSecretLen {
		log.Printf("[auth] WARNING: JWT_SECRET is shorter than %d characters; use a stronger secret", minJWTSecretLen)
	}
	log.Printf("[auth] auth service initialized")
	return &AuthService{
		db:        db,
		jwtSecret: jwtSecret,
		jwtExpiry: defaultJWTExpiry,
		logger:    log.Default(),
	}
}

// CreateUser validates and creates a user with a bcrypt-hashed password.
func (s *AuthService) CreateUser(ctx context.Context, username, password string, isAdmin bool) (*models.User, error) {
	user := &models.User{Username: username, IsAdmin: isAdmin}
	if err := user.Validate(); err != nil {
		return nil, err
	}
	if err := models.ValidatePassword(password); err != nil {
		return nil, err
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).
		Where("LOWER(username) = LOWER(?)", username).
		Count(&count).Error; err != nil {
		return nil, fmt.Errorf("checking username availability: %w", err)
	}
	if count > 0 {
		return nil, fmt.Errorf("username %q is already taken", username)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	now := time.Now()
	user.ID = uuid.New()
	user.PasswordHash = string(hash)
	user.CreatedAt = now
	user.UpdatedAt = now

	if err := s.db.WithContext(ctx).Create(user).Error; err != nil {
		return nil, fmt.Errorf("creating user: %w", err)
	}

	s.logger.Printf("[auth] user created: %s (admin=%t)", username, isAdmin)
	return user, nil
}

// GetUserByUsername fetches a user by (case-insensitive) username. The returned
// struct carries the password hash for internal verification, but the hash is
// never serialized (json:"-").
func (s *AuthService) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	var user models.User
	err := s.db.WithContext(ctx).First(&user, "LOWER(username) = LOWER(?)", username).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user %q not found: %w", username, err)
		}
		return nil, fmt.Errorf("fetching user %q: %w", username, err)
	}
	return &user, nil
}

// GetUserByID fetches a user by ID.
func (s *AuthService) GetUserByID(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	var user models.User
	err := s.db.WithContext(ctx).First(&user, "id = ?", userID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("user %s not found: %w", userID, err)
		}
		return nil, fmt.Errorf("fetching user %s: %w", userID, err)
	}
	return &user, nil
}

// VerifyPassword checks a username/password pair. On success it records the
// login time and returns the user; on failure it returns a generic error (so as
// not to reveal whether the username exists).
func (s *AuthService) VerifyPassword(ctx context.Context, username, password string) (*models.User, error) {
	user, err := s.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, errors.New("invalid username or password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, errors.New("invalid username or password")
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", user.ID).
		Update("last_login", now).Error; err != nil {
		s.logger.Printf("[auth] warning: could not update last_login for %s: %v", username, err)
	}
	user.LastLogin = &now

	s.logger.Printf("[auth] user logged in: %s", username)
	return user, nil
}

// GenerateJWT issues a signed HS256 token for a user.
func (s *AuthService) GenerateJWT(userID uuid.UUID, username string, isAdmin bool) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id":  userID.String(),
		"username": username,
		"is_admin": isAdmin,
		"sub":      userID.String(),
		"iat":      now.Unix(),
		"exp":      now.Add(s.jwtExpiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", fmt.Errorf("signing token: %w", err)
	}
	s.logger.Printf("[auth] JWT generated for: %s", username)
	return signed, nil
}

// VerifyJWT parses and validates a token, returning its claims.
func (s *AuthService) VerifyJWT(tokenString string) (map[string]interface{}, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}
	return map[string]interface{}{
		"user_id":  claims["user_id"],
		"username": claims["username"],
		"is_admin": claims["is_admin"],
		"exp":      claims["exp"],
	}, nil
}

// RefreshJWT issues a new token from a still-valid token's claims.
func (s *AuthService) RefreshJWT(ctx context.Context, oldTokenString string) (string, error) {
	claims, err := s.VerifyJWT(oldTokenString)
	if err != nil {
		return "", err
	}
	userIDStr, _ := claims["user_id"].(string)
	username, _ := claims["username"].(string)
	isAdmin, _ := claims["is_admin"].(bool)

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return "", fmt.Errorf("invalid user_id in token: %w", err)
	}
	// Confirm the user still exists.
	if _, err := s.GetUserByID(ctx, userID); err != nil {
		return "", err
	}

	s.logger.Printf("[auth] JWT refreshed for: %s", username)
	return s.GenerateJWT(userID, username, isAdmin)
}

// SetupMFA generates a TOTP secret and backup codes for a user and enables MFA.
// It returns the secret and an otpauth:// URL suitable for a QR code.
func (s *AuthService) SetupMFA(ctx context.Context, userID uuid.UUID) (string, string, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return "", "", err
	}

	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Sentinel", AccountName: user.Username})
	if err != nil {
		return "", "", fmt.Errorf("generating TOTP secret: %w", err)
	}

	codes := make(models.StringSlice, backupCodeCount)
	for i := range codes {
		codes[i] = generateBackupCode()
	}

	// Store the secret + backup codes but leave MFA disabled until the user
	// confirms with a valid TOTP code (see ConfirmMFASetup). This keeps an
	// abandoned setup from locking the account into MFA.
	user.MFASecret = key.Secret()
	user.MFABackupCodes = codes
	user.MFAEnabled = false
	user.UpdatedAt = time.Now()
	if err := s.db.WithContext(ctx).Save(user).Error; err != nil {
		return "", "", fmt.Errorf("saving MFA setup: %w", err)
	}

	s.logger.Printf("[auth] MFA setup initiated for: %s", user.Username)
	return key.Secret(), key.URL(), nil
}

// ConfirmMFASetup verifies a TOTP code against the stored (not-yet-enabled)
// secret and, on success, enables MFA for the user.
func (s *AuthService) ConfirmMFASetup(ctx context.Context, userID uuid.UUID, totpCode string) error {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if user.MFASecret == "" {
		return errors.New("MFA is not set up for this user")
	}
	if !totp.Validate(totpCode, user.MFASecret) {
		return errors.New("Invalid TOTP code")
	}

	user.MFAEnabled = true
	user.UpdatedAt = time.Now()
	if err := s.db.WithContext(ctx).Save(user).Error; err != nil {
		return fmt.Errorf("enabling MFA: %w", err)
	}

	s.logger.Printf("[auth] MFA confirmed and enabled for: %s", user.Username)
	return nil
}

// VerifyMFA validates a 6-digit TOTP code (with the standard 30s window).
func (s *AuthService) VerifyMFA(ctx context.Context, userID uuid.UUID, totpCode string) (bool, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	if user.MFASecret == "" {
		return false, errors.New("MFA is not set up for this user")
	}
	if totp.Validate(totpCode, user.MFASecret) {
		s.logger.Printf("[auth] MFA verified for: %s", user.Username)
		return true, nil
	}
	return false, nil
}

// VerifyMFABackupCode checks a one-time backup code, consuming it on success.
func (s *AuthService) VerifyMFABackupCode(ctx context.Context, userID uuid.UUID, backupCode string) (bool, error) {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	for i, code := range user.MFABackupCodes {
		if code == backupCode {
			// Remove the used code and persist.
			user.MFABackupCodes = append(user.MFABackupCodes[:i], user.MFABackupCodes[i+1:]...)
			user.UpdatedAt = time.Now()
			if err := s.db.WithContext(ctx).Save(user).Error; err != nil {
				return false, fmt.Errorf("consuming backup code: %w", err)
			}
			s.logger.Printf("[auth] MFA backup code used for: %s", user.Username)
			return true, nil
		}
	}
	return false, nil
}

// DisableMFA clears a user's MFA secret and backup codes.
func (s *AuthService) DisableMFA(ctx context.Context, userID uuid.UUID) error {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	user.MFASecret = ""
	user.MFABackupCodes = nil
	user.MFAEnabled = false
	user.UpdatedAt = time.Now()
	if err := s.db.WithContext(ctx).Save(user).Error; err != nil {
		return fmt.Errorf("disabling MFA: %w", err)
	}
	s.logger.Printf("[auth] MFA disabled for: %s", user.Username)
	return nil
}

// ChangePassword verifies the current password and sets a new one (after
// validating its strength).
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	user, err := s.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return errors.New("current password is incorrect")
	}
	if err := models.ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing new password: %w", err)
	}
	if err := s.db.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{"password_hash": string(hash), "updated_at": time.Now()}).Error; err != nil {
		return fmt.Errorf("updating password: %w", err)
	}
	s.logger.Printf("[auth] password changed for user %s", userID)
	return nil
}

// UpdateUserTheme persists a user's theme colors and (when provided) mode.
func (s *AuthService) UpdateUserTheme(ctx context.Context, userID uuid.UUID, primary, accent, mode string) error {
	updates := map[string]interface{}{
		"theme_primary_color": primary,
		"theme_accent_color":  accent,
		"updated_at":          time.Now(),
	}
	if mode != "" {
		updates["theme_mode"] = mode
	}
	result := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("updating theme for user %s: %w", userID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user %s not found: %w", userID, gorm.ErrRecordNotFound)
	}
	s.logger.Printf("[auth] theme updated for user %s", userID)
	return nil
}

// GenerateMFAToken issues a short-lived (5-minute) token used to complete an
// MFA challenge after a correct password. It carries an mfa_pending claim and
// grants no API access on its own.
func (s *AuthService) GenerateMFAToken(userID uuid.UUID, username string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"user_id":     userID.String(),
		"username":    username,
		"mfa_pending": true,
		"iat":         now.Unix(),
		"exp":         now.Add(mfaTokenExpiry).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return "", fmt.Errorf("signing MFA token: %w", err)
	}
	return signed, nil
}

// VerifyMFAToken validates an MFA-pending token and returns its user ID and
// username. It rejects ordinary access tokens (mfa_pending must be true).
func (s *AuthService) VerifyMFAToken(tokenString string) (uuid.UUID, string, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("invalid MFA token: %w", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return uuid.Nil, "", errors.New("invalid MFA token")
	}
	if pending, _ := claims["mfa_pending"].(bool); !pending {
		return uuid.Nil, "", errors.New("token is not an MFA token")
	}
	userIDStr, _ := claims["user_id"].(string)
	username, _ := claims["username"].(string)
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("invalid user_id in MFA token: %w", err)
	}
	return userID, username, nil
}

// ListUsers returns all users ordered by username (for share pickers). The
// password hash is never serialized (json:"-").
func (s *AuthService) ListUsers(ctx context.Context) ([]models.User, error) {
	var users []models.User
	if err := s.db.WithContext(ctx).Order("username ASC").Find(&users).Error; err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	return users, nil
}

// HasAnyUser reports whether any user accounts exist (used to make the first
// registered account an admin).
func (s *AuthService) HasAnyUser(ctx context.Context) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Count(&count).Error; err != nil {
		return false, fmt.Errorf("counting users: %w", err)
	}
	return count > 0, nil
}

// UpdateLastLogin records the current time as a user's last login.
func (s *AuthService) UpdateLastLogin(ctx context.Context, userID uuid.UUID) error {
	result := s.db.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", userID).
		Update("last_login", time.Now())
	if result.Error != nil {
		return fmt.Errorf("updating last login for %s: %w", userID, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("user %s not found: %w", userID, gorm.ErrRecordNotFound)
	}
	return nil
}

// generateBackupCode returns a random 8-character backup code.
func generateBackupCode() string {
	b := make([]byte, backupCodeLen)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is exceptional; fall back to a time-seeded value.
		return fmt.Sprintf("%08d", time.Now().UnixNano()%1e8)
	}
	out := make([]byte, backupCodeLen)
	for i := range b {
		out[i] = backupCodeCharset[int(b[i])%len(backupCodeCharset)]
	}
	return string(out)
}
