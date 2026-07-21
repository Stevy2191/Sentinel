package api

import (
	"log"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

var themeHexPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// updateRegistrationRequest is the body for PATCH /settings/registration.
type updateRegistrationRequest struct {
	Enabled *bool `json:"enabled"`
}

// GetSettingsHandler handles GET /api/v1/settings (admin). It returns the
// runtime-adjustable settings the admin UI needs.
func GetSettingsHandler(settingsService *services.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		respondSuccess(c, http.StatusOK, gin.H{
			"registration_enabled": settingsService.RegistrationEnabled(c.Request.Context()),
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

// updateThemeRequest is the body for PATCH /settings/theme.
type updateThemeRequest struct {
	PrimaryColor string `json:"primary_color"`
	AccentColor  string `json:"accent_color"`
	Mode         string `json:"mode"`
}

// UpdateUserThemeHandler handles PATCH /api/v1/settings/theme. It saves the
// authenticated user's theme (per-user, not admin-gated) so it syncs across
// their devices.
func UpdateUserThemeHandler(authService *services.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, _, ok := GetUserFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "authentication required")
			return
		}
		var req updateThemeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if !themeHexPattern.MatchString(req.PrimaryColor) || !themeHexPattern.MatchString(req.AccentColor) {
			respondError(c, http.StatusBadRequest, "primary_color and accent_color must be hex like #10b981")
			return
		}
		switch req.Mode {
		case "", "light", "dark", "auto":
		default:
			respondError(c, http.StatusBadRequest, "mode must be light, dark, or auto")
			return
		}
		if err := authService.UpdateUserTheme(c.Request.Context(), userID, req.PrimaryColor, req.AccentColor, req.Mode); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"primary_color": req.PrimaryColor,
			"accent_color":  req.AccentColor,
			"mode":          req.Mode,
		})
	}
}

// RegisterSettingsRoutes mounts admin-only settings endpoints on the given group
// (already protected by AuthMiddleware); RequireAdmin further restricts them.
func RegisterSettingsRoutes(rg *gin.RouterGroup, settingsService *services.SettingsService) {
	settings := rg.Group("/settings")
	settings.Use(RequireAdmin())
	settings.GET("", GetSettingsHandler(settingsService))
	settings.PATCH("/registration", UpdateRegistrationHandler(settingsService))
}
