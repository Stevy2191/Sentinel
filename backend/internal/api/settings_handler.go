package api

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

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

// RegisterSettingsRoutes mounts admin-only settings endpoints on the given group
// (already protected by AuthMiddleware); RequireAdmin further restricts them.
func RegisterSettingsRoutes(rg *gin.RouterGroup, settingsService *services.SettingsService) {
	settings := rg.Group("/settings")
	settings.Use(RequireAdmin())
	settings.GET("", GetSettingsHandler(settingsService))
	settings.PATCH("/registration", UpdateRegistrationHandler(settingsService))
}
