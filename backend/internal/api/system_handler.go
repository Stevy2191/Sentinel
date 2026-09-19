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

// RegisterSystemRoutes mounts the host resource route. Readable by any
// signed-in user: it is the same dashboard data the rest of the page shows,
// and says nothing an operator of this instance should not see.
func RegisterSystemRoutes(rg *gin.RouterGroup, sampler *hoststats.Sampler) {
	rg.GET("/system/resources", SystemResourcesHandler(sampler))
}
