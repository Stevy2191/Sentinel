package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// scanSubnetRequest is the body for POST /api/v1/discovery/scan.
type scanSubnetRequest struct {
	// CIDR is the subnet to sweep, e.g. "192.168.1.0/24".
	CIDR string `json:"cidr"`
	// TimeoutMs is an optional per-host timeout override, in milliseconds.
	// Zero uses DiscoveryService's default; it is clamped to its max.
	TimeoutMs int `json:"timeout_ms"`
}

// ScanSubnetHandler handles POST /api/v1/discovery/scan. It sweeps the given
// CIDR for hosts that answer a ping (ICMP, falling back to a handful of common
// TCP ports) and returns everything that responded, so the caller can offer
// them up as candidate Ping Monitors.
//
// This makes the server originate traffic toward whatever subnet the caller
// names. That is deliberate — discovering a home lab or internal network is
// the feature's purpose — but it is still a real network scan, so it is kept
// behind authentication (like every other /api/v1 route) and bounded by
// DiscoveryService's own size/duration/rate limits rather than trusted to the
// client.
func ScanSubnetHandler(discoveryService *services.DiscoveryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req scanSubnetRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
		req.CIDR = strings.TrimSpace(req.CIDR)
		if req.CIDR == "" {
			respondError(c, http.StatusBadRequest, "cidr is required, e.g. \"192.168.1.0/24\"")
			return
		}

		var perHostTimeout time.Duration
		if req.TimeoutMs > 0 {
			perHostTimeout = time.Duration(req.TimeoutMs) * time.Millisecond
		}

		result, err := discoveryService.ScanSubnet(c.Request.Context(), req.CIDR, perHostTimeout)
		if err != nil {
			respondError(c, classifyDiscoveryError(err), err.Error())
			return
		}

		respondSuccess(c, http.StatusOK, result)
	}
}

// classifyDiscoveryError maps a DiscoveryService error to a status code. Every
// failure ScanSubnet returns today is caused by the request itself (a bad
// CIDR, an oversized subnet, or an IPv6 address), so 400 is the right default;
// a context deadline is the one exception, which is still the caller's timeout
// budget rather than a server fault.
func classifyDiscoveryError(err error) int {
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusRequestTimeout
	}
	return http.StatusBadRequest
}

// RegisterDiscoveryRoutes mounts the network discovery endpoint.
func RegisterDiscoveryRoutes(rg *gin.RouterGroup, discoveryService *services.DiscoveryService) {
	discovery := rg.Group("/discovery")
	discovery.POST("/scan", ScanSubnetHandler(discoveryService))
}
