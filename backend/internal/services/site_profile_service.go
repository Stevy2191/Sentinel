package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// Site profile errors. A network or circuit of another site is not found, the
// same as one that does not exist.
var (
	ErrSiteNetworkNotFound  = errors.New("network not found")
	ErrSiteCircuitNotFound  = errors.New("circuit not found")
	ErrCircuitPortNotAtSite = errors.New("that port is not on a device at this site")
)

// SubnetTakenError is a subnet the site already records.
type SubnetTakenError struct{ Site, CIDR string }

func (e *SubnetTakenError) Error() string { return fmt.Sprintf("%s already has %s", e.Site, e.CIDR) }

// SiteProfileService keeps each site's networks, circuits and notes.
type SiteProfileService struct {
	db    *gorm.DB
	ports *PortService
}

// NewSiteProfileService builds the service; ports reads circuits' ports live.
func NewSiteProfileService(db *gorm.DB, ports *PortService) *SiteProfileService {
	return &SiteProfileService{db: db, ports: ports}
}

// SiteCircuitView is a circuit plus the live state of the port it is tied
// to: nil when it is not tied to one, or that port's device is no longer at
// the site.
type SiteCircuitView struct {
	models.SiteCircuit
	Port *LivePort `json:"port"`
}

// SiteProfile is what the site page shows about the place itself.
type SiteProfile struct {
	Notes    *string              `json:"notes"`
	Networks []models.SiteNetwork `json:"networks"`
	Circuits []SiteCircuitView    `json:"circuits"`
}

// siteNotes reads a site's notes, or ErrSiteNotFound.
func (s *SiteProfileService) siteNotes(ctx context.Context, siteID uuid.UUID) (*string, error) {
	var row struct{ Notes *string }
	res := s.db.WithContext(ctx).Table("sites").Select("notes").Where("id = ?", siteID).Scan(&row)
	if res.Error != nil {
		return nil, fmt.Errorf("loading site notes: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, ErrSiteNotFound
	}
	return row.Notes, nil
}

// Profile loads a site's notes, networks (by subnet, IPv4 first) and
// circuits (by provider, then age), each circuit's port read live.
func (s *SiteProfileService) Profile(ctx context.Context, siteID uuid.UUID) (*SiteProfile, error) {
	notes, err := s.siteNotes(ctx, siteID)
	if err != nil {
		return nil, err
	}
	out := &SiteProfile{Notes: notes, Networks: []models.SiteNetwork{}, Circuits: []SiteCircuitView{}}
	if err := s.db.WithContext(ctx).Where("site_id = ?", siteID).Order("family(cidr), cidr").Find(&out.Networks).Error; err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}
	if out.Networks == nil {
		out.Networks = []models.SiteNetwork{}
	}
	var circuits []models.SiteCircuit
	if err := s.db.WithContext(ctx).Where("site_id = ?", siteID).Order("lower(provider), created_at").Find(&circuits).Error; err != nil {
		return nil, fmt.Errorf("listing circuits: %w", err)
	}
	var ids []uuid.UUID
	for _, c := range circuits {
		if c.InterfaceID != nil {
			ids = append(ids, *c.InterfaceID)
		}
	}
	live, err := s.ports.LivePorts(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, c := range circuits {
		v := SiteCircuitView{SiteCircuit: c}
		if c.InterfaceID != nil {
			if p, ok := live[*c.InterfaceID]; ok && p.SiteID == siteID {
				v.Port = &p
			}
			// A dropped port drops its stale id too, so a client's GET, modify,
			// PUT does not send back a port that is no longer at this site.
			if v.Port == nil {
				v.InterfaceID = nil
			}
		}
		out.Circuits = append(out.Circuits, v)
	}
	return out, nil
}

// SetNotes replaces a site's notes (nil clears them) and returns them before
// and after, for the audit log.
func (s *SiteProfileService) SetNotes(ctx context.Context, siteID uuid.UUID, notes *string) (*string, *string, error) {
	before, err := s.siteNotes(ctx, siteID)
	if err != nil {
		return nil, nil, err
	}
	if err := s.db.WithContext(ctx).Table("sites").Where("id = ?", siteID).
		Updates(map[string]any{"notes": notes, "updated_at": time.Now().UTC()}).Error; err != nil {
		return nil, nil, fmt.Errorf("saving site notes: %w", err)
	}
	return before, notes, nil
}

// network loads one of a site's networks.
func (s *SiteProfileService) network(ctx context.Context, siteID, id uuid.UUID) (*models.SiteNetwork, error) {
	var n models.SiteNetwork
	err := s.db.WithContext(ctx).Where("id = ? AND site_id = ?", id, siteID).First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSiteNetworkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading network: %w", err)
	}
	return &n, nil
}

// checkSubnetFree refuses a subnet the site already records under another
// network than except (the one being edited, or uuid.Nil).
func (s *SiteProfileService) checkSubnetFree(ctx context.Context, siteID uuid.UUID, cidr string, except uuid.UUID) error {
	var n int64
	if err := s.db.WithContext(ctx).Model(&models.SiteNetwork{}).
		Where("site_id = ? AND cidr = ? AND id <> ?", siteID, cidr, except).Count(&n).Error; err != nil {
		return fmt.Errorf("checking subnet: %w", err)
	}
	if n == 0 {
		return nil
	}
	var site struct{ Name string }
	if err := s.db.WithContext(ctx).Table("sites").Select("name").Where("id = ?", siteID).Scan(&site).Error; err != nil {
		return fmt.Errorf("loading site name: %w", err)
	}
	return &SubnetTakenError{Site: site.Name, CIDR: cidr}
}

