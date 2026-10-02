// This file defines the MIB library, metric profile and custom metric
// entities (network monitoring phase 3): the parsed MIB modules and objects
// used to look up OIDs by name, the profiles of custom metrics a device can
// be polled for, and the per-device attach/detach overrides and poll-run
// bookkeeping that go with them.
package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MIB module sources.
const (
	MIBSourceBuiltin = "builtin"
	MIBSourceUpload  = "upload"
)

// MIB module statuses: "waiting" means an IMPORTS dependency is not yet
// loaded, so some of its objects have no resolved OID.
const (
	MIBStatusReady   = "ready"
	MIBStatusWaiting = "waiting"
)

// Custom metric sources: where a profile_metric's value comes from.
const (
	MetricSourceScalar      = "scalar"
	MetricSourceColumn      = "column"
	MetricSourceUsedFreePct = "used_free_pct"
)

// Custom metric kinds: how a profile_metric's value behaves over time.
const (
	MetricKindGauge   = "gauge"
	MetricKindCounter = "counter"
	MetricKindStatus  = "status"
)

// Label modes: how a profile_metric's row gets its human label.
const (
	LabelModeIndex     = "index"
	LabelModeColumn    = "column"
	LabelModeSameIndex = "same_index"
	LabelModePointer   = "pointer"
)

// Rule kinds: how a profile_metric's rule_value is compared, or "" when the
// metric has no rule. RuleEnabled still has to be true for the rule to fire.
const (
	RuleKindNone  = ""
	RuleKindAbove = "above"
	RuleKindBelow = "below"
	RuleKindNotOK = "not_ok"
)

// StringArray is a slice of strings persisted as a Postgres text[] column
// (not JSONB: the schema's array operators and defaults assume a native
// array). It implements driver.Valuer and sql.Scanner directly against the
// Postgres array literal format, since the project has no lib/pq dependency
// to borrow pq.StringArray from.
type StringArray []string

// Value renders the array in Postgres's text[] literal format. Every column
// that uses StringArray is NOT NULL DEFAULT '{}', so a nil or empty slice is
// rendered as '{}' rather than SQL NULL.
func (a StringArray) Value() (driver.Value, error) {
	return encodePGTextArray([]string(a)), nil
}

// Scan parses a Postgres text[] value back into the slice.
func (a *StringArray) Scan(value any) error {
	if value == nil {
		*a = StringArray{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning StringArray: %w", err)
	}
	elems, err := parsePGArray(string(data))
	if err != nil {
		return fmt.Errorf("scanning StringArray: %w", err)
	}
	out := make(StringArray, len(elems))
	copy(out, elems)
	*a = out
	return nil
}

// Int64Array is a slice of int64 persisted as a Postgres bigint[] column, the
// same way StringArray is a text[].
type Int64Array []int64

// Value renders the array in Postgres's bigint[] literal format. Every column
// that uses Int64Array is NOT NULL DEFAULT '{}'.
func (a Int64Array) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

// Scan parses a Postgres bigint[] value back into the slice.
func (a *Int64Array) Scan(value any) error {
	if value == nil {
		*a = Int64Array{}
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning Int64Array: %w", err)
	}
	elems, err := parsePGArray(string(data))
	if err != nil {
		return fmt.Errorf("scanning Int64Array: %w", err)
	}
	out := make(Int64Array, 0, len(elems))
	for _, e := range elems {
		if e == "" {
			continue
		}
		n, err := strconv.ParseInt(e, 10, 64)
		if err != nil {
			return fmt.Errorf("scanning Int64Array: parsing element %q: %w", e, err)
		}
		out = append(out, n)
	}
	*a = out
	return nil
}

// encodePGTextArray renders a slice of strings as a Postgres array literal,
// quoting every element so there is never an ambiguity with Postgres's
// unquoted-element rules (NULL, empty string, embedded punctuation).
func encodePGTextArray(elems []string) string {
	if len(elems) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, s := range elems {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		for _, r := range s {
			if r == '"' || r == '\\' {
				b.WriteByte('\\')
			}
			b.WriteRune(r)
		}
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// parsePGArray parses a Postgres one-dimensional array's text representation
// ("{a,\"b c\",d}") into its elements. It handles quoted elements (with
// backslash-escaped quotes and backslashes) and bare elements; it does not
// handle multi-dimensional arrays, which this package never stores.
func parsePGArray(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "NULL") {
		return nil, nil
	}
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return nil, fmt.Errorf("invalid array literal %q", s)
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		return []string{}, nil
	}
	var elems []string
	var cur strings.Builder
	inQuotes := false
	escaped := false
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case escaped:
			cur.WriteByte(c)
			escaped = false
		case inQuotes && c == '\\':
			escaped = true
		case c == '"':
			inQuotes = !inQuotes
		case c == ',' && !inQuotes:
			elems = append(elems, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	elems = append(elems, cur.String())
	return elems, nil
}

// EnumMap maps an integer SNMP enum value to its name (e.g. 1 -> "normal"),
// stored as a JSONB object with string keys ("1": "normal"), since JSON
// object keys are always strings.
type EnumMap map[int64]string

// Value serializes the map to a JSON object with string keys. A nil or empty
// map is stored as SQL NULL: the columns that use EnumMap are nullable
// JSONB, and an absent enum/state-name map is meaningfully different from an
// empty one.
func (m EnumMap) Value() (driver.Value, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strconv.FormatInt(k, 10)] = v
	}
	return json.Marshal(out)
}

