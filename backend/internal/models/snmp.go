package models

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// SNMP versions a credential profile can use.
const (
	SNMPVersion1  = "1"
	SNMPVersion2c = "2c"
	SNMPVersion3  = "3"

	SNMPProtoNone = "none"
)

// ValidAuthProtocols and ValidPrivProtocols are the v3 protocols Sentinel
// supports, named as the schema's CHECK constraints name them.
var (
	ValidAuthProtocols = map[string]bool{
		SNMPProtoNone: true, "MD5": true, "SHA": true, "SHA224": true, "SHA256": true, "SHA384": true, "SHA512": true,
	}
	ValidPrivProtocols = map[string]bool{
		SNMPProtoNone: true, "DES": true, "AES": true, "AES192": true, "AES256": true,
	}
)

// minV3PasswordLen is RFC 3414's minimum for USM passphrases; shorter ones are
// rejected by most agents with an unhelpful error.
const minV3PasswordLen = 8

// SNMPCredential is a credential profile. Community and the v3 passwords hold
// ciphertext (cryptutil) and never leave the server: json:"-".
type SNMPCredential struct {
	ID           uuid.UUID  `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name         string     `json:"name" gorm:"column:name;not null"`
	SiteID       *uuid.UUID `json:"site_id" gorm:"column:site_id;type:uuid"`
	Version      string     `json:"version" gorm:"column:version;not null"`
	Community    string     `json:"-" gorm:"column:community"`
	Username     string     `json:"username" gorm:"column:username"`
	AuthProtocol string     `json:"auth_protocol" gorm:"column:auth_protocol;not null;default:none"`
	AuthPassword string     `json:"-" gorm:"column:auth_password"`
	PrivProtocol string     `json:"priv_protocol" gorm:"column:priv_protocol;not null;default:none"`
	PrivPassword string     `json:"-" gorm:"column:priv_password"`
	CreatedBy    *uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid"`
	CreatedAt    time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (SNMPCredential) TableName() string { return "snmp_credentials" }

// CredentialInput is a create or update request. Secrets are plaintext here;
// nil or "" means "keep what is stored" on update.
type CredentialInput struct {
	Name         string     `json:"name"`
	SiteID       *uuid.UUID `json:"site_id"`
	Version      string     `json:"version"`
	Community    *string    `json:"community"`
	Username     string     `json:"username"`
	AuthProtocol string     `json:"auth_protocol"`
	AuthPassword *string    `json:"auth_password"`
	PrivProtocol string     `json:"priv_protocol"`
	PrivPassword *string    `json:"priv_password"`
}

func blankSecret(s *string) bool { return s == nil || *s == "" }

// NormalizeCredentialInput validates a profile against its version and clears
// every field that does not belong to that version. existing is the stored
// profile on update (nil on create): a blank secret is valid when existing
// already holds one, and is returned as nil ("keep").
func NormalizeCredentialInput(in CredentialInput, existing *SNMPCredential) (CredentialInput, error) {
	out := CredentialInput{
		Name:         strings.TrimSpace(in.Name),
		SiteID:       in.SiteID,
		Version:      strings.TrimSpace(in.Version),
		AuthProtocol: SNMPProtoNone,
		PrivProtocol: SNMPProtoNone,
	}
	if out.Name == "" {
		return CredentialInput{}, errors.New("name is required")
	}
	if utf8.RuneCountInString(out.Name) > 255 {
		return CredentialInput{}, errors.New("name must be 255 characters or fewer")
	}
	has := func(stored string) bool { return existing != nil && stored != "" }

	switch out.Version {
	case SNMPVersion1, SNMPVersion2c:
		if blankSecret(in.Community) {
			if !has(existingField(existing, "community")) {
				return CredentialInput{}, errors.New("community is required for SNMP v1 and v2c")
			}
		} else {
			out.Community = in.Community
		}
	case SNMPVersion3:
		out.Username = strings.TrimSpace(in.Username)
		if out.Username == "" {
			return CredentialInput{}, errors.New("username is required for SNMP v3")
		}
		out.AuthProtocol = strings.TrimSpace(in.AuthProtocol)
		if out.AuthProtocol == "" {
			out.AuthProtocol = SNMPProtoNone
		}
		out.PrivProtocol = strings.TrimSpace(in.PrivProtocol)
		if out.PrivProtocol == "" {
			out.PrivProtocol = SNMPProtoNone
		}
		if !ValidAuthProtocols[out.AuthProtocol] {
			return CredentialInput{}, fmt.Errorf("unknown auth protocol %q", out.AuthProtocol)
		}
		if !ValidPrivProtocols[out.PrivProtocol] {
			return CredentialInput{}, fmt.Errorf("unknown privacy protocol %q", out.PrivProtocol)
		}
		if out.PrivProtocol != SNMPProtoNone && out.AuthProtocol == SNMPProtoNone {
			return CredentialInput{}, errors.New("SNMP v3 cannot use privacy without authentication")
		}
		if out.AuthProtocol != SNMPProtoNone {
			if blankSecret(in.AuthPassword) {
				if !has(existingField(existing, "auth")) {
					return CredentialInput{}, errors.New("an auth password is required for the chosen auth protocol")
				}
			} else if utf8.RuneCountInString(*in.AuthPassword) < minV3PasswordLen {
				return CredentialInput{}, errors.New("the auth password must be at least 8 characters")
			} else {
				out.AuthPassword = in.AuthPassword
			}
		}
		if out.PrivProtocol != SNMPProtoNone {
			if blankSecret(in.PrivPassword) {
				if !has(existingField(existing, "priv")) {
					return CredentialInput{}, errors.New("a privacy password is required for the chosen privacy protocol")
				}
			} else if utf8.RuneCountInString(*in.PrivPassword) < minV3PasswordLen {
				return CredentialInput{}, errors.New("the privacy password must be at least 8 characters")
			} else {
				out.PrivPassword = in.PrivPassword
			}
		}
	default:
		return CredentialInput{}, fmt.Errorf("version must be 1, 2c or 3, not %q", out.Version)
	}
	return out, nil
}

func existingField(c *SNMPCredential, which string) string {
	if c == nil {
		return ""
	}
	switch which {
	case "community":
		return c.Community
	case "auth":
		return c.AuthPassword
	default:
		return c.PrivPassword
	}
}

// Device statuses.
const (
	DeviceStatusPending = "pending"
	DeviceStatusUp      = "up"
	DeviceStatusDown    = "down"
	DeviceStatusPaused  = "paused"
	DeviceStatusError   = "error"
)

// Device is an SNMP-polled device. It belongs to exactly one site.
type Device struct {
	ID                  uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID              uuid.UUID   `json:"site_id" gorm:"column:site_id;type:uuid;not null"`
	CredentialID        uuid.UUID   `json:"credential_id" gorm:"column:credential_id;type:uuid;not null"`
	Name                string      `json:"name" gorm:"column:name;not null"`
	Host                string      `json:"host" gorm:"column:host;not null"`
	Port                int         `json:"port" gorm:"column:port;not null;default:161"`
	Enabled             bool        `json:"enabled" gorm:"column:enabled;not null;default:true"`
	PollInterval        int         `json:"poll_interval" gorm:"column:poll_interval;not null;default:60"`
	TimeoutMs           int         `json:"timeout_ms" gorm:"column:timeout_ms;not null;default:3000"`
	Retries             int         `json:"retries" gorm:"column:retries;not null;default:1"`
	NotifyChannels      StringSlice `json:"notify_channels" gorm:"column:notify_channels;type:jsonb"`
	Status              string      `json:"status" gorm:"column:status;not null;default:pending"`
	StatusDetail        string      `json:"status_detail" gorm:"column:status_detail"`
	ConsecutiveFailures int         `json:"consecutive_failures" gorm:"column:consecutive_failures;not null;default:0"`
	LastPolledAt        *time.Time  `json:"last_polled_at" gorm:"column:last_polled_at"`
	LastSeenAt          *time.Time  `json:"last_seen_at" gorm:"column:last_seen_at"`
	LastInventoryAt     *time.Time  `json:"last_inventory_at" gorm:"column:last_inventory_at"`
	SysName             string      `json:"sys_name" gorm:"column:sys_name"`
	SysDescr            string      `json:"sys_descr" gorm:"column:sys_descr"`
	SysObjectID         string      `json:"sys_object_id" gorm:"column:sys_object_id"`
	SysLocation         string      `json:"sys_location" gorm:"column:sys_location"`
	SysContact          string      `json:"sys_contact" gorm:"column:sys_contact"`
	SysUptimeSeconds    *int64      `json:"sys_uptime_seconds" gorm:"column:sys_uptime_seconds"`
	Vendor              string      `json:"vendor" gorm:"column:vendor"`
	Model               string      `json:"model" gorm:"column:model"`
	Serial              string      `json:"serial" gorm:"column:serial"`
	CreatedBy           *uuid.UUID  `json:"created_by" gorm:"column:created_by;type:uuid"`
	CreatedAt           time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt           time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (Device) TableName() string { return "devices" }

// DeviceInput is a create or update request.
type DeviceInput struct {
	SiteID         uuid.UUID   `json:"site_id"`
	CredentialID   uuid.UUID   `json:"credential_id"`
	Name           string      `json:"name"`
	Host           string      `json:"host"`
	Port           int         `json:"port"`
	Enabled        *bool       `json:"enabled"`
	PollInterval   int         `json:"poll_interval"`
	TimeoutMs      int         `json:"timeout_ms"`
	Retries        int         `json:"retries"`
	NotifyChannels StringSlice `json:"notify_channels"`
}

// NormalizeDeviceInput trims, applies defaults for zero values and checks
// ranges (the same ranges as the schema's CHECK constraints). A zero value
// means "default"; a negative or out-of-range one is an error.
func NormalizeDeviceInput(in DeviceInput) (DeviceInput, error) {
	out := in
	out.Name = strings.TrimSpace(in.Name)
	out.Host = strings.TrimSpace(in.Host)
	if out.Host == "" {
		return DeviceInput{}, errors.New("host is required")
	}
	if strings.ContainsAny(out.Host, " \t/") || utf8.RuneCountInString(out.Host) > 255 {
		return DeviceInput{}, errors.New("host must be an IP address or a DNS name")
	}
	if utf8.RuneCountInString(out.Name) > 255 {
		return DeviceInput{}, errors.New("name must be 255 characters or fewer")
	}
	def := func(v *int, d int) {
		if *v == 0 {
			*v = d
		}
	}
	def(&out.Port, 161)
	def(&out.PollInterval, 60)
	def(&out.TimeoutMs, 3000)
	if out.Retries == 0 && in.Retries == 0 {
		out.Retries = 1
	}
	if out.Enabled == nil {
		t := true
		out.Enabled = &t
	}
	switch {
	case out.Port < 1 || out.Port > 65535:
		return DeviceInput{}, errors.New("port must be between 1 and 65535")
	case out.PollInterval < 10 || out.PollInterval > 3600:
		return DeviceInput{}, errors.New("poll interval must be between 10 and 3600 seconds")
	case out.TimeoutMs < 200 || out.TimeoutMs > 30000:
		return DeviceInput{}, errors.New("timeout must be between 200 and 30000 ms")
	case out.Retries < 0 || out.Retries > 5:
		return DeviceInput{}, errors.New("retries must be between 0 and 5")
	}
	return out, nil
}

// DeviceInterface is one row of a device's interface table.
type DeviceInterface struct {
	ID                uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	DeviceID          uuid.UUID `json:"device_id" gorm:"column:device_id;type:uuid;not null"`
	IfIndex           int       `json:"if_index" gorm:"column:if_index;not null"`
	Name              string    `json:"name" gorm:"column:name"`
	Descr             string    `json:"descr" gorm:"column:descr"`
	Alias             string    `json:"alias" gorm:"column:alias"`
	IfType            int       `json:"if_type" gorm:"column:if_type"`
	SpeedBps          int64     `json:"speed_bps" gorm:"column:speed_bps"`
	MAC               string    `json:"mac" gorm:"column:mac"`
	AdminStatus       string    `json:"admin_status" gorm:"column:admin_status"`
	OperStatus        string    `json:"oper_status" gorm:"column:oper_status"`
	LastChangeSeconds int64     `json:"last_change_seconds" gorm:"column:last_change_seconds"`
	Present           bool      `json:"present" gorm:"column:present;not null;default:true"`
	UpdatedAt         time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (DeviceInterface) TableName() string { return "device_interfaces" }
