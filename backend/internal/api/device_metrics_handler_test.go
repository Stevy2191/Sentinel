package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type fakeDeviceMetrics struct{ called bool }

func (f *fakeDeviceMetrics) DeviceMetrics(context.Context, uuid.UUID) ([]services.DeviceMetric, error) {
	f.called = true
	return []services.DeviceMetric{{Metric: "if_in_bps", Label: "Traffic in", Unit: "bps"}}, nil
}

func TestDeviceMetricsNeedsSiteAccess(t *testing.T) {
	site := uuid.New()
	devs := &fakeDevices{device: services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}}
	for level, want := range map[services.SiteAccessLevel]int{services.SiteAccessNone: http.StatusNotFound, services.SiteAccessReadonly: http.StatusOK} {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user_id", uuid.New())
			c.Set("username", "u")
			c.Set("is_admin", false)
			c.Next()
		})
		lister := &fakeDeviceMetrics{}
		RegisterDeviceMetricsRoute(r.Group("/api/v1"), lister, devs, fakeSiteLevels{site: level})
		w := do(r, http.MethodGet, "/api/v1/devices/"+devs.device.ID.String()+"/metrics", nil)
		if w.Code != want || lister.called != (want == http.StatusOK) {
			t.Errorf("level %v: status %d, listed %v; want %d", level, w.Code, lister.called, want)
		}
	}
}
