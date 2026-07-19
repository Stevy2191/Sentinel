package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// classifyStatusPageError maps a status-page service error to an HTTP status.
func classifyStatusPageError(err error) int {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return http.StatusNotFound
	case errors.Is(err, services.ErrMonitorAlreadyOnPage):
		return http.StatusBadRequest
	case strings.Contains(err.Error(), "invalid status page"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// uptimePercent returns the uptime percentage for a monitor over [since, until],
// degrading to 100 (assume up) if the calculation fails so a single monitor
// cannot break the whole page.
func uptimePercent(ctx context.Context, incidentService *services.IncidentService, monitorID uuid.UUID, since, until time.Time) float64 {
	down, err := incidentService.GetDowntimePercentage(ctx, monitorID, since, until)
	if err != nil {
		log.Printf("[statuspage] uptime calc failed for monitor %s: %v", monitorID, err)
		return 100
	}
	return round2(100 - down)
}

// CreateStatusPageHandler handles POST /api/v1/status-pages.
func CreateStatusPageHandler(statusPageService *services.StatusPageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var page models.StatusPage
		if err := c.ShouldBindJSON(&page); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		created, err := statusPageService.CreateStatusPage(c.Request.Context(), &page)
		if err != nil {
			respondError(c, classifyStatusPageError(err), err.Error())
			return
		}

		log.Printf("Status page created: %s", created.Slug)
		respondSuccess(c, http.StatusCreated, created)
	}
}

// statusPageWithCount embeds a StatusPage and adds the number of monitors
// associated with it for the list response.
type statusPageWithCount struct {
	models.StatusPage
	MonitorCount int `json:"monitor_count"`
}

// GetStatusPagesHandler handles GET /api/v1/status-pages.
func GetStatusPagesHandler(statusPageService *services.StatusPageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		pages, err := statusPageService.ListStatusPages(ctx)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		// Enrich each page with its monitor count.
		out := make([]statusPageWithCount, 0, len(pages))
		for i := range pages {
			count := 0
			if entries, _, err := statusPageService.GetPageMonitors(ctx, pages[i].Slug); err == nil {
				count = len(entries)
			}
			out = append(out, statusPageWithCount{StatusPage: pages[i], MonitorCount: count})
		}

		respondSuccess(c, http.StatusOK, gin.H{"pages": out})
	}
}

// GetStatusPageHandler handles GET /api/v1/status-pages/:slug (admin view).
func GetStatusPageHandler(statusPageService *services.StatusPageService, incidentService *services.IncidentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")
		ctx := c.Request.Context()

		page, err := statusPageService.GetStatusPageBySlug(ctx, slug)
		if err != nil {
			respondError(c, classifyStatusPageError(err), err.Error())
			return
		}

		entries, monitors, err := statusPageService.GetPageMonitors(ctx, slug)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		entryByMonitor := make(map[uuid.UUID]models.StatusPageMonitor, len(entries))
		for _, e := range entries {
			entryByMonitor[e.MonitorID] = e
		}

		now := time.Now().UTC()
		monitorsResp := make([]gin.H, 0, len(monitors))
		for _, m := range monitors {
			e := entryByMonitor[m.ID]
			monitorsResp = append(monitorsResp, gin.H{
				"id":               m.ID,
				"name":             m.Name,
				"group_name":       e.GroupName,
				"status":           m.CurrentStatus,
				"response_time_ms": m.LastResponseTimeMs,
				"uptime_percent":   uptimePercent(ctx, incidentService, m.ID, now.AddDate(0, 0, -30), now),
				"position":         e.Position,
			})
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"page":     page,
			"monitors": monitorsResp,
		})
	}
}

// UpdateStatusPageHandler handles PUT /api/v1/status-pages/:slug.
func UpdateStatusPageHandler(statusPageService *services.StatusPageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")

		var updates models.StatusPage
		if err := c.ShouldBindJSON(&updates); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		updated, err := statusPageService.UpdateStatusPage(c.Request.Context(), slug, &updates)
		if err != nil {
			respondError(c, classifyStatusPageError(err), err.Error())
			return
		}

		log.Printf("Status page updated: %s", slug)
		respondSuccess(c, http.StatusOK, updated)
	}
}

