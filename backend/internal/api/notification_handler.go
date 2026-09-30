package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
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

// notificationLister lists notification history; an interface so the handler
// is tested without a database.
type notificationLister interface {
	ListNotifications(ctx context.Context, opts notifications.ListNotificationsOptions) ([]models.Notification, int64, error)
}

// notificationMonitorNamer resolves the monitors a viewer may use to label
// notification history rows - the same accessible-monitor rule the monitor
// list and incident list use.
type notificationMonitorNamer interface {
	ListAccessibleMonitors(ctx context.Context, userID uuid.UUID, isAdmin bool, filters map[string]interface{}) ([]models.Monitor, error)
}

// notificationAgentNamer resolves server-agent names. Agents are admin-only,
// so the handler only calls this for admins.
type notificationAgentNamer interface {
	List(ctx context.Context) ([]models.Agent, error)
}

// notificationDeviceNamer resolves the network devices a viewer may see, to
// label device notification rows. DeviceService.List already scopes this by
// site sharing (admins: every device; members: devices in sites shared with
// them), the same rule device incidents use.
type notificationDeviceNamer interface {
	List(ctx context.Context, userID uuid.UUID, isAdmin bool, f services.DeviceFilter) ([]services.DeviceView, error)
}

// notificationDeviceSiteLookup resolves a single device, so the retry handler
// can find which site a device notification record belongs to and check the
// caller's access to it.
type notificationDeviceSiteLookup interface {
	Get(ctx context.Context, id uuid.UUID) (*services.DeviceView, error)
}

// GetNotificationHistoryHandler handles GET /api/v1/notifications/history.
// Members see only records for monitors they own or have been shared; admins
// see everything, including server-agent alerts.
func GetNotificationHistoryHandler(
	manager notificationLister,
	monitorService notificationMonitorNamer,
	agentService notificationAgentNamer,
	deviceService notificationDeviceNamer,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		userID, _, isAdmin, ok := GetUserFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "authentication required")
			return
		}

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

		opts := notifications.ListNotificationsOptions{
			Viewer: &notifications.NotificationViewer{UserID: userID, IsAdmin: isAdmin},
			Limit:  limit,
			Offset: offset,
		}
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
			respondInternal(c, "GetNotificationHistoryHandler", err)
			return
		}

		// Names are resolved only for what the caller may see: their own
		// accessible monitors (the same set ListNotifications just filtered
		// to), and - for admins only - agents, which are admin-only entities.
		names := map[uuid.UUID]string{}
		if monitors, err := monitorService.ListAccessibleMonitors(ctx, userID, isAdmin, nil); err == nil {
			for _, m := range monitors {
				names[m.ID] = m.Name
			}
		}
		agentNames := map[uuid.UUID]string{}
		if isAdmin {
			if agentRows, err := agentService.List(ctx); err == nil {
				for _, a := range agentRows {
					agentNames[a.ID] = a.Name
				}
			}
		}
		// Same idea for devices: a device alert's name lookup falls back to
		// its host, the way the poller's own displayName does, rather than
		// showing an empty name. Scoped to what the caller may see, the same
		// as the monitor names above.
		deviceNames := map[uuid.UUID]string{}
		if deviceRows, err := deviceService.List(ctx, userID, isAdmin, services.DeviceFilter{}); err == nil {
			for _, d := range deviceRows {
				name := d.Name
				if name == "" {
					name = d.Host
				}
				deviceNames[d.ID] = name
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
			// A record belongs to a monitor, a server agent, or a network
			// device. The name lookup is per-subject, so whichever one is set
			// gets its real name instead of an empty string.
			subject := ""
			switch {
			case r.MonitorID != nil:
				subject = names[*r.MonitorID]
			case r.AgentID != nil:
				subject = agentNames[*r.AgentID]
			case r.DeviceID != nil:
				subject = deviceNames[*r.DeviceID]
			}
			items = append(items, gin.H{
				"id":            r.ID,
				"monitor_id":    r.MonitorID,
				"agent_id":      r.AgentID,
				"device_id":     r.DeviceID,
				"monitor_name":  subject,
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
// It sends into whatever channel is actually configured, so
// RegisterNotificationRoutes mounts it behind RequireAdmin: any signed-in user
// able to trigger it could otherwise spam a real Slack/Discord/webhook/etc.
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

		// This endpoint names a channel type, not a specific channel; several
		// may share a type, so send through the first loaded one of that type.
		if err := manager.SendToChannelType(c.Request.Context(), channel, message); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"message": "Test notification sent to " + channel,
			"channel": channel,
		})
	}
}

// notificationRetryStore loads a single notification record and re-sends it;
// an interface so the handler is tested without a database.
type notificationRetryStore interface {
	GetNotificationByID(ctx context.Context, id uuid.UUID) (*models.Notification, error)
	RetryNotification(ctx context.Context, id uuid.UUID) error
}

