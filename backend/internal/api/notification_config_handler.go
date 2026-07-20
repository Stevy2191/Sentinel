package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// validChannelParam reads and validates the :channel URL parameter, writing a
// 400 and returning ok=false for an unknown channel.
func validChannelParam(c *gin.Context) (string, bool) {
	channel := c.Param("channel")
	if !models.ValidNotificationChannels[channel] {
		respondError(c, http.StatusBadRequest, "unknown notification channel: "+channel)
		return "", false
	}
	return channel, true
}

// GetNotificationConfigsHandler handles GET /settings/notification-channels
// (admin). Returns all channel configs with secrets stripped.
func GetNotificationConfigsHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		configs, err := service.GetAllConfigs(c.Request.Context())
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, configs)
	}
}

// GetNotificationConfigHandler handles GET /settings/notification-channels/:channel
// (admin). Returns a single config including secrets, for form editing.
func GetNotificationConfigHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := validChannelParam(c)
		if !ok {
			return
		}
		config, err := service.GetConfig(c.Request.Context(), channel)
		if err != nil {
			if errors.Is(err, services.ErrConfigNotFound) {
				respondError(c, http.StatusNotFound, "no configuration for channel "+channel)
				return
			}
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, config)
	}
}

// UpdateNotificationConfigHandler handles POST /settings/notification-channels/:channel
// (admin). Creates or updates the channel config.
func UpdateNotificationConfigHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := validChannelParam(c)
		if !ok {
			return
		}
		var config models.NotificationConfig
		if err := c.ShouldBindJSON(&config); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		// The URL param is authoritative; reject a mismatched body channel.
		if config.Channel != "" && config.Channel != channel {
			respondError(c, http.StatusBadRequest, "channel in body does not match URL")
			return
		}
		config.Channel = channel

		if err := service.CreateOrUpdateConfig(c.Request.Context(), &config); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		config.HideSecrets()
		respondSuccess(c, http.StatusOK, config)
	}
}

// TestNotificationConfigHandler handles POST /settings/notification-channels/:channel/test
// (admin). Sends a test message using the stored config and records the result.
func TestNotificationConfigHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := validChannelParam(c)
		if !ok {
			return
		}
		success, testErr, err := service.TestConnection(c.Request.Context(), channel)
		if err != nil {
			if errors.Is(err, services.ErrConfigNotFound) {
				respondError(c, http.StatusNotFound, "no configuration for channel "+channel)
				return
			}
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		// A failed test is still a 200: the request succeeded, the delivery didn't.
		var testErrOut *string
		if testErr != "" {
			testErrOut = &testErr
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"channel":      channel,
			"test_success": success,
			"test_error":   testErrOut,
			"last_test_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// DeleteNotificationConfigHandler handles DELETE /settings/notification-channels/:channel
// (admin). Disables and clears the channel config.
func DeleteNotificationConfigHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, ok := validChannelParam(c)
		if !ok {
			return
		}
		if err := service.DeleteConfig(c.Request.Context(), channel); err != nil {
			if errors.Is(err, services.ErrConfigNotFound) {
				respondError(c, http.StatusNotFound, "no configuration for channel "+channel)
				return
			}
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "Channel " + channel + " disabled"})
	}
}

// RegisterNotificationConfigRoutes mounts the admin-only notification-channel
// configuration endpoints on the given group (already behind AuthMiddleware).
func RegisterNotificationConfigRoutes(rg *gin.RouterGroup, service *services.NotificationConfigService) {
	g := rg.Group("/settings/notification-channels")
	g.Use(RequireAdmin())
	g.GET("", GetNotificationConfigsHandler(service))
	g.GET("/:channel", GetNotificationConfigHandler(service))
	g.POST("/:channel", UpdateNotificationConfigHandler(service))
	g.POST("/:channel/test", TestNotificationConfigHandler(service))
	g.DELETE("/:channel", DeleteNotificationConfigHandler(service))
}
