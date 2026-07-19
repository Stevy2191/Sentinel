package api

import (
	"math"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// round2 rounds a float to two decimal places.
func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// GetMonitorReportHandler handles GET /api/v1/monitors/:id/report, producing an
// uptime/SLA report over a date range (default: last 30 days).
//
// Note: the status breakdown and average response time load all checks in the
// range via GetChecksInRange. For very long ranges on high-frequency monitors,
// add DB-level status aggregation (GROUP BY status, AVG) to CheckService.
func GetMonitorReportHandler(
	monitorService *services.MonitorService,
	checkService *services.CheckService,
	incidentService *services.IncidentService,
) gin.HandlerFunc {
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

		start, end, err := parseTimeRange(c)
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid time range: start_time/end_time must be RFC3339")
			return
		}
		if end.Before(start) {
			respondError(c, http.StatusBadRequest, "end_time must be after start_time")
			return
		}

		ctx := c.Request.Context()

		downtimePct, err := incidentService.GetDowntimePercentage(ctx, id, start, end)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		totalDowntime, err := incidentService.GetIncidentDuration(ctx, id, start, end)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		incidentCount, err := incidentService.GetIncidentCount(ctx, id, start, end)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		// Total count comes straight from the database for the exact range, so
		// it is accurate regardless of how many checks exist (not capped).
		totalChecks, err := checkService.CountChecks(ctx, id, start, end)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		// Pull every check within the range (limit 0 = no limit) to compute the
		// status breakdown and average response time accurately over the full
		// range rather than only the most recent checks.
		rangeChecks, err := checkService.GetChecksInRange(ctx, id, start, end, 0, 0)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		var success, failed, timeout, sumResponse int
		for _, ch := range rangeChecks {
			switch ch.Status {
			case "success":
				success++
				sumResponse += ch.ResponseTimeMs
			case "failed":
				failed++
			case "timeout":
				timeout++
			}
		}
		avgResponse := 0
		if success > 0 {
			avgResponse = sumResponse / success
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"monitor": gin.H{
				"id":             monitor.ID,
				"name":           monitor.Name,
				"url":            monitor.URL,
				"current_status": monitor.CurrentStatus,
			},
			"range": gin.H{
				"start_time": start.UTC().Format(time.RFC3339),
				"end_time":   end.UTC().Format(time.RFC3339),
			},
			"uptime": gin.H{
				"uptime_percentage":      round2(100 - downtimePct),
				"downtime_percentage":    round2(downtimePct),
				"total_downtime_seconds": int(totalDowntime.Seconds()),
				"incident_count":         incidentCount,
			},
			"checks": gin.H{
				"total":                totalChecks,
				"success":              success,
				"failed":               failed,
				"timeout":              timeout,
				"avg_response_time_ms": avgResponse,
			},
		})
	}
}

// RegisterReportRoutes mounts the reporting endpoint under /monitors/:id.
func RegisterReportRoutes(
	rg *gin.RouterGroup,
	monitorService *services.MonitorService,
	checkService *services.CheckService,
	incidentService *services.IncidentService,
) {
	monitors := rg.Group("/monitors")
	monitors.GET("/:id/report", GetMonitorReportHandler(monitorService, checkService, incidentService))
}
