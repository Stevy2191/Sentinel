package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// knownChannels is the fixed set of supported notification channels, with
// human-readable descriptions.
var knownChannels = []struct {
	Name        string
	Description string
}{
	{"email", "Email via SMTP"},
	{"slack", "Slack webhooks"},
	{"discord", "Discord webhooks"},
	{"ntfy", "ntfy push notifications"},
	{"telegram", "Telegram Bot API"},
	{"webhook", "Custom webhooks"},
}

func isKnownChannel(name string) bool {
	for _, c := range knownChannels {
		if c.Name == name {
			return true
		}
	}
	return false
}

// GetNotificationChannelsHandler handles GET /api/v1/notifications/channels. The
// "enabled" flag reflects whether the channel is actually registered (i.e. its
// environment is configured).
func GetNotificationChannelsHandler(manager *notifications.NotificationManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channels := make([]gin.H, 0, len(knownChannels))
		for _, ch := range knownChannels {
			channels = append(channels, gin.H{
				"name":        ch.Name,
				"enabled":     manager.IsRegistered(ch.Name),
				"description": ch.Description,
			})
		}
		respondSuccess(c, http.StatusOK, gin.H{"channels": channels})
	}
}

// GetNotificationHistoryHandler handles GET /api/v1/notifications/history.
func GetNotificationHistoryHandler(
	manager *notifications.NotificationManager,
	monitorService *services.MonitorService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		limit := queryInt(c, "limit", 50)
		if limit < 1 {
			limit = 50
		}
		if limit > 500 {
			limit = 500
		}
		offset := queryInt(c, "offset", 0)
		if offset < 0 {
			offset = 0
		}

		opts := notifications.ListNotificationsOptions{Limit: limit, Offset: offset}
		if status := c.Query("status"); status != "" {
			if status != "pending" && status != "sent" && status != "failed" {
				respondError(c, http.StatusBadRequest, "invalid 'status': must be pending, sent, or failed")
				return
			}
			opts.Status = status
		}
		if v := c.Query("start"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				respondError(c, http.StatusBadRequest, "invalid 'start': must be RFC3339")
				return
			}
			opts.Start = &t
		}
		if v := c.Query("end"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				respondError(c, http.StatusBadRequest, "invalid 'end': must be RFC3339")
				return
			}
			opts.End = &t
		}

		records, total, err := manager.ListNotifications(ctx, opts)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		// Enrich with monitor names via a single lookup.
		names := map[uuid.UUID]string{}
		if monitors, err := monitorService.ListMonitors(ctx, nil); err == nil {
			for _, m := range monitors {
				names[m.ID] = m.Name
			}
		}

		items := make([]gin.H, 0, len(records))
		for _, r := range records {
			var errMsg interface{}
			if r.ErrorMessage != "" {
				errMsg = r.ErrorMessage
			}
			var sentAt interface{}
			if r.SentAt != nil {
				sentAt = r.SentAt.UTC().Format(time.RFC3339)
			}
			items = append(items, gin.H{
				"id":            r.ID,
				"monitor_id":    r.MonitorID,
				"monitor_name":  names[r.MonitorID],
				"channel":       r.Channel,
				"status":        r.Status,
				"error_message": errMsg,
				"sent_at":       sentAt,
				"created_at":    r.CreatedAt.UTC().Format(time.RFC3339),
			})
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"notifications": items,
			"pagination": gin.H{
				"limit":  limit,
				"offset": offset,
				"total":  total,
			},
		})
	}
}

// SendTestNotificationHandler handles POST /api/v1/notifications/test/:channel.
func SendTestNotificationHandler(manager *notifications.NotificationManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel := strings.ToLower(c.Param("channel"))
		if !isKnownChannel(channel) {
			respondError(c, http.StatusBadRequest, "unknown channel: "+channel)
			return
		}

		message := &notifications.NotificationMessage{
			MonitorID:      uuid.New(),
			MonitorName:    "Test Monitor",
			MonitorURL:     "http://example.com",
			Status:         "down",
			Message:        "This is a test notification from Sentinel",
			PreviousStatus: "up",
			Timestamp:      time.Now(),
			ResponseTimeMs: 0,
		}

		if err := manager.SendToChannel(c.Request.Context(), channel, message); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"message": "Test notification sent to " + channel,
			"channel": channel,
		})
	}
}

// RegisterNotificationRoutes mounts the notification endpoints under
// /api/v1/notifications.
func RegisterNotificationRoutes(
	router *gin.Engine,
	manager *notifications.NotificationManager,
	monitorService *services.MonitorService,
) {
	group := router.Group("/api/v1/notifications")
	group.GET("/channels", GetNotificationChannelsHandler(manager))
	group.GET("/history", GetNotificationHistoryHandler(manager, monitorService))
	group.POST("/test/:channel", SendTestNotificationHandler(manager))
}
