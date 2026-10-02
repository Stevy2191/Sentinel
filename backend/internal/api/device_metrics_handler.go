package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// deviceMetricsLister is services.MetricsStore.DeviceMetrics.
type deviceMetricsLister interface {
	DeviceMetrics(ctx context.Context, deviceID uuid.UUID) ([]services.DeviceMetric, error)
}

// RegisterDeviceMetricsRoute mounts GET /devices/:id/metrics: the metrics and
// instances a device has, for the dashboard editor. Readonly site access.
func RegisterDeviceMetricsRoute(rg *gin.RouterGroup, metrics deviceMetricsLister, devices deviceStore, sites siteAccessChecker) {
	rg.GET("/devices/:id/metrics", func(c *gin.Context) {
		d, ok := loadDevice(c, devices, sites, services.SiteAccessReadonly)
		if !ok {
			return
		}
		list, err := metrics.DeviceMetrics(c.Request.Context(), d.ID)
		if err != nil {
			respondInternal(c, "deviceMetrics", err)
			return
		}
		respondSuccess(c, http.StatusOK, list)
	})
}
