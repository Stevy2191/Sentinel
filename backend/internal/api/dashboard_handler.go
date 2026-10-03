package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/dashboards"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// dashboardStore is what the dashboard handlers need from dashboards.Service.
type dashboardStore interface {
	List(ctx context.Context, v dashboards.Viewer, siteID *uuid.UUID) ([]dashboards.DashboardView, error)
	Get(ctx context.Context, v dashboards.Viewer, id uuid.UUID) (*dashboards.DashboardDetail, error)
	Create(ctx context.Context, v dashboards.Viewer, in dashboards.CreateInput) (*dashboards.DashboardDetail, error)
	Save(ctx context.Context, v dashboards.Viewer, id uuid.UUID, in dashboards.SaveInput) (*dashboards.DashboardDetail, error)
	Delete(ctx context.Context, v dashboards.Viewer, id uuid.UUID) (*models.Dashboard, error)
	ListShares(ctx context.Context, v dashboards.Viewer, id uuid.UUID) ([]dashboards.ShareView, error)
	UpsertShare(ctx context.Context, v dashboards.Viewer, id, userID uuid.UUID, permission string) error
	RemoveShare(ctx context.Context, v dashboards.Viewer, id, userID uuid.UUID) error
	PublicLink(ctx context.Context, v dashboards.Viewer, id uuid.UUID) (*dashboards.LinkView, error)
	CreatePublicLink(ctx context.Context, v dashboards.Viewer, id uuid.UUID) (*models.DashboardPublicLink, bool, error)
	RevokePublicLink(ctx context.Context, v dashboards.Viewer, id uuid.UUID) error
	Widget(ctx context.Context, v dashboards.Viewer, dashboardID, widgetID uuid.UUID) (*models.DashboardWidget, *dashboards.DashboardView, error)
	ResolveToken(ctx context.Context, token string) (*models.Dashboard, error)
	PublicLayout(ctx context.Context, d *models.Dashboard) (*dashboards.PublicDashboard, error)
	PublicWidget(ctx context.Context, d *models.Dashboard, widgetID uuid.UUID) (*models.DashboardWidget, error)
}

// widgetResolver is what the data routes need from dashboards.Resolver.
type widgetResolver interface {
	Resolve(ctx context.Context, v dashboards.Viewer, w *models.DashboardWidget, override string) (*dashboards.Response, error)
	ResolvePublic(ctx context.Context, d *models.Dashboard, w *models.DashboardWidget) (*dashboards.Response, error)
	Preview(ctx context.Context, v dashboards.Viewer, typ string, cfg json.RawMessage, override string) (*dashboards.Response, error)
}

// RegisterDashboardRoutes mounts /dashboards. Reading and editing are open to
// signed-in users and decided per dashboard by the service; public links are
// admin-only and re-checked against the database by RequireAdmin.
func RegisterDashboardRoutes(rg *gin.RouterGroup, store dashboardStore, resolver widgetResolver, audit auditRecorder, users adminChecker) {
	g := rg.Group("/dashboards")
	g.GET("", listDashboardsHandler(store))
	g.POST("", createDashboardHandler(store, audit))
	g.GET("/:id", getDashboardHandler(store))
	g.PUT("/:id", saveDashboardHandler(store, audit))
	g.DELETE("/:id", deleteDashboardHandler(store, audit))
	g.GET("/:id/shares", listDashboardSharesHandler(store))
	g.PUT("/:id/shares/:user_id", shareDashboardHandler(store, audit))
	g.DELETE("/:id/shares/:user_id", unshareDashboardHandler(store, audit))
	g.GET("/:id/widgets/:wid/data", widgetDataHandler(store, resolver))
	g.POST("/:id/widgets/preview", widgetPreviewHandler(store, resolver))

	admin := g.Group("", RequireAdmin(users))
	admin.GET("/:id/public-link", getPublicLinkHandler(store))
	admin.POST("/:id/public-link", createPublicLinkHandler(store, audit))
	admin.DELETE("/:id/public-link", revokePublicLinkHandler(store, audit))
}

