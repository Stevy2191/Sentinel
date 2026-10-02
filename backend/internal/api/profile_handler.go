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
)

// profileStore is what the handlers need from services.ProfileService.
type profileStore interface {
	List(ctx context.Context) ([]services.ProfileView, error)
	Get(ctx context.Context, id uuid.UUID) (*services.ProfileDetail, error)
	Create(ctx context.Context, in services.ProfileInput) (*models.MetricProfile, error)
	Update(ctx context.Context, id uuid.UUID, in services.ProfileInput) (*models.MetricProfile, *models.MetricProfile, error)
	Delete(ctx context.Context, id uuid.UUID) (*models.MetricProfile, error)
	Copy(ctx context.Context, id uuid.UUID) (*services.ProfileDetail, error)
	GetMetric(ctx context.Context, id uuid.UUID) (*models.ProfileMetric, error)
	CreateMetric(ctx context.Context, profileID uuid.UUID, m models.ProfileMetric) (*models.ProfileMetric, error)
	UpdateMetric(ctx context.Context, id uuid.UUID, m models.ProfileMetric) (*models.ProfileMetric, *models.ProfileMetric, error)
	DeleteMetric(ctx context.Context, id uuid.UUID) (*models.ProfileMetric, error)
	MetricDataDevices(ctx context.Context, key string) (int, error)
	ProfilesForDevice(ctx context.Context, d models.Device) ([]services.ProfileWithMetrics, error)
	DeviceProfiles(ctx context.Context, d models.Device) ([]services.DeviceProfileView, error)
	SetDeviceProfile(ctx context.Context, deviceID, profileID uuid.UUID, mode string) error
}

// metricPreviewer is DeviceWalker's PreviewDevice: it resolves the device's
// SNMP target and evaluates a (not yet saved) metric definition against it
// right now, for the metric editor's "preview" button.
type metricPreviewer interface {
	PreviewDevice(ctx context.Context, d *services.DeviceView, m models.ProfileMetric) (*services.MetricPreview, error)
}

// RegisterProfileRoutes mounts metric profile and custom metric management
// (admin) and the per-device profile standing and override (site access:
// readonly to see, editable to change), plus the metric-preview button
// (admin, site access editable: it sends SNMP to a real device, like
// test-walk and Test connection).
func RegisterProfileRoutes(rg *gin.RouterGroup, profiles profileStore, devices deviceStore, sites siteAccessChecker, audit auditRecorder, users adminChecker, previewer metricPreviewer) {
	profileAdmin := rg.Group("/network/profiles", RequireAdmin(users))
	profileAdmin.GET("", listProfilesHandler(profiles))
	profileAdmin.POST("", createProfileHandler(profiles, audit))
	profileAdmin.GET("/:id", getProfileHandler(profiles))
	profileAdmin.PUT("/:id", updateProfileHandler(profiles, audit))
	profileAdmin.DELETE("/:id", deleteProfileHandler(profiles, audit))
	profileAdmin.POST("/:id/copy", copyProfileHandler(profiles, audit))
	profileAdmin.POST("/:id/metrics", createMetricHandler(profiles, audit))

	metricAdmin := rg.Group("/network/metrics", RequireAdmin(users))
	metricAdmin.PUT("/:id", updateMetricHandler(profiles, audit))
	metricAdmin.DELETE("/:id", deleteMetricHandler(profiles, audit))
	metricAdmin.GET("/:id/data-devices", metricDataDevicesHandler(profiles))

	rg.GET("/devices/:id/profiles", deviceProfilesHandler(profiles, devices, sites))
	rg.PUT("/devices/:id/profiles/:profileId", setDeviceProfileHandler(profiles, devices, sites, audit))
	rg.POST("/devices/:id/metric-preview", RequireAdmin(users), metricPreviewHandler(devices, sites, previewer))
}

// deviceHealthFunc is ProfileService.DeviceHealth with its metrics store and
// incident service bound.
type deviceHealthFunc func(ctx context.Context, d *services.DeviceView) (*services.DeviceHealthView, error)

