package models

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits on a site profile's fields; the migration's column sizes match.
const (
	maxNetworkNameLen   = 100
	maxNetworkNoteLen   = 500
	maxProviderLen      = 100
	maxCircuitRefLen    = 100
	maxAccountNumberLen = 100
	maxSupportPhoneLen  = 50
	maxCircuitNotesLen  = 1000
	maxSiteNotesLen     = 10000
	maxCircuitMbps      = 100000
)

// SiteNetwork is one subnet at a site: what it is for, its VLAN and gateway.
type SiteNetwork struct {
	ID        uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID    uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid;not null"`
	Name      string    `json:"name" gorm:"column:name;not null"`
	CIDR      string    `json:"cidr" gorm:"column:cidr;type:cidr;not null"`
	VLAN      *int      `json:"vlan" gorm:"column:vlan"`
	Gateway   *string   `json:"gateway" gorm:"column:gateway;type:inet"`
	Note      *string   `json:"note" gorm:"column:note"`
	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table.
func (SiteNetwork) TableName() string { return "site_networks" }

// SiteNetworkInput is a network as a client sends it.
type SiteNetworkInput struct {
	Name    string  `json:"name"`
	CIDR    string  `json:"cidr"`
	VLAN    *int    `json:"vlan"`
	Gateway *string `json:"gateway"`
	Note    *string `json:"note"`
}

// NormalizeSiteNetworkInput trims and checks a network. The subnet is stored
// as its network address, so 10.20.0.5/24 becomes 10.20.0.0/24 and the two
// spellings collide as the duplicate they are; a gateway must be an address
// inside the subnet.
func NormalizeSiteNetworkInput(in SiteNetworkInput) (SiteNetworkInput, error) {
	out := SiteNetworkInput{Name: strings.TrimSpace(in.Name), VLAN: in.VLAN}
	if out.Name == "" {
		return SiteNetworkInput{}, errors.New("name is required")
	}
	if utf8.RuneCountInString(out.Name) > maxNetworkNameLen {
		return SiteNetworkInput{}, fmt.Errorf("name must be %d characters or fewer", maxNetworkNameLen)
	}
	raw := strings.TrimSpace(in.CIDR)
	if raw == "" {
		return SiteNetworkInput{}, errors.New("subnet is required")
	}
	_, subnet, err := net.ParseCIDR(raw)
	if err != nil {
		return SiteNetworkInput{}, fmt.Errorf("subnet %q is not a network like 10.20.0.0/24", raw)
	}
	out.CIDR = subnet.String()
	if out.VLAN != nil && (*out.VLAN < 1 || *out.VLAN > 4094) {
		return SiteNetworkInput{}, errors.New("VLAN must be between 1 and 4094")
	}
	if gw := trimOptional(in.Gateway); gw != nil {
		ip := net.ParseIP(*gw)
		if ip == nil {
			return SiteNetworkInput{}, fmt.Errorf("gateway %q is not an IP address", *gw)
		}
		if !subnet.Contains(ip) {
			return SiteNetworkInput{}, fmt.Errorf("%s is outside %s", ip, out.CIDR)
		}
		s := ip.String()
		out.Gateway = &s
	}
	if out.Note, err = optionalText(in.Note, maxNetworkNoteLen, "note"); err != nil {
		return SiteNetworkInput{}, err
	}
	return out, nil
}

// CircuitKinds are the kinds of circuit a site can record.
var CircuitKinds = map[string]bool{
	"fiber": true, "cable": true, "dsl": true, "fixed_wireless": true, "cellular": true, "copper": true, "other": true,
}

// SiteCircuit is one ISP circuit at a site, optionally tied to the device
// port it plugs into.
type SiteCircuit struct {
	ID            uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID        uuid.UUID  `json:"site_id" gorm:"column:site_id;type:uuid;not null"`
	Provider      string     `json:"provider" gorm:"column:provider;not null"`
	CircuitRef    *string    `json:"circuit_ref" gorm:"column:circuit_ref"`
	Kind          string     `json:"kind" gorm:"column:kind;not null"`
	DownloadMbps  *float64   `json:"download_mbps" gorm:"column:download_mbps"`
	UploadMbps    *float64   `json:"upload_mbps" gorm:"column:upload_mbps"`
	SupportPhone  *string    `json:"support_phone" gorm:"column:support_phone"`
	AccountNumber *string    `json:"account_number" gorm:"column:account_number"`
	Notes         *string    `json:"notes" gorm:"column:notes"`
	InterfaceID   *uuid.UUID `json:"interface_id" gorm:"column:interface_id;type:uuid"`
	CreatedAt     time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName pins the table.
func (SiteCircuit) TableName() string { return "site_circuits" }

// SiteCircuitInput is a circuit as a client sends it.
type SiteCircuitInput struct {
	Provider      string     `json:"provider"`
	CircuitRef    *string    `json:"circuit_ref"`
	Kind          string     `json:"kind"`
	DownloadMbps  *float64   `json:"download_mbps"`
	UploadMbps    *float64   `json:"upload_mbps"`
	SupportPhone  *string    `json:"support_phone"`
	AccountNumber *string    `json:"account_number"`
	Notes         *string    `json:"notes"`
	InterfaceID   *uuid.UUID `json:"interface_id"`
}

// NormalizeSiteCircuitInput trims and checks a circuit. A blank type means
// "other". Whether the port is at the site is checked by the service, which
// can see the database.
func NormalizeSiteCircuitInput(in SiteCircuitInput) (SiteCircuitInput, error) {
	out := SiteCircuitInput{Provider: strings.TrimSpace(in.Provider), InterfaceID: in.InterfaceID,
		DownloadMbps: in.DownloadMbps, UploadMbps: in.UploadMbps}
	if out.Provider == "" {
		return SiteCircuitInput{}, errors.New("provider is required")
	}
	if utf8.RuneCountInString(out.Provider) > maxProviderLen {
		return SiteCircuitInput{}, fmt.Errorf("provider must be %d characters or fewer", maxProviderLen)
	}
	out.Kind = strings.ToLower(strings.TrimSpace(in.Kind))
	if out.Kind == "" {
		out.Kind = "other"
	}
	if !CircuitKinds[out.Kind] {
		return SiteCircuitInput{}, errors.New("type must be one of fiber, cable, dsl, fixed_wireless, cellular, copper, other")
	}
	for _, sp := range []struct {
		label string
		v     *float64
	}{{"download", out.DownloadMbps}, {"upload", out.UploadMbps}} {
		if sp.v != nil && !(*sp.v > 0 && *sp.v <= maxCircuitMbps) {
			return SiteCircuitInput{}, fmt.Errorf("%s speed must be more than 0 and at most %d Mbps", sp.label, maxCircuitMbps)
		}
	}
	var err error
	if out.CircuitRef, err = optionalText(in.CircuitRef, maxCircuitRefLen, "circuit ID"); err != nil {
		return SiteCircuitInput{}, err
	}
	if out.SupportPhone, err = optionalText(in.SupportPhone, maxSupportPhoneLen, "support phone"); err != nil {
		return SiteCircuitInput{}, err
	}
	if out.AccountNumber, err = optionalText(in.AccountNumber, maxAccountNumberLen, "account number"); err != nil {
		return SiteCircuitInput{}, err
	}
	if out.Notes, err = optionalText(in.Notes, maxCircuitNotesLen, "notes"); err != nil {
		return SiteCircuitInput{}, err
	}
	return out, nil
}

// NormalizeSiteNotes trims a site's notes; blank clears them. Line breaks
// inside are kept.
func NormalizeSiteNotes(s string) (*string, error) {
	return optionalText(&s, maxSiteNotesLen, "notes")
}

// optionalText trims an optional field (blank becomes nil) and checks its
// length; label names the field in the error.
func optionalText(s *string, max int, label string) (*string, error) {
	t := trimOptional(s)
	if t != nil && utf8.RuneCountInString(*t) > max {
		return nil, fmt.Errorf("%s must be %d characters or fewer", label, max)
	}
	return t, nil
}
