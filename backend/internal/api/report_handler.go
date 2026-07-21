package api

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// round2 rounds a float to two decimal places.
func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// displayUptime prevents a monitor that is offline right now from showing a
// perfect 100%. Incident-based uptime is historically correct, but a brief
// ongoing incident can round away over a long window, and a monitor can be
// offline with no recorded incident (e.g. it went down during a maintenance
// window, where incidents are suppressed). When the monitor is currently
// offline, cap the shown uptime just below 100% so active downtime is visible.
func displayUptime(uptimePct float64, currentlyOffline bool) float64 {
	if currentlyOffline && uptimePct >= 100 {
		return 99.99
	}
	return uptimePct
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
		if !authorizeMonitor(c, monitorService, id, "view") {
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

		// Whether the monitor is offline right now, and for how long — so the UI
		// can flag live downtime instead of only reflecting it in the percentage.
		ongoing, currentDowntime, err := incidentService.GetCurrentDowntime(ctx, id)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		// A monitor reported offline right now is "down", even if its ongoing
		// incident hasn't been recorded yet (e.g. suppressed during maintenance).
		currentlyOffline := monitor.CurrentStatus == "offline"
		ongoing = ongoing || currentlyOffline

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
				"uptime_percentage":        displayUptime(round2(100-downtimePct), currentlyOffline),
				"downtime_percentage":      round2(downtimePct),
				"total_downtime_seconds":   int(totalDowntime.Seconds()),
				"incident_count":           incidentCount,
				"ongoing_incident":         ongoing,
				"current_downtime_minutes": round2(currentDowntime.Minutes()),
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

// GetUptimeHistoryHandler handles GET /api/v1/monitors/:id/uptime-history. It
// returns 24h/7d/30d uptime (incident-based, consistent with the other reports),
// a 24-bucket hourly uptime series for sparklines, and a 24-hour hourly response
// time series for the detail chart — all in one request.
func GetUptimeHistoryHandler(
	monitorService *services.MonitorService,
	checkService *services.CheckService,
	incidentService *services.IncidentService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseMonitorID(c)
		if !ok {
			return
		}
		if !authorizeMonitor(c, monitorService, id, "view") {
			return
		}
		monitor, err := monitorService.GetMonitor(c.Request.Context(), id)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}
		currentlyOffline := monitor.CurrentStatus == "offline"
		switch c.DefaultQuery("range", "24h") {
		case "24h", "7d", "30d":
		default:
			respondError(c, http.StatusBadRequest, "range must be 24h, 7d, or 30d")
			return
		}

		ctx := c.Request.Context()
		now := time.Now()

		uptimeOver := func(d time.Duration) float64 {
			down, err := incidentService.GetDowntimePercentage(ctx, id, now.Add(-d), now)
			if err != nil {
				return 100
			}
			return round2(100 - down)
		}

		// Bucket the last 24h of checks by hour (UTC).
		checks, err := checkService.GetChecksInRange(ctx, id, now.Add(-24*time.Hour), now, 0, 0)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}
		type bucket struct{ total, failed, sumResp, respN int }
		buckets := make(map[time.Time]*bucket)
		truncHour := func(t time.Time) time.Time {
			t = t.UTC()
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
		}
		for _, ch := range checks {
			k := truncHour(ch.Timestamp)
			b := buckets[k]
			if b == nil {
				b = &bucket{}
				buckets[k] = b
			}
			b.total++
			if ch.Status == "success" {
				b.sumResp += ch.ResponseTimeMs
				b.respN++
			} else {
				b.failed++
			}
		}

		hourly := make([]gin.H, 0, 24)
		responseData := make([]gin.H, 0, 24)
		curHour := truncHour(now)
		for i := 23; i >= 0; i-- {
			k := curHour.Add(time.Duration(-i) * time.Hour)
			b := buckets[k]
			status := "nodata"
			uptime := 0.0
			avg := 0
			if b != nil && b.total > 0 {
				uptime = round2(float64(b.total-b.failed) / float64(b.total) * 100)
				switch {
				case b.failed == 0:
					status = "up"
				case b.failed == b.total:
					status = "down"
				default:
					status = "partial"
				}
				if b.respN > 0 {
					avg = b.sumResp / b.respN
				}
			}
			hourly = append(hourly, gin.H{"hour": k.Hour(), "uptime": uptime, "status": status})
			responseData = append(responseData, gin.H{"time": fmt.Sprintf("%02d:00", k.Hour()), "responseTime": avg})
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"uptime_24h":         displayUptime(uptimeOver(24*time.Hour), currentlyOffline),
			"uptime_7d":          displayUptime(uptimeOver(7*24*time.Hour), currentlyOffline),
			"uptime_30d":         displayUptime(uptimeOver(30*24*time.Hour), currentlyOffline),
			"hourly_data":        hourly,
			"response_time_data": responseData,
		})
	}
}

