package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/sitemap"
	"github.com/Stevy2191/Sentinel/backend/internal/stream"
)

// siteMapStore is what the handlers need from services.SiteMapService. Every
// method checks the viewer's access itself.
type siteMapStore interface {
	Snapshot(ctx context.Context, v services.Viewer, siteID uuid.UUID) (*services.MapSnapshot, error)
	PublicSnapshot(ctx context.Context, siteID uuid.UUID) (*services.MapSnapshot, error)
	OnMap(ctx context.Context, siteID, deviceID uuid.UUID) (bool, error)
	SetPositions(ctx context.Context, v services.Viewer, siteID uuid.UUID, ps []services.PositionInput) error
	SetDevice(ctx context.Context, v services.Viewer, siteID, deviceID uuid.UUID, hidden, added *bool) error
	AddLink(ctx context.Context, v services.Viewer, siteID uuid.UUID, a, b sitemap.End) (*models.SiteMapLink, error)
	UpdateLink(ctx context.Context, v services.Viewer, siteID, linkID uuid.UUID, a, b sitemap.End) error
	DeleteLink(ctx context.Context, v services.Viewer, siteID, linkID uuid.UUID) error
	SetLinkHidden(ctx context.Context, v services.Viewer, siteID uuid.UUID, key string, hidden bool) error
	KeepGuess(ctx context.Context, v services.Viewer, siteID uuid.UUID, key string) (*models.SiteMapLink, error)
	ResetLayout(ctx context.Context, v services.Viewer, siteID uuid.UUID) error
	PublicLink(ctx context.Context, v services.Viewer, siteID uuid.UUID) (*models.SiteMapPublicLink, error)
	CreatePublicLink(ctx context.Context, v services.Viewer, siteID uuid.UUID) (*models.SiteMapPublicLink, bool, error)
	RevokePublicLink(ctx context.Context, v services.Viewer, siteID uuid.UUID) error
	ResolveToken(ctx context.Context, token string) (uuid.UUID, error)
}

// mapTraffic is what the handlers need from services.LinkPoller.
type mapTraffic interface {
	Watch(siteID uuid.UUID) (release func())
	Touch(siteID uuid.UUID)
	Latest(ctx context.Context, siteID uuid.UUID) services.MapTraffic
}

type mapDeviceGetter interface {
	Get(ctx context.Context, id uuid.UUID) (*services.DeviceView, error)
}

type mapPortsReader interface {
	DevicePorts(ctx context.Context, d *services.DeviceView) (*services.DevicePortsView, error)
}

// SiteMapHandler serves site maps, their live stream, their edits and their
// public links.
type SiteMapHandler struct {
	maps    siteMapStore
	traffic mapTraffic
	sites   siteAccessChecker
	devices mapDeviceGetter
	ports   mapPortsReader
	hub     *stream.Hub
	audit   auditRecorder

	// keepAlive: a quiet stream's comment; recheck: how often an open
	// stream re-checks the viewer may still see the map; topology: how
	// often it re-sends the whole map (the L2 poll's pace).
	keepAlive, recheck, topology time.Duration
}

func NewSiteMapHandler(maps siteMapStore, traffic mapTraffic, sites siteAccessChecker, devices mapDeviceGetter, ports mapPortsReader,
	hub *stream.Hub, audit auditRecorder) *SiteMapHandler {
	return &SiteMapHandler{maps: maps, traffic: traffic, sites: sites, devices: devices, ports: ports, hub: hub, audit: audit,
		keepAlive: stream.KeepAlive, recheck: 10 * time.Second, topology: 5 * time.Minute}
}

// mapHubKey is the hub key a site's map messages travel on: "traffic"
// frames from the fast poll, and "changed" nudges after an edit.
func mapHubKey(siteID uuid.UUID) string { return "sitemap:" + siteID.String() }

// PublishTraffic sends a site's traffic to its open streams.
func (h *SiteMapHandler) PublishTraffic(siteID uuid.UUID, t services.MapTraffic) {
	data, err := json.Marshal(t)
	if err != nil {
		return
	}
	h.hub.Publish(mapHubKey(siteID), stream.Message{Event: "traffic", Data: data})
}

// Changed tells a site's open streams to re-read the map.
func (h *SiteMapHandler) Changed(siteID uuid.UUID) {
	h.hub.Publish(mapHubKey(siteID), stream.Message{Event: "changed"})
}

// RegisterSiteMapRoutes mounts the signed-in routes under /network/sites/:id/map.
func RegisterSiteMapRoutes(rg *gin.RouterGroup, h *SiteMapHandler) {
	g := rg.Group("/network/sites/:id/map")
	g.GET("", h.get)
	g.GET("/stream", h.follow)
	g.PUT("/positions", h.positions)
	g.PUT("/devices/:deviceId", h.device)
	g.POST("/links", h.addLink)
	g.PUT("/links/:linkId", h.updateLink)
	g.DELETE("/links/:linkId", h.deleteLink)
	g.PUT("/link-state", h.linkState)
	g.POST("/link-state/keep", h.keep)
	g.POST("/reset", h.reset)
	g.GET("/public-link", h.publicLink)
	g.POST("/public-link", h.createPublicLink)
	g.DELETE("/public-link", h.revokePublicLink)
}

