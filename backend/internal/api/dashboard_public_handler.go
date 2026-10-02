package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/dashboards"
)

// linkGone is what a dead public link shows, so a TV says why it is blank.
const linkGone = "This dashboard link is no longer available"

// RegisterPublicDashboardRoutes mounts the public-link routes: no login, a
// per-IP limit sized for several TVs behind one office address (spec §4).
// Every request resolves the token again, so a revoked link, a regenerated
// one, or a demoted creator stops working on the next request.
func RegisterPublicDashboardRoutes(router *gin.Engine, store dashboardStore, resolver widgetResolver) {
	limiter := NewRateLimiter(600, time.Minute, 120)
	g := router.Group("/api/v1/public/dashboards", limiter.Middleware("public-dashboard", ByIP))
	g.GET("/:token", publicDashboardHandler(store))
	g.GET("/:token/widgets/:wid/data", publicWidgetDataHandler(store, resolver))
}

func publicDashboardHandler(store dashboardStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		d, err := store.ResolveToken(c.Request.Context(), c.Param("token"))
		if errors.Is(err, dashboards.ErrNotFound) {
			respondError(c, http.StatusNotFound, linkGone)
			return
		}
		if err != nil {
			respondInternal(c, "publicDashboard", err)
			return
		}
		layout, err := store.PublicLayout(c.Request.Context(), d)
		if err != nil {
			respondInternal(c, "publicDashboard", err)
			return
		}
		respondSuccess(c, http.StatusOK, layout)
	}
}

func publicWidgetDataHandler(store dashboardStore, resolver widgetResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		wid, err := uuid.Parse(c.Param("wid"))
		if err != nil {
			respondError(c, http.StatusBadRequest, "invalid widget id")
			return
		}
		d, err := store.ResolveToken(c.Request.Context(), c.Param("token"))
		if errors.Is(err, dashboards.ErrNotFound) {
			respondError(c, http.StatusNotFound, linkGone)
			return
		}
		if err != nil {
			respondInternal(c, "publicWidgetData", err)
			return
		}
		w, err := store.PublicWidget(c.Request.Context(), d, wid)
		if errors.Is(err, dashboards.ErrWidgetNotFound) {
			respondError(c, http.StatusNotFound, "widget not found")
			return
		}
		if err != nil {
			respondInternal(c, "publicWidgetData", err)
			return
		}
		resp, err := resolver.ResolvePublic(c.Request.Context(), d, w)
		if err != nil {
			respondInternal(c, "publicWidgetData", err)
			return
		}
		respondSuccess(c, http.StatusOK, resp)
	}
}
