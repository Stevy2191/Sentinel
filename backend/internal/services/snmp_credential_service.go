package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/cryptutil"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

var (
	ErrCredentialNotFound     = errors.New("credential profile not found")
	ErrCredentialNameTaken    = errors.New("a credential profile with this name already exists")
	ErrCredentialInUse        = errors.New("this credential profile is used by devices; move them to another profile first")
	ErrCredentialSiteMismatch = errors.New("devices in other sites use this profile, so it cannot be limited to one site")
	// ErrCredentialDecryptFailed wraps a failure to decrypt a stored secret
	// (wrong/rotated key, corrupt ciphertext), distinguishing it from a
	// database error in the lookup that came before it.
	ErrCredentialDecryptFailed = errors.New("credential profile could not be decrypted")
)

// CredentialView is a profile as the API shows it: never a secret, only
// whether each is set.
type CredentialView struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	SiteID          *uuid.UUID `json:"site_id"`
	SiteName        string     `json:"site_name"`
	Version         string     `json:"version"`
	Username        string     `json:"username"`
	AuthProtocol    string     `json:"auth_protocol"`
	PrivProtocol    string     `json:"priv_protocol"`
	HasCommunity    bool       `json:"has_community"`
	HasAuthPassword bool       `json:"has_auth_password"`
	HasPrivPassword bool       `json:"has_priv_password"`
	UsedBy          int64      `json:"used_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// CredentialOption is what editors see when choosing a profile for a device.
type CredentialOption struct {
	ID      uuid.UUID  `json:"id"`
	Name    string     `json:"name"`
	Version string     `json:"version"`
	SiteID  *uuid.UUID `json:"site_id"`
}

// ViewOf builds the API view of a stored profile.
func ViewOf(c models.SNMPCredential, usedBy int64, siteName string) CredentialView {
	return CredentialView{
		ID: c.ID, Name: c.Name, SiteID: c.SiteID, SiteName: siteName, Version: c.Version,
		Username: c.Username, AuthProtocol: c.AuthProtocol, PrivProtocol: c.PrivProtocol,
		HasCommunity: c.Community != "", HasAuthPassword: c.AuthPassword != "", HasPrivPassword: c.PrivPassword != "",
		UsedBy: usedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// SNMPCredentialService stores credential profiles, encrypting secrets.
type SNMPCredentialService struct {
	db *gorm.DB
}

func NewSNMPCredentialService(db *gorm.DB) *SNMPCredentialService {
	return &SNMPCredentialService{db: db}
}

type credentialRow struct {
	models.SNMPCredential
	UsedBy   int64  `gorm:"column:used_by"`
	SiteName string `gorm:"column:site_name"`
}

func (s *SNMPCredentialService) query(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Table("snmp_credentials AS c").
		Select(`c.*, COALESCE(st.name, '') AS site_name,
			(SELECT count(*) FROM devices d WHERE d.credential_id = c.id) AS used_by`).
		Joins("LEFT JOIN sites st ON st.id = c.site_id")
}

// List returns every profile, by name.
func (s *SNMPCredentialService) List(ctx context.Context) ([]CredentialView, error) {
	var rows []credentialRow
	if err := s.query(ctx).Order("lower(c.name)").Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("listing credential profiles: %w", err)
	}
	out := make([]CredentialView, 0, len(rows))
	for _, r := range rows {
		out = append(out, ViewOf(r.SNMPCredential, r.UsedBy, r.SiteName))
	}
	return out, nil
}

func (s *SNMPCredentialService) view(ctx context.Context, id uuid.UUID) (CredentialView, error) {
	var r credentialRow
	if err := s.query(ctx).Where("c.id = ?", id).Scan(&r).Error; err != nil {
		return CredentialView{}, fmt.Errorf("loading credential profile: %w", err)
	}
	if r.ID == uuid.Nil {
		return CredentialView{}, ErrCredentialNotFound
	}
	return ViewOf(r.SNMPCredential, r.UsedBy, r.SiteName), nil
}

func (s *SNMPCredentialService) get(ctx context.Context, id uuid.UUID) (*models.SNMPCredential, error) {
	var c models.SNMPCredential
	err := s.db.WithContext(ctx).First(&c, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading credential profile: %w", err)
	}
	return &c, nil
}

// seal encrypts a new secret; nil means "keep" and returns keep.
func seal(secret *string, keep string) (string, error) {
	if secret == nil {
		return keep, nil
	}
	return cryptutil.Encrypt(*secret)
}

// apply writes a normalized input onto c, encrypting new secrets and keeping
// stored ones only where the version still uses them.
func apply(c *models.SNMPCredential, in models.CredentialInput) error {
	var err error
	c.Name, c.SiteID, c.Version = in.Name, in.SiteID, in.Version
	c.Username, c.AuthProtocol, c.PrivProtocol = in.Username, in.AuthProtocol, in.PrivProtocol
	if in.Version == models.SNMPVersion3 {
		c.Community = ""
		if in.AuthProtocol == models.SNMPProtoNone {
			c.AuthPassword = ""
		} else if c.AuthPassword, err = seal(in.AuthPassword, c.AuthPassword); err != nil {
			return err
		}
		if in.PrivProtocol == models.SNMPProtoNone {
			c.PrivPassword = ""
		} else if c.PrivPassword, err = seal(in.PrivPassword, c.PrivPassword); err != nil {
			return err
		}
		return nil
	}
	c.AuthPassword, c.PrivPassword = "", ""
	c.Community, err = seal(in.Community, c.Community)
	return err
}

func mapCredentialWriteError(err error) error {
	switch {
	case isDuplicateKey(err):
		return ErrCredentialNameTaken
	case isForeignKeyViolation(err):
		return ErrSiteNotFound
	default:
		return fmt.Errorf("saving credential profile: %w", err)
	}
}

// Create stores a new profile.
func (s *SNMPCredentialService) Create(ctx context.Context, raw models.CredentialInput, by uuid.UUID) (CredentialView, error) {
	in, err := models.NormalizeCredentialInput(raw, nil)
	if err != nil {
		return CredentialView{}, err
	}
	c := models.SNMPCredential{}
	if by != uuid.Nil {
		c.CreatedBy = &by
	}
	if err := apply(&c, in); err != nil {
		return CredentialView{}, err
	}
	if err := s.db.WithContext(ctx).Create(&c).Error; err != nil {
		return CredentialView{}, mapCredentialWriteError(err)
	}
	return s.view(ctx, c.ID)
}

// Update changes a profile. Blank secrets keep the stored ones.
func (s *SNMPCredentialService) Update(ctx context.Context, id uuid.UUID, raw models.CredentialInput) (CredentialView, CredentialView, error) {
	before, err := s.view(ctx, id)
	if err != nil {
		return CredentialView{}, CredentialView{}, err
	}
	c, err := s.get(ctx, id)
	if err != nil {
		return CredentialView{}, CredentialView{}, err
	}
	in, err := models.NormalizeCredentialInput(raw, c)
	if err != nil {
		return CredentialView{}, CredentialView{}, err
	}
	if in.SiteID != nil {
		var elsewhere int64
		if err := s.db.WithContext(ctx).Model(&models.Device{}).
			Where("credential_id = ? AND site_id <> ?", id, *in.SiteID).Count(&elsewhere).Error; err != nil {
			return CredentialView{}, CredentialView{}, fmt.Errorf("checking devices using the profile: %w", err)
		}
		if elsewhere > 0 {
			return CredentialView{}, CredentialView{}, ErrCredentialSiteMismatch
		}
	}
	if err := apply(c, in); err != nil {
		return CredentialView{}, CredentialView{}, err
	}
	err = s.db.WithContext(ctx).Model(&models.SNMPCredential{}).Where("id = ?", id).Updates(map[string]any{
		"name": c.Name, "site_id": c.SiteID, "version": c.Version, "community": c.Community,
		"username": c.Username, "auth_protocol": c.AuthProtocol, "auth_password": c.AuthPassword,
		"priv_protocol": c.PrivProtocol, "priv_password": c.PrivPassword, "updated_at": gorm.Expr("now()"),
	}).Error
	if err != nil {
		return CredentialView{}, CredentialView{}, mapCredentialWriteError(err)
	}
	after, err := s.view(ctx, id)
	return before, after, err
}

// Delete removes an unused profile.
func (s *SNMPCredentialService) Delete(ctx context.Context, id uuid.UUID) (CredentialView, error) {
	v, err := s.view(ctx, id)
	if err != nil {
		return CredentialView{}, err
	}
	if v.UsedBy > 0 {
		return CredentialView{}, ErrCredentialInUse
	}
	if err := s.db.WithContext(ctx).Delete(&models.SNMPCredential{}, "id = ?", id).Error; err != nil {
		if isForeignKeyViolation(err) { // a device was added in between
			return CredentialView{}, ErrCredentialInUse
		}
		return CredentialView{}, fmt.Errorf("deleting credential profile: %w", err)
	}
	return v, nil
}

// ForSite lists the profiles a site's devices may use: global ones and the
// site's own.
func (s *SNMPCredentialService) ForSite(ctx context.Context, siteID uuid.UUID) ([]CredentialOption, error) {
	var out []CredentialOption
	err := s.db.WithContext(ctx).Model(&models.SNMPCredential{}).
		Select("id, name, version, site_id").
		Where("site_id IS NULL OR site_id = ?", siteID).
		Order("lower(name)").Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("listing credential profiles for site: %w", err)
	}
	return out, nil
}

// UsableAt reports whether a profile may be used by a device in siteID.
func (s *SNMPCredentialService) UsableAt(ctx context.Context, credentialID, siteID uuid.UUID) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&models.SNMPCredential{}).
		Where("id = ? AND (site_id IS NULL OR site_id = ?)", credentialID, siteID).Count(&n).Error
	return n > 0, err
}

// Decrypted returns a profile's secrets in plaintext, for polling only.
func (s *SNMPCredentialService) Decrypted(ctx context.Context, id uuid.UUID) (snmp.Credential, error) {
	c, err := s.get(ctx, id)
	if err != nil {
		return snmp.Credential{}, err
	}
	out := snmp.Credential{Version: c.Version, Username: c.Username, AuthProtocol: c.AuthProtocol, PrivProtocol: c.PrivProtocol}
	for _, f := range []struct {
		dst *string
		src string
	}{{&out.Community, c.Community}, {&out.AuthPassword, c.AuthPassword}, {&out.PrivPassword, c.PrivPassword}} {
		if *f.dst, err = cryptutil.Decrypt(f.src); err != nil {
			return snmp.Credential{}, fmt.Errorf("decrypting credential profile %q: %w: %w", c.Name, ErrCredentialDecryptFailed, err)
		}
	}
	return out, nil
}