// dashboardViewer is the signed-in caller as a dashboards.Viewer.
func dashboardViewer(c *gin.Context) (dashboards.Viewer, bool) {
	userID, _, isAdmin, ok := GetUserFromContext(c)
	if !ok {
		respondAuthError(c, http.StatusUnauthorized, "authentication required")
		return dashboards.Viewer{}, false
	}
	return dashboards.Viewer{UserID: userID, IsAdmin: isAdmin}, true
}

// respondDashboardError maps dashboards errors to responses. 404 covers
// "missing" and "not yours" alike.
func respondDashboardError(c *gin.Context, op string, err error) {
	var vc *dashboards.VersionConflictError
	var we *dashboards.WidgetError
	var fe *dashboards.FieldError
	switch {
	case errors.Is(err, dashboards.ErrNotFound), errors.Is(err, dashboards.ErrSiteNotFound),
		errors.Is(err, dashboards.ErrWidgetNotFound), errors.Is(err, dashboards.ErrNoLink):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, dashboards.ErrForbidden), errors.Is(err, dashboards.ErrPublished):
		respondError(c, http.StatusForbidden, err.Error())
	case errors.As(err, &vc):
		c.JSON(http.StatusConflict, gin.H{"success": false, "error": vc.Error(), "current_version": vc.Current})
	case errors.As(err, &we):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": we.Error(), "widget_index": we.Index, "field": we.Field})
	case errors.As(err, &fe):
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": fe.Error(), "field": fe.Field})
	case errors.Is(err, dashboards.ErrInvalid), errors.Is(err, dashboards.ErrShareSiteDashboard),
		errors.Is(err, dashboards.ErrShareOwner), errors.Is(err, dashboards.ErrUnknownUser):
		respondError(c, http.StatusBadRequest, err.Error())
	default:
		respondInternal(c, op, err)
	}
}

func dashboardSummary(d *models.Dashboard) map[string]any {
	return map[string]any{"name": d.Name, "site_id": d.SiteID, "owner_id": d.OwnerID, "version": d.Version}
}

func listDashboardsHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		var siteID *uuid.UUID
		if raw := c.Query("site_id"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				respondError(c, http.StatusBadRequest, "site_id must be a UUID")
				return
			}
			siteID = &id
		}
		list, err := store.List(c.Request.Context(), v, siteID)
		if err != nil {
			respondDashboardError(c, "listDashboards", err)
			return
		}
		respondSuccess(c, http.StatusOK, list)
	}
}

func createDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		var in dashboards.CreateInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		d, err := store.Create(c.Request.Context(), v, in)
		if err != nil {
			respondDashboardError(c, "createDashboard", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardCreated, models.ResourceDashboard,
			&d.ID, models.AuditChanges{Summary: dashboardSummary(&d.Dashboard)})
		respondSuccess(c, http.StatusCreated, d)
	}
}

func getDashboardHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		d, err := store.Get(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "getDashboard", err)
			return
		}
		respondSuccess(c, http.StatusOK, d)
	}
}

func saveDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		var in dashboards.SaveInput
		if err := c.ShouldBindJSON(&in); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		d, err := store.Save(c.Request.Context(), v, id, in)
		if err != nil {
			respondDashboardError(c, "saveDashboard", err)
			return
		}
		after := dashboardSummary(&d.Dashboard)
		after["widget_count"] = len(d.Widgets)
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardUpdated, models.ResourceDashboard,
			&id, models.AuditChanges{After: after})
		respondSuccess(c, http.StatusOK, d)
	}
}

func deleteDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		d, err := store.Delete(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "deleteDashboard", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardDeleted, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: dashboardSummary(d)})
		respondSuccess(c, http.StatusOK, gin.H{"deleted": true})
	}
}

func listDashboardSharesHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		shares, err := store.ListShares(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "listDashboardShares", err)
			return
		}
		if shares == nil {
			shares = []dashboards.ShareView{}
		}
		respondSuccess(c, http.StatusOK, shares)
	}
}

func shareDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		userID, ok := parseUUIDParam(c, "user_id", "invalid user id")
		if !ok {
			return
		}
		var body struct {
			Permission string `json:"permission"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := store.UpsertShare(c.Request.Context(), v, id, userID, body.Permission); err != nil {
			respondDashboardError(c, "shareDashboard", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardShared, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: map[string]any{"user_id": userID, "permission": body.Permission}})
		respondSuccess(c, http.StatusOK, gin.H{"shared": true})
	}
}

func unshareDashboardHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		userID, ok := parseUUIDParam(c, "user_id", "invalid user id")
		if !ok {
			return
		}
		if err := store.RemoveShare(c.Request.Context(), v, id, userID); err != nil {
			respondDashboardError(c, "unshareDashboard", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardUnshared, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: map[string]any{"user_id": userID}})
		respondSuccess(c, http.StatusOK, gin.H{"unshared": true})
	}
}

// rangeOverride reads ?range=, answering 400 for an unknown key.
func rangeOverride(c *gin.Context) (string, bool) {
	r := c.Query("range")
	if r != "" && !dashboards.ValidRange(r) {
		respondError(c, http.StatusBadRequest, "range must be 1h, 6h, 24h, 7d, 30d, 90d or 1y")
		return "", false
	}
	return r, true
}

func widgetDataHandler(store dashboardStore, resolver widgetResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		wid, ok := parseUUIDParam(c, "wid", "invalid widget id")
		if !ok {
			return
		}
		override, ok := rangeOverride(c)
		if !ok {
			return
		}
		w, _, err := store.Widget(c.Request.Context(), v, id, wid)
		if err != nil {
			respondDashboardError(c, "widgetData", err)
			return
		}
		resp, err := resolver.Resolve(c.Request.Context(), v, w, override)
		if err != nil {
			respondDashboardError(c, "widgetData", err)
			return
		}
		respondSuccess(c, http.StatusOK, resp)
	}
}

type previewRequest struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
	Range  string          `json:"range"`
}

// widgetPreviewHandler resolves an unsaved widget for its editor. It needs
// edit access to the dashboard, and resolves as the editor, so a preview can
// never show what the editor could not see anyway.
func widgetPreviewHandler(store dashboardStore, resolver widgetResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		var req previewRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Range != "" && !dashboards.ValidRange(req.Range) {
			respondError(c, http.StatusBadRequest, "range must be 1h, 6h, 24h, 7d, 30d, 90d or 1y")
			return
		}
		d, err := store.Get(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "widgetPreview", err)
			return
		}
		if d.Access != dashboards.LevelEdit.String() && d.Access != dashboards.LevelManage.String() {
			respondDashboardError(c, "widgetPreview", dashboards.ErrForbidden)
			return
		}
		resp, err := resolver.Preview(c.Request.Context(), v, req.Type, req.Config, req.Range)
		if err != nil {
			respondDashboardError(c, "widgetPreview", err)
			return
		}
		respondSuccess(c, http.StatusOK, resp)
	}
}

func getPublicLinkHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		link, err := store.PublicLink(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "getPublicLink", err)
			return
		}
		respondSuccess(c, http.StatusOK, link)
	}
}

func createPublicLinkHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		link, replaced, err := store.CreatePublicLink(c.Request.Context(), v, id)
		if err != nil {
			respondDashboardError(c, "createPublicLink", err)
			return
		}
		// The token itself is never written to the audit log.
		summary := map[string]any{"dashboard_id": id}
		if replaced {
			summary["regenerated"] = true
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardLinkCreated, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: summary})
		respondSuccess(c, http.StatusOK, link)
	}
}

func revokePublicLinkHandler(store dashboardStore, audit auditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, ok := dashboardViewer(c)
		if !ok {
			return
		}
		id, ok := parseUUIDParam(c, "id", "invalid dashboard id")
		if !ok {
			return
		}
		if err := store.RevokePublicLink(c.Request.Context(), v, id); err != nil {
			respondDashboardError(c, "revokePublicLink", err)
			return
		}
		audit.Record(c.Request.Context(), actorFrom(c), models.ActionDashboardLinkRevoked, models.ResourceDashboard,
			&id, models.AuditChanges{Summary: map[string]any{"dashboard_id": id}})
		respondSuccess(c, http.StatusOK, gin.H{"revoked": true})
	}
}
