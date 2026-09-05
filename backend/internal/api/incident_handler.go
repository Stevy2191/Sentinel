package api

import (
	"errors"
	"net/http"
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

// RegisterIncidentRoutes mounts the incident annotation endpoint.
func RegisterIncidentRoutes(rg *gin.RouterGroup, incidentService *services.IncidentService, monitorService *services.MonitorService, db *gorm.DB) {
	rg.PATCH("/incidents/:id", UpdateIncidentHandler(incidentService, monitorService, db))
}