func respondSiteMapError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrSiteMapNotFound):
		respondError(c, http.StatusNotFound, "site not found")
	case errors.Is(err, services.ErrSiteMapForbidden):
		respondError(c, http.StatusForbidden, err.Error())
	case errors.Is(err, services.ErrSiteMapInvalid):
		respondError(c, http.StatusBadRequest, err.Error())
	default:
		respondInternal(c, op, err)
	}
}

func uuidParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

func (h *SiteMapHandler) get(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	m, err := h.maps.Snapshot(c.Request.Context(), viewerFrom(c), site)
	if err != nil {
		respondSiteMapError(c, "siteMap", err)
		return
	}
	respondSuccess(c, http.StatusOK, gin.H{"map": m, "traffic": h.traffic.Latest(c.Request.Context(), site)})
}

// edited records an edit and answers 204.
func (h *SiteMapHandler) edited(c *gin.Context, site uuid.UUID, op string, err error) {
	if err != nil {
		respondSiteMapError(c, "siteMap."+op, err)
		return
	}
	h.audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteMapEdited, models.ResourceSiteMap, &site,
		models.AuditChanges{Summary: map[string]any{"operation": op}})
	c.Status(http.StatusNoContent)
}

func (h *SiteMapHandler) positions(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		Positions []services.PositionInput `json:"positions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Positions) == 0 || len(req.Positions) > 500 {
		respondError(c, http.StatusBadRequest, "positions must list 1 to 500 devices")
		return
	}
	h.edited(c, site, "positions", h.maps.SetPositions(c.Request.Context(), viewerFrom(c), site, req.Positions))
}

func (h *SiteMapHandler) device(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	dev, ok := uuidParam(c, "deviceId")
	if !ok {
		return
	}
	var req struct {
		Hidden *bool `json:"hidden"`
		Added  *bool `json:"added"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || (req.Hidden == nil && req.Added == nil) {
		respondError(c, http.StatusBadRequest, "send hidden or added")
		return
	}
	h.edited(c, site, "device", h.maps.SetDevice(c.Request.Context(), viewerFrom(c), site, dev, req.Hidden, req.Added))
}

type mapEndInput struct {
	DeviceID uuid.UUID `json:"device_id"`
	IfIndex  *int      `json:"if_index"`
}

func (e mapEndInput) end() sitemap.End {
	out := sitemap.End{DeviceID: e.DeviceID}
	if e.IfIndex != nil {
		out.IfIndex = *e.IfIndex
	}
	return out
}

type mapLinkInput struct {
	A mapEndInput `json:"a"`
	B mapEndInput `json:"b"`
}

func bindLink(c *gin.Context) (mapLinkInput, bool) {
	var req mapLinkInput
	if err := c.ShouldBindJSON(&req); err != nil || req.A.DeviceID == uuid.Nil || req.B.DeviceID == uuid.Nil ||
		(req.A.IfIndex != nil && *req.A.IfIndex <= 0) || (req.B.IfIndex != nil && *req.B.IfIndex <= 0) {
		respondError(c, http.StatusBadRequest, "a link needs two devices, and ports above 0 when given")
		return req, false
	}
	return req, true
}

func (h *SiteMapHandler) addLink(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	req, ok := bindLink(c)
	if !ok {
		return
	}
	link, err := h.maps.AddLink(c.Request.Context(), viewerFrom(c), site, req.A.end(), req.B.end())
	if err != nil {
		respondSiteMapError(c, "siteMap.addLink", err)
		return
	}
	h.audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteMapEdited, models.ResourceSiteMap, &site,
		models.AuditChanges{Summary: map[string]any{"operation": "add_link"}})
	respondSuccess(c, http.StatusCreated, link)
}

func (h *SiteMapHandler) updateLink(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	link, ok := uuidParam(c, "linkId")
	if !ok {
		return
	}
	req, ok := bindLink(c)
	if !ok {
		return
	}
	h.edited(c, site, "update_link", h.maps.UpdateLink(c.Request.Context(), viewerFrom(c), site, link, req.A.end(), req.B.end()))
}

func (h *SiteMapHandler) deleteLink(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	link, ok := uuidParam(c, "linkId")
	if !ok {
		return
	}
	h.edited(c, site, "delete_link", h.maps.DeleteLink(c.Request.Context(), viewerFrom(c), site, link))
}

