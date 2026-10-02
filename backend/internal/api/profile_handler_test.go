package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// fakeProfiles is an in-memory profileStore. setCalls/setMode record the
// last SetDeviceProfile call so a test can check what reached the service.
type fakeProfiles struct {
	setCalls int
	setMode  string
}

func (f *fakeProfiles) List(context.Context) ([]services.ProfileView, error) {
	return []services.ProfileView{}, nil
}
func (f *fakeProfiles) Get(context.Context, uuid.UUID) (*services.ProfileDetail, error) {
	return &services.ProfileDetail{}, nil
}
func (f *fakeProfiles) Create(context.Context, services.ProfileInput) (*models.MetricProfile, error) {
	return &models.MetricProfile{ID: uuid.New()}, nil
}
func (f *fakeProfiles) Update(_ context.Context, id uuid.UUID, _ services.ProfileInput) (*models.MetricProfile, *models.MetricProfile, error) {
	return &models.MetricProfile{ID: id}, &models.MetricProfile{ID: id}, nil
}
func (f *fakeProfiles) Delete(_ context.Context, id uuid.UUID) (*models.MetricProfile, error) {
	return &models.MetricProfile{ID: id}, nil
}
func (f *fakeProfiles) Copy(context.Context, uuid.UUID) (*services.ProfileDetail, error) {
	return &services.ProfileDetail{MetricProfile: models.MetricProfile{ID: uuid.New()}}, nil
}
func (f *fakeProfiles) GetMetric(context.Context, uuid.UUID) (*models.ProfileMetric, error) {
	return &models.ProfileMetric{Key: "acme_fan_state"}, nil
}

// CreateMetric runs the real validation, so a handler test can prove an
// invalid body never reaches the service as a "valid" metric.
func (f *fakeProfiles) CreateMetric(_ context.Context, profileID uuid.UUID, m models.ProfileMetric) (*models.ProfileMetric, error) {
	if err := services.ValidateMetric(m); err != nil {
		return nil, err
	}
	m.ProfileID = profileID
	return &m, nil
}
func (f *fakeProfiles) UpdateMetric(_ context.Context, id uuid.UUID, _ models.ProfileMetric) (*models.ProfileMetric, *models.ProfileMetric, error) {
	return &models.ProfileMetric{ID: id}, &models.ProfileMetric{ID: id}, nil
}
func (f *fakeProfiles) DeleteMetric(_ context.Context, id uuid.UUID) (*models.ProfileMetric, error) {
	return &models.ProfileMetric{ID: id}, nil
}
func (f *fakeProfiles) MetricDataDevices(context.Context, string) (int, error) { return 0, nil }
func (f *fakeProfiles) ProfilesForDevice(context.Context, models.Device) ([]services.ProfileWithMetrics, error) {
	return []services.ProfileWithMetrics{}, nil
}
func (f *fakeProfiles) DeviceProfiles(context.Context, models.Device) ([]services.DeviceProfileView, error) {
	return []services.DeviceProfileView{}, nil
}
func (f *fakeProfiles) SetDeviceProfile(_ context.Context, _, _ uuid.UUID, mode string) error {
	f.setCalls++
	f.setMode = mode
	return nil
}

// fakeMetricPreviewer is a metricPreviewer: it records the metric it was
// asked to preview and returns either err or preview, as a test sets them up.
type fakeMetricPreviewer struct {
	calls   int
	metric  models.ProfileMetric
	err     error
	preview *services.MetricPreview
}

func (f *fakeMetricPreviewer) PreviewDevice(_ context.Context, _ *services.DeviceView, m models.ProfileMetric) (*services.MetricPreview, error) {
	f.calls++
	f.metric = m
	if f.err != nil {
		return nil, f.err
	}
	if f.preview != nil {
		return f.preview, nil
	}
	return &services.MetricPreview{Rows: []services.PreviewRow{}, Errors: map[string]string{}}, nil
}

// profileRouter mounts the profile routes as a signed-in user with the given
// admin claim. stubUsers (auth_middleware_test.go) confirms the claim.
func profileRouter(profiles *fakeProfiles, devs *fakeDevices, levels fakeSiteLevels, audit *fakeAudit, isAdmin bool, previewer *fakeMetricPreviewer) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", uuid.New())
		c.Set("username", "someone")
		c.Set("is_admin", isAdmin)
		c.Next()
	})
	RegisterProfileRoutes(r.Group("/api/v1"), profiles, devs, levels, audit, stubUsers{user: &models.User{IsAdmin: isAdmin}}, previewer)
	return r
}