// monitorAccessChecker is declared in incident_handler.go and reused here: it
// answers view and edit permission questions about a monitor, so the retry
// handler can 404 what the caller cannot see and 403 what they can see but
// not edit.

// RetryFailedNotificationHandler handles POST
// /api/v1/notifications/retry/:notification_id. A member may retry a record
// only for a monitor they can edit; a device record instead follows the
// device's site (readonly access or better, via site sharing, same as device
// incidents); a record they cannot even view - or a server-agent record,
// which is admin-only - answers 404 like one that does not exist, before any
// retry is attempted.
func RetryFailedNotificationHandler(store notificationRetryStore, monitors monitorAccessChecker, devices notificationDeviceSiteLookup, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("notification_id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid notification_id: must be a UUID")
			return
		}

		userID, _, isAdmin, ok := GetUserFromContext(c)
		if !ok {
			respondAuthError(c, http.StatusUnauthorized, "authentication required")
			return
		}

		record, err := store.GetNotificationByID(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(c, http.StatusNotFound, "notification not found")
				return
			}
			respondInternal(c, "RetryFailedNotificationHandler", err)
			return
		}

		// Checked before any retry happens, so nothing about a notification
		// the caller cannot see is acted on, let alone reported back.
		if !isAdmin {
			switch {
			case record.MonitorID != nil:
				canView, err := monitors.CanUserViewMonitor(c.Request.Context(), userID, *record.MonitorID)
				if err != nil {
					respondError(c, classifyServiceError(err), err.Error())
					return
				}
				if !canView {
					respondError(c, http.StatusNotFound, "notification not found")
					return
				}
				canEdit, err := monitors.CanUserEditMonitor(c.Request.Context(), userID, *record.MonitorID)
				if err != nil {
					respondError(c, classifyServiceError(err), err.Error())
					return
				}
				if !canEdit {
					respondError(c, http.StatusForbidden, "you do not have permission to retry this notification")
					return
				}
			case record.DeviceID != nil:
				// A device record has no editable/viewable notion of its own;
				// visibility follows the device's site, the same rule device
				// incidents use. Retrying it still 400s (ErrNotificationNotRetryable)
				// below - this only decides whether the caller may be told that.
				device, err := devices.Get(c.Request.Context(), *record.DeviceID)
				if err != nil {
					if errors.Is(err, services.ErrDeviceNotFound) {
						respondError(c, http.StatusNotFound, "notification not found")
						return
					}
					respondInternal(c, "RetryFailedNotificationHandler", err)
					return
				}
				level, err := sites.SiteAccess(c.Request.Context(), userID, false, device.SiteID)
				if err != nil {
					if errors.Is(err, services.ErrSiteNotFound) {
						respondError(c, http.StatusNotFound, "notification not found")
						return
					}
					respondError(c, classifyServiceError(err), err.Error())
					return
				}
				if level < services.SiteAccessReadonly {
					respondError(c, http.StatusNotFound, "notification not found")
					return
				}
			default:
				// Server-agent records (and anything without a monitor or
				// device) are admin-only, because agents are admin-only.
				respondError(c, http.StatusNotFound, "notification not found")
				return
			}
		}

		if err := store.RetryNotification(c.Request.Context(), id); err != nil {
			switch {
			case errors.Is(err, gorm.ErrRecordNotFound):
				respondError(c, http.StatusNotFound, err.Error())
			case errors.Is(err, notifications.ErrNotificationNotFailed):
				respondError(c, http.StatusBadRequest, err.Error())
			case errors.Is(err, notifications.ErrNotificationNotRetryable):
				respondError(c, http.StatusBadRequest, err.Error())
			default:
				respondInternal(c, "RetryFailedNotificationHandler", err)
			}
			return
		}

		respondSuccess(c, http.StatusOK, gin.H{"message": "Notification retry sent"})
	}
}

// RegisterNotificationRoutes mounts the notification endpoints under the given
// group's /notifications path. Sending a test notification is admin-only
// (RequireAdmin, matching the other admin-gated routes); history and retry are
// open to any signed-in user but scoped to what they may see and edit.
func RegisterNotificationRoutes(
	rg *gin.RouterGroup,
	manager *notifications.NotificationManager,
	monitorService *services.MonitorService,
	agentService *services.AgentService,
	deviceService *services.DeviceService,
	sites siteAccessChecker,
	users adminChecker,
) {
	group := rg.Group("/notifications")
	group.GET("/channels", GetNotificationChannelsHandler(manager))
	group.GET("/history", GetNotificationHistoryHandler(manager, monitorService, agentService, deviceService))
	group.POST("/retry/:notification_id", RetryFailedNotificationHandler(manager, monitorService, deviceService, sites))

	admin := group.Group("", RequireAdmin(users))
	admin.POST("/test/:channel", SendTestNotificationHandler(manager))
}
