package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/dashboards"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// mapLinkGone is what a revoked or unknown map link answers.
const mapLinkGone = "This map link is no longer available"

// RegisterPublicSiteMapRoutes mounts a site map's public link: the map, its
// live stream and its devices' ports for the realistic drawings, all trimmed
// of addresses and details and using stand-in ids, and rate-limited by address as dashboards are.
func RegisterPublicSiteMapRoutes(router *gin.Engine, h *SiteMapHandler) {
	limiter := NewRateLimiter(600, time.Minute, 120)
	g := router.Group("/api/v1/public/maps", limiter.Middleware("public-map", ByIP))
	g.GET("/:token", h.publicMap)
	g.GET("/:token/stream", h.publicFollow)
	g.GET("/:token/devices/:deviceId/ports", h.publicPorts)
}

// publicSite is the site a token opens, answering 404 when it opens none.
func (h *SiteMapHandler) publicSite(c *gin.Context) (uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	site, err := h.maps.ResolveToken(c.Request.Context(), c.Param("token"))
	if errors.Is(err, services.ErrSiteMapNotFound) {
		respondError(c, http.StatusNotFound, mapLinkGone)
		return uuid.Nil, false
	}
	if err != nil {
		respondInternal(c, "publicMap", err)
		return uuid.Nil, false
	}
	return site, true
}

func (h *SiteMapHandler) publicMap(c *gin.Context) {
	site, ok := h.publicSite(c)
	if !ok {
		return
	}
	m, err := h.maps.PublicSnapshot(c.Request.Context(), site)
	if err != nil {
		respondSiteMapError(c, "publicMap", err)
		return
	}
	h.traffic.Touch(site)
	respondSuccess(c, http.StatusOK, gin.H{"map": m, "traffic": services.PublicTraffic(site, h.traffic.Latest(c.Request.Context(), site))})
}

func (h *SiteMapHandler) publicFollow(c *gin.Context) {
	site, ok := h.publicSite(c)
	if !ok {
		return
	}
	token := c.Param("token")
	snapshot := func(ctx context.Context) (*services.MapSnapshot, error) { return h.maps.PublicSnapshot(ctx, site) }
	allowed := func(ctx context.Context) bool {
		s, err := h.maps.ResolveToken(ctx, token)
		return err == nil && s == site
	}
	h.serveStream(c, site, snapshot, allowed, func(t services.MapTraffic) services.MapTraffic { return services.PublicTraffic(site, t) })
}

func (h *SiteMapHandler) publicPorts(c *gin.Context) {
	site, ok := h.publicSite(c)
	if !ok {
		return
	}
	alias, ok := uuidParam(c, "deviceId")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	dev, on, err := h.maps.PublicDevice(ctx, site, alias)
	if err != nil {
		respondSiteMapError(c, "publicMapPorts", err)
		return
	}
	if !on {
		respondError(c, http.StatusNotFound, "device not found")
		return
	}
	d, err := h.devices.Get(ctx, dev)
	if errors.Is(err, services.ErrDeviceNotFound) {
		respondError(c, http.StatusNotFound, "device not found")
		return
	}
	if err != nil {
		respondInternal(c, "publicMapPorts", err)
		return
	}
	view, err := h.ports.DevicePorts(ctx, d)
	if err != nil {
		respondInternal(c, "publicMapPorts", err)
		return
	}
	for i := range view.Ports {
		dashboards.TrimPortForPublic(&view.Ports[i])
	}
	view.DeviceState.StatusDetail = ""
	view.LayoutNotice = nil
	respondSuccess(c, http.StatusOK, view)
}