// parseReportTimeRange parses required RFC3339 "start"/"end" query params.
func parseReportTimeRange(c *gin.Context) (start, end time.Time, ok bool) {
	s, err := time.Parse(time.RFC3339, c.Query("start"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid or missing 'start': must be RFC3339")
		return time.Time{}, time.Time{}, false
	}
	e, err := time.Parse(time.RFC3339, c.Query("end"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid or missing 'end': must be RFC3339")
		return time.Time{}, time.Time{}, false
	}
	if e.Before(s) {
		respondError(c, http.StatusBadRequest, "'end' must be after 'start'")
		return time.Time{}, time.Time{}, false
	}
	return s, e, true
}

type timelineBucket struct {
	total   int
	failed  int
	sumResp int
	respN   int
}

// GetTimelineReportHandler handles GET /api/v1/reports/timeline, returning a
// bucketed (hourly/daily) uptime + response-time series for one monitor.
func GetTimelineReportHandler(
	checkService *services.CheckService,
	monitorService *services.MonitorService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Query("monitor_id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid or missing 'monitor_id': must be a UUID")
			return
		}
		if !authorizeMonitor(c, monitorService, id, "view") {
			return
		}
		start, end, ok := parseReportTimeRange(c)
		if !ok {
			return
		}
		granularity := c.Query("granularity")
		if granularity == "" {
			granularity = "hourly"
		}
		if granularity != "hourly" && granularity != "daily" {
			respondError(c, http.StatusBadRequest, "'granularity' must be 'hourly' or 'daily'")
			return
		}

		ctx := c.Request.Context()
		monitor, err := monitorService.GetMonitor(ctx, id)
		if err != nil {
			respondError(c, classifyServiceError(err), err.Error())
			return
		}

		checks, err := checkService.GetChecksInRange(ctx, id, start, end, 0, 0)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		// Bucket checks by truncated timestamp (UTC).
		buckets := make(map[time.Time]*timelineBucket)
		truncate := func(t time.Time) time.Time {
			t = t.UTC()
			if granularity == "daily" {
				return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			}
			return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, time.UTC)
		}
		for _, ch := range checks {
			key := truncate(ch.Timestamp)
			b := buckets[key]
			if b == nil {
				b = &timelineBucket{}
				buckets[key] = b
			}
			b.total++
			if ch.Status == "success" {
				b.sumResp += ch.ResponseTimeMs
				b.respN++
			} else {
				b.failed++
			}
		}

		keys := make([]time.Time, 0, len(buckets))
		for k := range buckets {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })

		timeline := make([]gin.H, 0, len(keys))
		for _, k := range keys {
			b := buckets[k]
			uptime := 0.0
			if b.total > 0 {
				uptime = round2(float64(b.total-b.failed) / float64(b.total) * 100)
			}
			avg := 0
			if b.respN > 0 {
				avg = b.sumResp / b.respN
			}
			timeline = append(timeline, gin.H{
				"timestamp":            k.Format(time.RFC3339),
				"uptime_percent":       uptime,
				"avg_response_time_ms": avg,
				"checks_total":         b.total,
				"checks_failed":        b.failed,
			})
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"monitor_id":   monitor.ID,
			"monitor_name": monitor.Name,
			"granularity":  granularity,
			"period": gin.H{
				"start": start.UTC().Format(time.RFC3339),
				"end":   end.UTC().Format(time.RFC3339),
			},
			"timeline": timeline,
		})
	}
}

