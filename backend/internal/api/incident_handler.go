package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// validSeverities mirrors the CHECK constraint on incidents.severity, so a bad
// value returns a clear 400 rather than a database constraint error.
var validSeverities = map[string]bool{
	"low": true, "medium": true, "high": true, "critical": true,
}

// updateIncidentRequest carries the human-authored context an operator adds to
// an incident after the fact. Every field is optional; only those present are
// applied.
type updateIncidentRequest struct {
	RootCause       *string `json:"root_cause"`
	ResolutionNotes *string `json:"resolution_notes"`
	Notes           *string `json:"notes"`
	Severity        *string `json:"severity"`
	// Status is "ongoing" or "resolved". The incidents table has no status
	// column - status is derived from end_time - so setting it closes or
	// reopens the incident rather than writing a field.
	Status *string `json:"status"`
}

// UpdateIncidentHandler handles PATCH /api/v1/incidents/:id, letting an operator
// annotate an incident with root cause and resolution notes for reporting.
func UpdateIncidentHandler(incidentService *services.IncidentService, monitorService *services.MonitorService, db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		incidentID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid incident id")
			return
		}

		var req updateIncidentRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		ctx := c.Request.Context()
		var incident models.Incident
		if err := db.WithContext(ctx).First(&incident, "id = ?", incidentID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(c, http.StatusNotFound, "incident not found")
				return
			}
			respondInternal(c, "loading incident", err)
			return
		}

		// An incident inherits its monitor's permissions: editing the annotation
		// requires edit rights on the monitor it belongs to.
		if !authorizeMonitor(c, monitorService, incident.MonitorID, "edit") {
			return
		}

		if req.Severity != nil && !validSeverities[*req.Severity] {
			respondError(c, http.StatusBadRequest, "severity must be one of: low, medium, high, critical")
			return
		}

		updates := map[string]interface{}{"updated_at": time.Now()}
		if req.RootCause != nil {
			updates["root_cause"] = *req.RootCause
		}
		if req.ResolutionNotes != nil {
			updates["resolution_notes"] = *req.ResolutionNotes
		}
		if req.Notes != nil {
			updates["notes"] = *req.Notes
		}
		if req.Severity != nil {
			updates["severity"] = *req.Severity
		}

		if req.Status != nil {
			switch *req.Status {
			case "resolved":
				// Closing an open incident stamps the end time and duration,
				// matching what IncidentService.CloseIncident records.
				if incident.EndTime == nil {
					now := time.Now()
					updates["end_time"] = now
					updates["duration_seconds"] = int(now.Sub(incident.StartTime).Seconds())
				}
			case "ongoing":
				updates["end_time"] = nil
				updates["duration_seconds"] = 0
			default:
				respondError(c, http.StatusBadRequest, "status must be \"ongoing\" or \"resolved\"")
				return
			}
		}

		if len(updates) == 1 { // only updated_at
			respondError(c, http.StatusBadRequest, "no updatable fields provided")
			return
		}

		// An explicit map, so clearing end_time back to NULL is actually written
		// (a struct update would skip the zero value).
		if err := db.WithContext(ctx).Model(&models.Incident{}).
			Where("id = ?", incidentID).Updates(updates).Error; err != nil {
			respondInternal(c, "updating incident", err)
			return
		}

		var updated models.Incident
		if err := db.WithContext(ctx).First(&updated, "id = ?", incidentID).Error; err != nil {
			respondInternal(c, "reloading incident", err)
			return
		}
		respondSuccess(c, http.StatusOK, incidentResponse{Incident: updated, Status: updated.Status()})
	}
}

// incidentResponse adds the derived status alongside the stored incident, since
// Status is a method and would not otherwise appear in the JSON.
type incidentResponse struct {
	models.Incident
	Status string `json:"status"`
}

// incidentView is a listed incident with the values the table needs computed
// once here, rather than each client re-deriving them slightly differently.
type incidentView struct {
	services.IncidentWithMonitor
	Status          string `json:"status"`
	DurationSeconds int    `json:"duration_seconds"`
}

func toIncidentView(row services.IncidentWithMonitor, now time.Time) incidentView {
	inc := row.Incident
	return incidentView{
		IncidentWithMonitor: row,
		Status:              inc.Status(),
		// An ongoing incident is measured to now, so the number keeps moving
		// instead of sitting at the zero it was stored with.
		DurationSeconds: int(inc.Duration(now).Seconds()),
	}
}