// RegisterDeviceHealthRoute mounts a device's Health view (site access:
// readonly).
func RegisterDeviceHealthRoute(rg *gin.RouterGroup, profiles *services.ProfileService, metrics *services.MetricsStore, incidents *services.IncidentService, devices deviceStore, sites siteAccessChecker) {
	health := func(ctx context.Context, d *services.DeviceView) (*services.DeviceHealthView, error) {
		return profiles.DeviceHealth(ctx, d, metrics, incidents)
	}
	rg.GET("/devices/:id/health", deviceHealthHandler(health, devices, sites))
}

func deviceHealthHandler(health deviceHealthFunc, devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		view, err := health(c.Request.Context(), d)
		if err != nil {
			respondInternal(c, "deviceHealth", err)
			return
		}
		respondSuccess(c, http.StatusOK, view)
	}
}

func respondProfileError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrProfileNotFound), errors.Is(err, services.ErrMetricNotFound):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrProfileBuiltin):
		respondError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrProfileNameTaken), errors.Is(err, services.ErrMetricKeyTaken):
		respondError(c, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrMetricKeyImmutable):
		respondError(c, http.StatusBadRequest, err.Error())
	case isInternal(err):
		respondInternal(c, op, err)
	default: // a validation message from ValidateMetric or normalizeProfileInput
		respondError(c, http.StatusBadRequest, err.Error())
	}
}

func profileAudit(p models.MetricProfile) map[string]any {
	return map[string]any{"name": p.Name, "match_prefixes": []string(p.MatchPrefixes),
		"poll_interval_minutes": p.PollIntervalMinutes, "builtin": p.Builtin}
}

func metricAudit(m models.ProfileMetric) map[string]any {
	return map[string]any{"name": m.Name, "key": m.Key, "profile_id": m.ProfileID, "source": m.Source, "kind": m.Kind}
}

func parseUUIDParam(c *gin.Context, name, message string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		respondError(c, http.StatusBadRequest, message)
		return uuid.UUID{}, false
	}
	return id, true
}