// CreateNetwork records a network; in must already be normalized.
func (s *SiteProfileService) CreateNetwork(ctx context.Context, siteID uuid.UUID, in models.SiteNetworkInput) (*models.SiteNetwork, error) {
	if err := s.checkSubnetFree(ctx, siteID, in.CIDR, uuid.Nil); err != nil {
		return nil, err
	}
	n := models.SiteNetwork{SiteID: siteID, Name: in.Name, CIDR: in.CIDR, VLAN: in.VLAN, Gateway: in.Gateway, Note: in.Note}
	if err := s.db.WithContext(ctx).Create(&n).Error; err != nil {
		return nil, fmt.Errorf("saving network: %w", err)
	}
	return s.network(ctx, siteID, n.ID)
}

// UpdateNetwork replaces a network's fields; in must already be normalized.
func (s *SiteProfileService) UpdateNetwork(ctx context.Context, siteID, id uuid.UUID, in models.SiteNetworkInput) (*models.SiteNetwork, *models.SiteNetwork, error) {
	before, err := s.network(ctx, siteID, id)
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkSubnetFree(ctx, siteID, in.CIDR, id); err != nil {
		return nil, nil, err
	}
	if err := s.db.WithContext(ctx).Model(&models.SiteNetwork{}).Where("id = ?", id).Updates(map[string]any{
		"name": in.Name, "cidr": in.CIDR, "vlan": in.VLAN, "gateway": in.Gateway, "note": in.Note, "updated_at": time.Now().UTC(),
	}).Error; err != nil {
		return nil, nil, fmt.Errorf("updating network: %w", err)
	}
	after, err := s.network(ctx, siteID, id)
	return before, after, err
}

// DeleteNetwork removes a network and returns what it was.
func (s *SiteProfileService) DeleteNetwork(ctx context.Context, siteID, id uuid.UUID) (*models.SiteNetwork, error) {
	n, err := s.network(ctx, siteID, id)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Delete(&models.SiteNetwork{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting network: %w", err)
	}
	return n, nil
}

// circuit loads one of a site's circuits.
func (s *SiteProfileService) circuit(ctx context.Context, siteID, id uuid.UUID) (*models.SiteCircuit, error) {
	var c models.SiteCircuit
	err := s.db.WithContext(ctx).Where("id = ? AND site_id = ?", id, siteID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSiteCircuitNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading circuit: %w", err)
	}
	return &c, nil
}

// checkPortAtSite refuses a port that is not on a device at this site.
func (s *SiteProfileService) checkPortAtSite(ctx context.Context, siteID uuid.UUID, interfaceID *uuid.UUID) error {
	if interfaceID == nil {
		return nil
	}
	var n int64
	if err := s.db.WithContext(ctx).Table("device_interfaces AS di").Joins("JOIN devices AS d ON d.id = di.device_id").
		Where("di.id = ? AND d.site_id = ?", *interfaceID, siteID).Count(&n).Error; err != nil {
		return fmt.Errorf("checking port: %w", err)
	}
	if n == 0 {
		return ErrCircuitPortNotAtSite
	}
	return nil
}

// CreateCircuit records a circuit; in must already be normalized.
func (s *SiteProfileService) CreateCircuit(ctx context.Context, siteID uuid.UUID, in models.SiteCircuitInput) (*models.SiteCircuit, error) {
	if err := s.checkPortAtSite(ctx, siteID, in.InterfaceID); err != nil {
		return nil, err
	}
	c := models.SiteCircuit{SiteID: siteID, Provider: in.Provider, CircuitRef: in.CircuitRef, Kind: in.Kind,
		DownloadMbps: in.DownloadMbps, UploadMbps: in.UploadMbps, SupportPhone: in.SupportPhone,
		AccountNumber: in.AccountNumber, Notes: in.Notes, InterfaceID: in.InterfaceID}
	if err := s.db.WithContext(ctx).Create(&c).Error; err != nil {
		return nil, fmt.Errorf("saving circuit: %w", err)
	}
	return s.circuit(ctx, siteID, c.ID)
}

// UpdateCircuit replaces a circuit's fields; in must already be normalized.
func (s *SiteProfileService) UpdateCircuit(ctx context.Context, siteID, id uuid.UUID, in models.SiteCircuitInput) (*models.SiteCircuit, *models.SiteCircuit, error) {
	before, err := s.circuit(ctx, siteID, id)
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkPortAtSite(ctx, siteID, in.InterfaceID); err != nil {
		return nil, nil, err
	}
	if err := s.db.WithContext(ctx).Model(&models.SiteCircuit{}).Where("id = ?", id).Updates(map[string]any{
		"provider": in.Provider, "circuit_ref": in.CircuitRef, "kind": in.Kind,
		"download_mbps": in.DownloadMbps, "upload_mbps": in.UploadMbps, "support_phone": in.SupportPhone,
		"account_number": in.AccountNumber, "notes": in.Notes, "interface_id": in.InterfaceID,
		"updated_at": time.Now().UTC(),
	}).Error; err != nil {
		return nil, nil, fmt.Errorf("updating circuit: %w", err)
	}
	after, err := s.circuit(ctx, siteID, id)
	return before, after, err
}

// DeleteCircuit removes a circuit and returns what it was.
func (s *SiteProfileService) DeleteCircuit(ctx context.Context, siteID, id uuid.UUID) (*models.SiteCircuit, error) {
	c, err := s.circuit(ctx, siteID, id)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Delete(&models.SiteCircuit{}, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("deleting circuit: %w", err)
	}
	return c, nil
}
