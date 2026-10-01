package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type deviceStore interface {
	List(ctx context.Context, userID uuid.UUID, isAdmin bool, f services.DeviceFilter) ([]services.DeviceView, error)
	Get(ctx context.Context, id uuid.UUID) (*services.DeviceView, error)
	Create(ctx context.Context, in models.DeviceInput, by uuid.UUID) (*models.Device, error)
	Update(ctx context.Context, id uuid.UUID, in models.DeviceInput) (*models.Device, *models.Device, error)
	Delete(ctx context.Context, id uuid.UUID) (*models.Device, error)
	Interfaces(ctx context.Context, id uuid.UUID, includeAbsent bool) ([]models.DeviceInterface, error)
	RequestRefresh(ctx context.Context, id uuid.UUID) error
	UpdateDetails(ctx context.Context, id uuid.UUID, p models.DeviceDetailsPatch) (*models.Device, *models.Device, error)
}

type deviceProber interface {
	Identify(ctx context.Context, host string, port int, credentialID uuid.UUID, timeout time.Duration, retries int) (snmp.System, error)
	UsableAt(ctx context.Context, credentialID, siteID uuid.UUID) (bool, error)
}

// RegisterDeviceRoutes mounts /devices. Every route is gated on the device's
// site through SiteAccess: readonly to read, editable to change.
func RegisterDeviceRoutes(rg *gin.RouterGroup, devices deviceStore, prober deviceProber, sites siteAccessChecker, audit auditRecorder) {
	g := rg.Group("/devices")
	g.GET("", listDevicesHandler(devices))
	g.POST("", createDeviceHandler(devices, sites, audit))
	// 10 per minute per user: it sends SNMP to an address the caller chooses.
	g.POST("/test", NewRateLimiter(10, time.Minute, 10).Middleware("snmp-test", ByUser), testDeviceHandler(prober, sites))
	g.GET("/:id", getDeviceHandler(devices, sites))
	g.PUT("/:id", updateDeviceHandler(devices, sites, audit))
	g.DELETE("/:id", deleteDeviceHandler(devices, sites, audit))
	g.GET("/:id/interfaces", deviceInterfacesHandler(devices, sites))
	g.POST("/:id/refresh", refreshDeviceHandler(devices, sites))
	g.PATCH("/:id/details", updateDeviceDetailsHandler(devices, sites, audit))
}

func respondDeviceError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrDeviceNotFound):
		respondError(c, http.StatusNotFound, "device not found")
	case errors.Is(err, services.ErrDeviceHostTaken):
		respondError(c, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrCredentialNotUsable), errors.Is(err, services.ErrSiteNotFound),
		errors.Is(err, services.ErrCredentialNotFound):
		respondError(c, http.StatusBadRequest, err.Error())
	case isInternal(err):
		respondInternal(c, op, err)
	default: // validation message from NormalizeDeviceInput
		respondError(c, http.StatusBadRequest, err.Error())
	}
}

// loadDevice resolves :id and checks the caller has at least want on its
// site. A device on a site the caller cannot see is a 404.
func loadDevice(c *gin.Context, devices deviceStore, sites siteAccessChecker, want services.SiteAccessLevel) (*services.DeviceView, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid device id")
		return nil, false
	}
	d, err := devices.Get(c.Request.Context(), id)
	if err != nil {
		respondDeviceError(c, "loadDevice", err)
		return nil, false
	}
	if !requireSiteLevel(c, sites, d.SiteID, want) {
		return nil, false
	}
	return d, true
}

func deviceAudit(d *models.Device) map[string]any {
	return map[string]any{"name": d.Name, "host": d.Host, "port": d.Port, "site_id": d.SiteID, "enabled": d.Enabled}
}

func listDevicesHandler(devices deviceStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, isAdmin, _ := GetUserFromContext(c)
		f := services.DeviceFilter{Status: c.Query("status")}
		if raw := c.Query("site_id"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				respondError(c, http.StatusBadRequest, "site_id must be a UUID")
				return
			}
			f.SiteID = &id
		}
		list, err := devices.List(c.Request.Context(), userID, isAdmin, f)
		if err != nil {
			respondInternal(c, "listDevices", err)
			return
		}
		if list == nil {
			list = []services.DeviceView{}
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func getDeviceHandler(devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		respondSuccess(c, http.StatusOK, d)
	}
}

