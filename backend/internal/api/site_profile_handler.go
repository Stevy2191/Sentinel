package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// siteProfileStore is what the site profile handlers need from
// SiteProfileService.
type siteProfileStore interface {
	Profile(ctx context.Context, siteID uuid.UUID) (*services.SiteProfile, error)
	SetNotes(ctx context.Context, siteID uuid.UUID, notes *string) (*string, *string, error)
	CreateNetwork(ctx context.Context, siteID uuid.UUID, in models.SiteNetworkInput) (*models.SiteNetwork, error)
	UpdateNetwork(ctx context.Context, siteID, id uuid.UUID, in models.SiteNetworkInput) (*models.SiteNetwork, *models.SiteNetwork, error)
	DeleteNetwork(ctx context.Context, siteID, id uuid.UUID) (*models.SiteNetwork, error)
	CreateCircuit(ctx context.Context, siteID uuid.UUID, in models.SiteCircuitInput) (*models.SiteCircuit, error)
	UpdateCircuit(ctx context.Context, siteID, id uuid.UUID, in models.SiteCircuitInput) (*models.SiteCircuit, *models.SiteCircuit, error)
	DeleteCircuit(ctx context.Context, siteID, id uuid.UUID) (*models.SiteCircuit, error)
}

// RegisterSiteProfileRoutes mounts a site's profile. Anyone who can see the
// site reads it; editable sharers and admins change it.
func RegisterSiteProfileRoutes(rg *gin.RouterGroup, profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) {
	g := rg.Group("/sites/:id")
	g.GET("/profile", getSiteProfileHandler(profiles, sites))
	g.PUT("/notes", putSiteNotesHandler(profiles, sites, audit))
	g.POST("/networks", createSiteNetworkHandler(profiles, sites, audit))
	g.PUT("/networks/:networkId", updateSiteNetworkHandler(profiles, sites, audit))
	g.DELETE("/networks/:networkId", deleteSiteNetworkHandler(profiles, sites, audit))
	g.POST("/circuits", createSiteCircuitHandler(profiles, sites, audit))
	g.PUT("/circuits/:circuitId", updateSiteCircuitHandler(profiles, sites, audit))
	g.DELETE("/circuits/:circuitId", deleteSiteCircuitHandler(profiles, sites, audit))
}

// siteForChange parses :id and requires edit access to the site, answering
// 404 or 403 otherwise.
func siteForChange(c *gin.Context, sites siteAccessChecker) (uuid.UUID, bool) {
	id, ok := parseSiteID(c)
	if !ok {
		return uuid.Nil, false
	}
	return id, requireSiteLevel(c, sites, id, services.SiteAccessEditable)
}

// parseChildID reads a network or circuit id from the path.
func parseChildID(c *gin.Context, param, what string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(param))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid "+what+" id")
		return uuid.Nil, false
	}
	return id, true
}

// respondSiteProfileError maps SiteProfileService errors to responses.
func respondSiteProfileError(c *gin.Context, op string, err error) {
	var taken *services.SubnetTakenError
	switch {
	case errors.As(err, &taken), errors.Is(err, services.ErrCircuitPortNotAtSite):
		respondError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, services.ErrSiteNetworkNotFound), errors.Is(err, services.ErrSiteCircuitNotFound):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrSiteNotFound):
		respondError(c, http.StatusNotFound, "site not found")
	default:
		respondInternal(c, op, err)
	}
}

func networkSummary(n *models.SiteNetwork) map[string]any {
	return map[string]any{"name": n.Name, "cidr": n.CIDR, "vlan": n.VLAN, "gateway": n.Gateway, "note": n.Note}
}

// circuitSummary is what the audit log keeps of a circuit. Account number,
// support phone and notes are left out: they can hold things (PINs, account
// details) the audit log should not spread.
func circuitSummary(c *models.SiteCircuit) map[string]any {
	return map[string]any{"provider": c.Provider, "circuit_ref": c.CircuitRef, "kind": c.Kind,
		"download_mbps": c.DownloadMbps, "upload_mbps": c.UploadMbps, "interface_id": c.InterfaceID}
}

