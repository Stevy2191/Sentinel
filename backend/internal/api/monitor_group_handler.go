package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// groupUptimeWindow is the range over which a group's roll-up uptime is computed.
const groupUptimeWindow = 24 * time.Hour

// monitorGroupBody is the create/update request body.
type monitorGroupBody struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
}

type reorderGroupBody struct {
	Position *int `json:"position"`
}

type moveMonitorBody struct {
	GroupID *uuid.UUID `json:"group_id"`
}

// parseGroupID reads and validates the :id URL parameter as a group UUID.
func parseGroupID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid group id: must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

// avgUptime returns the average uptime (0-100) across the given monitors over
// [start, end]. An empty group is treated as 100% (nothing is down).
func avgUptime(c *gin.Context, incidentService *services.IncidentService, monitors []models.Monitor, start, end time.Time) float64 {
	if len(monitors) == 0 {
		return 100.0
	}
	var sum float64
	for i := range monitors {
		down, err := incidentService.GetDowntimePercentage(c.Request.Context(), monitors[i].ID, start, end)
		if err != nil {
			down = 0
		}
		sum += 100 - down
	}
	return round2(sum / float64(len(monitors)))
}

// GetMonitorGroupsHandler handles GET /api/v1/monitor-groups. It returns each
// group with its monitors, a monitor count, and a rolled-up uptime percentage.
func GetMonitorGroupsHandler(monitorService *services.MonitorService, incidentService *services.IncidentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		groups, err := monitorService.GetMonitorGroups(c.Request.Context())
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		end := time.Now()
		start := end.Add(-groupUptimeWindow)

		out := make([]gin.H, 0, len(groups))
		for i := range groups {
			g := groups[i]
			out = append(out, gin.H{
				"id":            g.ID,
				"name":          g.Name,
				"description":   g.Description,
				"color":         g.Color,
				"position":      g.Position,
				"monitors":      g.Monitors,
				"monitor_count": len(g.Monitors),
				"group_uptime":  avgUptime(c, incidentService, g.Monitors, start, end),
				"created_at":    g.CreatedAt,
				"updated_at":    g.UpdatedAt,
			})
		}
		respondSuccess(c, http.StatusOK, out)
	}
}

// CreateMonitorGroupHandler handles POST /api/v1/monitor-groups.
func CreateMonitorGroupHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body monitorGroupBody
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		group, err := monitorService.CreateMonitorGroup(c.Request.Context(), body.Name, body.Description, body.Color)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		respondSuccess(c, http.StatusCreated, group)
	}
}

// UpdateMonitorGroupHandler handles PUT /api/v1/monitor-groups/:id.
func UpdateMonitorGroupHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseGroupID(c)
		if !ok {
			return
		}
		var body monitorGroupBody
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		group, err := monitorService.UpdateMonitorGroup(c.Request.Context(), id, body.Name, body.Description, body.Color)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, group)
	}
}

// DeleteMonitorGroupHandler handles DELETE /api/v1/monitor-groups/:id.
func DeleteMonitorGroupHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseGroupID(c)
		if !ok {
			return
		}
		if err := monitorService.DeleteMonitorGroup(c.Request.Context(), id); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "group deleted; its monitors were ungrouped"})
	}
}

// ReorderMonitorGroupHandler handles POST /api/v1/monitor-groups/:id/reorder.
func ReorderMonitorGroupHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseGroupID(c)
		if !ok {
			return
		}
		var body reorderGroupBody
		if err := c.ShouldBindJSON(&body); err != nil || body.Position == nil {
			respondError(c, http.StatusBadRequest, "request body must include an integer \"position\"")
			return
		}
		if err := monitorService.ReorderMonitorGroup(c.Request.Context(), id, *body.Position); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "group reordered"})
	}
}

// MoveMonitorToGroupHandler handles POST /api/v1/monitors/:id/group. A null
// group_id ungroups the monitor.
func MoveMonitorToGroupHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		monitorID, ok := parseMonitorID(c)
		if !ok {
			return
		}
		var body moveMonitorBody
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := monitorService.MoveMonitorToGroup(c.Request.Context(), monitorID, body.GroupID); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"message": "monitor group updated"})
	}
}

// RegisterMonitorGroupRoutes mounts the monitor-group endpoints plus the
// per-monitor group-assignment endpoint.
func RegisterMonitorGroupRoutes(rg *gin.RouterGroup, monitorService *services.MonitorService, incidentService *services.IncidentService) {
	groups := rg.Group("/monitor-groups")
	groups.GET("", GetMonitorGroupsHandler(monitorService, incidentService))
	groups.POST("", CreateMonitorGroupHandler(monitorService))
	groups.PUT("/:id", UpdateMonitorGroupHandler(monitorService))
	groups.DELETE("/:id", DeleteMonitorGroupHandler(monitorService))
	groups.POST("/:id/reorder", ReorderMonitorGroupHandler(monitorService))

	rg.POST("/monitors/:id/group", MoveMonitorToGroupHandler(monitorService))
}