func createDeviceHandler(devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in models.DeviceInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if !requireSiteLevel(c, sites, in.SiteID, services.SiteAccessEditable) {
			return
		}
		userID, _, _, _ := GetUserFromContext(c)
		d, err := devices.Create(c.Request.Context(), in, userID)
		if err != nil {
			respondDeviceError(c, "createDevice", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceCreated, models.ResourceDevice, &d.ID,
			models.AuditChanges{Summary: deviceAudit(d)})
		respondSuccess(c, http.StatusCreated, d)
	}
}

func updateDeviceHandler(devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		var in models.DeviceInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		// Retries of 0 in a request means "default" (1); see NormalizeDeviceInput.
		if in.SiteID == uuid.Nil {
			in.SiteID = d.SiteID
		}
		if in.SiteID != d.SiteID && !requireSiteLevel(c, sites, in.SiteID, services.SiteAccessEditable) {
			return
		}
		before, after, err := devices.Update(c.Request.Context(), d.ID, in)
		if err != nil {
			respondDeviceError(c, "updateDevice", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceUpdated, models.ResourceDevice, &d.ID,
			models.AuditChanges{Before: deviceAudit(before), After: deviceAudit(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteDeviceHandler(devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		deleted, err := devices.Delete(c.Request.Context(), d.ID)
		if err != nil {
			respondDeviceError(c, "deleteDevice", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceDeleted, models.ResourceDevice, &d.ID,
			models.AuditChanges{Summary: deviceAudit(deleted)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func deviceInterfacesHandler(devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		list, err := devices.Interfaces(c.Request.Context(), d.ID, c.Query("include_absent") == "true")
		if err != nil {
			respondInternal(c, "deviceInterfaces", err)
			return
		}
		if list == nil {
			list = []models.DeviceInterface{}
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func refreshDeviceHandler(devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		if err := devices.RequestRefresh(c.Request.Context(), d.ID); err != nil {
			respondInternal(c, "refreshDevice", err)
			return
		}
		respondSuccess(c, http.StatusAccepted, gin.H{"queued": true})
	}
}

func deviceDetailsAudit(d *models.Device) map[string]any {
	return map[string]any{"vendor_override": d.VendorOverride, "model_override": d.ModelOverride,
		"location_override": d.LocationOverride, "device_type": d.DeviceType,
		"faceplate_rows": d.FaceplateRows, "faceplate_sfp_ports": d.FaceplateSFPPorts,
		"faceplate_port_style": d.FaceplatePortStyle,
		"ups_low_battery_pct":  d.UPSLowBatteryPct, "ups_high_load_pct": d.UPSHighLoadPct}
}

// updateDeviceDetailsHandler handles PATCH /devices/:id/details: the user's
// overrides of what SNMP reported, which inventory never overwrites.
func updateDeviceDetailsHandler(devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		var p models.DeviceDetailsPatch
		if err := c.ShouldBindJSON(&p); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		before, after, err := devices.UpdateDetails(c.Request.Context(), d.ID, p)
		if err != nil {
			respondDeviceError(c, "updateDeviceDetails", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceUpdated, models.ResourceDevice, &d.ID,
			models.AuditChanges{Before: deviceDetailsAudit(before), After: deviceDetailsAudit(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

type testDeviceRequest struct {
	SiteID       uuid.UUID `json:"site_id"`
	CredentialID uuid.UUID `json:"credential_id"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
}

// testDeviceHandler reads a device's identity before it is saved.
func testDeviceHandler(prober deviceProber, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req testDeviceRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if !requireSiteLevel(c, sites, req.SiteID, services.SiteAccessEditable) {
			return
		}
		in, err := models.NormalizeDeviceInput(models.DeviceInput{Host: req.Host, Port: req.Port})
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		usable, err := prober.UsableAt(c.Request.Context(), req.CredentialID, req.SiteID)
		if err != nil {
			respondInternal(c, "testDevice", err)
			return
		}
		if !usable {
			respondError(c, http.StatusBadRequest, services.ErrCredentialNotUsable.Error())
			return
		}
		sys, err := prober.Identify(c.Request.Context(), in.Host, in.Port, req.CredentialID, 3*time.Second, 0)
		if err != nil {
			// Reported as a result, not a server error: "no answer" is the
			// answer the user asked for.
			respondSuccess(c, http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		vendor, _ := snmp.Identity(sys.ObjectID, sys.Descr, "")
		respondSuccess(c, http.StatusOK, gin.H{"ok": true, "system": sys, "vendor": vendor})
	}
}
