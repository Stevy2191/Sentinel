package toolruns

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/nettools"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Settings are the admin's network-tools settings.
type Settings struct {
	Allowlist     []string `json:"allowlist"`
	ServerEnabled bool     `json:"server_enabled"`
	RetentionDays int      `json:"retention_days"`
}

// SettingsError is a save refused for bad input. Entries lists each bad
// allowlist entry; it is empty when the retention was the problem.
type SettingsError struct {
	Message string
	Entries []nettools.EntryError
}

func (e *SettingsError) Error() string { return e.Message }

// LoadSettings reads the settings, with the defaults (an empty allowlist,
// runs from the server allowed, 30 days) for anything unset. A stored
// allowlist that is not a JSON array of strings reads as empty, and a
// retention outside 1-365 as the default, so a bad row can neither open the
// allowlist nor make the prune delete everything.
func LoadSettings(ctx context.Context, ss *services.SettingsService) Settings {
	list := []string{}
	raw := ss.GetString(ctx, models.SettingNetToolsAllowlist, "[]")
	if err := json.Unmarshal([]byte(raw), &list); err != nil || list == nil {
		if err != nil {
			log.Printf("[tools] stored %s is not a JSON array of strings; treating it as empty: %v",
				models.SettingNetToolsAllowlist, err)
		}
		list = []string{}
	}
	days := ss.GetInt(ctx, models.SettingNetToolsRetentionDays, DefaultRetentionDays)
	if days < MinRetentionDays || days > MaxRetentionDays {
		days = DefaultRetentionDays
	}
	return Settings{
		Allowlist:     list,
		ServerEnabled: ss.GetBool(ctx, models.SettingNetToolsServerEnabled, true),
		RetentionDays: days,
	}
}

// SaveSettings validates and stores s and returns what was stored. Entries
// are trimmed and lower-cased, blank ones dropped and repeats removed. Any
// bad entry refuses the whole save with a *SettingsError listing every bad
// entry, and nothing is written; so does a retention outside 1-365.
func SaveSettings(ctx context.Context, ss *services.SettingsService, s Settings) (Settings, error) {
	if s.RetentionDays < MinRetentionDays || s.RetentionDays > MaxRetentionDays {
		return Settings{}, &SettingsError{
			Message: fmt.Sprintf("retention_days must be between %d and %d", MinRetentionDays, MaxRetentionDays),
		}
	}
	clean := cleanAllowlist(s.Allowlist)
	if _, bad := nettools.ParseAllowlist(clean); len(bad) > 0 {
		return Settings{}, &SettingsError{Message: "some allowlist entries are not valid", Entries: bad}
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return Settings{}, fmt.Errorf("encoding the allowlist: %w", err)
	}
	if err := ss.SetString(ctx, models.SettingNetToolsAllowlist, string(encoded)); err != nil {
		return Settings{}, err
	}
	if err := ss.SetBool(ctx, models.SettingNetToolsServerEnabled, s.ServerEnabled); err != nil {
		return Settings{}, err
	}
	if err := ss.SetInt(ctx, models.SettingNetToolsRetentionDays, s.RetentionDays); err != nil {
		return Settings{}, err
	}
	return LoadSettings(ctx, ss), nil
}

// cleanAllowlist trims and lower-cases entries, drops blank ones and keeps
// the first of any repeat, in order. Never nil.
func cleanAllowlist(entries []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, e := range entries {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}