// DeleteStatusPageHandler handles DELETE /api/v1/status-pages/:slug.
func DeleteStatusPageHandler(statusPageService *services.StatusPageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")

		if err := statusPageService.DeleteStatusPage(c.Request.Context(), slug); err != nil {
			respondError(c, classifyStatusPageError(err), err.Error())
			return
		}

		log.Printf("Status page deleted: %s", slug)
		c.Status(http.StatusNoContent)
	}
}

// addMonitorRequest is the body for AddMonitorToPageHandler.
type addMonitorRequest struct {
	MonitorID string `json:"monitor_id"`
	GroupName string `json:"group_name"`
	Position  int    `json:"position"`
}

// AddMonitorToPageHandler handles POST /api/v1/status-pages/:slug/monitors.
func AddMonitorToPageHandler(statusPageService *services.StatusPageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")

		var req addMonitorRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		monitorID, err := uuid.Parse(req.MonitorID)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid monitor_id: must be a UUID")
			return
		}

		if err := statusPageService.AddMonitorToPage(c.Request.Context(), slug, monitorID, req.GroupName, req.Position); err != nil {
			respondError(c, classifyStatusPageError(err), err.Error())
			return
		}

		log.Printf("Monitor added to status page %s", slug)
		respondSuccess(c, http.StatusCreated, gin.H{
			"message":    "Monitor added to status page",
			"monitor_id": monitorID,
		})
	}
}

// RemoveMonitorFromPageHandler handles DELETE
// /api/v1/status-pages/:slug/monitors/:monitor_id.
func RemoveMonitorFromPageHandler(statusPageService *services.StatusPageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")
		monitorID, err := uuid.Parse(c.Param("monitor_id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid monitor_id: must be a UUID")
			return
		}

		if err := statusPageService.RemoveMonitorFromPage(c.Request.Context(), slug, monitorID); err != nil {
			respondError(c, classifyStatusPageError(err), err.Error())
			return
		}

		log.Printf("Monitor removed from status page %s", slug)
		c.Status(http.StatusNoContent)
	}
}

// updatePositionRequest is the body for UpdateMonitorPositionHandler.
type updatePositionRequest struct {
	Position int `json:"position"`
}

// UpdateMonitorPositionHandler handles PATCH
// /api/v1/status-pages/:slug/monitors/:monitor_id/position.
func UpdateMonitorPositionHandler(statusPageService *services.StatusPageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")
		monitorID, err := uuid.Parse(c.Param("monitor_id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid monitor_id: must be a UUID")
			return
		}

		var req updatePositionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		if req.Position < 1 {
			respondError(c, http.StatusBadRequest, "position must be a positive integer")
			return
		}

		if err := statusPageService.UpdateMonitorPosition(c.Request.Context(), slug, monitorID, req.Position); err != nil {
			respondError(c, classifyStatusPageError(err), err.Error())
			return
		}

		log.Printf("Monitor position updated on status page %s", slug)
		respondSuccess(c, http.StatusOK, gin.H{"message": "Monitor position updated"})
	}
}

