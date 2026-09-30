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
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

type fakeDevices struct {
	device    services.DeviceView
	mutations int
}

func (f *fakeDevices) List(context.Context, uuid.UUID, bool, services.DeviceFilter) ([]services.DeviceView, error) {
	return []services.DeviceView{f.device}, nil
}
func (f *fakeDevices) Get(context.Context, uuid.UUID) (*services.DeviceView, error) {
	d := f.device
	return &d, nil
}
func (f *fakeDevices) Create(_ context.Context, in models.DeviceInput, _ uuid.UUID) (*models.Device, error) {
	f.mutations++
	return &models.Device{ID: uuid.New(), SiteID: in.SiteID, Host: in.Host}, nil
}
func (f *fakeDevices) Update(_ context.Context, id uuid.UUID, in models.DeviceInput) (*models.Device, *models.Device, error) {
	f.mutations++
	return &models.Device{ID: id}, &models.Device{ID: id, SiteID: in.SiteID}, nil
}
func (f *fakeDevices) Delete(_ context.Context, id uuid.UUID) (*models.Device, error) {
	f.mutations++
	return &models.Device{ID: id}, nil
}
func (f *fakeDevices) Interfaces(context.Context, uuid.UUID, bool) ([]models.DeviceInterface, error) {
	return nil, nil
}
func (f *fakeDevices) RequestRefresh(context.Context, uuid.UUID) error { f.mutations++; return nil }

func (f *fakeDevices) UpdateDetails(_ context.Context, id uuid.UUID, p models.DeviceDetailsPatch) (*models.Device, *models.Device, error) {
	if _, err := p.Updates(); err != nil {
		return nil, nil, err
	}
	f.mutations++
	return &models.Device{ID: id}, &models.Device{ID: id}, nil
}

type fakeProber struct {
	calls    int
	unusable bool
}

func (f *fakeProber) Identify(context.Context, string, int, uuid.UUID, time.Duration, int) (snmp.System, error) {
	f.calls++
	return snmp.System{Name: "core-sw-1", ObjectID: "1.3.6.1.4.1.4413"}, nil
}
func (f *fakeProber) UsableAt(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return !f.unusable, nil
}

// fakeSiteLevels grants a level per site id; unknown sites get none.
type fakeSiteLevels map[uuid.UUID]services.SiteAccessLevel

func (f fakeSiteLevels) SiteAccess(_ context.Context, _ uuid.UUID, _ bool, id uuid.UUID) (services.SiteAccessLevel, error) {
	return f[id], nil
}

func deviceRouter(devs *fakeDevices, prober *fakeProber, levels fakeSiteLevels) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	uid := uuid.New() // one caller per router, so ByUser rate limiting sees one user
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uid)
		c.Set("username", "someone")
		c.Set("is_admin", false)
		c.Next()
	})
	RegisterDeviceRoutes(r.Group("/api/v1"), devs, prober, levels, &fakeAudit{})
	return r
}

func TestDeviceAccess(t *testing.T) {
	siteA, siteB := uuid.New(), uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: siteA}}
	path := "/api/v1/devices/" + dev.ID.String()
	body := map[string]any{"site_id": siteA, "credential_id": uuid.New(), "host": "10.0.0.2"}

	// No access to the device's site: 404 for every route, nothing changes.
	devs := &fakeDevices{device: dev}
	r := deviceRouter(devs, &fakeProber{}, fakeSiteLevels{})
	for _, rt := range []struct{ m, p string }{
		{http.MethodGet, path}, {http.MethodPut, path}, {http.MethodDelete, path},
		{http.MethodGet, path + "/interfaces"}, {http.MethodPost, path + "/refresh"},
	} {
		if w := do(r, rt.m, rt.p, body); w.Code != http.StatusNotFound {
			t.Errorf("no access %s %s: %d, want 404", rt.m, rt.p, w.Code)
		}
	}

	// Readonly: can read, cannot change.
	r = deviceRouter(devs, &fakeProber{}, fakeSiteLevels{siteA: services.SiteAccessReadonly})
	if w := do(r, http.MethodGet, path, nil); w.Code != http.StatusOK {
		t.Errorf("readonly get: %d", w.Code)
	}
	for _, rt := range []struct{ m, p string }{{http.MethodPut, path}, {http.MethodDelete, path}, {http.MethodPost, path + "/refresh"}, {http.MethodPost, "/api/v1/devices"}} {
		if w := do(r, rt.m, rt.p, body); w.Code != http.StatusForbidden {
			t.Errorf("readonly %s %s: %d, want 403", rt.m, rt.p, w.Code)
		}
	}
	if devs.mutations != 0 {
		t.Fatalf("refused requests changed data: %d", devs.mutations)
	}

	// Editable on A but not B: cannot move a device from A to B.
	r = deviceRouter(devs, &fakeProber{}, fakeSiteLevels{siteA: services.SiteAccessEditable, siteB: services.SiteAccessReadonly})
	moved := map[string]any{"site_id": siteB, "credential_id": uuid.New(), "host": "10.0.0.2"}
	if w := do(r, http.MethodPut, path, moved); w.Code != http.StatusForbidden || devs.mutations != 0 {
		t.Errorf("move to readonly site: %d, mutations %d", w.Code, devs.mutations)
	}
	if w := do(r, http.MethodPost, "/api/v1/devices", body); w.Code != http.StatusCreated {
		t.Errorf("editable create: %d, want 201", w.Code)
	}
}

// Test connection sends SNMP to an address of the caller's choosing, so it is
// rate limited per user: the 11th request in a minute is refused.
func TestDeviceTestIsRateLimited(t *testing.T) {
	site := uuid.New()
	prober := &fakeProber{}
	r := deviceRouter(&fakeDevices{}, prober, fakeSiteLevels{site: services.SiteAccessEditable})
	body := map[string]any{"site_id": site, "credential_id": uuid.New(), "host": "10.0.0.2"}
	codes := map[int]int{}
	for i := 0; i < 11; i++ {
		codes[do(r, http.MethodPost, "/api/v1/devices/test", body).Code]++
	}
	if codes[http.StatusOK] != 10 || codes[http.StatusTooManyRequests] != 1 || prober.calls != 10 {
		t.Errorf("codes %v, probes %d", codes, prober.calls)
	}
}

// Testing with another site's credential profile is refused before any SNMP
// is sent.
func TestDeviceTestChecksCredentialScope(t *testing.T) {
	site := uuid.New()
	prober := &fakeProber{unusable: true}
	r := deviceRouter(&fakeDevices{}, prober, fakeSiteLevels{site: services.SiteAccessEditable})
	w := do(r, http.MethodPost, "/api/v1/devices/test", map[string]any{"site_id": site, "credential_id": uuid.New(), "host": "10.0.0.2"})
	if w.Code != http.StatusBadRequest || prober.calls != 0 {
		t.Errorf("status %d, probes %d; want 400, 0", w.Code, prober.calls)
	}
}
