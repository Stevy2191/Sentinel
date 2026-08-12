package api

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Guardrails for the two assisted creation flows.
const (
	// maxTestTimeoutSeconds bounds how long an unsaved-config test may run, so a
	// caller cannot hold a request open for minutes at a time.
	maxTestTimeoutSeconds = 30
	// maxBulkMonitors caps one import so a single request cannot be used to
	// create an unbounded number of rows.
	maxBulkMonitors = 500
)

// testConfigRequest is a monitor configuration to probe without saving it.
type testConfigRequest struct {
	Type           string            `json:"type"`
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	Body           string            `json:"body"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}

// TestMonitorConfigHandler handles POST /api/v1/monitors/test-config. It runs a
// single real check against a configuration that has not been saved and returns
// the outcome, so the creation wizard can tell the user whether a target
// responds before they commit to monitoring it.
//
// Nothing is persisted: no monitor row, and no entry in check history.
//
// This lets an authenticated user make the server probe an address of their
// choosing. That is not a new capability — anyone who can create a monitor can
// already cause exactly the same request — and reaching internal hosts is the
// product's purpose, so private ranges are deliberately not blocked.
func TestMonitorConfigHandler(checkService *services.CheckService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req testConfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}

		timeout := req.TimeoutSeconds
		if timeout <= 0 {
			timeout = 10
		}
		if timeout > maxTestTimeoutSeconds {
			timeout = maxTestTimeoutSeconds
		}

		// A transient monitor, never saved. Name and interval are placeholders
		// that satisfy Validate; only type, url and timeout affect the probe.
		probe := &models.Monitor{
			Name:            "connection test",
			Type:            req.Type,
			URL:             req.URL,
			Method:          req.Method,
			Headers:         models.StringMap(req.Headers),
			Body:            req.Body,
			IntervalSeconds: maxTestTimeoutSeconds + 1,
			TimeoutSeconds:  timeout,
		}
		if err := probe.Validate(); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}

		check, err := checkService.ExecuteCheck(c.Request.Context(), probe)
		if err != nil {
			respondError(c, http.StatusInternalServerError, err.Error())
			return
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"status":           check.Status, // success | failed | timeout
			"ok":               check.Status == "success",
			"response_time_ms": check.ResponseTimeMs,
			"status_code":      check.StatusCode,
			"error_message":    check.ErrorMessage,
			"checked_at":       time.Now().UTC().Format(time.RFC3339),
		})
	}
}

// bulkCreateRequest is a batch of monitors to validate and optionally create.
type bulkCreateRequest struct {
	Monitors []models.Monitor `json:"monitors"`
	// DryRun validates the batch and reports per-row verdicts without creating
	// anything, which is what powers the import preview.
	DryRun bool `json:"dry_run"`
}

// bulkRowResult is the verdict for one row of the batch, keyed by its position
// in the submitted list so the client can line results up with its own rows.
type bulkRowResult struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Valid   bool   `json:"valid"`
	Created bool   `json:"created"`
	ID      string `json:"id,omitempty"`
	Error   string `json:"error,omitempty"`
}

// BulkCreateMonitorsHandler handles POST /api/v1/monitors/bulk.
//
// Every row is validated and reported on individually, and a bad row does not
// sink the batch: the valid rows are created and the rest come back with the
// reason they were skipped. A 200-row import should not be lost to one typo.
// The same call with dry_run set validates without writing, so the preview the
// user approves is produced by the same code that performs the import.
func BulkCreateMonitorsHandler(monitorService *services.MonitorService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req bulkCreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		if len(req.Monitors) == 0 {
			respondError(c, http.StatusBadRequest, "no monitors supplied")
			return
		}
		if len(req.Monitors) > maxBulkMonitors {
			respondError(c, http.StatusBadRequest, "too many monitors in one request (max "+itoa(maxBulkMonitors)+")")
			return
		}

		userID, _, _, hasUser := GetUserFromContext(c)
		ctx := c.Request.Context()

		results := make([]bulkRowResult, 0, len(req.Monitors))
		var validCount, createdCount int

		for i := range req.Monitors {
			monitor := req.Monitors[i]
			row := bulkRowResult{Index: i, Name: monitor.Name}

			// Mirror the defaults a single create would apply, so an import only
			// has to carry the columns the user actually cares about.
			if monitor.IntervalSeconds == 0 {
				monitor.IntervalSeconds = 60
			}
			if monitor.TimeoutSeconds == 0 {
				monitor.TimeoutSeconds = 10
			}
			if monitor.Type == "" {
				monitor.Type = models.MonitorTypeHTTP
			}
			monitor.Enabled = true

			if err := monitor.Validate(); err != nil {
				row.Error = err.Error()
				results = append(results, row)
				continue
			}
			row.Valid = true
			validCount++

			if req.DryRun {
				results = append(results, row)
				continue
			}

			if hasUser {
				monitor.OwnerID = &userID
			}
			created, err := monitorService.CreateMonitor(ctx, &monitor)
			if err != nil {
				// Validation already passed, so this is a storage failure for this
				// row alone; the rest of the batch still goes in.
				row.Valid = false
				row.Error = err.Error()
				results = append(results, row)
				continue
			}
			row.Created = true
			row.ID = created.ID.String()
			createdCount++
			results = append(results, row)
		}

		if !req.DryRun {
			log.Printf("Bulk import: %d/%d monitors created", createdCount, len(req.Monitors))
		}

		respondSuccess(c, http.StatusOK, gin.H{
			"dry_run": req.DryRun,
			"total":   len(req.Monitors),
			"valid":   validCount,
			"invalid": len(req.Monitors) - validCount,
			"created": createdCount,
			"results": results,
		})
	}
}

// itoa avoids pulling strconv in for a single message.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// RegisterMonitorCreationRoutes mounts the endpoints backing the guided wizard
// and the CSV import.
func RegisterMonitorCreationRoutes(rg *gin.RouterGroup, monitorService *services.MonitorService, checkService *services.CheckService) {
	monitors := rg.Group("/monitors")
	monitors.POST("/test-config", TestMonitorConfigHandler(checkService))
	monitors.POST("/bulk", BulkCreateMonitorsHandler(monitorService))
}