// GetSummaryReportHandler handles GET /api/v1/reports/summary, returning uptime
// figures for many monitors plus an aggregate.
func GetSummaryReportHandler(
	monitorService *services.MonitorService,
	incidentService *services.IncidentService,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		start, end, ok := parseReportTimeRange(c)
		if !ok {
			return
		}
		ctx := c.Request.Context()

		// Only summarize monitors the user can access (admins see all).
		userID, _, isAdmin, _ := GetUserFromContext(c)
		all, err := monitorService.ListAccessibleMonitors(ctx, userID, isAdmin, nil)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		// Optional monitor_ids filter.
		if raw := strings.TrimSpace(c.Query("monitor_ids")); raw != "" {
			wanted := map[string]bool{}
			for _, part := range strings.Split(raw, ",") {
				if p := strings.TrimSpace(part); p != "" {
					wanted[p] = true
				}
			}
			filtered := all[:0]
			for _, m := range all {
				if wanted[m.ID.String()] {
					filtered = append(filtered, m)
				}
			}
			all = filtered
		}

		monitorsResp := make([]gin.H, 0, len(all))
		var sumUptime float64
		best, worst := 0.0, 100.0
		var totalIncidents int64
		var totalDowntimeMinutes float64

		for i := range all {
			m := all[i]
			downPct, err := incidentService.GetDowntimePercentage(ctx, m.ID, start, end)
			if err != nil {
				downPct = 0
			}
			uptime := displayUptime(round2(100-downPct), m.CurrentStatus == "offline")
			downtime, err := incidentService.GetIncidentDuration(ctx, m.ID, start, end)
			if err != nil {
				downtime = 0
			}
			count, err := incidentService.GetIncidentCount(ctx, m.ID, start, end)
			if err != nil {
				count = 0
			}
			downtimeMinutes := round2(downtime.Minutes())

			sumUptime += uptime
			if uptime > best {
				best = uptime
			}
			if uptime < worst {
				worst = uptime
			}
			totalIncidents += count
			totalDowntimeMinutes += downtimeMinutes

			monitorsResp = append(monitorsResp, gin.H{
				"monitor_id":       m.ID,
				"monitor_name":     m.Name,
				"uptime_percent":   uptime,
				"downtime_minutes": downtimeMinutes,
				"status":           m.CurrentStatus,
			})
		}

		avgUptime := 0.0
		if len(all) > 0 {
			avgUptime = round2(sumUptime / float64(len(all)))
		} else {
			worst = 0
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"period": gin.H{
				"start": start.UTC().Format(time.RFC3339),
				"end":   end.UTC().Format(time.RFC3339),
			},
			"monitors": monitorsResp,
			"aggregate": gin.H{
				"avg_uptime":             avgUptime,
				"best_uptime":            best,
				"worst_uptime":           worst,
				"total_incidents":        totalIncidents,
				"total_downtime_minutes": round2(totalDowntimeMinutes),
			},
		})
	}
}

// RegisterReportRoutes mounts the per-monitor report and the timeline/summary
// report endpoints.
func RegisterReportRoutes(
	rg *gin.RouterGroup,
	monitorService *services.MonitorService,
	checkService *services.CheckService,
	incidentService *services.IncidentService,
) {
	monitors := rg.Group("/monitors")
	monitors.GET("/:id/report", GetMonitorReportHandler(monitorService, checkService, incidentService))
	monitors.GET("/:id/uptime-history", GetUptimeHistoryHandler(monitorService, checkService, incidentService))

	reports := rg.Group("/reports")
	reports.GET("/timeline", GetTimelineReportHandler(checkService, monitorService))
	reports.GET("/summary", GetSummaryReportHandler(monitorService, incidentService))
}
