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

// credentialStore is what the handlers need from SNMPCredentialService.
type credentialStore interface {
	List(ctx context.Context) ([]services.CredentialView, error)
	Create(ctx context.Context, in models.CredentialInput, by uuid.UUID) (services.CredentialView, error)
	Update(ctx context.Context, id uuid.UUID, in models.CredentialInput) (services.CredentialView, services.CredentialView, error)
	Delete(ctx context.Context, id uuid.UUID) (services.CredentialView, error)
	ForSite(ctx context.Context, siteID uuid.UUID) ([]services.CredentialOption, error)
}

// RegisterSNMPCredentialRoutes mounts credential profile management (admin)
// and the per-site option list (editors of that site).
func RegisterSNMPCredentialRoutes(rg *gin.RouterGroup, creds credentialStore, sites siteAccessChecker, audit auditRecorder, users adminChecker) {
	admin := rg.Group("/snmp-credentials", RequireAdmin(users))
	admin.GET("", listCredentialsHandler(creds))
	admin.POST("", createCredentialHandler(creds, audit))
	admin.PUT("/:id", updateCredentialHandler(creds, audit))
	admin.DELETE("/:id", deleteCredentialHandler(creds, audit))

	rg.GET("/sites/:id/credentials", siteCredentialOptionsHandler(creds, sites))
}

func respondCredentialError(c *gin.Context, op string, err error) {
	switch {
	case errors.Is(err, services.ErrCredentialNotFound):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, services.ErrCredentialNameTaken), errors.Is(err, services.ErrCredentialInUse),
		errors.Is(err, services.ErrCredentialSiteMismatch):
		respondError(c, http.StatusConflict, err.Error())
	case errors.Is(err, services.ErrSiteNotFound):
		respondError(c, http.StatusBadRequest, "no such site")
	default:
		// Validation errors from NormalizeCredentialInput are plain errors
		// with a user-facing message and never contain a secret.
		if isInternal(err) {
			respondInternal(c, op, err)
			return
		}
		respondError(c, http.StatusBadRequest, err.Error())
	}
}

// isInternal separates wrapped database/crypto failures (which carry "saving",
// "loading", "decrypting", ... prefixes from the service) from validation
// messages.
func isInternal(err error) bool {
	var wrapped interface{ Unwrap() error }
	return errors.As(err, &wrapped)
}

func credentialAudit(v services.CredentialView) map[string]any {
	// Never a secret: the view has none.
	return map[string]any{"name": v.Name, "version": v.Version, "site_id": v.SiteID}
}

func listCredentialsHandler(creds credentialStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		list, err := creds.List(c.Request.Context())
		if err != nil {
			respondInternal(c, "listCredentials", err)
			return
		}
		if list == nil {
			list = []services.CredentialView{}
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func createCredentialHandler(creds credentialStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in models.CredentialInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		userID, _, _, _ := GetUserFromContext(c)
		v, err := creds.Create(c.Request.Context(), in, userID)
		if err != nil {
			respondCredentialError(c, "createCredential", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionCredentialCreated, models.ResourceSNMPCredential,
			&v.ID, models.AuditChanges{Summary: credentialAudit(v)})
		respondSuccess(c, http.StatusCreated, v)
	}
}

func updateCredentialHandler(creds credentialStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid credential id")
			return
		}
		var in models.CredentialInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		before, after, err := creds.Update(c.Request.Context(), id, in)
		if err != nil {
			respondCredentialError(c, "updateCredential", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionCredentialUpdated, models.ResourceSNMPCredential,
			&id, models.AuditChanges{Before: credentialAudit(before), After: credentialAudit(after)})
		respondSuccess(c, http.StatusOK, after)
	}
}

func deleteCredentialHandler(creds credentialStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid credential id")
			return
		}
		v, err := creds.Delete(c.Request.Context(), id)
		if err != nil {
			respondCredentialError(c, "deleteCredential", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionCredentialDeleted, models.ResourceSNMPCredential,
			&id, models.AuditChanges{Summary: credentialAudit(v)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

// siteCredentialOptionsHandler lists profile names usable at a site, for
// people who can add devices there.
func siteCredentialOptionsHandler(creds credentialStore, sites siteAccessChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		siteID, ok := parseSiteID(c)
		if !ok {
			return
		}
		if !requireSiteLevel(c, sites, siteID, services.SiteAccessEditable) {
			return
		}
		opts, err := creds.ForSite(c.Request.Context(), siteID)
		if err != nil {
			respondInternal(c, "siteCredentialOptions", err)
			return
		}
		if opts == nil {
			opts = []services.CredentialOption{}
		}
		respondSuccess(c, http.StatusOK, opts)
	}
}

// requireSiteLevel writes the response and returns false unless the caller has
// at least want on the site: no access is a 404, too little a 403.
func requireSiteLevel(c *gin.Context, sites siteAccessChecker, siteID uuid.UUID, want services.SiteAccessLevel) bool {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondAuthError(c, http.StatusUnauthorized, "authentication required")
		return false
	}
	level, err := sites.SiteAccess(c.Request.Context(), userID, isAdmin, siteID)
	if errors.Is(err, services.ErrSiteNotFound) || (err == nil && level == services.SiteAccessNone) {
		respondError(c, http.StatusNotFound, "site not found")
		return false
	}
	if err != nil {
		respondInternal(c, "site access", err)
		return false
	}
	if level < want {
		respondError(c, http.StatusForbidden, "you need edit access to this site")
		return false
	}
	return true
}
