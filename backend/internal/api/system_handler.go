package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/hoststats"
)

// SystemResourcesHandler handles GET /api/v1/system/resources.
//
// The Sentinel server's own CPU, memory and disk — the machine the dashboard
// is running on, not the hosts it watches. Those have agents and their own
// page; this answers "is the box I am looking at healthy", which needs no
// agent installed and is available on a fresh install.
//
// Served from a background sampler, so the cost is a mutex read rather than a
// pass over /proc per request.
func SystemResourcesHandler(sampler *hoststats.Sampler) gin.HandlerFunc {
	return func(c *gin.Context) {
		respondSuccess(c, http.StatusOK, sampler.Snapshot())
	}
}

// VersionHandler handles GET /api/v1/system/version.
//
// Read from the running binary rather than a config value, so the About page
// shows what is actually deployed instead of whatever a stale build-time
// constant happens to say elsewhere.
func VersionHandler(version string) gin.HandlerFunc {
	return func(c *gin.Context) {
		respondSuccess(c, http.StatusOK, gin.H{"version": version})
	}
}

// RegisterSystemRoutes mounts the host resource and version routes. Readable
// by any signed-in user: neither says anything an operator of this instance
// should not see.
func RegisterSystemRoutes(rg *gin.RouterGroup, sampler *hoststats.Sampler, version string) {
	rg.GET("/system/resources", SystemResourcesHandler(sampler))
	rg.GET("/system/version", VersionHandler(version))
}
