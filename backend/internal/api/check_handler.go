package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

const (
	defaultCheckLimit = 100
	maxCheckLimit     = 1000
	// maxCheckFetch bounds how many recent checks are pulled from the store
	// before in-memory date filtering and pagination are applied.
	maxCheckFetch      = 1000
	defaultReportRange = 30 * 24 * time.Hour
)

// verifyMonitorExists returns true if the monitor exists; otherwise it writes an
// appropriate error response (404 when not found) and returns false.
func verifyMonitorExists(c *gin.Context, monitorService *services.MonitorService, id uuid.UUID) bool {
	if _, err := monitorService.GetMonitor(c.Request.Context(), id); err != nil {
		respondError(c, classifyServiceError(err), err.Error())
		return false
	}
	return true
}

// parseOptionalTime parses an RFC3339 query parameter. It returns ok=false when
// the parameter is absent, and an error when present but unparseable.
func parseOptionalTime(c *gin.Context, key string) (t time.Time, ok bool, err error) {
	v := c.Query(key)
	if v == "" {
		return time.Time{}, false, nil
	}
	t, err = time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

// parseTimeRange resolves start_time/end_time query params, defaulting to the
// last 30 days when absent.
func parseTimeRange(c *gin.Context) (start, end time.Time, err error) {
	end = time.Now().UTC()
	start = end.Add(-defaultReportRange)

	if t, ok, e := parseOptionalTime(c, "start_time"); e != nil {
		return start, end, e
	} else if ok {
		start = t
	}
	if t, ok, e := parseOptionalTime(c, "end_time"); e != nil {
		return start, end, e
	} else if ok {
		end = t
	}
	return start, end, nil
}

// GetMonitorChecksHandler handles GET /api/v1/monitors/:id/checks.
func GetMonitorChecksHandler(checkService *services.CheckService, monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}
		if !verifyMonitorExists(c, monitorService, id) {
			return
		}

		limit := queryInt(c, "limit", defaultCheckLimit)
		if limit < 1 {
			limit = defaultCheckLimit
		}
		if limit > maxCheckLimit {
			limit = maxCheckLimit
		}
		offset := queryInt(c, "offset", 0)
		if offset < 0 {
			offset = 0
		}

		start, hasStart, err := parseOptionalTime(c, "start_time")
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid start_time: must be RFC3339")
			return
		}
		end, hasEnd, err := parseOptionalTime(c, "end_time")
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid end_time: must be RFC3339")
			return
		}

		checks, err := checkService.GetRecentChecks(c.Request.Context(), id, maxCheckFetch)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		// Apply an optional date-range filter when both bounds are provided.
		if hasStart && hasEnd {
			filtered := make([]models.Check, 0, len(checks))
			for _, ch := range checks {
				if !ch.Timestamp.Before(start) && !ch.Timestamp.After(end) {
					filtered = append(filtered, ch)
				}
			}
			checks = filtered
		}

		total := len(checks)
		pageChecks := []models.Check{}
		if offset < total {
			endIdx := offset + limit
			if endIdx > total {
				endIdx = total
			}
			pageChecks = checks[offset:endIdx]
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"checks": pageChecks,
			"pagination": gin.H{
				"limit":  limit,
				"offset": offset,
				"total":  total,
			},
		})
	}
}

// GetMonitorIncidentsHandler handles GET /api/v1/monitors/:id/incidents.
func GetMonitorIncidentsHandler(incidentService *services.IncidentService, monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}
		if !verifyMonitorExists(c, monitorService, id) {
			return
		}

		start, end, err := parseTimeRange(c)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid time range: start_time/end_time must be RFC3339")
			return
		}
		if end.Before(start) {
			respondError(c, http.StatusBadRequest, "end_time must be after start_time")
			return
		}

		incidents, err := incidentService.GetIncidents(c.Request.Context(), id, start, end)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"incidents": incidents,
			"count":     len(incidents),
			"range": gin.H{
				"start_time": start.UTC().Format(time.RFC3339),
				"end_time":   end.UTC().Format(time.RFC3339),
			},
		})
	}
}

// RegisterCheckRoutes mounts the check/incident endpoints under /monitors/:id.
func RegisterCheckRoutes(rg *gin.RouterGroup, checkService *services.CheckService, incidentService *services.IncidentService, monitorService *services.MonitorService) {
	monitors := rg.Group("/monitors")
	monitors.GET("/:id/checks", GetMonitorChecksHandler(checkService, monitorService))
	monitors.GET("/:id/incidents", GetMonitorIncidentsHandler(incidentService, monitorService))
}