func listProfilesHandler(profiles profileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := profiles.List(c.Request.Context())
		if err != nil {
			respondInternal(c, "listProfiles", err)
			return
		}
		if list == nil {
			list = []services.ProfileView{}
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func getProfileHandler(profiles profileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUUIDParam(c, "id", "invalid profile id")
		if !ok {
			return
		}
		d, err := profiles.Get(c.Request.Context(), id)
		if err != nil {
			respondProfileError(c, "getProfile", err)
			return
		}
		respondSuccess(c, http.StatusOK, d)
	}
}

func createProfileHandler(profiles profileStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in services.ProfileInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		p, err := profiles.Create(c.Request.Context(), in)
		if err != nil {
			respondProfileError(c, "createProfile", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionProfileCreated, models.ResourceMetricProfile,
			&p.ID, models.AuditChanges{Summary: profileAudit(*p)})
		respondSuccess(c, http.StatusCreated, p)
	}
}

func updateProfileHandler(profiles profileStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUUIDParam(c, "id", "invalid profile id")
		if !ok {
			return
		}
		var in services.ProfileInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		before, after, err := profiles.Update(c.Request.Context(), id, in)
		if err != nil {
			respondProfileError(c, "updateProfile", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionProfileUpdated, models.ResourceMetricProfile,
			&id, models.AuditChanges{Before: profileAudit(*before), After: profileAudit(*after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteProfileHandler(profiles profileStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUUIDParam(c, "id", "invalid profile id")
		if !ok {
			return
		}
		p, err := profiles.Delete(c.Request.Context(), id)
		if err != nil {
			respondProfileError(c, "deleteProfile", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionProfileDeleted, models.ResourceMetricProfile,
			&id, models.AuditChanges{Summary: profileAudit(*p)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func copyProfileHandler(profiles profileStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUUIDParam(c, "id", "invalid profile id")
		if !ok {
			return
		}
		cp, err := profiles.Copy(c.Request.Context(), id)
		if err != nil {
			respondProfileError(c, "copyProfile", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionProfileCreated, models.ResourceMetricProfile,
			&cp.ID, models.AuditChanges{Summary: profileAudit(cp.MetricProfile)})
		respondSuccess(c, http.StatusCreated, cp)
	}
}

func createMetricHandler(profiles profileStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		profileID, ok := parseUUIDParam(c, "id", "invalid profile id")
		if !ok {
			return
		}
		var m models.ProfileMetric
		if err := c.ShouldBindJSON(&m); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		created, err := profiles.CreateMetric(c.Request.Context(), profileID, m)
		if err != nil {
			respondProfileError(c, "createMetric", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionMetricCreated, models.ResourceCustomMetric,
			&created.ID, models.AuditChanges{Summary: metricAudit(*created)})
		respondSuccess(c, http.StatusCreated, created)
	}
}

func updateMetricHandler(profiles profileStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUUIDParam(c, "id", "invalid metric id")
		if !ok {
			return
		}
		var m models.ProfileMetric
		if err := c.ShouldBindJSON(&m); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		before, after, err := profiles.UpdateMetric(c.Request.Context(), id, m)
		if err != nil {
			respondProfileError(c, "updateMetric", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionMetricUpdated, models.ResourceCustomMetric,
			&id, models.AuditChanges{Before: metricAudit(*before), After: metricAudit(*after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteMetricHandler(profiles profileStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUUIDParam(c, "id", "invalid metric id")
		if !ok {
			return
		}
		m, err := profiles.DeleteMetric(c.Request.Context(), id)
		if err != nil {
			respondProfileError(c, "deleteMetric", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionMetricDeleted, models.ResourceCustomMetric,
			&id, models.AuditChanges{Summary: metricAudit(*m)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func metricDataDevicesHandler(profiles profileStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseUUIDParam(c, "id", "invalid metric id")
		if !ok {
			return
		}
		m, err := profiles.GetMetric(c.Request.Context(), id)
		if err != nil {
			respondProfileError(c, "metricDataDevices", err)
			return
		}
		n, err := profiles.MetricDataDevices(c.Request.Context(), m.Key)
		if err != nil {
			respondInternal(c, "metricDataDevices", err)
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"devices": n})
	}
}

func deviceProfilesHandler(profiles profileStore, devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		views, err := profiles.DeviceProfiles(c.Request.Context(), d.Device)
		if err != nil {
			respondInternal(c, "deviceProfiles", err)
			return
		}
		if views == nil {
			views = []services.DeviceProfileView{}
		}
		respondSuccess(c, http.StatusOK, views)
	}
}

func setDeviceProfileHandler(profiles profileStore, devices deviceStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		profileID, ok := parseUUIDParam(c, "profileId", "invalid profile id")
		if !ok {
			return
		}
		var body struct {
			Mode string `json:"mode"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := profiles.SetDeviceProfile(c.Request.Context(), d.ID, profileID, body.Mode); err != nil {
			respondProfileError(c, "setDeviceProfile", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionProfileUpdated, models.ResourceMetricProfile,
			&profileID, models.AuditChanges{Summary: map[string]any{"device_id": d.ID, "mode": body.Mode}})
		respondSuccess(c, http.StatusOK, gin.H{"mode": body.Mode})
	}
}

// metricPreviewHandler evaluates a metric definition (not necessarily saved
// yet) against one device right now. An invalid metric is a 400; an SNMP or
// target failure is reported as a result, not a server error (the same
// shape test-walk and Test connection use), since the server did its job and
// the device simply did not answer.
func metricPreviewHandler(devices deviceStore, sites siteAccessChecker, previewer metricPreviewer) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		var m models.ProfileMetric
		if err := c.ShouldBindJSON(&m); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()
		preview, err := previewer.PreviewDevice(ctx, d, m)
		if err != nil {
			if errors.Is(err, services.ErrInvalidMetric) {
				respondError(c, http.StatusBadRequest, err.Error())
				return
			}
			respondSuccess(c, http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"ok": true, "preview": preview})
	}
}
