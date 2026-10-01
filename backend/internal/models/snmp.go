package models

import (
	"errors"
	"fmt"
	"sort"
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

// Device types. device_type is the user's override; device_type_detected is
// what inventory concluded (portmon.DetectDeviceType).
const (
	DeviceTypeSwitch      = "switch"
	DeviceTypeRouter      = "router"
	DeviceTypeAccessPoint = "access_point"
	DeviceTypeNVR         = "nvr"
	DeviceTypeUPS         = "ups"
	DeviceTypeOther       = "other"
)

var ValidDeviceTypes = map[string]bool{
	DeviceTypeSwitch: true, DeviceTypeRouter: true, DeviceTypeAccessPoint: true, DeviceTypeNVR: true, DeviceTypeUPS: true, DeviceTypeOther: true,
}

// Port roles: what each port connects to. Set by the user (PortPatch.Role);
// inventory never writes it (SaveInventory's upsert omits the column).
const (
	PortRoleAccess = "access" // a device is plugged in (camera, PC, AP).
	PortRoleUplink = "uplink" // a link between the user's own network devices.
	PortRoleWAN    = "wan"    // the internet side.
)

var ValidPortRoles = map[string]bool{PortRoleAccess: true, PortRoleUplink: true, PortRoleWAN: true}

// Device is an SNMP-polled device. It belongs to exactly one site.
type Device struct {
	ID                  uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	SiteID              uuid.UUID   `json:"site_id" gorm:"column:site_id;type:uuid;not null"`
	CredentialID        uuid.UUID   `json:"credential_id" gorm:"column:credential_id;type:uuid;not null"`
	Name                string      `json:"name" gorm:"column:name;not null"`
	Host                string      `json:"host" gorm:"column:host;not null"`
	Port                int         `json:"port" gorm:"column:port;not null;default:161"`
	Enabled             bool        `json:"enabled" gorm:"column:enabled;not null"`
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
	// Overrides: when set, the UI shows these instead of what SNMP reported,
	// and inventory never writes them.
	VendorOverride   *string `json:"vendor_override" gorm:"column:vendor_override"`
	ModelOverride    *string `json:"model_override" gorm:"column:model_override"`
	LocationOverride *string `json:"location_override" gorm:"column:location_override"`
	// DeviceType is the user's override; DeviceTypeDetected is inventory's.
	DeviceType         *string `json:"device_type" gorm:"column:device_type"`
	DeviceTypeDetected string  `json:"device_type_detected" gorm:"column:device_type_detected;not null;default:other"`
	// Faceplate layout overrides (nil = automatic).
	FaceplateRows       *int       `json:"faceplate_rows" gorm:"column:faceplate_rows"`
	FaceplateSFPPorts   IntSlice   `json:"faceplate_sfp_ports" gorm:"column:faceplate_sfp_ports;type:jsonb"`
	LastStatsAt         *time.Time `json:"last_stats_at" gorm:"column:last_stats_at"`
	LastStatsDurationMs *int       `json:"last_stats_duration_ms" gorm:"column:last_stats_duration_ms"`
	CreatedBy           *uuid.UUID `json:"created_by" gorm:"column:created_by;type:uuid"`
	CreatedAt           time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt           time.Time  `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
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
	Present           bool      `json:"present" gorm:"column:present;not null"`
	ConnectorPresent  *bool     `json:"connector_present" gorm:"column:connector_present"`
	// Role is what this port connects to (access/uplink/wan); never written by
	// inventory, only by PortPatch.
	Role string `json:"role" gorm:"column:role;not null;default:access"`
	// NeighborDeviceID/NeighborIfIndex: the device (and optionally its port)
	// on the other end of a Network link port. Only PortService.UpdatePort
	// writes these; a non-null NeighborDeviceID implies Role is uplink.
	NeighborDeviceID *uuid.UUID `json:"neighbor_device_id" gorm:"column:neighbor_device_id;type:uuid"`
	NeighborIfIndex  *int       `json:"neighbor_if_index" gorm:"column:neighbor_if_index"`
	// StackUnit is this port's stack member (1, 2, ...) when the device is a
	// stack of more than one switch, 0 otherwise. Set by inventory only.
	StackUnit int `json:"stack_unit" gorm:"column:stack_unit;not null"`
	// HasIfX: the ifXTable has answered for this interface at least once, so
	// its name, alias and high speed are not overwritten by an inventory whose
	// ifXTable walk failed.
	HasIfX bool `json:"-" gorm:"column:has_ifx;not null"`
	// Collect is the user's choice (nil = CollectDefault, the classifier's).
	Collect              *bool        `json:"collect" gorm:"column:collect"`
	CollectDefault       bool         `json:"collect_default" gorm:"column:collect_default;not null"`
	Important            bool         `json:"important" gorm:"column:important;not null"`
	UtilThresholdPct     *int         `json:"util_threshold_pct" gorm:"column:util_threshold_pct"`
	ErrorThresholdPerMin *int         `json:"error_threshold_per_min" gorm:"column:error_threshold_per_min"`
	DownGraceSeconds     *int         `json:"down_grace_seconds" gorm:"column:down_grace_seconds"`
	UsualSpeedBps        *int64       `json:"usual_speed_bps" gorm:"column:usual_speed_bps"`
	Conditions           ConditionSet `json:"conditions" gorm:"column:conditions;type:jsonb;not null"`
	ConditionsSince      TimeMap      `json:"conditions_since" gorm:"column:conditions_since;type:jsonb;not null"`
	OperChangedAt        *time.Time   `json:"oper_changed_at" gorm:"column:oper_changed_at"`
	UpdatedAt            time.Time    `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (DeviceInterface) TableName() string { return "device_interfaces" }

// CollectEffective is whether the stats poll reads this interface.
func (i DeviceInterface) CollectEffective() bool {
	if i.Collect != nil {
		return *i.Collect
	}
	return i.CollectDefault
}

// Port conditions (portmon.Condition values), as stored in
// device_interfaces.conditions and incidents.condition.
const (
	PortConditionLinkDown  = "link_down"
	PortConditionErrors    = "errors"
	PortConditionFlapping  = "flapping"
	PortConditionSlowLink  = "slow_link"
	PortConditionSaturated = "saturated"
)

// Port event kinds.
const (
	PortEventLinkUp      = "link_up"
	PortEventLinkDown    = "link_down"
	PortEventFlapping    = "flapping"
	PortEventSpeedChange = "speed_change"
	PortEventErrors      = "errors"
	PortEventSaturated   = "saturated"
	PortEventSlowLink    = "slow_link"
	PortEventAdminUp     = "admin_up"
	PortEventAdminDown   = "admin_down"
)

// PortEvent is one row of the port event log. Span events (flapping, errors,
// saturated, slow_link) get EndedAt when they end.
type PortEvent struct {
	ID          int64      `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	DeviceID    uuid.UUID  `json:"device_id" gorm:"column:device_id;type:uuid;not null"`
	InterfaceID uuid.UUID  `json:"interface_id" gorm:"column:interface_id;type:uuid;not null"`
	IfIndex     int        `json:"if_index" gorm:"column:if_index;not null"`
	Kind        string     `json:"kind" gorm:"column:kind;not null"`
	StartedAt   time.Time  `json:"started_at" gorm:"column:started_at;not null"`
	EndedAt     *time.Time `json:"ended_at" gorm:"column:ended_at"`
	Detail      JSONMap    `json:"detail" gorm:"column:detail;type:jsonb;not null"`
}

func (PortEvent) TableName() string { return "port_events" }

// PortPatch is PATCH /devices/:id/ports/:ifIndex. Absent fields are left
// alone; null clears an override back to the default (collect: back to the
// classifier's choice; thresholds: back to the instance defaults).
type PortPatch struct {
	Important            Opt[bool]      `json:"important"`
	Collect              Opt[bool]      `json:"collect"`
	UtilThresholdPct     Opt[int]       `json:"util_threshold_pct"`
	ErrorThresholdPerMin Opt[int]       `json:"error_threshold_per_min"`
	DownGraceSeconds     Opt[int]       `json:"down_grace_seconds"`
	Role                 Opt[string]    `json:"role"`
	NeighborDeviceID     Opt[uuid.UUID] `json:"neighbor_device_id"`
	NeighborIfIndex      Opt[int]       `json:"neighbor_if_index"`
}

// Validate checks ranges (the same as the schema's CHECKs) and the
// combinations PortService.UpdatePort relies on not having to untangle:
// neighbor_if_index only makes sense with a neighbor device, and a port
// cannot be both an endpoint/WAN port and point at a neighbor in the same
// patch (UpdatePort itself clears the neighbor when the role alone changes).
func (p PortPatch) Validate() error {
	if !p.Important.Set && !p.Collect.Set && !p.UtilThresholdPct.Set && !p.ErrorThresholdPerMin.Set &&
		!p.DownGraceSeconds.Set && !p.Role.Set && !p.NeighborDeviceID.Set && !p.NeighborIfIndex.Set {
		return errors.New("nothing to change")
	}
	if p.Important.Set && p.Important.Value == nil {
		return errors.New("important must be true or false")
	}
	if p.Role.Set && (p.Role.Value == nil || !ValidPortRoles[*p.Role.Value]) {
		return errors.New("role must be access, uplink or wan")
	}
	if p.NeighborIfIndex.Set && p.NeighborIfIndex.Value != nil && *p.NeighborIfIndex.Value < 1 {
		return errors.New("neighbor port must be a positive port number")
	}
	if p.NeighborDeviceID.Set && p.NeighborDeviceID.Value == nil && p.NeighborIfIndex.Set && p.NeighborIfIndex.Value != nil {
		return errors.New("neighbor port cannot be given without a neighbor device")
	}
	if p.Role.Set && p.Role.Value != nil && (*p.Role.Value == PortRoleAccess || *p.Role.Value == PortRoleWAN) &&
		p.NeighborDeviceID.Set && p.NeighborDeviceID.Value != nil {
		return errors.New("an endpoint or internet (WAN) port cannot have a neighbor device")
	}
	inRange := func(o Opt[int], name string, min, max int) error {
		if o.Value != nil && (*o.Value < min || *o.Value > max) {
			return fmt.Errorf("%s must be between %d and %d", name, min, max)
		}
		return nil
	}
	if err := inRange(p.UtilThresholdPct, "the busy threshold", MinPortUtilThresholdPct, MaxPortUtilThresholdPct); err != nil {
		return err
	}
	if err := inRange(p.ErrorThresholdPerMin, "the error threshold", MinPortErrorThresholdPerMin, MaxPortErrorThresholdPerMin); err != nil {
		return err
	}
	return inRange(p.DownGraceSeconds, "the down grace period", MinPortDownGraceSeconds, MaxPortDownGraceSeconds)
}

// Updates is the column map for a validated patch.
func (p PortPatch) Updates() map[string]any {
	u := map[string]any{"updated_at": time.Now()}
	if p.Important.Set {
		u["important"] = *p.Important.Value
	}
	if p.Collect.Set {
		u["collect"] = optValue(p.Collect)
	}
	if p.UtilThresholdPct.Set {
		u["util_threshold_pct"] = optValue(p.UtilThresholdPct)
	}
	if p.ErrorThresholdPerMin.Set {
		u["error_threshold_per_min"] = optValue(p.ErrorThresholdPerMin)
	}
	if p.DownGraceSeconds.Set {
		u["down_grace_seconds"] = optValue(p.DownGraceSeconds)
	}
	if p.Role.Set {
		u["role"] = *p.Role.Value
	}
	if p.NeighborDeviceID.Set {
		u["neighbor_device_id"] = optValue(p.NeighborDeviceID)
	}
	if p.NeighborIfIndex.Set {
		u["neighbor_if_index"] = optValue(p.NeighborIfIndex)
	}
	return u
}

// optValue is the value to store for a set Opt: nil (SQL NULL) or the value.
func optValue[T any](o Opt[T]) any {
	if o.Value == nil {
		return nil
	}
	return *o.Value
}

// DeviceDetailsPatch is PATCH /devices/:id/details: the user's overrides. An
// empty string or null clears an override, so the SNMP value shows again.
type DeviceDetailsPatch struct {
	VendorOverride    Opt[string] `json:"vendor_override"`
	ModelOverride     Opt[string] `json:"model_override"`
	LocationOverride  Opt[string] `json:"location_override"`
	DeviceType        Opt[string] `json:"device_type"`
	FaceplateRows     Opt[int]    `json:"faceplate_rows"`
	FaceplateSFPPorts Opt[[]int]  `json:"faceplate_sfp_ports"`
}

// Updates validates the patch and returns its column map.
func (p DeviceDetailsPatch) Updates() (map[string]any, error) {
	u := map[string]any{}
	text := func(o Opt[string], col, label string) error {
		if !o.Set {
			return nil
		}
		if o.Value == nil || strings.TrimSpace(*o.Value) == "" {
			u[col] = nil
			return nil
		}
		v := strings.TrimSpace(*o.Value)
		if utf8.RuneCountInString(v) > 255 {
			return fmt.Errorf("%s must be 255 characters or fewer", label)
		}
		u[col] = v
		return nil
	}
	for _, f := range []struct {
		o          Opt[string]
		col, label string
	}{{p.VendorOverride, "vendor_override", "vendor"}, {p.ModelOverride, "model_override", "model"},
		{p.LocationOverride, "location_override", "location"}} {
		if err := text(f.o, f.col, f.label); err != nil {
			return nil, err
		}
	}
	if p.DeviceType.Set {
		switch {
		case p.DeviceType.Value == nil || *p.DeviceType.Value == "":
			u["device_type"] = nil
		case ValidDeviceTypes[*p.DeviceType.Value]:
			u["device_type"] = *p.DeviceType.Value
		default:
			return nil, fmt.Errorf("device type must be switch, router, access_point, nvr, ups or other")
		}
	}
	if p.FaceplateRows.Set {
		if v := p.FaceplateRows.Value; v != nil && *v != 1 && *v != 2 {
			return nil, errors.New("faceplate rows must be 1 or 2")
		}
		u["faceplate_rows"] = optValue(p.FaceplateRows)
	}
	if p.FaceplateSFPPorts.Set {
		if p.FaceplateSFPPorts.Value == nil || len(*p.FaceplateSFPPorts.Value) == 0 {
			u["faceplate_sfp_ports"] = nil
		} else {
			seen := map[int]bool{}
			var ports IntSlice
			for _, n := range *p.FaceplateSFPPorts.Value {
				if n < 1 || n > 9999 {
					return nil, errors.New("SFP port numbers must be between 1 and 9999")
				}
				if !seen[n] {
					seen[n] = true
					ports = append(ports, n)
				}
			}
			if len(ports) > 64 {
				return nil, errors.New("at most 64 SFP ports")
			}
			sort.Ints(ports)
			u["faceplate_sfp_ports"] = ports
		}
	}
	if len(u) == 0 {
		return nil, errors.New("nothing to change")
	}
	return u, nil
}
