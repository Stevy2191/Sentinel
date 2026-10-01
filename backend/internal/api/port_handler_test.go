package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

type fakePorts struct {
	updates                int
	trafficFrom, trafficTo time.Time
}

func (f *fakePorts) DevicePorts(context.Context, *services.DeviceView) (*services.DevicePortsView, error) {
	return &services.DevicePortsView{Ports: []services.PortView{}}, nil
}
func (f *fakePorts) Port(_ context.Context, _ *services.DeviceView, ifIndex int) (*services.PortDetailView, error) {
	if ifIndex != 1 {
		return nil, services.ErrPortNotFound
	}
	return &services.PortDetailView{}, nil
}
func (f *fakePorts) UpdatePort(_ context.Context, _ uuid.UUID, _ int, p models.PortPatch) (*models.DeviceInterface, *models.DeviceInterface, error) {
	if err := p.Validate(); err != nil {
		return nil, nil, err
	}
	f.updates++
	return &models.DeviceInterface{}, &models.DeviceInterface{}, nil
}
func (f *fakePorts) Events(context.Context, services.PortEventFilter) ([]services.PortEventView, int64, error) {
	return []services.PortEventView{}, 0, nil
}
func (f *fakePorts) SiteSummary(context.Context, uuid.UUID) (*services.SitePortSummary, error) {
	return &services.SitePortSummary{}, nil
}
func (f *fakePorts) PhysicalInterfaceIDs(context.Context, []uuid.UUID) ([]uuid.UUID, error) {
	return []uuid.UUID{uuid.New()}, nil
}
func (f *fakePorts) SiteTraffic(_ context.Context, _ uuid.UUID, from, to time.Time) (*services.SiteTraffic, error) {
	f.trafficFrom, f.trafficTo = from, to
	return &services.SiteTraffic{NorthSouth: []services.NorthSouthPoint{}, EastWest: []services.EastWestPoint{}}, nil
}

func (f *fakePorts) UPSStatus(context.Context, *services.DeviceView) (*services.UPSStatusView, error) {
	return &services.UPSStatusView{Readings: map[string]float64{}, Conditions: []string{}}, nil
}

type fakeMetricsQuery struct{ last *services.MetricsQuery }

func (f *fakeMetricsQuery) Query(_ context.Context, q services.MetricsQuery) (*services.MetricsResult, error) {
	f.last = &q
	return &services.MetricsResult{Series: []services.MetricSeries{}}, nil
}

type fakeNetSettings struct{ patched int }

func (f *fakeNetSettings) Get(context.Context) services.NetworkSettings {
	return services.NetworkSettings{}
}
func (f *fakeNetSettings) Update(context.Context, services.NetworkSettingsPatch) (services.NetworkSettings, error) {
	f.patched++
	return services.NetworkSettings{}, nil
}

type portRig struct {
	r       *gin.Engine
	devs    *fakeDevices
	ports   *fakePorts
	metrics *fakeMetricsQuery
	netSet  *fakeNetSettings
}

func newPortRig(dev services.DeviceView, levels fakeSiteLevels, isAdmin bool) *portRig {
	gin.SetMode(gin.TestMode)
	rig := &portRig{r: gin.New(), devs: &fakeDevices{device: dev}, ports: &fakePorts{}, metrics: &fakeMetricsQuery{}, netSet: &fakeNetSettings{}}
	rig.r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	g := rig.r.Group("/api/v1")
	RegisterDeviceRoutes(g, rig.devs, &fakeProber{}, levels, &fakeAudit{})
	RegisterPortRoutes(g, rig.devs, rig.ports, levels, &fakeAudit{})
	RegisterNetworkRoutes(g, rig.devs, rig.ports, rig.metrics, rig.netSet, levels, stubUsers{user: &models.User{IsAdmin: isAdmin}})
	return rig
}

func TestPortRoutesAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	base := "/api/v1/devices/" + dev.ID.String()

	rig := newPortRig(dev, fakeSiteLevels{}, false)
	for _, p := range []string{base + "/ports", base + "/ports/1", base + "/ups", base + "/events", "/api/v1/sites/" + site.String() + "/port-events",
		"/api/v1/sites/" + site.String() + "/ports/summary"} {
		if w := do(rig.r, http.MethodGet, p, nil); w.Code != http.StatusNotFound {
			t.Errorf("no access GET %s: %d, want 404", p, w.Code)
		}
	}

	rig = newPortRig(dev, fakeSiteLevels{site: services.SiteAccessReadonly}, false)
	if w := do(rig.r, http.MethodGet, base+"/ports", nil); w.Code != http.StatusOK {
		t.Errorf("readonly ports: %d", w.Code)
	}
	if w := do(rig.r, http.MethodGet, base+"/ups", nil); w.Code != http.StatusOK {
		t.Errorf("readonly ups: %d", w.Code)
	}
	if w := do(rig.r, http.MethodGet, base+"/ports/7", nil); w.Code != http.StatusNotFound {
		t.Errorf("missing port: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/ports/1", map[string]any{"important": true}); w.Code != http.StatusForbidden || rig.ports.updates != 0 {
		t.Errorf("readonly patch: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/details", map[string]any{"model_override": "x"}); w.Code != http.StatusForbidden {
		t.Errorf("readonly details: %d", w.Code)
	}

	rig = newPortRig(dev, fakeSiteLevels{site: services.SiteAccessEditable}, false)
	if w := do(rig.r, http.MethodPatch, base+"/ports/1", map[string]any{"util_threshold_pct": 5}); w.Code != http.StatusBadRequest {
		t.Errorf("bad threshold: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/ports/1", map[string]any{"important": true}); w.Code != http.StatusOK || rig.ports.updates != 1 {
		t.Errorf("editable patch: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/details", map[string]any{"device_type": "toaster"}); w.Code != http.StatusBadRequest {
		t.Errorf("bad device type: %d", w.Code)
	}
	if w := do(rig.r, http.MethodPatch, base+"/details", map[string]any{"model_override": "UNVR"}); w.Code != http.StatusOK {
		t.Errorf("details: %d", w.Code)
	}
}

// PATCH with neighbor fields follows the same access rule as any other port
// patch: 403 for a readonly caller, 200 for an editable one.
func TestPortPatchNeighborAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	base := "/api/v1/devices/" + dev.ID.String()
	neighbor := uuid.New()
	body := map[string]any{"neighbor_device_id": neighbor.String(), "neighbor_if_index": 10}

	rig := newPortRig(dev, fakeSiteLevels{site: services.SiteAccessReadonly}, false)
	if w := do(rig.r, http.MethodPatch, base+"/ports/1", body); w.Code != http.StatusForbidden || rig.ports.updates != 0 {
		t.Errorf("readonly neighbor patch: %d updates %d", w.Code, rig.ports.updates)
	}

	rig = newPortRig(dev, fakeSiteLevels{site: services.SiteAccessEditable}, false)
	if w := do(rig.r, http.MethodPatch, base+"/ports/1", body); w.Code != http.StatusOK || rig.ports.updates != 1 {
		t.Errorf("editable neighbor patch: %d updates %d", w.Code, rig.ports.updates)
	}
}

// Review Focus 4: a foreign device in a metrics query is a 404, and nothing
// is queried.
func TestMetricsQueryAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	q := "/api/v1/network/metrics/query?metric=if_in_bps&range=24h&device_id=" + dev.ID.String()

	rig := newPortRig(dev, fakeSiteLevels{}, false)
	if w := do(rig.r, http.MethodGet, q, nil); w.Code != http.StatusNotFound || rig.metrics.last != nil {
		t.Errorf("foreign device: %d, queried %v", w.Code, rig.metrics.last != nil)
	}

	rig = newPortRig(dev, fakeSiteLevels{site: services.SiteAccessReadonly}, false)
	if w := do(rig.r, http.MethodGet, q, nil); w.Code != http.StatusOK || rig.metrics.last == nil || rig.metrics.last.DeviceIDs[0] != dev.ID {
		t.Fatalf("own device: %d %+v", w.Code, rig.metrics.last)
	}
	for _, bad := range []string{
		"/api/v1/network/metrics/query?metric=if_in_octets&range=24h&device_id=" + dev.ID.String(),
		"/api/v1/network/metrics/query?metric=if_in_bps&range=2h&device_id=" + dev.ID.String(),
		"/api/v1/network/metrics/query?metric=if_in_bps&range=24h",
		"/api/v1/network/metrics/query?metric=if_in_bps&range=24h&site_id=" + site.String(),
	} {
		if w := do(rig.r, http.MethodGet, bad, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, w.Code)
		}
	}
	w := do(rig.r, http.MethodGet, "/api/v1/network/metrics/query?metric=if_in_bps&range=24h&agg=sum&ports=physical&site_id="+site.String(), nil)
	if w.Code != http.StatusOK || !rig.metrics.last.Sum || len(rig.metrics.last.InterfaceIDs) != 1 {
		t.Errorf("site total: %d %+v", w.Code, rig.metrics.last)
	}
}

func TestSiteTrafficRoute(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	url := "/api/v1/sites/" + site.String() + "/traffic"

	rig := newPortRig(dev, fakeSiteLevels{}, false)
	if w := do(rig.r, http.MethodGet, url+"?range=24h", nil); w.Code != http.StatusNotFound {
		t.Errorf("no access: %d, want 404", w.Code)
	}

	rig = newPortRig(dev, fakeSiteLevels{site: services.SiteAccessReadonly}, false)
	if w := do(rig.r, http.MethodGet, url+"?range=2h", nil); w.Code != http.StatusBadRequest {
		t.Errorf("bad range: %d, want 400", w.Code)
	}
	if w := do(rig.r, http.MethodGet, url, nil); w.Code != http.StatusBadRequest {
		t.Errorf("missing range: %d, want 400", w.Code)
	}
	if w := do(rig.r, http.MethodGet, url+"?range=24h", nil); w.Code != http.StatusOK {
		t.Errorf("readonly access: %d, want 200", w.Code)
	}
}

func TestNetworkSettingsAdminOnly(t *testing.T) {
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: uuid.New()}}
	rig := newPortRig(dev, fakeSiteLevels{}, false)
	if w := do(rig.r, http.MethodPatch, "/api/v1/network/settings", map[string]any{"port_util_threshold_pct": 70}); w.Code != http.StatusForbidden || rig.netSet.patched != 0 {
		t.Errorf("member patch: %d", w.Code)
	}
	rig = newPortRig(dev, fakeSiteLevels{}, true)
	if w := do(rig.r, http.MethodPatch, "/api/v1/network/settings", map[string]any{"port_util_threshold_pct": 70}); w.Code != http.StatusOK {
		t.Errorf("admin patch: %d", w.Code)
	}
}