// GetPublicStatusPageHandler handles GET /public/status/:slug. This endpoint is
// public and requires no authentication. Unpublished pages return 404 so their
// existence is not leaked.
func GetPublicStatusPageHandler(statusPageService *services.StatusPageService, incidentService *services.IncidentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")
		ctx := c.Request.Context()

		page, err := statusPageService.GetStatusPageBySlug(ctx, slug)
		if err != nil {
			respondError(c, classifyStatusPageError(err), err.Error())
			return
		}
		if !page.Published {
			respondError(c, http.StatusNotFound, "status page not found")
			return
		}

		entries, monitors, err := statusPageService.GetPageMonitors(ctx, slug)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		groupByMonitor := make(map[uuid.UUID]string, len(entries))
		for _, e := range entries {
			groupByMonitor[e.MonitorID] = e.GroupName
		}

		now := time.Now().UTC()
		var online, offline int
		monitorsResp := make([]gin.H, 0, len(monitors))
		for _, m := range monitors {
			switch m.CurrentStatus {
			case models.StatusOnline:
				online++
			case models.StatusOffline:
				offline++
			}

			var lastCheck interface{}
			if m.LastCheckAt != nil {
				lastCheck = m.LastCheckAt.UTC().Format(time.RFC3339)
			}

			// Recent incidents (up to 5) over the last 90 days.
			recent := make([]gin.H, 0, 5)
			if incs, err := incidentService.GetIncidents(ctx, m.ID, now.AddDate(0, 0, -90), now); err == nil {
				for i, inc := range incs {
					if i >= 5 {
						break
					}
					var endVal interface{}
					if inc.EndTime != nil {
						endVal = inc.EndTime.UTC().Format(time.RFC3339)
					}
					recent = append(recent, gin.H{
						"start":            inc.StartTime.UTC().Format(time.RFC3339),
						"end":              endVal,
						"duration_minutes": inc.DurationSeconds / 60,
					})
				}
			}

			monitorsResp = append(monitorsResp, gin.H{
				"id":               m.ID,
				"name":             m.Name,
				"group":            groupByMonitor[m.ID],
				"status":           m.CurrentStatus,
				"last_check":       lastCheck,
				"response_time_ms": m.LastResponseTimeMs,
				"uptime": gin.H{
					"last_7_days":  uptimePercent(ctx, incidentService, m.ID, now.AddDate(0, 0, -7), now),
					"last_30_days": uptimePercent(ctx, incidentService, m.ID, now.AddDate(0, 0, -30), now),
					"last_90_days": uptimePercent(ctx, incidentService, m.ID, now.AddDate(0, 0, -90), now),
				},
				"recent_incidents": recent,
			})
		}

		log.Printf("Public status page viewed: %s", slug)
		respondSuccess(c, http.StatusOK, gin.H{
			"page": gin.H{
				"name":        page.Name,
				"description": page.Description,
				"logo_url":    page.LogoURL,
				"theme_color": page.ThemeColor,
				"updated_at":  page.UpdatedAt.UTC().Format(time.RFC3339),
			},
			"monitors": monitorsResp,
			"summary": gin.H{
				"total_monitors": len(monitors),
				"online":         online,
				"offline":        offline,
				"last_updated":   now.Format(time.RFC3339),
			},
		})
	}
}

// RegisterStatusPageRoutes mounts the admin status-page routes under the given
// group's /status-pages path (so auth middleware on the group applies to them).
func RegisterStatusPageRoutes(rg *gin.RouterGroup, statusPageService *services.StatusPageService, incidentService *services.IncidentService) {
	pages := rg.Group("/status-pages")
	pages.POST("", CreateStatusPageHandler(statusPageService))
	pages.GET("", GetStatusPagesHandler(statusPageService))
	pages.GET("/:slug", GetStatusPageHandler(statusPageService, incidentService))
	pages.PUT("/:slug", UpdateStatusPageHandler(statusPageService))
	pages.DELETE("/:slug", DeleteStatusPageHandler(statusPageService))
	pages.POST("/:slug/monitors", AddMonitorToPageHandler(statusPageService))
	pages.DELETE("/:slug/monitors/:monitor_id", RemoveMonitorFromPageHandler(statusPageService))
	pages.PATCH("/:slug/monitors/:monitor_id/position", UpdateMonitorPositionHandler(statusPageService))
}

// RegisterPublicStatusRoutes mounts the unauthenticated public status page at
// /public/status/:slug directly on the engine.
func RegisterPublicStatusRoutes(router *gin.Engine, statusPageService *services.StatusPageService, incidentService *services.IncidentService) {
	router.GET("/public/status/:slug", GetPublicStatusPageHandler(statusPageService, incidentService))
}
