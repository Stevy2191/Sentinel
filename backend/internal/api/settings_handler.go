package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// updateRegistrationRequest is the body for PATCH /settings/registration.
type updateRegistrationRequest struct {
	Enabled *bool `json:"enabled"`
}

// GetSettingsHandler handles GET /api/v1/settings (admin). It returns the
// runtime-adjustable settings the admin UI needs.
func GetSettingsHandler(settingsService *services.SettingsService, defaultInterval int) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		respondSuccess(c, http.StatusOK, gin.H{
			"registration_enabled":   settingsService.RegistrationEnabled(ctx),
			"app_name":               settingsService.AppName(ctx),
			"base_url":               settingsService.BaseURL(ctx),
			"default_check_interval": settingsService.DefaultCheckInterval(ctx, defaultInterval),
			"check_retention_days":   settingsService.CheckRetentionDays(ctx),
			"sentinel_external_url":  settingsService.GetString(ctx, models.SettingSentinelExternalURL, ""),
			"sentinel_internal_url":  settingsService.GetString(ctx, models.SettingSentinelInternalURL, ""),
			"report_timezone":        settingsService.ReportTimezone(ctx),
			"default_sla_target":     settingsService.DefaultSLATarget(ctx),
		})
	}
}

// updateSystemRequest is the body for PATCH /settings/system. Every field is a
// pointer so an omitted key means "leave alone" rather than "set to empty" —
// the UI can save one field without having to send the others back.
type updateSystemRequest struct {
	AppName              *string `json:"app_name"`
	BaseURL              *string `json:"base_url"`
	DefaultCheckInterval *int    `json:"default_check_interval"`
	// CheckRetentionDays bounds how long individual check results are kept.
	CheckRetentionDays *int `json:"check_retention_days"`
	// SentinelExternalURL is where agent install commands download from, and
	// SentinelInternalURL is where agents report back. Empty means derive.
	SentinelExternalURL *string `json:"sentinel_external_url"`
	SentinelInternalURL *string `json:"sentinel_internal_url"`
	// ReportTimezone is the IANA zone rendered reports are written in.
	ReportTimezone *string `json:"report_timezone"`
	// DefaultSLATarget is the uptime percentage a monitor is held to when it
	// carries no override of its own.
	DefaultSLATarget *float64 `json:"default_sla_target"`
}

// UpdateSystemSettingsHandler handles PATCH /api/v1/settings/system (admin).
// SchedulerReloader is the part of the report scheduler this handler needs:
// re-reading schedules after the timezone changes. An interface rather than the
// concrete service so settings does not depend on the scheduler package graph,
// and so nil means "no scheduler running", which is the case in tests.
type SchedulerReloader interface {
	Reload(ctx context.Context) error
}