// Scan deserializes a JSONB object with string keys into the map.
func (m *EnumMap) Scan(value any) error {
	if value == nil {
		*m = nil
		return nil
	}
	data, err := asBytes(value)
	if err != nil {
		return fmt.Errorf("scanning EnumMap: %w", err)
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("scanning EnumMap: %w", err)
	}
	out := make(EnumMap, len(raw))
	for k, v := range raw {
		n, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			return fmt.Errorf("scanning EnumMap: key %q is not an integer: %w", k, err)
		}
		out[n] = v
	}
	*m = out
	return nil
}

// MIBModule is one loaded MIB file: a built-in one embedded in the binary, or
// one an admin uploaded. Imports lists the module names it depends on;
// Missing is the subset of those not currently loaded by any module (so
// Status is "waiting").
type MIBModule struct {
	ID         uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name       string      `json:"name" gorm:"column:name;not null"`
	Source     string      `json:"source" gorm:"column:source;not null"`
	FileName   string      `json:"file_name" gorm:"column:file_name;not null"`
	SizeBytes  int         `json:"size_bytes" gorm:"column:size_bytes;not null"`
	SHA256     string      `json:"sha256" gorm:"column:sha256;not null"`
	Content    string      `json:"content" gorm:"column:content;not null"`
	Imports    StringArray `json:"imports" gorm:"column:imports;type:text[]"`
	Missing    StringArray `json:"missing" gorm:"column:missing;type:text[]"`
	Status     string      `json:"status" gorm:"column:status;not null"`
	UploadedBy *uuid.UUID  `json:"uploaded_by" gorm:"column:uploaded_by;type:uuid"`
	CreatedAt  time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt  time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (MIBModule) TableName() string { return "mib_modules" }

// MIBObject is one named node parsed out of a MIB module: an OID, a scalar or
// table column, or a notification. Enum holds the named values of an
// INTEGER-valued object with a named-number enumeration (e.g. ifAdminStatus);
// IndexColumns names the INDEX columns of a table row object, in order.
type MIBObject struct {
	ID           int64       `json:"id" gorm:"column:id;primaryKey;autoIncrement"`
	ModuleID     uuid.UUID   `json:"module_id" gorm:"column:module_id;type:uuid;not null"`
	Name         string      `json:"name" gorm:"column:name;not null"`
	OID          string      `json:"oid" gorm:"column:oid;not null"`
	ParentOID    string      `json:"parent_oid" gorm:"column:parent_oid;not null"`
	Kind         string      `json:"kind" gorm:"column:kind;not null"`
	BaseType     string      `json:"base_type" gorm:"column:base_type;not null"`
	TypeName     string      `json:"type_name" gorm:"column:type_name;not null"`
	Units        string      `json:"units" gorm:"column:units;not null"`
	Access       string      `json:"access" gorm:"column:access;not null"`
	Description  string      `json:"description" gorm:"column:description;not null"`
	Enum         EnumMap     `json:"enum" gorm:"column:enum;type:jsonb"`
	IndexColumns StringArray `json:"index_columns" gorm:"column:index_columns;type:text[]"`
}

func (MIBObject) TableName() string { return "mib_objects" }

// MetricProfile is a named set of custom metrics (profile_metrics) that
// applies to devices whose sysObjectID falls under one of MatchPrefixes
// (compared as whole OID arcs), unless a DeviceProfileOverride says
// otherwise. Builtin profiles (the Cisco starter profile) ship with Sentinel
// and are not user-deletable.
type MetricProfile struct {
	ID                  uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Name                string      `json:"name" gorm:"column:name;not null"`
	Description         string      `json:"description" gorm:"column:description;not null"`
	MatchPrefixes       StringArray `json:"match_prefixes" gorm:"column:match_prefixes;type:text[]"`
	PollIntervalMinutes int         `json:"poll_interval_minutes" gorm:"column:poll_interval_minutes;not null;default:1"`
	Builtin             bool        `json:"builtin" gorm:"column:builtin;not null"`
	CreatedAt           time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt           time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (MetricProfile) TableName() string { return "metric_profiles" }

// ProfileMetric is one custom metric within a MetricProfile: how to poll it
// (Source, OID, and for a column metric, OID2/FilterOID/FilterValues), how to
// label each row (LabelMode and its OIDs), and an optional alerting rule
// (RuleKind/RuleValue/RuleHoldMinutes/RuleEnabled). Key is the metrics.series
// metric name and is globally unique and reserved against the if_/ups_
// prefixes built-in metrics use.
type ProfileMetric struct {
	ID              uuid.UUID   `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	ProfileID       uuid.UUID   `json:"profile_id" gorm:"column:profile_id;type:uuid;not null"`
	Name            string      `json:"name" gorm:"column:name;not null"`
	Key             string      `json:"key" gorm:"column:key;not null"`
	Source          string      `json:"source" gorm:"column:source;not null"`
	Kind            string      `json:"kind" gorm:"column:kind;not null"`
	Units           string      `json:"units" gorm:"column:units;not null"`
	Scale           float64     `json:"scale" gorm:"column:scale;not null;default:1"`
	OID             string      `json:"oid" gorm:"column:oid;not null"`
	OID2            string      `json:"oid2" gorm:"column:oid2;not null"`
	PrecisionOID    string      `json:"precision_oid" gorm:"column:precision_oid;not null"`
	FilterOID       string      `json:"filter_oid" gorm:"column:filter_oid;not null"`
	FilterValues    StringArray `json:"filter_values" gorm:"column:filter_values;type:text[]"`
	LabelMode       string      `json:"label_mode" gorm:"column:label_mode;not null;default:index"`
	LabelOID        string      `json:"label_oid" gorm:"column:label_oid;not null"`
	LabelPointerOID string      `json:"label_pointer_oid" gorm:"column:label_pointer_oid;not null"`
	LabelTargetOID  string      `json:"label_target_oid" gorm:"column:label_target_oid;not null"`
	OKStates        Int64Array  `json:"ok_states" gorm:"column:ok_states;type:bigint[]"`
	StateNames      EnumMap     `json:"state_names" gorm:"column:state_names;type:jsonb"`
	RuleKind        string      `json:"rule_kind" gorm:"column:rule_kind;not null;default:''"`
	RuleValue       *float64    `json:"rule_value" gorm:"column:rule_value"`
	RuleHoldMinutes int         `json:"rule_hold_minutes" gorm:"column:rule_hold_minutes;not null;default:0"`
	RuleEnabled     bool        `json:"rule_enabled" gorm:"column:rule_enabled;not null"`
	Position        int         `json:"position" gorm:"column:position;not null;default:0"`
	CreatedAt       time.Time   `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt       time.Time   `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

func (ProfileMetric) TableName() string { return "profile_metrics" }

// Device profile override modes: attach forces a profile onto a device whose
// sysObjectID would not otherwise match it; detach removes a profile that
// would otherwise apply.
const (
	ProfileOverrideAttach = "attach"
	ProfileOverrideDetach = "detach"
)

// DeviceProfileOverride is one device's exception to automatic profile
// matching by sysObjectID prefix.
type DeviceProfileOverride struct {
	DeviceID  uuid.UUID `json:"device_id" gorm:"column:device_id;type:uuid;primaryKey"`
	ProfileID uuid.UUID `json:"profile_id" gorm:"column:profile_id;type:uuid;primaryKey"`
	Mode      string    `json:"mode" gorm:"column:mode;not null"`
}

func (DeviceProfileOverride) TableName() string { return "device_profile_overrides" }

// DeviceProfileRun records the most recent profile poll of a device: when it
// ran, whether it succeeded, and the error if not. One row per
// (device, profile): the poller upserts it after every run.
type DeviceProfileRun struct {
	DeviceID  uuid.UUID `json:"device_id" gorm:"column:device_id;type:uuid;primaryKey"`
	ProfileID uuid.UUID `json:"profile_id" gorm:"column:profile_id;type:uuid;primaryKey"`
	RanAt     time.Time `json:"ran_at" gorm:"column:ran_at;not null"`
	OK        bool      `json:"ok" gorm:"column:ok;not null"`
	Error     string    `json:"error" gorm:"column:error;not null"`
}

func (DeviceProfileRun) TableName() string { return "device_profile_runs" }