func TestProfileRoutesAdminOnly(t *testing.T) {
	r := profileRouter(&fakeProfiles{}, &fakeDevices{}, fakeSiteLevels{}, &fakeAudit{}, false, nil)
	if w := do(r, http.MethodGet, "/api/v1/network/profiles", nil); w.Code != http.StatusForbidden {
		t.Errorf("non-admin list: %d, want 403", w.Code)
	}
}

func TestProfileCreateMetricValidation(t *testing.T) {
	r := profileRouter(&fakeProfiles{}, &fakeDevices{}, fakeSiteLevels{}, &fakeAudit{}, true, nil)
	body := map[string]any{"name": "Bad", "key": "Not Valid", "source": "column", "kind": "gauge", "oid": "1.3.6.1.2.1.1.3", "scale": 1}
	path := "/api/v1/network/profiles/" + uuid.New().String() + "/metrics"
	w := do(r, http.MethodPost, path, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid metric: %d, want 400, body %s", w.Code, w.Body.String())
	}
	if w.Body.Len() == 0 {
		t.Error("expected a validation message in the response body")
	}
}

func TestDeviceProfileAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	devs := &fakeDevices{device: dev}
	profiles := &fakeProfiles{}
	base := "/api/v1/devices/" + dev.ID.String() + "/profiles"
	putPath := base + "/" + uuid.New().String()

	r := profileRouter(profiles, devs, fakeSiteLevels{}, &fakeAudit{}, false, nil)
	if w := do(r, http.MethodGet, base, nil); w.Code != http.StatusNotFound {
		t.Errorf("no access GET: %d, want 404", w.Code)
	}
	if w := do(r, http.MethodPut, putPath, map[string]any{"mode": "attach"}); w.Code != http.StatusNotFound {
		t.Errorf("no access PUT: %d, want 404", w.Code)
	}

	r = profileRouter(profiles, devs, fakeSiteLevels{site: services.SiteAccessReadonly}, &fakeAudit{}, false, nil)
	if w := do(r, http.MethodGet, base, nil); w.Code != http.StatusOK {
		t.Errorf("readonly GET: %d, want 200", w.Code)
	}
	if w := do(r, http.MethodPut, putPath, map[string]any{"mode": "attach"}); w.Code != http.StatusForbidden || profiles.setCalls != 0 {
		t.Errorf("readonly PUT: %d, calls %d", w.Code, profiles.setCalls)
	}

	r = profileRouter(profiles, devs, fakeSiteLevels{site: services.SiteAccessEditable}, &fakeAudit{}, false, nil)
	if w := do(r, http.MethodPut, putPath, map[string]any{"mode": "attach"}); w.Code != http.StatusOK || profiles.setCalls != 1 || profiles.setMode != "attach" {
		t.Errorf("editable PUT: %d, calls %d, mode %q, body %s", w.Code, profiles.setCalls, profiles.setMode, w.Body.String())
	}
}

func TestMetricPreviewRouteAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	devs := &fakeDevices{device: dev}
	base := "/api/v1/devices/" + dev.ID.String() + "/metric-preview"
	body := map[string]any{"name": "Temp", "source": "scalar", "kind": "gauge", "oid": "1.3.6.1.2.1.1.3", "scale": 1}

	// Non-admin, even with editable site access: 403, never reaching the previewer.
	previewer := &fakeMetricPreviewer{}
	r := profileRouter(&fakeProfiles{}, devs, fakeSiteLevels{site: services.SiteAccessEditable}, &fakeAudit{}, false, previewer)
	if w := do(r, http.MethodPost, base, body); w.Code != http.StatusForbidden || previewer.calls != 0 {
		t.Errorf("non-admin: %d calls %d, want 403 and no call", w.Code, previewer.calls)
	}

	// Admin, no site access at all: 404, same as every other device route.
	previewer = &fakeMetricPreviewer{}
	r = profileRouter(&fakeProfiles{}, devs, fakeSiteLevels{}, &fakeAudit{}, true, previewer)
	if w := do(r, http.MethodPost, base, body); w.Code != http.StatusNotFound || previewer.calls != 0 {
		t.Errorf("no site access: %d calls %d, want 404 and no call", w.Code, previewer.calls)
	}

	// Admin with only readonly site access: 403. Preview sends SNMP to a real
	// device (like test-walk and Test connection), so it needs editable, not
	// just read, access to the site.
	previewer = &fakeMetricPreviewer{}
	r = profileRouter(&fakeProfiles{}, devs, fakeSiteLevels{site: services.SiteAccessReadonly}, &fakeAudit{}, true, previewer)
	if w := do(r, http.MethodPost, base, body); w.Code != http.StatusForbidden || previewer.calls != 0 {
		t.Errorf("admin readonly: %d calls %d, want 403 and no call", w.Code, previewer.calls)
	}

	// Admin with editable site access: 200.
	previewer = &fakeMetricPreviewer{}
	r = profileRouter(&fakeProfiles{}, devs, fakeSiteLevels{site: services.SiteAccessEditable}, &fakeAudit{}, true, previewer)
	w := do(r, http.MethodPost, base, body)
	if w.Code != http.StatusOK || previewer.calls != 1 {
		t.Fatalf("admin editable: %d calls %d, want 200 and 1 call, body %s", w.Code, previewer.calls, w.Body.String())
	}
	var resp struct {
		Data struct {
			OK bool `json:"ok"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Data.OK {
		t.Errorf("body %s, want ok:true", w.Body.String())
	}
}

func TestMetricPreviewRouteInvalidMetricIs400(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	devs := &fakeDevices{device: dev}
	base := "/api/v1/devices/" + dev.ID.String() + "/metric-preview"

	previewer := &fakeMetricPreviewer{err: fmt.Errorf("%w: give the metric a name", services.ErrInvalidMetric)}
	r := profileRouter(&fakeProfiles{}, devs, fakeSiteLevels{site: services.SiteAccessEditable}, &fakeAudit{}, true, previewer)
	w := do(r, http.MethodPost, base, map[string]any{"source": "scalar", "kind": "gauge", "oid": "1.3.6.1.2.1.1.3", "scale": 1})
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid metric: %d, want 400, body %s", w.Code, w.Body.String())
	}
}

// An SNMP or target failure (bad credential, unreachable device) is a 200
// carrying ok:false, the same shape test-walk uses: the server did its job;
// the device just did not answer.
func TestMetricPreviewRouteSNMPFailureIsA200(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	devs := &fakeDevices{device: dev}
	base := "/api/v1/devices/" + dev.ID.String() + "/metric-preview"
	body := map[string]any{"name": "Temp", "source": "scalar", "kind": "gauge", "oid": "1.3.6.1.2.1.1.3", "scale": 1}

	previewer := &fakeMetricPreviewer{err: errors.New("request timeout")}
	r := profileRouter(&fakeProfiles{}, devs, fakeSiteLevels{site: services.SiteAccessEditable}, &fakeAudit{}, true, previewer)
	w := do(r, http.MethodPost, base, body)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
	var resp struct {
		Data struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.OK || resp.Data.Error != "request timeout" {
		t.Errorf("body %+v", resp)
	}
}

// Health follows site access: 404 without it, 200 for readonly.
func TestDeviceHealthAccess(t *testing.T) {
	site := uuid.New()
	dev := services.DeviceView{Device: models.Device{ID: uuid.New(), SiteID: site}}
	calls := 0
	health := func(context.Context, *services.DeviceView) (*services.DeviceHealthView, error) {
		calls++
		return &services.DeviceHealthView{Profiles: []services.DeviceProfileView{}, Metrics: []services.HealthMetric{{Key: "cisco_fan_envmon"}}}, nil
	}
	router := func(levels fakeSiteLevels) *gin.Engine {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			c.Set("user_id", uuid.New())
			c.Set("username", "someone")
			c.Set("is_admin", false)
			c.Next()
		})
		r.GET("/api/v1/devices/:id/health", deviceHealthHandler(health, &fakeDevices{device: dev}, levels))
		return r
	}
	path := "/api/v1/devices/" + dev.ID.String() + "/health"
	if w := do(router(fakeSiteLevels{}), http.MethodGet, path, nil); w.Code != http.StatusNotFound || calls != 0 {
		t.Errorf("no access: %d calls %d", w.Code, calls)
	}
	w := do(router(fakeSiteLevels{site: services.SiteAccessReadonly}), http.MethodGet, path, nil)
	if w.Code != http.StatusOK || calls != 1 {
		t.Fatalf("readonly: %d calls %d", w.Code, calls)
	}
	var body struct {
		Data services.DeviceHealthView `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Data.Metrics) != 1 || body.Data.Metrics[0].Key != "cisco_fan_envmon" {
		t.Errorf("body %s (%v)", w.Body.String(), err)
	}
}
