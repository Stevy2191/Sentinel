// Package api contains the Gin HTTP handlers exposing Sentinel's REST API.
package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

const (
	defaultPage  = 1
	defaultLimit = 50
	maxLimit     = 500
)

// respondSuccess writes the standard success envelope.
func respondSuccess(c *gin.Context, code int, data interface{}) {
	c.JSON(code, gin.H{"success": true, "data": data})
}

// respondError writes the standard error envelope.
func respondError(c *gin.Context, code int, message string) {
	c.JSON(code, gin.H{"success": false, "error": message})
}

// parseMonitorID reads and validates the :id URL parameter, writing a 400 and
// returning ok=false when it is not a valid UUID.
func parseMonitorID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid monitor id: must be a UUID")
		return uuid.Nil, false
	}
	return id, true
}

// classifyServiceError maps a service error to an HTTP status: 404 for
// not-found, 400 for validation failures, 500 otherwise.
func classifyServiceError(err error) int {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound
	case strings.Contains(err.Error(), "invalid monitor"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// queryInt returns the integer query parameter named key, or def if absent or
// unparseable.
func queryInt(c *gin.Context, key string, def int) int {
	if v := c.Query(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// CreateMonitorHandler handles POST /api/v1/monitors.
func CreateMonitorHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var monitor models.Monitor
		if err := c.ShouldBindJSON(&monitor); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		if err := monitor.Validate(); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}

		created, err := monitorService.CreateMonitor(c.Request.Context(), &monitor)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}

		log.Printf("Monitor created: %s (ID: %s)", created.Name, created.ID)
		respondSuccess(c, http.StatusCreated, created)
	}
}

// GetMonitorsHandler handles GET /api/v1/monitors with filtering and pagination.
func GetMonitorsHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		page := queryInt(c, "page", defaultPage)
		if page < 1 {
			page = defaultPage
		}
		limit := queryInt(c, "limit", defaultLimit)
		if limit < 1 {
			limit = defaultLimit
		}
		if limit > maxLimit {
			limit = maxLimit
		}

		filters := map[string]interface{}{}
		if v := c.Query("enabled"); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				respondError(c, http.StatusBadRequest, "invalid 'enabled' filter: must be true or false")
				return
			}
			filters["enabled"] = b
		}
		if v := c.Query("type"); v != "" {
			filters["type"] = v
		}
		if v := c.Query("status"); v != "" {
			filters["status"] = v
		}

		monitors, err := monitorService.ListMonitors(c.Request.Context(), filters)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		// Paginate the filtered, already-ordered (created_at DESC) result set.
		total := len(monitors)
		offset := (page - 1) * limit
		pageItems := []models.Monitor{}
		if offset < total {
			end := offset + limit
			if end > total {
				end = total
			}
			pageItems = monitors[offset:end]
		}

		pages := 0
		if total > 0 {
			pages = (total + limit - 1) / limit
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"monitors": pageItems,
			"pagination": gin.H{
				"page":  page,
				"limit": limit,
				"total": total,
				"pages": pages,
			},
		})
	}
}

// GetMonitorHandler handles GET /api/v1/monitors/:id.
func GetMonitorHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}

		monitor, err := monitorService.GetMonitor(c.Request.Context(), id)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, monitor)
	}
}

// UpdateMonitorHandler handles PUT /api/v1/monitors/:id.
func UpdateMonitorHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}

		var updates models.Monitor
		if err := c.ShouldBindJSON(&updates); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		updated, err := monitorService.UpdateMonitor(c.Request.Context(), id, &updates)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}

		log.Printf("Monitor updated: %s (ID: %s)", updated.Name, updated.ID)
		respondSuccess(c, http.StatusOK, updated)
	}
}

// DeleteMonitorHandler handles DELETE /api/v1/monitors/:id.
func DeleteMonitorHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}

		if err := monitorService.DeleteMonitor(c.Request.Context(), id); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}

		log.Printf("Monitor deleted: ID %s", id)
		respondSuccess(c, http.StatusOK, gin.H{"id": id, "deleted": true})
	}
}

// PauseMonitorHandler handles POST /api/v1/monitors/:id/pause.
func PauseMonitorHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}

		if err := monitorService.PauseMonitor(c.Request.Context(), id); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"id": id, "enabled": false})
	}
}

// ResumeMonitorHandler handles POST /api/v1/monitors/:id/resume.
func ResumeMonitorHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}

		if err := monitorService.ResumeMonitor(c.Request.Context(), id); err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"id": id, "enabled": true})
	}
}

// GetMonitorStatusHandler handles GET /api/v1/monitors/:id/status.
func GetMonitorStatusHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}

		status, err := monitorService.GetMonitorStatus(c.Request.Context(), id)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"id": id, "status": status})
	}
}

// TestMonitorHandler handles POST /api/v1/monitors/:id/test, running an
// immediate check and storing the result.
func TestMonitorHandler(monitorService *services.MonitorService, checkService *services.CheckService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}

		check, err := monitorService.TestMonitor(c.Request.Context(), id, checkService)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, check)
	}
}

// RegisterMonitorRoutes wires the monitor handlers onto a router group at
// /api/v1/monitors.
func RegisterMonitorRoutes(rg *gin.RouterGroup, monitorService *services.MonitorService, checkService *services.CheckService) {
	monitors := rg.Group("/monitors")
	monitors.POST("", CreateMonitorHandler(monitorService))
	monitors.GET("", GetMonitorsHandler(monitorService))
	monitors.GET("/:id", GetMonitorHandler(monitorService))
	monitors.PUT("/:id", UpdateMonitorHandler(monitorService))
	monitors.DELETE("/:id", DeleteMonitorHandler(monitorService))
	monitors.POST("/:id/pause", PauseMonitorHandler(monitorService))
	monitors.POST("/:id/resume", ResumeMonitorHandler(monitorService))
	monitors.GET("/:id/status", GetMonitorStatusHandler(monitorService))
	monitors.POST("/:id/test", TestMonitorHandler(monitorService, checkService))
}
