package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type portStore interface {
	DevicePorts(ctx context.Context, d *services.DeviceView) (*services.DevicePortsView, error)
	Port(ctx context.Context, d *services.DeviceView, ifIndex int) (*services.PortDetailView, error)
	UpdatePort(ctx context.Context, deviceID uuid.UUID, ifIndex int, p models.PortPatch) (*models.DeviceInterface, *models.DeviceInterface, error)
	Events(ctx context.Context, f services.PortEventFilter) ([]services.PortEventView, int64, error)
	SiteSummary(ctx context.Context, siteID uuid.UUID) (*services.SitePortSummary, error)
	PhysicalInterfaceIDs(ctx context.Context, deviceIDs []uuid.UUID) ([]uuid.UUID, error)
}

// RegisterPortRoutes mounts the port, event and site summary routes. Access
// is the device's (or site's) SiteAccess: readonly to read, editable to
// change, and a device or site the caller cannot see is a 404.
func RegisterPortRoutes(rg *gin.RouterGroup, devices deviceStore, ports portStore, sites siteAccessChecker, audit auditRecorder) {
	rg.GET("/devices/:id/ports", devicePortsHandler(devices, ports, sites))
	rg.GET("/devices/:id/ports/:ifIndex", portHandler(devices, ports, sites))
	rg.PATCH("/devices/:id/ports/:ifIndex", updatePortHandler(devices, ports, sites, audit))
	rg.GET("/devices/:id/events", deviceEventsHandler(devices, ports, sites))
	rg.GET("/sites/:id/port-events", siteEventsHandler(ports, sites))
	rg.GET("/sites/:id/ports/summary", siteSummaryHandler(ports, sites))
}

func respondPortError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrPortNotFound):
		respondError(c, http.StatusNotFound, "port not found")
	case isInternal(err):
		respondInternal(c, op, err)
	default: // validation message from PortPatch
		respondError(c, http.StatusBadRequest, err.Error())
	}
}

func parseIfIndex(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("ifIndex"))
	if err != nil || n < 1 {
		respondError(c, http.StatusBadRequest, "invalid port")
		return 0, false
	}
	return n, true
}

func pageParams(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	return page, limit
}

func devicePortsHandler(devices deviceStore, ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		v, err := ports.DevicePorts(c.Request.Context(), d)
		if err != nil {
			respondInternal(c, "devicePorts", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	}
}

func portHandler(devices deviceStore, ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		idx, ok := parseIfIndex(c)
		if !ok {
			return
		}
		v, err := ports.Port(c.Request.Context(), d, idx)
		if err != nil {
			respondPortError(c, "port", err)
			return
		}
		respondSuccess(c, http.StatusOK, v)
	}
}

func portAudit(i *models.DeviceInterface) map[string]any {
	return map[string]any{"if_index": i.IfIndex, "important": i.Important, "collect": i.Collect,
		"util_threshold_pct": i.UtilThresholdPct, "error_threshold_per_min": i.ErrorThresholdPerMin,
		"down_grace_seconds": i.DownGraceSeconds}
}

func updatePortHandler(devices deviceStore, ports portStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		idx, ok := parseIfIndex(c)
		if !ok {
			return
		}
		var p models.PortPatch
		if err := c.ShouldBindJSON(&p); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		before, after, err := ports.UpdatePort(c.Request.Context(), d.ID, idx, p)
		if err != nil {
			respondPortError(c, "updatePort", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceUpdated, models.ResourceDevice, &d.ID,
			models.AuditChanges{Before: portAudit(before), After: portAudit(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deviceEventsHandler(devices deviceStore, ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		f := services.PortEventFilter{DeviceID: &d.ID}
		if raw := c.Query("if_index"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 {
				respondError(c, http.StatusBadRequest, "if_index must be a port number")
				return
			}
			f.IfIndex = &n
		}
		f.Page, f.Limit = pageParams(c)
		respondEvents(c, ports, f)
	}
}

func siteEventsHandler(ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, id, services.SiteAccessReadonly) {
			return
		}
		f := services.PortEventFilter{SiteID: &id}
		f.Page, f.Limit = pageParams(c)
		respondEvents(c, ports, f)
	}
}

func respondEvents(c *gin.Context, ports portStore, f services.PortEventFilter) {
	rows, total, err := ports.Events(c.Request.Context(), f)
	if err != nil {
		respondInternal(c, "portEvents", err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"events": rows, "total": total})
}

func siteSummaryHandler(ports portStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, id, services.SiteAccessReadonly) {
			return
		}
		s, err := ports.SiteSummary(c.Request.Context(), id)
		if err != nil {
			respondInternal(c, "siteSummary", err)
			return
		}
		respondSuccess(c, http.StatusOK, s)
	}
}