func UpdateSystemSettingsHandler(settingsService *services.SettingsService, scheduler SchedulerReloader) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req updateSystemRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		ctx := c.Request.Context()

		if req.AppName != nil {
			name := strings.TrimSpace(*req.AppName)
			if name == "" {
				respondError(c, http.StatusBadRequest, "app_name cannot be empty")
				return
			}
			if utf8.RuneCountInString(name) > models.MaxAppNameLength {
				respondError(c, http.StatusBadRequest,
					fmt.Sprintf("app_name must be %d characters or fewer", models.MaxAppNameLength))
				return
			}
			if err := settingsService.SetString(ctx, models.SettingAppName, name); err != nil {
				respondInternal(c, "UpdateSystemSettingsHandler", err)
				return
			}
		}

		if req.BaseURL != nil {
			raw := strings.TrimRight(strings.TrimSpace(*req.BaseURL), "/")
			// Empty is allowed and means "unset": without it, email simply omits
			// links rather than emitting broken ones.
			if raw != "" {
				u, err := url.Parse(raw)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					respondError(c, http.StatusBadRequest,
						"base_url must be an absolute http(s) URL, e.g. https://sentinel.example.com")
					return
				}
			}
			if err := settingsService.SetString(ctx, models.SettingBaseURL, raw); err != nil {
				respondInternal(c, "UpdateSystemSettingsHandler", err)
				return
			}
		}

		if req.DefaultCheckInterval != nil {
			n := *req.DefaultCheckInterval
			if n < models.MinCheckIntervalSeconds || n > models.MaxCheckIntervalSeconds {
				respondError(c, http.StatusBadRequest,
					fmt.Sprintf("default_check_interval must be between %d and %d seconds",
						models.MinCheckIntervalSeconds, models.MaxCheckIntervalSeconds))
				return
			}
			if err := settingsService.SetInt(ctx, models.SettingDefaultCheckInterval, n); err != nil {
				respondInternal(c, "UpdateSystemSettingsHandler", err)
				return
			}
		}

		if req.ReportTimezone != nil {
			// Validated by loading it: an unknown name would otherwise be
			// stored happily and silently fall back to UTC at render time,
			// leaving the setting showing one zone and the report in another.
			if _, err := models.ParseReportTimezone(*req.ReportTimezone); err != nil {
				respondError(c, http.StatusBadRequest, err.Error())
				return
			}
			if err := settingsService.SetString(ctx, models.SettingReportTimezone,
				strings.TrimSpace(*req.ReportTimezone)); err != nil {
				respondInternal(c, "UpdateSystemSettingsHandler", err)
				return
			}
			// Schedule times are read in this zone, so the runner has to be
			// rebuilt. Left alone, a schedule set to 08:00 would keep firing at
			// 08:00 in the old zone until the next restart.
			if scheduler != nil {
				if err := scheduler.Reload(c.Request.Context()); err != nil {
					// The setting is saved and reports will render correctly;
					// only the delivery times are stale, which a restart fixes.
					log.Printf("[settings] report timezone saved but schedules could not be reloaded: %v", err)
				}
			}
		}

		if req.CheckRetentionDays != nil {
			n := *req.CheckRetentionDays
			if n < models.MinCheckRetentionDays || n > models.MaxCheckRetentionDays {
				respondError(c, http.StatusBadRequest,
					fmt.Sprintf("check_retention_days must be between %d and %d",
						models.MinCheckRetentionDays, models.MaxCheckRetentionDays))
				return
			}
			if err := settingsService.SetInt(ctx, models.SettingCheckRetentionDays, n); err != nil {
				respondInternal(c, "UpdateSystemSettingsHandler", err)
				return
			}
		}

		if req.DefaultSLATarget != nil {
			n := *req.DefaultSLATarget
			if n < models.MinSLATargetPercent || n > models.MaxSLATargetPercent {
				respondError(c, http.StatusBadRequest,
					fmt.Sprintf("default_sla_target must be between %.1f and %.1f",
						models.MinSLATargetPercent, models.MaxSLATargetPercent))
				return
			}
			if err := settingsService.SetFloat(ctx, models.SettingDefaultSLATarget, n); err != nil {
				respondInternal(c, "UpdateSystemSettingsHandler", err)
				return
			}
		}

		for _, u := range []struct {
			value *string
			key   string
			label string
		}{
			{req.SentinelExternalURL, models.SettingSentinelExternalURL, "sentinel_external_url"},
			{req.SentinelInternalURL, models.SettingSentinelInternalURL, "sentinel_internal_url"},
		} {
			if u.value == nil {
				continue
			}
			raw := trimURL(*u.value)
			if err := validateSentinelURL(raw); err != nil {
				respondError(c, http.StatusBadRequest, u.label+" "+err.Error())
				return
			}
			if err := settingsService.SetString(ctx, u.key, raw); err != nil {
				respondInternal(c, "UpdateSystemSettingsHandler", err)
				return
			}
		}

		_, username, _, _ := GetUserFromContext(c)
		log.Printf("System settings updated by %s", username)
		respondSuccess(c, http.StatusOK, gin.H{
			"app_name":               settingsService.AppName(ctx),
			"base_url":               settingsService.BaseURL(ctx),
			"default_check_interval": settingsService.DefaultCheckInterval(ctx, models.MinCheckIntervalSeconds),
			"check_retention_days":   settingsService.CheckRetentionDays(ctx),
			"sentinel_external_url":  settingsService.GetString(ctx, models.SettingSentinelExternalURL, ""),
			"sentinel_internal_url":  settingsService.GetString(ctx, models.SettingSentinelInternalURL, ""),
			"default_sla_target":     settingsService.DefaultSLATarget(ctx),
			"message":                "System settings updated",
		})
	}
}

// UpdateRegistrationHandler handles PATCH /api/v1/settings/registration (admin).
// It toggles whether new users may self-register.
func UpdateRegistrationHandler(settingsService *services.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req updateRegistrationRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
			respondAuthError(c, http.StatusBadRequest, "request body must include a boolean \"enabled\" field")
			return
		}
		if err := settingsService.SetRegistrationEnabled(c.Request.Context(), *req.Enabled); err != nil {
			respondAuthError(c, http.StatusInternalServerError, err.Error())
			return
		}
		_, username, _, _ := GetUserFromContext(c)
		log.Printf("Registration %s by %s", map[bool]string{true: "enabled", false: "disabled"}[*req.Enabled], username)
		respondSuccess(c, http.StatusOK, gin.H{
			"registration_enabled": *req.Enabled,
			"message":              "Registration settings updated",
		})
	}
}

// RegisterSettingsRoutes mounts admin-only settings endpoints on the given group
// (already protected by AuthMiddleware); RequireAdmin further restricts them.
func RegisterSettingsRoutes(rg *gin.RouterGroup, settingsService *services.SettingsService, defaultInterval int, users adminChecker, scheduler SchedulerReloader) {
	settings := rg.Group("/settings")
	settings.Use(RequireAdmin(users))
	settings.GET("", GetSettingsHandler(settingsService, defaultInterval))
	settings.PATCH("/registration", UpdateRegistrationHandler(settingsService))
	settings.PATCH("/system", UpdateSystemSettingsHandler(settingsService, scheduler))
	settings.GET("/incident-retention-days", GetIncidentRetentionHandler(settingsService))
	settings.PATCH("/incident-retention-days", UpdateIncidentRetentionHandler(settingsService))
}
