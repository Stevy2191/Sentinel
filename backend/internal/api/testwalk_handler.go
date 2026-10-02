package api

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

var oidPattern = regexp.MustCompile(`^\d+(\.\d+)+$`)

// testWalker is MIBLibrary's TestWalk with the device's target resolved
// (services.DeviceWalker).
type testWalker interface {
	TestWalkDevice(ctx context.Context, d *services.DeviceView, oid string) (*services.TestWalkResult, error)
}

// RegisterTestWalkRoutes mounts the MIB browser's "test walk" button: it
// walks a chosen OID against one device. Access is the device's site's,
// editable (it sends SNMP to a real device, like Test connection does).
func RegisterTestWalkRoutes(rg *gin.RouterGroup, devices deviceStore, sites siteAccessChecker, walker testWalker) {
	rg.POST("/devices/:id/test-walk", func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessEditable)
		if !ok {
			return
		}
		var req struct {
			OID string `json:"oid"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || !oidPattern.MatchString(strings.Trim(req.OID, ".")) {
			respondError(c, http.StatusBadRequest, "give a numeric OID such as 1.3.6.1.2.1.1")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		defer cancel()
		res, err := walker.TestWalkDevice(ctx, d, strings.Trim(req.OID, "."))
		if err != nil {
			// Reported as a result, not a server error: see testDeviceHandler.
			respondSuccess(c, http.StatusOK, gin.H{"ok": false, "error": err.Error()})
			return
		}
		respondSuccess(c, http.StatusOK, gin.H{"ok": true, "result": res})
	})
}