func getSiteProfileHandler(profiles siteProfileStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok || !requireSiteLevel(c, sites, id, services.SiteAccessReadonly) {
			return
		}
		p, err := profiles.Profile(c.Request.Context(), id)
		if err != nil {
			respondSiteProfileError(c, "getSiteProfile", err)
			return
		}
		respondSuccess(c, http.StatusOK, p)
	}
}

func putSiteNotesHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		var body struct {
			Notes string `json:"notes"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		notes, err := models.NormalizeSiteNotes(body.Notes)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		before, after, err := profiles.SetNotes(c.Request.Context(), id, notes)
		if err != nil {
			respondSiteProfileError(c, "putSiteNotes", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteNotesUpdated, models.ResourceSite, &id,
			models.AuditChanges{Before: map[string]any{"notes": before}, After: map[string]any{"notes": after}})
		respondSuccess(c, http.StatusOK, gin.H{"notes": after})
	}
}

func bindNetwork(c *gin.Context) (models.SiteNetworkInput, bool) {
	var raw models.SiteNetworkInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return models.SiteNetworkInput{}, false
	}
	in, err := models.NormalizeSiteNetworkInput(raw)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return models.SiteNetworkInput{}, false
	}
	return in, true
}

func bindCircuit(c *gin.Context) (models.SiteCircuitInput, bool) {
	var raw models.SiteCircuitInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return models.SiteCircuitInput{}, false
	}
	in, err := models.NormalizeSiteCircuitInput(raw)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return models.SiteCircuitInput{}, false
	}
	return in, true
}

func createSiteNetworkHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		in, ok := bindNetwork(c)
		if !ok {
			return
		}
		n, err := profiles.CreateNetwork(c.Request.Context(), id, in)
		if err != nil {
			respondSiteProfileError(c, "createSiteNetwork", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteNetworkCreated, models.ResourceSite, &id,
			models.AuditChanges{Summary: networkSummary(n)})
		respondSuccess(c, http.StatusCreated, n)
	}
}

func updateSiteNetworkHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		networkID, ok := parseChildID(c, "networkId", "network")
		if !ok {
			return
		}
		in, ok := bindNetwork(c)
		if !ok {
			return
		}
		before, after, err := profiles.UpdateNetwork(c.Request.Context(), id, networkID, in)
		if err != nil {
			respondSiteProfileError(c, "updateSiteNetwork", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteNetworkUpdated, models.ResourceSite, &id,
			models.AuditChanges{Before: networkSummary(before), After: networkSummary(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteSiteNetworkHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		networkID, ok := parseChildID(c, "networkId", "network")
		if !ok {
			return
		}
		n, err := profiles.DeleteNetwork(c.Request.Context(), id, networkID)
		if err != nil {
			respondSiteProfileError(c, "deleteSiteNetwork", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteNetworkDeleted, models.ResourceSite, &id,
			models.AuditChanges{Summary: networkSummary(n)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func createSiteCircuitHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		in, ok := bindCircuit(c)
		if !ok {
			return
		}
		ckt, err := profiles.CreateCircuit(c.Request.Context(), id, in)
		if err != nil {
			respondSiteProfileError(c, "createSiteCircuit", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteCircuitCreated, models.ResourceSite, &id,
			models.AuditChanges{Summary: circuitSummary(ckt)})
		respondSuccess(c, http.StatusCreated, ckt)
	}
}

func updateSiteCircuitHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		circuitID, ok := parseChildID(c, "circuitId", "circuit")
		if !ok {
			return
		}
		in, ok := bindCircuit(c)
		if !ok {
			return
		}
		before, after, err := profiles.UpdateCircuit(c.Request.Context(), id, circuitID, in)
		if err != nil {
			respondSiteProfileError(c, "updateSiteCircuit", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteCircuitUpdated, models.ResourceSite, &id,
			models.AuditChanges{Before: circuitSummary(before), After: circuitSummary(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteSiteCircuitHandler(profiles siteProfileStore, sites siteAccessChecker, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := siteForChange(c, sites)
		if !ok {
			return
		}
		circuitID, ok := parseChildID(c, "circuitId", "circuit")
		if !ok {
			return
		}
		ckt, err := profiles.DeleteCircuit(c.Request.Context(), id, circuitID)
		if err != nil {
			respondSiteProfileError(c, "deleteSiteCircuit", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteCircuitDeleted, models.ResourceSite, &id,
			models.AuditChanges{Summary: circuitSummary(ckt)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}
