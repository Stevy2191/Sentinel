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

// siteStore is what the site handlers need from SiteService. An interface so
// the handlers are tested without a database.
type siteStore interface {
	List(ctx context.Context, userID uuid.UUID, isAdmin bool) ([]models.Site, error)
	Get(ctx context.Context, id uuid.UUID) (*models.Site, error)
	SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)
	Create(ctx context.Context, in models.SiteInput, createdBy uuid.UUID) (*models.Site, error)
	Update(ctx context.Context, id uuid.UUID, in models.SiteInput) (*models.Site, *models.Site, error)
	Delete(ctx context.Context, id uuid.UUID) (*models.Site, error)
	ListShares(ctx context.Context, siteID uuid.UUID) ([]services.SiteShareView, error)
	UpsertShare(ctx context.Context, siteID, userID, sharedBy uuid.UUID, permission string) (*models.SiteSharing, error)
	RemoveShare(ctx context.Context, siteID, userID uuid.UUID) error
}

// siteWithAccess is a site plus what the caller may do with it, so the page
// knows which controls to show without a second request.
type siteWithAccess struct {
	models.Site
	Access string `json:"access"`
}

type shareSiteRequest struct {
	UserID     uuid.UUID `json:"user_id"`
	Permission string    `json:"permission"`
}

// RegisterSiteRoutes mounts /sites. Reading is open to any signed-in user and
// filtered by access; everything that changes a site or its sharing is
// admin-only.
func RegisterSiteRoutes(rg *gin.RouterGroup, sites siteStore, audit auditRecorder, users adminChecker) {
	g := rg.Group("/sites")
	g.GET("", listSitesHandler(sites))
	g.GET("/:id", getSiteHandler(sites))

	admin := g.Group("", RequireAdmin(users))
	admin.POST("", createSiteHandler(sites, audit))
	admin.PUT("/:id", updateSiteHandler(sites, audit))
	admin.DELETE("/:id", deleteSiteHandler(sites, audit))
	admin.GET("/:id/shares", listSiteSharesHandler(sites))
	admin.POST("/:id/shares", shareSiteHandler(sites, audit))
	admin.DELETE("/:id/shares/:user_id", unshareSiteHandler(sites, audit))
}

// parseSiteID reads :id, answering 400 for anything that is not a UUID.
func parseSiteID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respondError(c, http.StatusBadRequest, "invalid site id")
		return uuid.Nil, false
	}
	return id, true
}

// respondSiteError maps SiteService errors to responses.
func respondSiteError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrSiteNotFound):
		respondError(c, http.StatusNotFound, "site not found")
	case errors.Is(err, services.ErrSiteNameTaken), errors.Is(err, services.ErrSiteNotEmpty):
		respondError(c, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrSiteShareNotFound):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrSiteShareUnknownUser):
		respondError(c, http.StatusBadRequest, err.Error())
	default:
		respondInternal(c, op, err)
	}
}

func siteSummary(s *models.Site) map[string]any {
	return map[string]any{"name": s.Name, "description": s.Description, "street": s.Street, "city": s.City,
		"state": s.State, "zip": s.Zip}
}

func listSitesHandler(sites siteStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _, isAdmin, _ := GetUserFromContext(c)
		list, err := sites.List(c.Request.Context(), userID, isAdmin)
		if err != nil {
			respondSiteError(c, "listSites", err)
			return
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func getSiteHandler(sites siteStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		userID, _, isAdmin, _ := GetUserFromContext(c)
		level, err := sites.SiteAccess(c.Request.Context(), userID, isAdmin, id)
		if err != nil {
			respondSiteError(c, "getSite", err)
			return
		}
		// "Not yours" answers exactly like "does not exist", so a site's
		// existence is never confirmed to someone without access.
		if level == services.SiteAccessNone {
			respondError(c, http.StatusNotFound, "site not found")
			return
		}
		site, err := sites.Get(c.Request.Context(), id)
		if err != nil {
			respondSiteError(c, "getSite", err)
			return
		}
		respondSuccess(c, http.StatusOK, siteWithAccess{Site: *site, Access: level.String()})
	}
}

// bindSiteInput reads and normalizes a create/update body, answering 400 on
// any problem.
func bindSiteInput(c *gin.Context) (models.SiteInput, bool) {
	var raw models.SiteInput
	if err := c.ShouldBindJSON(&raw); err != nil {
		respondError(c, http.StatusBadRequest, "invalid request body")
		return models.SiteInput{}, false
	}
	in, err := models.NormalizeSiteInput(raw)
	if err != nil {
		respondError(c, http.StatusBadRequest, err.Error())
		return models.SiteInput{}, false
	}
	return in, true
}

func createSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		in, ok := bindSiteInput(c)
		if !ok {
			return
		}
		userID, _, _, _ := GetUserFromContext(c)
		site, err := sites.Create(c.Request.Context(), in, userID)
		if err != nil {
			respondSiteError(c, "createSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteCreated, models.ResourceSite,
			&site.ID, models.AuditChanges{Summary: siteSummary(site)})
		respondSuccess(c, http.StatusCreated, site)
	}
}

func updateSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		in, ok := bindSiteInput(c)
		if !ok {
			return
		}
		before, after, err := sites.Update(c.Request.Context(), id, in)
		if err != nil {
			respondSiteError(c, "updateSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteUpdated, models.ResourceSite,
			&id, models.AuditChanges{Before: siteSummary(before), After: siteSummary(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		site, err := sites.Delete(c.Request.Context(), id)
		if err != nil {
			respondSiteError(c, "deleteSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteDeleted, models.ResourceSite,
			&id, models.AuditChanges{Summary: siteSummary(site)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func listSiteSharesHandler(sites siteStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		shares, err := sites.ListShares(c.Request.Context(), id)
		if err != nil {
			respondSiteError(c, "listSiteShares", err)
			return
		}
		if shares == nil {
			shares = []services.SiteShareView{}
		}
		respondSuccess(c, http.StatusOK, shares)
	}
}

func shareSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		var req shareSiteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.UserID == uuid.Nil {
			respondError(c, http.StatusBadRequest, "user_id is required")
			return
		}
		if req.Permission == "" {
			req.Permission = models.PermissionReadonly
		}
		if err := models.ValidateSharePermission(req.Permission); err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			return
		}
		sharedBy, _, _, _ := GetUserFromContext(c)
		share, err := sites.UpsertShare(c.Request.Context(), id, req.UserID, sharedBy, req.Permission)
		if err != nil {
			respondSiteError(c, "shareSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteShared, models.ResourceSite,
			&id, models.AuditChanges{Summary: map[string]any{"user_id": req.UserID, "permission": req.Permission}})
		respondSuccess(c, http.StatusOK, share)
	}
}

func unshareSiteHandler(sites siteStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseSiteID(c)
		if !ok {
			return
		}
		userID, err := uuid.Parse(c.Param("user_id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid user_id: must be a UUID")
			return
		}
		if err := sites.RemoveShare(c.Request.Context(), id, userID); err != nil {
			respondSiteError(c, "unshareSite", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionSiteUnshared, models.ResourceSite,
			&id, models.AuditChanges{Summary: map[string]any{"user_id": userID}})
		respondSuccess(c, http.StatusOK, gin.H{"removed": true})
	}
}
