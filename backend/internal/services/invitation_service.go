package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

const invitationTTL = 7 * 24 * time.Hour

// ErrEmailNotConfigured is returned when SMTP is not set up.
var ErrEmailNotConfigured = errors.New("email is not configured (set SMTP_* environment variables)")

// InvitationService manages tokenized account invitations.
type InvitationService struct {
	db     *gorm.DB
	auth   *AuthService
	logger *log.Logger
}

// NewInvitationService returns an InvitationService. It uses AuthService to
// create the user when an invitation is accepted.
func NewInvitationService(db *gorm.DB, auth *AuthService) *InvitationService {
	return &InvitationService{db: db, auth: auth, logger: log.Default()}
}

func generateInviteToken() (string, error) {
	b := make([]byte, 24) // -> 32 base64url chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CreateInvitation creates a pending invitation for an email (admin action).
func (s *InvitationService) CreateInvitation(ctx context.Context, email, role string, invitedBy uuid.UUID) (*models.Invitation, error) {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, errors.New("a valid email is required")
	}
	if role == "" {
		role = models.RoleUser
	}
	if !models.ValidRole(role) {
		return nil, errors.New("role must be 'admin' or 'user'")
	}

	// Reject if the email already belongs to an account.
	if inUse, err := s.auth.emailInUse(ctx, email); err != nil {
		return nil, err
	} else if inUse {
		return nil, fmt.Errorf("an account already exists for %q", email)
	}
	// Reject if there is already an invitation for this email.
	var existing int64
	if err := s.db.WithContext(ctx).Model(&models.Invitation{}).
		Where("LOWER(email) = LOWER(?)", email).Count(&existing).Error; err != nil {
		return nil, fmt.Errorf("checking existing invitation: %w", err)
	}
	if existing > 0 {
		return nil, fmt.Errorf("%q has already been invited", email)
	}

	token, err := generateInviteToken()
	if err != nil {
		return nil, err
	}
	inv := &models.Invitation{
		ID:              uuid.New(),
		Email:           email,
		Token:           token,
		Role:            role,
		InvitedByUserID: invitedBy,
		ExpiresAt:       time.Now().Add(invitationTTL),
		CreatedAt:       time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(inv).Error; err != nil {
		return nil, fmt.Errorf("creating invitation: %w", err)
	}
	s.logger.Printf("[invite] created invitation for %s (role=%s) by %s", email, role, invitedBy)
	return inv, nil
}

// GetInvitationByToken fetches an invitation by its token.
func (s *InvitationService) GetInvitationByToken(ctx context.Context, token string) (*models.Invitation, error) {
	var inv models.Invitation
	err := s.db.WithContext(ctx).First(&inv, "token = ?", token).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("invitation not found: %w", err)
		}
		return nil, fmt.Errorf("fetching invitation: %w", err)
	}
	return &inv, nil
}

// AcceptInvitation creates the account for an invitation and marks it accepted.
func (s *InvitationService) AcceptInvitation(ctx context.Context, token, username, password string) (*models.User, error) {
	inv, err := s.GetInvitationByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if inv.Accepted {
		return nil, errors.New("this invitation has already been used")
	}
	if inv.IsExpired() {
		return nil, errors.New("this invitation has expired")
	}

	user, err := s.auth.CreateManagedUser(ctx, username, inv.Email, password, inv.Role)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&models.Invitation{}).
		Where("id = ?", inv.ID).
		Updates(map[string]interface{}{"accepted": true, "accepted_at": now}).Error; err != nil {
		s.logger.Printf("[invite] warning: user created but marking invitation accepted failed: %v", err)
	}
	s.logger.Printf("[invite] accepted by %s, created user %s", inv.Email, username)
	return user, nil
}

// ListPendingInvitations returns non-accepted, non-expired invitations.
func (s *InvitationService) ListPendingInvitations(ctx context.Context) ([]models.Invitation, error) {
	var invites []models.Invitation
	err := s.db.WithContext(ctx).
		Where("accepted = ? AND expires_at > ?", false, time.Now()).
		Order("created_at DESC").
		Find(&invites).Error
	if err != nil {
		return nil, fmt.Errorf("listing pending invitations: %w", err)
	}
	return invites, nil
}

// SendInvitationEmail emails the invitation link. Returns ErrEmailNotConfigured
// when SMTP is not set up.
func (s *InvitationService) SendInvitationEmail(inv *models.Invitation, inviterName string) error {
	host := strings.TrimSpace(os.Getenv("SMTP_HOST"))
	if host == "" {
		return ErrEmailNotConfigured
	}
	port := strings.TrimSpace(os.Getenv("SMTP_PORT"))
	if port == "" {
		port = "587"
	}
	user := strings.TrimSpace(os.Getenv("SMTP_USER"))
	pass := os.Getenv("SMTP_PASSWORD")
	from := strings.TrimSpace(os.Getenv("SMTP_FROM"))
	if from == "" {
		from = user
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("SENTINEL_BASE_URL")), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	link := fmt.Sprintf("%s/invitation/%s", base, inv.Token)

	if inviterName == "" {
		inviterName = "An administrator"
	}
	body := fmt.Sprintf(
		"Hi,\r\n\r\n%s has invited you to join Sentinel.\r\n\r\n"+
			"Accept your invitation here:\r\n%s\r\n\r\n"+
			"This link expires on %s.\r\n\r\nBest regards,\r\nSentinel",
		inviterName, link, inv.ExpiresAt.Format("Mon, 02 Jan 2006"),
	)
	msg := []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: You're invited to Sentinel\r\n"+
			"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		from, inv.Email, body,
	))

	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	if err := smtp.SendMail(host+":"+port, auth, from, []string{inv.Email}, msg); err != nil {
		return fmt.Errorf("sending invitation email: %w", err)
	}
	s.logger.Printf("[invite] invitation email sent to %s", inv.Email)
	return nil
}
