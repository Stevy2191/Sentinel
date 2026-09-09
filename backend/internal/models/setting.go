package models

import "time"

// Setting is a single persisted key/value application setting. Values are stored
// as text; typed accessors on the settings service handle parsing.
type Setting struct {
	Key       string    `json:"key" gorm:"column:key;primaryKey"`
	Value     string    `json:"value" gorm:"column:value;not null"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the Setting model.
func (Setting) TableName() string {
	return "settings"
}

// Setting keys. Kept as constants so callers don't hardcode strings.
const (
	// SettingRegistrationEnabled controls whether new users may self-register.
	SettingRegistrationEnabled = "registration_enabled"

	// SettingAppName is the instance's display name, shown in the sidebar, on
	// the sign-in screen, in the browser tab and in outgoing email.
	SettingAppName = "app_name"
	// SettingBaseURL is the absolute, externally reachable URL of this instance.
	// Every link in outgoing email is built from it, so it has to be what a
	// recipient can actually open, not what the container sees.
	SettingBaseURL = "base_url"
	// SettingDefaultCheckInterval is the check interval, in seconds, that new
	// monitors are created with.
	SettingDefaultCheckInterval = "default_check_interval"
	// SettingIncidentRetentionDays is how long resolved incident history is
	// kept before the nightly purge removes it.
	SettingIncidentRetentionDays = "incident_retention_days"
	// SettingSentinelExternalURL is the address install commands download
	// from — what a browser used to reach Sentinel. Empty means derive it from
	// the request.
	SettingSentinelExternalURL = "sentinel_external_url"
	// SettingSentinelInternalURL is the address agents report metrics to.
	// Differs from the external one only behind a reverse proxy. Empty means
	// use the external URL.
	SettingSentinelInternalURL = "sentinel_internal_url"

	// SettingCheckRetentionDays bounds how long individual check results are
	// kept. Without it the checks table grows without limit.
	SettingCheckRetentionDays = "check_retention_days"
)

// Bounds and defaults for the system settings above.
const (
	DefaultAppName = "Sentinel"
	// DefaultMonitorCheckInterval is the interval a new monitor gets when the
	// instance default has not been changed. Note this is unrelated to the
	// DEFAULT_CHECK_INTERVAL environment variable, which sets how often the
	// monitoring loop wakes to see which monitors are due — a scheduler tick,
	// not a per-monitor setting.
	DefaultMonitorCheckInterval = 60
	// MaxAppNameLength keeps the name to something a sidebar can render.
	MaxAppNameLength = 40
	// MinCheckIntervalSeconds/MaxCheckIntervalSeconds mirror the bounds the
	// monitor validator enforces, so a default can never be set to a value that
	// would then be rejected on every monitor created from it.
	MinCheckIntervalSeconds = 10
	MaxCheckIntervalSeconds = 3600

	// Incident retention bounds. The floor is a week because anything shorter
	// would delete an incident before a person is likely to have looked at it;
	// the ceiling is a year, past which the table grows without being read.
	// DefaultCheckRetentionDays keeps a quarter of history, which covers the
	// 90-day reporting range while bounding a table that gains a row per
	// monitor per check interval — 525,600 rows a year for a single monitor
	// checked every minute.
	DefaultCheckRetentionDays = 90
	MinCheckRetentionDays     = 1
	MaxCheckRetentionDays     = 3650

	DefaultIncidentRetentionDays = 90
	MinIncidentRetentionDays     = 7
	MaxIncidentRetentionDays     = 365
)