// ListIncidentsHandler handles GET /api/v1/incidents.
func ListIncidentsHandler(incidentService *services.IncidentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		opts := services.IncidentListOptions{
			Page:   atoiOr(c.Query("page"), 1),
			Limit:  atoiOr(c.Query("limit"), 50),
			Search: c.Query("search"),
			SortBy: c.DefaultQuery("sort", "started"),
			// Newest first is what an incident list is for.
			Desc: c.DefaultQuery("order", "desc") != "asc",
		}

		switch st := c.DefaultQuery("status", "all"); st {
		case "", "all":
		case models.IncidentStatusOngoing, models.IncidentStatusResolved:
			opts.Status = st
		default:
			respondError(c, http.StatusBadRequest, "status must be all, ongoing, or resolved")
			return
		}

		if raw := c.Query("monitor_id"); raw != "" && raw != "null" {
			id, err := uuid.Parse(raw)
			if err != nil {
				respondError(c, http.StatusBadRequest, "monitor_id must be a UUID")
				return
			}
			opts.MonitorID = &id
		}
		if opts.SortBy != "started" && opts.SortBy != "duration" {
			respondError(c, http.StatusBadRequest, "sort must be started or duration")
			return
		}

		rows, total, err := incidentService.ListIncidents(c.Request.Context(), opts)
		if err != nil {
			respondInternal(c, "ListIncidentsHandler", err)
			return
		}

		now := time.Now()
		out := make([]incidentView, 0, len(rows))
		for _, r := range rows {
			out = append(out, toIncidentView(r, now))
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"total":     total,
			"page":      opts.Page,
			"limit":     opts.Limit,
			"incidents": out,
		})
	}
}

// GetIncidentHandler handles GET /api/v1/incidents/:id, including the checks
// recorded while the incident was open.
func GetIncidentHandler(incidentService *services.IncidentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "incident id must be a UUID")
			return
		}
		row, err := incidentService.GetIncidentByID(c.Request.Context(), id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				respondError(c, http.StatusNotFound, "no such incident")
				return
			}
			respondInternal(c, "GetIncidentHandler", err)
			return
		}

		checks, err := incidentService.ChecksDuringIncident(c.Request.Context(), &row.Incident, 200)
		if err != nil {
			// The incident itself is still worth returning without its
			// timeline; failing the whole request would be worse.
			checks = nil
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"incident":     toIncidentView(*row, time.Now()),
			"checks":       checks,
			"total_checks": len(checks),
		})
	}
}

// atoiOr parses a query integer, falling back when absent or unparseable.
func atoiOr(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

// ---- retention setting ----------------------------------------------------

// retentionRequest is the body for PATCH /settings/incident-retention-days.
type retentionRequest struct {
	Days *int `json:"days"`
}

// GetIncidentRetentionHandler handles GET /api/v1/settings/incident-retention-days.
func GetIncidentRetentionHandler(settingsService *services.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		respondSuccess(c, http.StatusOK, gin.H{
			"days": settingsService.IncidentRetentionDays(c.Request.Context()),
			"min":  models.MinIncidentRetentionDays,
			"max":  models.MaxIncidentRetentionDays,
		})
	}
}

// UpdateIncidentRetentionHandler handles PATCH /api/v1/settings/incident-retention-days.
func UpdateIncidentRetentionHandler(settingsService *services.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req retentionRequest
		if err := c.ShouldBindJSON(&req); err != nil || req.Days == nil {
			respondError(c, http.StatusBadRequest, "request body must include an integer \"days\" field")
			return
		}
		if *req.Days < models.MinIncidentRetentionDays || *req.Days > models.MaxIncidentRetentionDays {
			respondError(c, http.StatusBadRequest, fmt.Sprintf(
				"days must be between %d and %d",
				models.MinIncidentRetentionDays, models.MaxIncidentRetentionDays))
			return
		}
		if err := settingsService.SetInt(c.Request.Context(), models.SettingIncidentRetentionDays, *req.Days); err != nil {
			respondInternal(c, "UpdateIncidentRetentionHandler", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{
			"days":    *req.Days,
			"message": "Incident retention updated; the next nightly purge will use it",
		})
	}
}

// RegisterIncidentRoutes mounts the incident endpoints. Reading is open to any
// authenticated user, as monitors are; annotating one stays where it was.
func RegisterIncidentRoutes(rg *gin.RouterGroup, incidentService *services.IncidentService, monitorService *services.MonitorService, db *gorm.DB) {
	rg.GET("/incidents", ListIncidentsHandler(incidentService))
	rg.GET("/incidents/:id", GetIncidentHandler(incidentService))
	rg.PATCH("/incidents/:id", UpdateIncidentHandler(incidentService, monitorService, db))
}