func (h *SiteMapHandler) linkState(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		Key    string `json:"key"`
		Hidden *bool  `json:"hidden"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Key == "" || req.Hidden == nil {
		respondError(c, http.StatusBadRequest, "send key and hidden")
		return
	}
	h.edited(c, site, "link_state", h.maps.SetLinkHidden(c.Request.Context(), viewerFrom(c), site, req.Key, *req.Hidden))
}

func (h *SiteMapHandler) keep(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		Key string `json:"key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Key == "" {
		respondError(c, http.StatusBadRequest, "send key")
		return
	}
	link, err := h.maps.KeepGuess(c.Request.Context(), viewerFrom(c), site, req.Key)
	if err != nil {
		respondSiteMapError(c, "siteMap.keep", err)
		return
	}
	h.audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteMapEdited, models.ResourceSiteMap, &site,
		models.AuditChanges{Summary: map[string]any{"operation": "keep_guess"}})
	respondSuccess(c, http.StatusCreated, link)
}

func (h *SiteMapHandler) reset(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	h.edited(c, site, "reset_layout", h.maps.ResetLayout(c.Request.Context(), viewerFrom(c), site))
}

func (h *SiteMapHandler) publicLink(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	link, err := h.maps.PublicLink(c.Request.Context(), viewerFrom(c), site)
	if err != nil {
		respondSiteMapError(c, "siteMap.publicLink", err)
		return
	}
	respondSuccess(c, http.StatusOK, link)
}

func (h *SiteMapHandler) createPublicLink(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	link, replaced, err := h.maps.CreatePublicLink(c.Request.Context(), viewerFrom(c), site)
	if err != nil {
		respondSiteMapError(c, "siteMap.createPublicLink", err)
		return
	}
	h.audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteMapLinkCreated, models.ResourceSiteMap, &site,
		models.AuditChanges{Summary: map[string]any{"replaced": replaced}})
	respondSuccess(c, http.StatusCreated, link)
}

func (h *SiteMapHandler) revokePublicLink(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	if err := h.maps.RevokePublicLink(c.Request.Context(), viewerFrom(c), site); err != nil {
		respondSiteMapError(c, "siteMap.revokePublicLink", err)
		return
	}
	h.audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteMapLinkRevoked, models.ResourceSiteMap, &site, models.AuditChanges{})
	c.Status(http.StatusNoContent)
}

// follow is the signed-in live stream.
func (h *SiteMapHandler) follow(c *gin.Context) {
	site, ok := uuidParam(c, "id")
	if !ok {
		return
	}
	v := viewerFrom(c)
	snapshot := func(ctx context.Context) (*services.MapSnapshot, error) { return h.maps.Snapshot(ctx, v, site) }
	allowed := func(ctx context.Context) bool {
		level, err := h.sites.SiteAccess(ctx, v.UserID, v.IsAdmin, site)
		return err == nil && level >= services.SiteAccessReadonly
	}
	h.serveStream(c, site, snapshot, allowed)
}

// serveStream sends the map, then the latest traffic, then whatever the hub
// brings: traffic as it is, and a fresh map after each edit and every few
// minutes. It ends when the viewer may no longer see the map.
func (h *SiteMapHandler) serveStream(c *gin.Context, site uuid.UUID, snapshot func(context.Context) (*services.MapSnapshot, error), allowed func(context.Context) bool) {
	ctx := c.Request.Context()
	first, err := snapshot(ctx)
	if err != nil {
		respondSiteMapError(c, "siteMap.stream", err)
		return
	}
	sub := h.hub.Subscribe(mapHubKey(site))
	defer sub.Close()
	release := h.traffic.Watch(site)
	defer release()

	w, err := stream.NewWriter(c.Writer)
	if err != nil {
		respondInternal(c, "siteMap.stream", err)
		return
	}
	if sendSSEJSON(w, "map", first) != nil || sendSSEJSON(w, "traffic", h.traffic.Latest(ctx, site)) != nil {
		return
	}
	sendMap := func() bool {
		m, err := snapshot(ctx)
		if err != nil {
			if !errors.Is(err, services.ErrSiteMapNotFound) {
				log.Printf("[sitemap] re-reading site %s: %v", site, err)
				return true
			}
			return false
		}
		return sendSSEJSON(w, "map", m) == nil
	}

	keepAlive := time.NewTicker(h.keepAlive)
	defer keepAlive.Stop()
	recheck := time.NewTicker(h.recheck)
	defer recheck.Stop()
	topology := time.NewTicker(h.topology)
	defer topology.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if w.Comment("keep-alive") != nil {
				return
			}
		case <-recheck.C:
			if !allowed(ctx) {
				return
			}
		case <-topology.C:
			if !sendMap() {
				return
			}
		case m, ok := <-sub.C():
			if !ok {
				return // dropped for falling behind: the browser reconnects
			}
			if m.Event == "changed" {
				if !sendMap() {
					return
				}
				continue
			}
			if w.Send(m) != nil {
				return
			}
		}
	}
}
