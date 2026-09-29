package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type scanRunner interface {
	Start(siteID uuid.UUID, cidr string, creds []services.ScanCredential, existing map[string]bool) (services.ScanJob, error)
	Get(siteID, jobID uuid.UUID) (services.ScanJob, error)
}

type scanCredentials interface {
	ForSite(ctx context.Context, siteID uuid.UUID) ([]services.CredentialOption, error)
	Decrypted(ctx context.Context, id uuid.UUID) (snmp.Credential, error)
}

type scanDevices interface {
	HostsInSite(ctx context.Context, siteID uuid.UUID) (map[string]bool, error)
	CreateMany(ctx context.Context, siteID uuid.UUID, in []models.DeviceInput, by uuid.UUID) ([]models.Device, []services.BulkAddError)
}

// RegisterScanRoutes mounts subnet scans and bulk add; both need editable
// access to the site.
func RegisterScanRoutes(rg *gin.RouterGroup, scans scanRunner, creds scanCredentials, devices scanDevices, sites siteAccessChecker, audit auditRecorder) {
	rg.POST("/sites/:id/scans", startScanHandler(scans, creds, devices, sites))
	rg.GET("/sites/:id/scans/:job", getScanHandler(scans, sites))
	rg.POST("/sites/:id/devices", bulkAddDevicesHandler(devices, sites, audit))
}

type startScanRequest struct {
	CIDR          string      `json:"cidr"`
	CredentialIDs []uuid.UUID `json:"credential_ids"`
}

func startScanHandler(scans scanRunner, creds scanCredentials, devices scanDevices, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		siteID, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, siteID, services.SiteAccessEditable) {
			return
		}
		var req startScanRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		ctx := c.Request.Context()
		options, err := creds.ForSite(ctx, siteID)
		if err != nil {
			respondInternal(c, "startScan", err)
			return
		}
		allowed := map[uuid.UUID]services.CredentialOption{}
		for _, o := range options {
			allowed[o.ID] = o
		}
		ids := req.CredentialIDs
		if len(ids) == 0 {
			for _, o := range options {
				ids = append(ids, o.ID)
			}
		}
		var scanCreds []services.ScanCredential
		for _, id := range ids {
			o, ok := allowed[id]
			if !ok {
				respondError(c, http.StatusBadRequest, services.ErrCredentialNotUsable.Error())
				return
			}
			cred, err := creds.Decrypted(ctx, id)
			if err != nil {
				respondInternal(c, "startScan", err)
				return
			}
			scanCreds = append(scanCreds, services.ScanCredential{ID: id, Name: o.Name, Cred: cred})
		}
		existing, err := devices.HostsInSite(ctx, siteID)
		if err != nil {
			respondInternal(c, "startScan", err)
			return
		}
		job, err := scans.Start(siteID, req.CIDR, scanCreds, existing)
		switch {
		case errors.Is(err, services.ErrScanRunning):
			respondError(c, http.StatusConflict, err.Error())
		case err != nil:
			respondError(c, http.StatusBadRequest, err.Error())
		default:
			respondSuccess(c, http.StatusAccepted, job)
		}
	}
}

func getScanHandler(scans scanRunner, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		siteID, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, siteID, services.SiteAccessEditable) {
			return
		}
		jobID, err := uuid.Parse(c.Param("job"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid scan id")
			return
		}
		job, err := scans.Get(siteID, jobID)
		if err != nil {
			respondError(c, http.StatusNotFound, err.Error())
			return
		}
		respondSuccess(c, http.StatusOK, job)
	}
}

type bulkAddRequest struct {
	Devices []models.DeviceInput `json:"devices"`
}

func bulkAddDevicesHandler(devices scanDevices, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		siteID, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, siteID, services.SiteAccessEditable) {
			return
		}
		var req bulkAddRequest
		if err := c.ShouldBindJSON(&req); err != nil || len(req.Devices) == 0 {
			respondError(c, http.StatusBadRequest, "send at least one device")
			return
		}
		if len(req.Devices) > maxBulkAdd {
			respondError(c, http.StatusBadRequest, "at most 1024 devices per request")
			return
		}
		userID, _, _, _ := GetUserFromContext(c)
		added, failed := devices.CreateMany(c.Request.Context(), siteID, req.Devices, userID)
		// One entry for the batch: a scan can add hundreds of devices at once.
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDeviceCreated, models.ResourceDevice, nil,
			models.AuditChanges{Summary: map[string]any{"site_id": siteID, "added": len(added), "failed": len(failed)}})
		if added == nil {
			added = []models.Device{}
		}
		if failed == nil {
			failed = []services.BulkAddError{}
		}
		respondSuccess(c, http.StatusOK, gin.H{"added": added, "failed": failed})
	}
}

// maxBulkAdd matches the largest scan (a /22).
const maxBulkAdd = 1024
