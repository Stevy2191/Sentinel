package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/l2"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// endpointStore is what the handlers need from services.EndpointService.
type endpointStore interface {
	Search(ctx context.Context, v services.Viewer, q string) ([]services.Endpoint, error)
	ForDevice(ctx context.Context, v services.Viewer, deviceID uuid.UUID) (*services.ConnectedView, error)
	Get(ctx context.Context, v services.Viewer, mac string) (*services.Endpoint, []services.SightingView, error)
	SetName(ctx context.Context, v services.Viewer, mac, name string) (string, string, error)
}

const (
	endpointQueryMax = 100
	endpointNameMax  = 64
)

// RegisterEndpointRoutes mounts Find a device, a switch's connected view,
// one device's record and naming it. Every route follows site access: the
// service leaves out what the caller can't see.
func RegisterEndpointRoutes(rg *gin.RouterGroup, endpoints endpointStore, devices deviceStore, sites siteAccessChecker, audit auditRecorder) {
	rg.GET("/network/endpoints/search", searchEndpointsHandler(endpoints))
	rg.GET("/network/endpoints/:mac", getEndpointHandler(endpoints))
	rg.PUT("/network/endpoints/:mac/name", nameEndpointHandler(endpoints, audit))
	rg.GET("/devices/:id/endpoints", deviceEndpointsHandler(endpoints, devices, sites))
}

func viewerFrom(c *gin.Context) services.Viewer {
	id, _, admin, _ := GetUserFromContext(c)
	return services.Viewer{UserID: id, IsAdmin: admin}
}

func respondEndpointError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrEndpointNotFound):
		respondError(c, http.StatusNotFound, "device not found")
	case errors.Is(err, services.ErrDeviceNotFound):
		respondError(c, http.StatusNotFound, "device not found")
	case errors.Is(err, services.ErrEndpointForbidden):
		respondError(c, http.StatusForbidden, err.Error())
	case errors.Is(err, services.ErrEndpointNameTooLong):
		respondError(c, http.StatusBadRequest, err.Error())
	default:
		respondInternal(c, op, err)
	}
}

// macParam is the :mac parameter in colon form, or a 400.
func macParam(c *gin.Context) (string, bool) {
	mac, ok := l2.NormalizeMAC(c.Param("mac"))
	if !ok {
		respondError(c, http.StatusBadRequest, "invalid MAC address")
	}
	return mac, ok
}

func searchEndpointsHandler(endpoints endpointStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		q := strings.TrimSpace(c.Query("q"))
		if q == "" || utf8.RuneCountInString(q) > endpointQueryMax {
			respondError(c, http.StatusBadRequest, "q must be 1 to 100 characters")
			return
		}
		out, err := endpoints.Search(c.Request.Context(), viewerFrom(c), q)
		if err != nil {
			respondEndpointError(c, "searchEndpoints", err)
			return
		}
		if out == nil {
			out = []services.Endpoint{}
		}
		respondSuccess(c, http.StatusOK, out)
	}
}

func getEndpointHandler(endpoints endpointStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		mac, ok := macParam(c)
		if !ok {
			return
		}
		e, history, err := endpoints.Get(c.Request.Context(), viewerFrom(c), mac)
		if err != nil {
			respondEndpointError(c, "getEndpoint", err)
			return
		}
		if history == nil {
			history = []services.SightingView{}
		}
		respondSuccess(c, http.StatusOK, gin.H{"endpoint": e, "sightings": history})
	}
}

func nameEndpointHandler(endpoints endpointStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		mac, ok := macParam(c)
		if !ok {
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if utf8.RuneCountInString(strings.TrimSpace(req.Name)) > endpointNameMax {
			respondError(c, http.StatusBadRequest, services.ErrEndpointNameTooLong.Error())
			return
		}
		before, after, err := endpoints.SetName(c.Request.Context(), viewerFrom(c), mac, req.Name)
		if err != nil {
			respondEndpointError(c, "nameEndpoint", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionEndpointNamed, models.ResourceEndpoint, nil,
			models.AuditChanges{Before: map[string]any{"name": before}, After: map[string]any{"name": after},
				Summary: map[string]any{"mac": mac}})
		respondSuccess(c, http.StatusOK, gin.H{"mac": mac, "name": after})
	}
}

func deviceEndpointsHandler(endpoints endpointStore, devices deviceStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		view, err := endpoints.ForDevice(c.Request.Context(), viewerFrom(c), d.ID)
		if err != nil {
			respondEndpointError(c, "deviceEndpoints", err)
			return
		}
		respondSuccess(c, http.StatusOK, view)
	}
}
