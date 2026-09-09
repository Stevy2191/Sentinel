package api

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// maxChannelNameLength keeps a channel's label to something a table cell can
// render without truncating everything around it.
const maxChannelNameLength = 60

// validChannels is the sorted set of channel types, for error messages.
var validChannels = func() []string {
	out := make([]string, 0, len(models.ValidNotificationChannels))
	for name := range models.ValidNotificationChannels {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}()

func isValidChannel(name string) bool {
	return models.ValidNotificationChannels[name]
}

// GetNotificationConfigsHandler handles GET /settings/notification-channels
// (admin). Returns all channel configs with secrets stripped.
func GetNotificationConfigsHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		configs, err := service.GetAllConfigs(c.Request.Context())
		if err != nil {
			respondInternal(c, "GetNotificationConfigsHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, configs)
	}
}

// ListAvailableChannelsHandler handles GET /notification-channels for any
// authenticated user. It answers exactly one question — which channels can
// deliver an alert right now — so a monitor form can offer them.
//
// Deliberately separate from the admin listing rather than relaxing that one.
// The admin response carries configuration: SMTP host, port and username,
// webhook URLs. A webhook URL is itself a credential — anyone holding it can
// post as the integration — so it must not reach a non-admin who only needs to
// tick a box. This returns the channel name and whether it is on, nothing else.
func ListAvailableChannelsHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		configs, err := service.GetAllConfigs(c.Request.Context())
		if err != nil {
			respondInternal(c, "ListAvailableChannelsHandler", err)
			return
		}
		out := make([]gin.H, 0, len(configs))
		for _, cfg := range configs {
			out = append(out, gin.H{
				"id":      cfg.ID,
				"name":    cfg.Name,
				"channel": cfg.Channel,
				"enabled": cfg.Enabled,
			})
		}
		respondSuccess(c, http.StatusOK, out)
	}
}

// channelIDParam parses and validates the :id path parameter.
func channelIDParam(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "channel id must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

// GetNotificationConfigHandler handles GET /settings/notification-channels/:id
// (admin). Returns a single channel including secrets, for form editing.
func GetNotificationConfigHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := channelIDParam(c)
		if !ok {
			return
		}
		config, err := service.GetConfig(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, services.ErrConfigNotFound) {
				respondError(c, http.StatusNotFound, "no such notification channel")
				return
			}
			respondInternal(c, "GetNotificationConfigHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, config)
	}
}

// bindChannelBody reads and sanity-checks a channel payload.
func bindChannelBody(c *gin.Context) (*models.NotificationConfig, bool) {
	var config models.NotificationConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	config.Name = strings.TrimSpace(config.Name)
	if config.Name == "" {
		respondError(c, http.StatusBadRequest, "name is required")
		return nil, false
	}
	if utf8.RuneCountInString(config.Name) > maxChannelNameLength {
		respondError(c, http.StatusBadRequest,
			fmt.Sprintf("name must be %d characters or fewer", maxChannelNameLength))
		return nil, false
	}
	if !isValidChannel(config.Channel) {
		respondError(c, http.StatusBadRequest, "channel must be one of: "+strings.Join(validChannels, ", "))
		return nil, false
	}
	return &config, true
}

// CreateNotificationConfigHandler handles POST /settings/notification-channels
// (admin). Adds a channel. An install may hold several of the same type.
func CreateNotificationConfigHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		config, ok := bindChannelBody(c)
		if !ok {
			return
		}
		// The id is server-assigned; a client-supplied one would let a caller
		// overwrite an unrelated channel by guessing its id.
		config.ID = uuid.Nil
		if err := service.CreateConfig(c.Request.Context(), config); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		config.HideSecrets()
		respondSuccess(c, http.StatusCreated, config)
	}
}

// UpdateNotificationConfigHandler handles PUT /settings/notification-channels/:id
// (admin).
func UpdateNotificationConfigHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := channelIDParam(c)
		if !ok {
			return
		}
		config, ok := bindChannelBody(c)
		if !ok {
			return
		}
		if err := service.UpdateConfig(c.Request.Context(), id, config); err != nil {
			if errors.Is(err, services.ErrConfigNotFound) {
				respondError(c, http.StatusNotFound, "no such notification channel")
				return
			}
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		config.HideSecrets()
		respondSuccess(c, http.StatusOK, config)
	}
}

// setEnabledRequest is the body for PATCH /settings/notification-channels/:id/enabled.
type setEnabledRequest struct {
	Enabled *bool `json:"enabled"`
}

// SetChannelEnabledHandler handles PATCH /settings/notification-channels/:id/enabled
// (admin). Switches one channel on or off, leaving its configuration alone.
func SetChannelEnabledHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := channelIDParam(c)
		if !ok {
			return
		}
		var req setEnabledRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
			respondError(c, http.StatusBadRequest, "request body must include a boolean \"enabled\" field")
			return
		}
		if err := service.SetEnabled(c.Request.Context(), id, *req.Enabled); err != nil {
			if errors.Is(err, services.ErrConfigNotFound) {
				respondError(c, http.StatusNotFound, "no such notification channel")
				return
			}
			respondInternal(c, "SetChannelEnabledHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"id": id, "enabled": *req.Enabled})
	}
}

// TestNotificationConfigHandler handles POST /settings/notification-channels/:id/test
// (admin). Sends a test message using the stored config and records the result.
func TestNotificationConfigHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := channelIDParam(c)
		if !ok {
			return
		}
		success, testErr, err := service.TestConnection(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, services.ErrConfigNotFound) {
				respondError(c, http.StatusNotFound, "no such notification channel")
				return
			}
			respondInternal(c, "TestNotificationConfigHandler", err)
			return
		}
		// A failed test is still a 200: the request succeeded, the delivery didn't.
		var testErrOut *string
		if testErr != "" {
			testErrOut = &testErr
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"id":           id,
			"test_success": success,
			"test_error":   testErrOut,
			"last_test_at": time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// DeleteNotificationConfigHandler handles DELETE /settings/notification-channels/:id
// (admin). Removes the channel.
func DeleteNotificationConfigHandler(service *services.NotificationConfigService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := channelIDParam(c)
		if !ok {
			return
		}
		if err := service.DeleteConfig(c.Request.Context(), id); err != nil {
			if errors.Is(err, services.ErrConfigNotFound) {
				respondError(c, http.StatusNotFound, "no such notification channel")
				return
			}
			respondInternal(c, "DeleteNotificationConfigHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "Channel deleted"})
	}
}

// RegisterNotificationConfigRoutes mounts the notification-channel endpoints on
// the given group (already behind AuthMiddleware).
func RegisterNotificationConfigRoutes(rg *gin.RouterGroup, service *services.NotificationConfigService, users adminChecker) {
	// Readable by any authenticated user: choosing where a monitor's alerts go
	// is part of creating a monitor, which is not an admin-only action.
	rg.GET("/notification-channels", ListAvailableChannelsHandler(service))

	g := rg.Group("/settings/notification-channels")
	g.Use(RequireAdmin(users))
	g.GET("", GetNotificationConfigsHandler(service))
	g.POST("", CreateNotificationConfigHandler(service))
	g.GET("/:id", GetNotificationConfigHandler(service))
	g.PUT("/:id", UpdateNotificationConfigHandler(service))
	g.PATCH("/:id/enabled", SetChannelEnabledHandler(service))
	g.POST("/:id/test", TestNotificationConfigHandler(service))
	g.DELETE("/:id", DeleteNotificationConfigHandler(service))
}
